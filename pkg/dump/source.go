package dump

type Source interface {
	Type() SourceType
	ReadChunk(ChunkMeta) (*Chunk, error)
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
