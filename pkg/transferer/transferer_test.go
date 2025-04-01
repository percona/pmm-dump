package transferer

import (
	"io"
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

func (s fakeSource) ReadChunks(m dump.ChunkMeta) ([]*dump.Chunk, error) {
	return []*dump.Chunk{{
		ChunkMeta: m,
		Content:   []byte("content"),
		Filename:  m.String() + ".bin",
	}}, nil
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
}
