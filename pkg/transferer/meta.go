package transferer

import (
	"archive/tar"
	"encoding/json"
	"fmt"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
	"io"
	"io/ioutil"
	"pmm-transferer/pkg/dump"
)

func writeMetafile(tw *tar.Writer, meta dump.Meta) error {
	log.Debug().Msg("Writing dump meta")

	metaContent, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("failed to marshal dump meta: %s", err)
	}

	err = tw.WriteHeader(&tar.Header{
		Typeflag: tar.TypeReg,
		Name:     dump.MetaFilename,
		Size:     int64(len(metaContent)),
		Mode:     0600,
	})
	if err != nil {
		return errors.Wrap(err, "failed to write dump meta")
	}

	if _, err = tw.Write(metaContent); err != nil {
		return errors.Wrap(err, "failed to write dump meta content")
	}

	return nil
}

func readMetafile(r io.Reader) (*dump.Meta, error) {
	metaBytes, err := ioutil.ReadAll(r)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read bytes")
	}

	meta := &dump.Meta{}

	if err := json.Unmarshal(metaBytes, meta); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal")
	}

	return meta, nil
}

func readAndCompareDumpMeta(r io.Reader, runtimeMeta dump.Meta) {
	dumpMeta, err := readMetafile(r)
	if err != nil {
		log.Err(err).Msgf("Failed to read meta file. No version checks could be performed")
		return
	}

	if dumpMeta.PMMServerVersion != runtimeMeta.PMMServerVersion {
		log.Warn().Msgf("PMM Versions mismatch\nExported:\t%v\nCurrent:\t%v",
			dumpMeta.PMMServerVersion, runtimeMeta.PMMServerVersion)
	}

	if dumpMeta.Version.GitCommit != runtimeMeta.Version.GitCommit {
		log.Warn().Msgf("Transferer version mismatch\nExported:\t%v\nCurrent:\t%v",
			dumpMeta.Version.GitCommit, runtimeMeta.Version.GitCommit)
	}
}
