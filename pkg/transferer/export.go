package transferer

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"path"
	"pmm-dump/pkg/dump"
	"sync"
	"time"

	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
	"golang.org/x/sync/errgroup"
)

func (t Transferer) Export(ctx context.Context, lc LoadStatusGetter, meta dump.Meta, pool ChunkPool, logBuffer *bytes.Buffer) error {
	log.Info().Msg("Exporting metrics...")

	chunksCh := make(chan *dump.Chunk, maxChunksInMem)
	log.Debug().
		Int("size", maxChunksInMem).
		Msg("Created chunks channel")

	readWG := &sync.WaitGroup{}
	g, gCtx := errgroup.WithContext(ctx)

	log.Debug().Msgf("Starting %d goroutines to read chunks from sources...", t.workersCount)
	readWG.Add(t.workersCount)
	for i := 0; i < t.workersCount; i++ {
		g.Go(func() error {
			defer log.Debug().Msgf("Exiting from read chunks goroutine")
			defer readWG.Done()

			if err := t.readChunksFromSource(gCtx, lc, pool, chunksCh); err != nil {
				return errors.Wrap(err, "failed to read chunks from source")
			}
			return nil
		})
	}

	log.Debug().Msgf("Starting goroutine to close channel after read finish...")
	go func() {
		readWG.Wait()
		close(chunksCh)
		log.Debug().Msgf("Exiting from goroutine waiting for read to finish")
	}()

	log.Debug().Msg("Starting single goroutine for writing chunks to the dump...")
	g.Go(func() error {
		defer log.Debug().Msgf("Exiting from write chunks goroutine")
		if err := t.writeChunksToFile(meta, chunksCh, logBuffer); err != nil {
			return errors.Wrap(err, "failed to write chunks to the dump")
		}
		return nil
	})
	log.Debug().Msg("Waiting for all chunks to be processed...")
	if err := g.Wait(); err != nil {
		log.Debug().Msg("Got error, finishing export")
		return err
	}

	log.Info().Msg("Successfully exported!")

	return nil
}

func (t Transferer) readChunksFromSource(ctx context.Context, lc LoadStatusGetter, p ChunkPool, chunkC chan<- *dump.Chunk) error {
	for {
		log.Debug().Msg("New chunks reading loop iteration has been started")

		select {
		case <-ctx.Done():
			log.Debug().Msg("Context is done, stopping chunks reading")
			return ctx.Err()
		default:
			status, count := lc.GetLatestStatus()
			switch status {
			case LoadStatusWait:
				if count > MaxWaitStatusInSequence {
					log.Warn().Msgf("Too many %v in a sequence. Aborting", LoadStatusWait)
					return fmt.Errorf("terminated by exceeding max load (got wait load status) threshold %d times. Check --max-load value or use --ignore-load", MaxWaitStatusInSequence)
				}
				log.Debug().Msgf("Exceeded max load threshold(got wait load status): putting chunks reading to sleep for %v", MaxLoadWaitDuration)
				time.Sleep(MaxLoadWaitDuration)
				continue
			case LoadStatusTerminate:
				log.Debug().Msg("Got terminate load status: stopping chunks reading")
				return errors.New("terminated by exceeding critical load threshold (got terminate load status). Check --critical-load value or use --ignore-load")
			case LoadStatusOK:
			default:
				return errors.New("unknown load status")
			}

			chMeta, ok := p.Next()
			if !ok {
				log.Debug().Msg("Pool is empty: stopping chunks reading")
				return nil
			}

			s, ok := t.sourceByType(chMeta.Source)
			if !ok {
				return errors.New("failed to find source to read chunk")
			}

			c, err := s.ReadChunk(chMeta)
			if err != nil {
				return errors.Wrap(err, "failed to read chunk")
			}

			log.Debug().
				Stringer("source", c.Source).
				Str("filename", c.Filename).
				Msg("Successfully read chunk. Sending to chunks channel...")

			chunkC <- c
		}
	}
}

func (t Transferer) writeChunksToFile(meta dump.Meta, chunkC <-chan *dump.Chunk, logBuffer *bytes.Buffer) error {
	gzw, err := gzip.NewWriterLevel(t.file, gzip.BestCompression)
	if err != nil {
		return errors.Wrap(err, "failed to create gzip writer")
	}
	defer gzw.Close()

	tw := tar.NewWriter(gzw)
	defer tw.Close()

	for {
		log.Debug().Msg("New chunks writing loop iteration has been started")

		c, ok := <-chunkC
		if !ok {
			if err := writeMetafile(tw, meta); err != nil {
				return err
			}

			if err = writeLog(tw, logBuffer); err != nil {
				return err
			}

			log.Debug().Msg("Chunks channel is closed: stopping chunks writing")
			return nil
		}

		s, _ := t.sourceByType(c.Source) // there is no need to check for error as incoming chunk always has correct source

		log.Info().
			Stringer("source", c.Source).
			Str("filename", c.Filename).
			Msg("Writing chunk to the dump...")

		chunkSize := int64(len(c.Content))
		if chunkSize > meta.MaxChunkSize {
			meta.MaxChunkSize = chunkSize
		}

		err = tw.WriteHeader(&tar.Header{
			Typeflag: tar.TypeReg,
			Name:     path.Join(s.Type().String(), c.Filename),
			Size:     chunkSize,
			Mode:     0600,
			ModTime:  time.Now(),
		})
		if err != nil {
			return errors.Wrap(err, "failed to write file header")
		}

		if _, err = tw.Write(c.Content); err != nil {
			return errors.Wrap(err, "failed to write chunk content")
		}
	}
}

func writeLog(tw *tar.Writer, logBuffer *bytes.Buffer) error {
	log.Debug().Msg("Writing dump log")

	byteLog := logBuffer.Bytes()

	err := tw.WriteHeader(&tar.Header{
		Typeflag: tar.TypeReg,
		Name:     dump.LogFilename,
		Size:     int64(len(byteLog)),
		Mode:     0600,
		ModTime:  time.Now(),
	})
	if err != nil {
		return errors.Wrap(err, "failed to write dump log header")
	}

	if _, err = tw.Write(byteLog); err != nil {
		return errors.Wrap(err, "failed to write dump log content")
	}

	return nil
}
