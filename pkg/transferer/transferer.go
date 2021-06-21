package transferer

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"os"
	"path"
	"pmm-transferer/pkg/dump"
	"time"

	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
)

type Transferer struct {
	dumpPath string
	sources  []dump.Source
}

func New(dumpPath string, s []dump.Source) (*Transferer, error) {
	if len(s) == 0 {
		return nil, errors.New("failed to create transferer with no sources")
	}

	return &Transferer{
		dumpPath: dumpPath,
		sources:  s,
	}, nil
}

func (t Transferer) Export(start, end *time.Time) error {
	exportTS := time.Now().UTC()

	filepath := fmt.Sprintf("pmm-dump-%v.tar.gz", exportTS.Unix())
	if t.dumpPath != "" {
		filepath = path.Join(t.dumpPath, filepath)
	}

	log.Info().Msgf("Preparing dump file: %s", filepath)
	file, err := os.Create(filepath)
	if err != nil {
		return errors.Wrapf(err, "failed to create %s", filepath)
	}
	defer file.Close()

	w, err := gzip.NewWriterLevel(file, gzip.BestCompression)
	if err != nil {
		return errors.Wrap(err, "failed to create gzip writer")
	}
	defer w.Close()

	tw := tar.NewWriter(w)
	defer tw.Close()

	for _, s := range t.sources {
		ch, err := s.ReadChunk(dump.ChunkMeta{
			Source: s.Type(),
			Start:  start,
			End:    end,
		})
		if err != nil {
			return errors.Wrap(err, "failed to read chunk")
		}

		err = tw.WriteHeader(&tar.Header{
			Typeflag:   tar.TypeReg,
			Name:       path.Join(s.Type().String(), ch.Filename),
			Size:       int64(len(ch.Content)),
			ModTime:    exportTS,
			AccessTime: exportTS,
			ChangeTime: exportTS,
			Mode:       0600,
		})
		if err != nil {
			return errors.Wrap(err, "failed to write file header")
		}

		if _, err = tw.Write(ch.Content); err != nil {
			return errors.Wrap(err, "failed to write chunk content")
		}
	}

	return nil
}

func (t Transferer) Import() error {
	return nil // TODO: implement
}
