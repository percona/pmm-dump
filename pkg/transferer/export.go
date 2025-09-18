// Copyright 2023 Percona LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package transferer

import (
	"archive/tar"
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"path"
	"reflect"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	"golang.org/x/sync/errgroup"

	"pmm-dump/pkg/clickhouse"
	"pmm-dump/pkg/clickhouse/tsv"
	"pmm-dump/pkg/dump"
	"pmm-dump/pkg/encryption"
)

func (t Transferer) Export(ctx context.Context, lc LoadStatusGetter, meta dump.Meta, pool ChunkPool, logBuffer *bytes.Buffer, e encryption.Options) error {
	log.Info().Msg("Exporting metrics...")
	chunksCh := make(chan *dump.Chunk, maxChunksInMem)
	log.Debug().
		Int("size", maxChunksInMem).
		Msg("Created chunks channel")

	var readWG sync.WaitGroup
	g, gCtx := errgroup.WithContext(ctx)

	log.Debug().Msgf("Starting %d goroutines to read chunks from sources...", t.workersCount)
	readWG.Add(t.workersCount)
	for range t.workersCount {
		g.Go(func() error {
			defer log.Debug().Msgf("Exiting from read chunks goroutine")
			defer readWG.Done()

			if err := t.readChunksFromSource(gCtx, lc, pool, chunksCh); err != nil {
				return fmt.Errorf("failed to read chunks from source: %w", err)
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
		if err := t.writeChunksToFile(meta, chunksCh, logBuffer, e); err != nil {
			return fmt.Errorf("failed to write chunks to the dump: %w", err)
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

			chunks, err := s.ReadChunks(chMeta)
			if err != nil {
				return fmt.Errorf("failed to read chunk: %w", err)
			}

			if len(chunks) > 1 {
				log.Info().Msgf("Chunk was split into several parts %d", len(chunks))
			}

			for _, c := range chunks {
				chunkC <- c
			}
		}
	}
}

func (t Transferer) writeChunksToFile(meta dump.Meta, chunkC <-chan *dump.Chunk, logBuffer *bytes.Buffer, e encryption.Options) error {
	w, err := dump.NewWriter(t.file, &e)
	if err != nil {
		return fmt.Errorf("failed to create writer: %w", err)
	}
	defer w.Close() //nolint:errcheck
	tw := w.GetTarWriter()
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

			if err = e.OutputPass(); err != nil {
				return err
			}

			log.Debug().Msg("Chunks channel is closed: stopping chunks writing")
			return nil
		}

		s, _ := t.sourceByType(c.Source) // there is no need to check for error as incoming chunk always has correct source

		if c.Source == dump.ClickHouse {
			err := convertTimeToUTC(s, c)
			if err != nil {
				return fmt.Errorf("failed to convert timezones for Clickhouse chunks %w", err)
			}
		}

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
			Mode:     filePermission,
			ModTime:  time.Now(),
		})
		if err != nil {
			return fmt.Errorf("failed to write file header: %w", err)
		}

		if _, err = tw.Write(c.Content); err != nil {
			return fmt.Errorf("failed to write chunk content: %w", err)
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
		Mode:     filePermission,
		ModTime:  time.Now(),
	})
	if err != nil {
		return fmt.Errorf("failed to write dump log header: %w", err)
	}

	if _, err = tw.Write(byteLog); err != nil {
		return fmt.Errorf("failed to write dump log content: %w", err)
	}

	return nil
}

func convertTimeToUTC(s dump.Source, c *dump.Chunk) error {
	var columnTypes []*sql.ColumnType
	switch chS := s.(type) {
	case clickhouse.Source:
		columnTypes = chS.ColumnTypes()
	case *clickhouse.Source:
		columnTypes = chS.ColumnTypes()
	default:
		typeName := reflect.TypeOf(s).String()
		if typeName == "*transferer.fakeSource" {
			return nil
		}
		return fmt.Errorf("type of source in dump specified as Clickhouse but got error when casting, wanted clickhouse.Source got %s", typeName)
	}

	var chunkRead bytes.Buffer
	var chunkWrite bytes.Buffer
	chunkRead.Write(c.Content)
	reader := tsv.NewReader(&chunkRead, columnTypes)
	writer := tsv.NewWriter(&chunkWrite)

	for {
		records, err := reader.Reader.Read()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return fmt.Errorf("failed to read Clickhouse chunk: %w", err)
		}
		for i, record := range records {
			if columnTypes[i].ScanType().Name() == "Time" {
				timeParsed, err := time.Parse("2006-01-02 15:04:05 -0700 MST", record)
				if err != nil {
					return fmt.Errorf("failed to parse time from Clickhouse chunk: %w", err)
				}
				records[i] = timeParsed.UTC().String()
			}
		}
		err = writer.Write(records)
		if err != nil {
			return fmt.Errorf("failed to write records to buffer: %w", err)
		}
	}
	writer.Flush()
	if writer.Error() != nil {
		return fmt.Errorf("failed to flush records to buffer: %w", writer.Error())
	}
	c.Content = chunkWrite.Bytes()

	return nil
}
