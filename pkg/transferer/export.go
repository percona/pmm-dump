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
	"compress/gzip"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path"
	"sync"
	"time"

	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"golang.org/x/sync/errgroup"

	"pmm-dump/pkg/dump"
)

func (t Transferer) Export(ctx context.Context, lc LoadStatusGetter, meta dump.Meta, pool ChunkPool, logBuffer *bytes.Buffer, justKey, toFile bool) error {
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
		if err := t.writeChunksToFile(meta, chunksCh, logBuffer, justKey, toFile); err != nil {
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
func (t Transferer) chekcOrGenerateIVAndKeyAndCipher() ([]byte, []byte, cipher.Block, error) {
	key := make([]byte, 32)
	iv := make([]byte, aes.BlockSize)
	var err error
	if *t.key != "" && *t.iv != "" { // provided key and iv
		key, err = hex.DecodeString(*t.key)
		if err != nil {
			return nil, nil, nil, errors.Wrap(err, "Failed to decode key to hex string")
		}
		iv, err = hex.DecodeString(*t.iv)
		if err != nil {
			return nil, nil, nil, errors.Wrap(err, "Failed to decode iv to hex string")
		}
	} else if *t.key != "" { // provided only key
		key, err = hex.DecodeString(*t.key)
		if err != nil {
			return nil, nil, nil, errors.Wrap(err, "Failed to decode key to hex string")
		}
	} else { // key is not provided
		_, err = rand.Read(key)
		if err != nil {
			return nil, nil, nil, errors.Wrap(err, "Failed to generate random string")
		}
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, nil, nil, errors.Wrap(err, "Failed to generate cipher")
	}

	return iv, key, block, nil
}

func (t Transferer) writeChunksToFile(meta dump.Meta, chunkC <-chan *dump.Chunk, logBuffer *bytes.Buffer, justKey, toFile bool) error {
	var (
		gzw   *gzip.Writer
		err   error
		iv    []byte
		key   []byte
		block cipher.Block
	)
	if !*t.encrypted {
		iv, key, block, err = t.chekcOrGenerateIVAndKeyAndCipher()
		if err != nil {
			return errors.Wrap(err, "Failed to generate key")
		}
		stream := cipher.NewCTR(block, iv)
		writer := &cipher.StreamWriter{S: stream, W: t.file}
		defer writer.Close() //nolint:errcheck

		gzw, err = gzip.NewWriterLevel(writer, gzip.BestCompression)
		if err != nil {
			return errors.Wrap(err, "Failed to create gzip writer")
		}
	} else {
		gzw, err = gzip.NewWriterLevel(t.file, gzip.BestCompression)
		if err != nil {
			return errors.Wrap(err, "Failed to create gzip writer")
		}
	}
	defer gzw.Close()

	tw := tar.NewWriter(gzw)
	defer tw.Close() //nolint:errcheck

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
			if !*t.encrypted {
				if justKey {
					wr := zerolog.ConsoleWriter{
						Out:     os.Stderr,
						NoColor: true,
					}
					wr.PartsOrder = []string{
						zerolog.MessageFieldName,
					}
					lo := log.Output(wr)
					lo.Info().Msg("Key: " + hex.EncodeToString(key))
					lo.Info().Msg("Iv: " + hex.EncodeToString(iv))

				} else {
					log.Info().Msg("Key: " + hex.EncodeToString(key))
					log.Info().Msg("Iv: " + hex.EncodeToString(iv))
				}
				if toFile {
					log.Info().Msg("Exporting key an iv to file")
					err := writeKeyToFile(key, iv)
					if err != nil {
						return err
					}
				}
			}

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
			Mode:     filePermission,
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
func writeKeyToFile(key, iv []byte) error {
	file, err := os.Create("EncKeys.txt") //nolint:gosec
	if err != nil {
		return errors.Wrap(err, "failed to create key file")
	}
	file.Write([]byte("key:"))
	file.Write([]byte(hex.EncodeToString(key)))
	file.Write([]byte("\niv:"))
	file.Write([]byte(hex.EncodeToString(iv)))
	return nil
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
		return errors.Wrap(err, "failed to write dump log header")
	}

	if _, err = tw.Write(byteLog); err != nil {
		return errors.Wrap(err, "failed to write dump log content")
	}

	return nil
}
