package transferer

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"path"

	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
	"golang.org/x/sync/errgroup"

	"pmm-dump/pkg/dump"
)

func (t Transferer) Import(ctx context.Context, runtimeMeta dump.Meta) error {
	log.Info().Msg("Importing metrics...")
	gzr, err := gzip.NewReader(t.file)
	if err != nil {
		return errors.Wrap(err, "failed to open as gzip")
	}
	defer gzr.Close() //nolint:errcheck

	tr := tar.NewReader(gzr)

	var metafileExists bool

	chunksC := make(chan *dump.Chunk, maxChunksInMem)

	g, gCtx := errgroup.WithContext(ctx)
	for i := 0; i < t.workersCount; i++ {
		g.Go(func() error {
			defer log.Debug().Msgf("Exiting from write chunks goroutine")
			if err := t.writeChunksToSource(gCtx, chunksC); err != nil {
				return errors.Wrap(err, "failed to write chunks to source")
			}
			return nil
		})
	}

	for {
		log.Debug().Msg("Reading file from dump...")

		header, err := tr.Next()

		if errors.Is(err, io.EOF) {
			log.Debug().Msg("Processed complete dump file")
			break
		}

		if err != nil {
			return errors.Wrap(err, "failed to read file from dump")
		}

		dir, filename := path.Split(header.Name)

		if filename == dump.MetaFilename {
			readAndCompareDumpMeta(tr, runtimeMeta)
			metafileExists = true
			continue
		}

		if filename == dump.LogFilename {
			continue
		}

		if len(dir) == 0 {
			return errors.Errorf("corrupted dump: found unknown file %s", filename)
		}

		log.Info().Msgf("Processing chunk '%s'...", header.Name)

		st := dump.ParseSourceType(dir[:len(dir)-1])
		if st == dump.UndefinedSource {
			return errors.Errorf("corrupted dump: found undefined source: %s", dir)
		}

		content, err := io.ReadAll(tr)
		if err != nil {
			return errors.Wrap(err, "failed to read chunk content")
		}

		if len(content) == 0 {
			log.Warn().Msgf("Chunk '%s' is empty, skipping", header.Name)
			continue
		}

		ch := &dump.Chunk{
			ChunkMeta: dump.ChunkMeta{
				Source: st,
			},
			Content:  content,
			Filename: filename,
		}

		isDone := false
		select {
		case <-gCtx.Done():
			isDone = true
		case chunksC <- ch:
			log.Debug().Msgf("Sending chunk '%s' to the channel...", header.Name)
		}
		if isDone {
			break
		}
	}

	close(chunksC)
	if err := g.Wait(); err != nil {
		log.Debug().Msg("Got error, finishing import")
		return err
	}

	if !metafileExists {
		log.Error().Msg("No meta file found in dump. No version checks performed")
	}

	log.Debug().Msg("Finalizing writes...")

	for _, s := range t.sources {
		if err = s.FinalizeWrites(); err != nil {
			return errors.Wrap(err, "failed to finalize import")
		}
	}

	log.Info().Msg("Successfully imported!")

	return nil
}

func (t Transferer) writeChunksToSource(ctx context.Context, chunkC <-chan *dump.Chunk) error {
	for {
		log.Debug().Msg("New chunks writing loop iteration has been started")

		select {
		case <-ctx.Done():
			log.Debug().Msg("Context is done, stopping chunks writing")
			return ctx.Err()
		default:
			c, ok := <-chunkC
			if !ok {
				log.Debug().Msg("Chunks channel is closed: stopping chunks writing")
				return nil
			}

			s, ok := t.sourceByType(c.Source)
			if !ok {
				log.Warn().Msgf("Found dump data for %v, but it's not specified - skipped", c.Source)
				continue
			}

			log.Debug().Msgf("Writing chunk '%v' to the source...", c.Filename)
			if err := s.WriteChunk(c.Filename, bytes.NewBuffer(c.Content)); err != nil {
				return errors.Wrap(err, "failed to write chunk")
			}
			log.Info().Msgf("Successfully processed '%v'", c.Filename)
		}
	}
}
