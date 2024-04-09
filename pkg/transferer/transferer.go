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
	"runtime"

	"github.com/pkg/errors"

	"pmm-dump/pkg/dump"
)

type Transferer struct {
	sources      []dump.Source
	workersCount int
	file         io.ReadWriter
}

func New(file io.ReadWriter, s []dump.Source, workersCount int) (*Transferer, error) {
	if len(s) == 0 {
		return nil, errors.New("no sources provided")
	}

	if workersCount <= 0 {
		workersCount = runtime.NumCPU()
	}

	return &Transferer{
		sources:      s,
		workersCount: workersCount,
		file:         file,
	}, nil
}

type ChunkPool interface {
	Next() (dump.ChunkMeta, bool)
}

type LoadStatusGetter interface {
	GetLatestStatus() (LoadStatus, int)
}

const maxChunksInMem = 4

func (t Transferer) sourceByType(st dump.SourceType) (dump.Source, bool) { //nolint:ireturn,nolintlint
	for _, s := range t.sources {
		if s.Type() == st {
			return s, true
		}
	}
	return nil, false
}
