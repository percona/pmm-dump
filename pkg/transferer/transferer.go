package transferer

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"pmm-transferer/pkg/dump"
	"runtime"
	"time"

	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
)

type Transferer struct {
	dumpPath string
	sources  []dump.Source
}

func New(dumpPath string, s []dump.Source) (*Transferer, error) {
	if len(s) == 0 {
		return nil, errors.New("failed to create transferer with no sources")
	}

	return &Transferer{
		dumpPath: dumpPath,
		sources:  s,
	}, nil
}

type ChunkPool interface {
	Next() (dump.ChunkMeta, bool)
}

var exportWorkersCount = runtime.NumCPU()

const maxChunksInMem = 4

func (t Transferer) readChunksFromSource(ctx context.Context, p ChunkPool, chunkC chan<- *dump.Chunk) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			chMeta, ok := p.Next()
			if !ok {
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

			chunkC <- c
		}
	}
}

func (t Transferer) writeChunksToFile(ctx context.Context, chunkC <-chan *dump.Chunk) error {
	exportTS := time.Now().UTC()

	filepath := fmt.Sprintf("pmm-dump-%v.tar.gz", exportTS.Unix())
	if t.dumpPath != "" {
		filepath = path.Join(t.dumpPath, filepath)
	}

	log.Info().Msgf("Preparing dump file: %s", filepath)
	file, err := os.Create(filepath)
	if err != nil {
		return errors.Wrapf(err, "failed to create %s", filepath)
	}
	defer file.Close()

	gzw, err := gzip.NewWriterLevel(file, gzip.BestCompression)
	if err != nil {
		return errors.Wrap(err, "failed to create gzip writer")
	}
	defer gzw.Close()

	tw := tar.NewWriter(gzw)
	defer tw.Close()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			c, ok := <-chunkC
			if !ok {
				return nil
			}

			s, ok := t.sourceByType(c.Source)
			if !ok {
				return errors.New("failed to find source to write chunk")
			}

			log.Info().Msgf("Writing retrieved metrics to the dump...")

			err = tw.WriteHeader(&tar.Header{
				Typeflag: tar.TypeReg,
				Name:     path.Join(s.Type().String(), c.Filename),
				Size:     int64(len(c.Content)),
				Mode:     0600,
			})
			if err != nil {
				return errors.Wrap(err, "failed to write file header")
			}

			if _, err = tw.Write(c.Content); err != nil {
				return errors.Wrap(err, "failed to write chunk content")
			}
		}
	}
}

func (t Transferer) Export(ctx context.Context, pool ChunkPool) error {
	chunksCh := make(chan *dump.Chunk, maxChunksInMem)

	readErrCh := make(chan error, exportWorkersCount)
	for i := 0; i < exportWorkersCount; i++ {
		go func() {
			readErrCh <- t.readChunksFromSource(ctx, pool, chunksCh)
		}()
	}

	writeErrCh := make(chan error, 1)
	go func() {
		writeErrCh <- t.writeChunksToFile(ctx, chunksCh)
	}()

	for i := 0; i < exportWorkersCount; i++ {
		if err := <-readErrCh; err != nil {
			return err
		}
	}

	close(chunksCh)

	if err := <-writeErrCh; err != nil {
		return err
	}

	return nil
}

func (t Transferer) Import() error {
	log.Info().Msgf("Opening dump file: %v", t.dumpPath)

	file, err := os.Open(t.dumpPath)
	if err != nil {
		return errors.Wrap(err, "failed to open file")
	}
	defer file.Close()

	gzr, err := gzip.NewReader(file)
	if err != nil {
		return errors.Wrap(err, "failed to open as gzip")
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)

	for {
		log.Info().Msgf("Reading file from dump...")

		header, err := tr.Next()

		if err == io.EOF {
			log.Info().Msg("Processed complete dump")
			break
		}

		if err != nil {
			return errors.Wrap(err, "failed to read file from dump")
		}

		log.Info().Msgf("Processing chunk '%s'", header.Name)

		dir, filename := path.Split(header.Name)

		st := dump.ParseSourceType(dir[:len(dir)-1])
		if st == dump.UndefinedSource {
			return errors.Errorf("corrupted dump: found undefined source: %s", dir)
		}

		s, ok := t.sourceByType(st)
		if !ok {
			log.Warn().Msgf("Found dump data for %v, but it's not specified - skipped", st)
			continue
		}

		log.Info().Msgf("Writing chunk to %v", s.Type())

		if err = s.WriteChunk(filename, tr); err != nil {
			return errors.Wrap(err, "failed to write chunk")
		}

		log.Info().Msgf("Successfully processed %v", header.Name)
	}

	log.Info().Msg("Finalizing writes...")

	for _, s := range t.sources {
		if err = s.FinalizeWrites(); err != nil {
			return errors.Wrap(err, "failed to finalize import")
		}
	}

	return nil
}

func (t Transferer) sourceByType(st dump.SourceType) (dump.Source, bool) {
	for _, s := range t.sources {
		if s.Type() == st {
			return s, true
		}
	}
	return nil, false
}
