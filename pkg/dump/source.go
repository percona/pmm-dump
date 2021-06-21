package dump

import "io"

type Source interface {
	Type() SourceType
	ReadChunk(ChunkMeta) (*Chunk, error)
	WriteChunk(filename string, r io.Reader) error
}

type SourceType int

const (
	UndefinedSource SourceType = iota
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
