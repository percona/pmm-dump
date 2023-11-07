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

package dump

import (
	"fmt"
	"sync"
	"time"

	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
)

const (
	MetaFilename = "meta.json"
	LogFilename  = "log.json"
)

type Meta struct {
	Version           PMMDumpVersion     `json:"version"`
	PMMServerVersion  string             `json:"pmm-server-version"`
	MaxChunkSize      int64              `json:"max_chunk_size"`
	PMMTimezone       *string            `json:"pmm-server-timezone"`
	Arguments         string             `json:"arguments"`
	VMDataFormat      string             `json:"vm-data-format"`
	PMMServerServices []PMMServerService `json:"pmm-server-services,omitempty"`
}

type PMMServerService struct {
	Name      string   `json:"name"`
	NodeID    string   `json:"node-id"`
	NodeName  string   `json:"node-name"`
	AgentsIDs []string `json:"agents-ids"`
}

type PMMDumpVersion struct {
	GitBranch string `json:"git-branch"`
	GitCommit string `json:"git-commit"`
}

type ChunkMeta struct {
	Source SourceType
	Start  *time.Time
	End    *time.Time

	Index   int
	RowsLen int
}

func (c ChunkMeta) String() string {
	var s, e int64
	if c.Start != nil {
		s = c.Start.Unix()
	}
	if c.End != nil {
		e = c.End.Unix()
	}
	return fmt.Sprintf("%d-%d", s, e)
}

type Chunk struct {
	ChunkMeta
	Content  []byte
	Filename string
}

type ChunkPool struct {
	mu         sync.Mutex
	chunks     []ChunkMeta
	currentIdx int
}

func NewChunkPool(c []ChunkMeta) (*ChunkPool, error) {
	if len(c) == 0 {
		return nil, errors.New("failed to create empty chunk pool")
	}

	log.Debug().Msgf("Created pool with %d chunks in total", len(c))

	return &ChunkPool{
		chunks: c,
	}, nil
}

func (p *ChunkPool) Next() (ChunkMeta, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.currentIdx >= len(p.chunks) {
		return ChunkMeta{}, false
	}

	m := p.chunks[p.currentIdx]
	p.currentIdx++

	log.Info().Msgf("Processing %d/%d chunk...", p.currentIdx, len(p.chunks))

	return m, true
}
