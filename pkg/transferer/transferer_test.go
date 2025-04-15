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
	"io"
	"os"
	"testing"

	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"pmm-dump/pkg/dump"
)

type fakeSource struct {
	sourceType    dump.SourceType
	failFinalizer bool
}

func (s fakeSource) Type() dump.SourceType {
	return s.sourceType
}

func (s fakeSource) ReadChunk(m dump.ChunkMeta) (*dump.Chunk, error) {
	return &dump.Chunk{
		ChunkMeta: m,
		Content:   []byte("content"),
		Filename:  m.String() + ".bin",
	}, nil
}

func (s fakeSource) WriteChunk(_ string, r io.Reader) error {
	chunkContent, err := io.ReadAll(r)
	if err != nil {
		return errors.Wrap(err, "failed to read chunk content")
	}
	if len(chunkContent) == 0 {
		return errors.New("chunk content is empty")
	}
	if string(chunkContent) == "invalid" {
		return errors.New("chunk content is empty")
	}
	return nil
}

func (s fakeSource) FinalizeWrites() error {
	if s.failFinalizer {
		return errors.New("fail")
	}
	return nil
}

func TestMain(m *testing.M) {
	log.Logger = zerolog.Nop()
	m.Run()
	os.Exit(0)
}
func newFalse() *bool {
	b := true
	return &b
}
func newString() *string {
	b := ""
	return &b
}
