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

import "io"

type Source interface {
	Type() SourceType
	ReadChunk(ChunkMeta) (*Chunk, error)
	WriteChunk(filename string, r io.Reader) error
	FinalizeWrites() error
}

type SourceType int

const (
	InvalidSource SourceType = iota - 1
	UndefinedSource
	VictoriaMetrics
	ClickHouse
)

func (s SourceType) String() string {
	switch s {
	case VictoriaMetrics:
		return "vm"
	case ClickHouse:
		return "ch"
	default:
		return "undefined"
	}
}

func ParseSourceType(v string) SourceType {
	switch v {
	case "vm":
		return VictoriaMetrics
	case "ch":
		return ClickHouse
	default:
		return UndefinedSource
	}
}
