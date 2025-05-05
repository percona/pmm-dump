// Copyright 2023 Percona LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package transferer

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"time"

	"github.com/rs/zerolog/log"

	"pmm-dump/pkg/dump"
)

func ReadMetaFromDump(dumpPath string, piped bool) (*dump.Meta, error) {
	var file *os.File
	if piped {
		file = os.Stdin
	} else {
		var err error
		file, err = os.Open(dumpPath) //nolint:gosec
		if err != nil {
			return nil, fmt.Errorf("failed to open file: %w", err)
		}
	}
	defer file.Close() //nolint:errcheck

	gzr, err := gzip.NewReader(file)
	if err != nil {
		return nil, fmt.Errorf("failed to open as gzip: %w", err)
	}
	defer gzr.Close() //nolint:errcheck

	tr := tar.NewReader(gzr)

	for {
		log.Debug().Msg("Reading files from dump...")

		header, err := tr.Next()

		if errors.Is(err, io.EOF) {
			log.Debug().Msg("Processed complete dump file - no meta found")
			return nil, errors.New("no meta file found in dump")
		}

		if err != nil {
			return nil, fmt.Errorf("failed to read a file from dump: %w", err)
		}

		_, filename := path.Split(header.Name)

		if filename != dump.MetaFilename {
			continue
		}

		log.Debug().Msg("Found meta file")

		meta, err := readMetafile(tr)
		if err != nil {
			return nil, fmt.Errorf("failed to read meta file: %w", err)
		}

		return meta, nil
	}
}

func writeMetafile(tw *tar.Writer, meta dump.Meta) error {
	log.Debug().Msg("Writing dump meta")

	metaContent, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("failed to marshal dump meta: %w", err)
	}

	err = tw.WriteHeader(&tar.Header{
		Typeflag: tar.TypeReg,
		Name:     dump.MetaFilename,
		Size:     int64(len(metaContent)),
		Mode:     filePermission,
		ModTime:  time.Now(),
	})
	if err != nil {
		return fmt.Errorf("failed to write dump meta: %w", err)
	}

	if _, err = tw.Write(metaContent); err != nil {
		return fmt.Errorf("failed to write dump meta content: %w", err)
	}

	return nil
}

func readMetafile(r io.Reader) (*dump.Meta, error) {
	metaBytes, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("failed to read bytes: %w", err)
	}

	var meta dump.Meta
	if err := json.Unmarshal(metaBytes, &meta); err != nil {
		return nil, fmt.Errorf("failed to unmarshal: %w", err)
	}

	return &meta, nil
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
		log.Warn().Msgf("pmm-dump version mismatch\nExported:\t%v\nCurrent:\t%v",
			dumpMeta.Version.GitCommit, runtimeMeta.Version.GitCommit)
	}
}
