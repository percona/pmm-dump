package importer

import (
	"pmm-transferer/pkg/dump"
)

type Importer struct {
}

type DataSource interface {
	ReadChunk(dump.ChunkMeta) (*dump.Chunk, error)
	CheckCurrentLoad() error
}

type DataDestination interface {
	SaveChunk(*dump.Chunk) error
	CheckCurrentLoad() error
}
