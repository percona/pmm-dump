package exporter

import (
	"pmm-transferer/pkg/dump"
)

type Exporter struct {
}

type DataSource interface {
	ReadChunk(dump.ChunkMeta) (*dump.Chunk, error)
	CheckCurrentLoad() error
}

type DataDestination interface {
	SaveChunk(*dump.Chunk) error
	CheckCurrentLoad() error
}
