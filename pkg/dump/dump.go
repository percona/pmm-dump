package dump

import (
	"fmt"
	"time"
)

type Meta struct {
}

type ChunkMeta struct {
	Source SourceType
	Start  *time.Time
	End    *time.Time
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

type TaskPool struct {
}
