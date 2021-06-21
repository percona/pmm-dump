package transferer

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
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

	gzw, err := gzip.NewWriterLevel(file, gzip.BestCompression)
	if err != nil {
		return errors.Wrap(err, "failed to create gzip writer")
	}
	defer gzw.Close()

	tw := tar.NewWriter(gzw)
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
	file, err := os.Open(t.dumpPath)
	if err != nil {
		return errors.Wrap(err, "failed to open file")
	}
	defer file.Close()

	gzr, err := gzip.NewReader(file)
	if err != nil {
		return errors.Wrap(err, "failed to open as gzip")
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)

	for {
		header, err := tr.Next()

		if err == io.EOF {
			log.Info().Msg("Processed complete dump")
			break
		}

		if err != nil {
			return errors.Wrap(err, "failed to read file from dump")
		}

		log.Info().Msgf("Processing chunk '%s'", header.Name)

		dir, filename := path.Split(header.Name)

		st := dump.ParseSourceType(dir[:len(dir)-1])
		if st == dump.UndefinedSource {
			return errors.Errorf("corrupted dump: found undefined source: %s", dir)
		}

		s, ok := t.sourceByType(st)
		if !ok {
			log.Warn().Msgf("Found dump data for %v, but it's not specified - skipped", st)
			continue
		}

		if err = s.WriteChunk(filename, tr); err != nil {
			return errors.Wrap(err, "failed to write chunk")
		}
	}

	return nil
}

func (t Transferer) sourceByType(st dump.SourceType) (dump.Source, bool) {
	for _, s := range t.sources {
		if s.Type() == st {
			return s, true
		}
	}
	return nil, false
}
