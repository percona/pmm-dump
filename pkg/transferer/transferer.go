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
		log.Debug().Msg("New chunks reading loop iteration has been started")

		select {
		case <-ctx.Done():
			log.Debug().Msg("Context is done, stopping chunks reading")
			return ctx.Err()
		default:
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

func getDumpFilepath(customPath string, ts time.Time) (string, error) {
	autoFilename := fmt.Sprintf("pmm-dump-%v.tar.gz", ts.Unix())
	if customPath == "" {
		return autoFilename, nil
	}

	customPathInfo, err := os.Stat(customPath)
	if err != nil && !os.IsNotExist(err) {
		return "", errors.Wrap(err, "failed to get custom path info")
	}

	if err != nil { // file doesn't exist
		if err = os.MkdirAll(path.Dir(customPath), 0777); err != nil {
			return "", errors.Wrap(err, "failed to create folders for the dump file")
		}
	}

	if customPathInfo.IsDir() || os.IsPathSeparator(customPath[len(customPath)-1]) {
		return path.Join(customPath, autoFilename), nil
	}

	return customPath, nil
}

func (t Transferer) writeChunksToFile(ctx context.Context, chunkC <-chan *dump.Chunk) error {
	exportTS := time.Now().UTC()

	filepath, err := getDumpFilepath(t.dumpPath, exportTS)
	if err != nil {
		return err
	}

	log.Debug().Msgf("Preparing dump file: %s", filepath)
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
				return errors.New("failed to find source to write chunk")
			}

			log.Info().
				Stringer("source", c.Source).
				Str("filename", c.Filename).
				Msg("Writing chunk to the dump...")

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
	log.Info().Msg("Exporting metrics...")

	chunksCh := make(chan *dump.Chunk, maxChunksInMem)
	log.Debug().
		Int("size", maxChunksInMem).
		Msg("Created chunks channel")

	log.Debug().Msgf("Starting %d goroutines to read chunks from sources...", exportWorkersCount)
	readErrCh := make(chan error, exportWorkersCount)
	for i := 0; i < exportWorkersCount; i++ {
		go func() {
			readErrCh <- t.readChunksFromSource(ctx, pool, chunksCh)
		}()
	}

	log.Debug().Msg("Starting single goroutine for writing chunks to the dump...")
	writeErrCh := make(chan error, 1)
	go func() {
		writeErrCh <- t.writeChunksToFile(ctx, chunksCh)
	}()

	log.Debug().Msg("Waiting for all chunks to be read...")
	for i := 0; i < exportWorkersCount; i++ {
		if err := <-readErrCh; err != nil {
			return err
		}
	}

	close(chunksCh)

	log.Debug().Msg("Waiting for all chunks to be written to the dump...")
	if err := <-writeErrCh; err != nil {
		return err
	}

	log.Info().Msg("Successfully exported!")

	return nil
}

func (t Transferer) Import() error {
	log.Info().Msg("Importing metrics...")

	log.Info().
		Str("path", t.dumpPath).
		Msg("Opening dump file...")

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
		log.Debug().Msg("Reading file from dump...")

		header, err := tr.Next()

		if err == io.EOF {
			log.Debug().Msg("Processed complete dump file")
			break
		}

		if err != nil {
			return errors.Wrap(err, "failed to read file from dump")
		}

		log.Info().Msgf("Processing chunk '%s'...", header.Name)

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

		if err = s.WriteChunk(filename, tr); err != nil {
			return errors.Wrap(err, "failed to write chunk")
		}

		log.Info().Msgf("Successfully processed '%v'", header.Name)
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

func (t Transferer) sourceByType(st dump.SourceType) (dump.Source, bool) {
	for _, s := range t.sources {
		if s.Type() == st {
			return s, true
		}
	}
	return nil, false
}
