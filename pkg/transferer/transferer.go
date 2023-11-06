package transferer

import (
	"io"
	"runtime"

	"pmm-dump/pkg/dump"

	"github.com/pkg/errors"
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

func (t Transferer) sourceByType(st dump.SourceType) (dump.Source, bool) {
	for _, s := range t.sources {
		if s.Type() == st {
			return s, true
		}
	}
	return nil, false
}
