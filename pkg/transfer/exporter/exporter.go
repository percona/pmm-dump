package exporter

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"os"
	"path"
	"pmm-transferer/pkg/dump"
	"time"

	"github.com/pkg/errors"
)

type Exporter struct {
	cfg     Config
	sources []dump.Source
}

func New(cfg Config, s ...dump.Source) *Exporter {
	if len(s) == 0 {
		panic("BUG: failed to create exporter with no sources")
	}
	return &Exporter{
		cfg:     cfg,
		sources: s,
	}
}

func (e *Exporter) Export() error {
	exportTS := time.Now().UTC()

	// TODO: no .gz if no compression set?
	filepath := fmt.Sprintf("pmm-dump-%v.tar.gz", exportTS.Unix())
	if e.cfg.OutPath != "" {
		filepath = path.Join(e.cfg.OutPath, filepath)
	}

	file, err := os.Create(filepath)
	if err != nil {
		return errors.Wrapf(err, "failed to create %s", filepath)
	}
	defer file.Close()

	// TODO: configurable compression level
	w, err := gzip.NewWriterLevel(file, gzip.BestCompression)
	if err != nil {
		return errors.Wrap(err, "failed to create gzip writer")
	}
	defer w.Close()

	tw := tar.NewWriter(w)
	defer tw.Close()

	for _, s := range e.sources {
		ch, err := s.ReadChunk(dump.ChunkMeta{
			Source: s.Type(),
			Start:  nil, // TODO: configurable start and end
			End:    nil,
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
