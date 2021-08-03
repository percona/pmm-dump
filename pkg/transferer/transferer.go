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
	"sync"
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

type LoadChecker interface {
	GetLatestStatus() LoadStatus
}

var readWorkersCount = runtime.NumCPU()

const maxChunksInMem = 4

func (t Transferer) readChunksFromSource(ctx context.Context, lc LoadChecker, p ChunkPool, chunkC chan<- *dump.Chunk) error {
	for {
		log.Debug().Msg("New chunks reading loop iteration has been started")

		select {
		case <-ctx.Done():
			log.Debug().Msg("Context is done, stopping chunks reading")
			return ctx.Err()
		default:
			if lc != nil {
				switch lc.GetLatestStatus() {
				case LoadStatusWait:
					time.Sleep(LoadStatusWaitSleepDuration)
					log.Debug().Msgf("Got wait load status: putting chunks reading to sleep for %d seconds", 10)
					continue
				case LoadStatusTerminate:
					log.Debug().Msg("Got terminate load status: stopping chunks reading")
					return errors.New("got terminate load status")
				case LoadStatusOK:
				default:
					return errors.New("unknown load status")
				}
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

func getDumpFilepath(customPath string, ts time.Time) (string, error) {
	autoFilename := fmt.Sprintf("pmm-dump-%v.tar.gz", ts.Unix())
	if customPath == "" {
		return autoFilename, nil
	}

	customPathInfo, err := os.Stat(customPath)
	if err != nil && !os.IsNotExist(err) {
		return "", errors.Wrap(err, "failed to get custom path info")
	}

	if (err == nil && customPathInfo.IsDir()) || os.IsPathSeparator(customPath[len(customPath)-1]) {
		// file exists and it's directory
		return path.Join(customPath, autoFilename), nil
	}

	return customPath, nil
}

func (t Transferer) writeChunksToFile(ctx context.Context, chunkC <-chan *dump.Chunk) error {
	exportTS := time.Now().UTC()

	log.Debug().Msgf("Trying to determine filepath")
	filepath, err := getDumpFilepath(t.dumpPath, exportTS)
	if err != nil {
		return err
	}

	log.Debug().Msgf("Preparing dump file: %s", filepath)
	if err := os.MkdirAll(path.Dir(filepath), 0777); err != nil {
		return errors.Wrap(err, "failed to create folders for the dump file")
	}
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

func (t Transferer) Export(ctx context.Context, lc LoadChecker, pool ChunkPool) error {
	log.Info().Msg("Exporting metrics...")

	chunksCh := make(chan *dump.Chunk, maxChunksInMem)
	log.Debug().
		Int("size", maxChunksInMem).
		Msg("Created chunks channel")

	errCh := make(chan error)

	readWG := &sync.WaitGroup{}

	log.Debug().Msgf("Starting %d goroutines to read chunks from sources...", readWorkersCount)
	readWG.Add(readWorkersCount)
	for i := 0; i < readWorkersCount; i++ {
		go func() {
			errCh <- t.readChunksFromSource(ctx, lc, pool, chunksCh)
			readWG.Done()
			log.Debug().Msgf("Exiting from read chunks goroutine")
		}()
	}

	log.Debug().Msgf("Starting goroutine to close channel after read finish...")
	go func() {
		readWG.Wait()
		close(chunksCh)
		log.Debug().Msgf("Exiting from goroutine waiting for read to finish")
	}()

	log.Debug().Msg("Starting single goroutine for writing chunks to the dump...")
	go func() {
		errCh <- t.writeChunksToFile(ctx, chunksCh)
		log.Debug().Msgf("Exiting from write chunks goroutine")
	}()

	log.Debug().Msg("Waiting for all chunks to be processed...")
	for i := 0; i < readWorkersCount+1; i++ {
		log.Debug().Msgf("Waiting for #%d status to be reported...", i)
		if err := <-errCh; err != nil {
			log.Debug().Msg("Got error, finishing export")
			return err
		}
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
