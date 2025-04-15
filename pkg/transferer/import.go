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
	"encoding/hex"
	"io"
	"path"

	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
	"golang.org/x/sync/errgroup"

	"pmm-dump/pkg/dump"
)

func (t Transferer) Import(ctx context.Context, runtimeMeta dump.Meta) error {
	log.Info().Msg("Importing metrics...")
	var (
		gzr *gzip.Reader
		err error
		key []byte
		iv  []byte
	)
	if !*t.encrypted {
		if *t.key == "" {
			return errors.Wrap(err, "key is empty, please specify key in arguments")
		}
		key, err = hex.DecodeString(*t.key)
		if err != nil {
			panic(err)
		}
		block, err := aes.NewCipher(key)
		if err != nil {
			panic(err)
		}
		iv = make([]byte, aes.BlockSize)
		if *t.iv != "" {
			iv, err = hex.DecodeString(*t.iv)
			if err != nil {
				panic(err)
			}
		}
		stream := cipher.NewCTR(block, iv)

		decReader := &cipher.StreamReader{S: stream, R: t.file}
		gzr, err = gzip.NewReader(decReader)
		if err != nil {
			return errors.Wrap(err, "failed to open as gzip")
		}
		defer gzr.Close() //nolint:errcheck
	} else {
		gzr, err = gzip.NewReader(t.file)
		if err != nil {
			return errors.Wrap(err, "failed to open as gzip")
		}
		defer gzr.Close() //nolint:errcheck
	}

	tr := tar.NewReader(gzr)

	var metafileExists bool

	chunksC := make(chan *dump.Chunk, maxChunksInMem)

	g, gCtx := errgroup.WithContext(ctx)
	for range t.workersCount {
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
				switch c.Source {
				case dump.ClickHouse:
					log.Warn().Msg("Found dump data for QAN, but `--dump-qan` option is not specified - skipped")
				case dump.VictoriaMetrics:
					log.Warn().Msg("Found dump data for VictoriaMetrics, but `--dump-vm` option is not specified - skipped")
				default:
					log.Warn().Msgf("Found dump data for %v, but it's not specified - skipped", c.Source)
				}
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
