//go:build e2e

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

package e2e

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"math/big"
	"os"
	"path"
	"path/filepath"
	"testing"
	"time"

	"pmm-dump/internal/test/deployment"
	"pmm-dump/internal/test/util"

	"pmm-dump/pkg/clickhouse"
	"pmm-dump/pkg/clickhouse/tsv"
	"pmm-dump/pkg/dump"
)

const (
	trytime = 3
)

func TestClickHouseTime(t *testing.T) {
	c := deployment.NewController(t)
	ctx := t.Context()
	pmm := c.NewPMM("time", ".env.test")
	if err := pmm.Deploy(ctx); err != nil {
		t.Fatal(err)
	}

	cSource, err := clickhouse.NewSource(ctx, clickhouse.Config{
		ConnectionURL: pmm.ClickhouseURL(),
	})
	if err != nil {
		t.Fatal(err)
	}

	pmm.Log("Waiting for QAN data for", trytime, "minutes")
	tCtx, cancel := context.WithTimeout(ctx, trytime)
	defer cancel()
	if err := util.RetryOnError(tCtx, func() error {
		rowsCount, err := cSource.Count("", nil, nil)
		if err != nil {
			return err
		}
		if rowsCount == 0 {
			return errors.New("no qan data")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	var b util.Binary
	tmpDir := util.CreateTestDir(t, "time-test")
	dumpPathOriginal := filepath.Join(tmpDir, "dumpOrg.tar.gz")

	pmm.Log("Exporting data to", dumpPathOriginal)
	stdout, stderr, err := b.Run(
		"export",
		"--ignore-load",
		"-d", dumpPathOriginal,
		"--pmm-url", pmm.PMMURL(),
		"--dump-qan",
		"--click-house-url", pmm.ClickhouseURL(),
		"--no-encryption",
		"-v")
	if err != nil {
		t.Fatal("failed to export", err, stdout, stderr)
	}

	dumpPathCopy := filepath.Join(tmpDir, "dumpCopy.tar.gz")
	pmm.Log("Overwriting time in a dump to have a random time zone and then copying an old dump to a new dump: ", dumpPathCopy)
	err = overwriteClickChunks(dumpPathOriginal, dumpPathCopy, cSource.ColumnTypes())
	if err != nil {
		t.Fatal("failed to overwrite", err, stdout, stderr)
	}

	pmm.Log("Importing data from", dumpPathCopy)
	stdout, stderr, err = b.Run(
		"import",
		"-d", dumpPathCopy,
		"--pmm-url", pmm.PMMURL(),
		"--dump-qan",
		"--click-house-url", pmm.ClickhouseURL(),
		"--no-encryption",
		"-v")
	if err != nil {
		t.Fatal("failed to import", err, stdout, stderr)
	}
}

// Go over the dump and change the ClickHouse time values to a different random time zone.
// This allows us to test a feature in the import process that converts all time zones to UTC.
// Since we can't change the values inside a formed TAR archive, we copy everything to a new archive.
func overwriteClickChunks(filename, filename2 string, columnTypes []*sql.ColumnType) error {
	oldDump, err := os.Open(filename) //nolint:gosec
	if err != nil {
		return fmt.Errorf("failed to open old dump: %w", err)
	}
	defer oldDump.Close() //nolint:errcheck

	newDump, err := os.Create(filename2) //nolint:gosec
	if err != nil {
		return fmt.Errorf("failed to open new dump: %w", err)
	}
	defer newDump.Close() //nolint:errcheck

	gzr, err := gzip.NewReader(oldDump)
	if err != nil {
		return fmt.Errorf("failed to open old dump as gzip: %w", err)
	}
	defer gzr.Close() //nolint:errcheck

	gzw := gzip.NewWriter(newDump)
	defer gzw.Close() //nolint:errcheck

	tr := tar.NewReader(gzr)
	tw := tar.NewWriter(gzw)
	defer tw.Close() //nolint:errcheck

	for {
		header, err := tr.Next()
		if err != nil {
			if err == io.EOF {
				break
			}
			return fmt.Errorf("failed to read next entry: %w", err)
		}

		dir, filename := path.Split(header.Name)
		switch filename {
		case dump.MetaFilename, dump.LogFilename:
			if err := tw.WriteHeader(header); err != nil {
				return fmt.Errorf("failed to write meta or log header: %w", err)
			}
			if _, err := io.Copy(tw, tr); err != nil { //nolint:gosec
				return fmt.Errorf("failed to copy meta or log entry: %w", err)
			}
			continue
		}

		if len(dir) == 0 {
			return fmt.Errorf("corrupted dump: found unknown file %s", filename)
		}

		st := dump.ParseSourceType(dir[:len(dir)-1])
		if st == dump.UndefinedSource {
			return fmt.Errorf("corrupted dump: found undefined source: %s", dir)
		}

		if st == dump.ClickHouse {
			content, err := readOverClickChunk(tr, columnTypes)
			if err != nil {
				return fmt.Errorf("failed to read over Click chunk: %w", err)
			}
			chunkSize := int64(len(content))
			header.Size = chunkSize
			err = tw.WriteHeader(header)
			if err != nil {
				return fmt.Errorf("failed to write file header: %w", err)
			}
			if _, err = tw.Write(content); err != nil {
				return fmt.Errorf("failed to write chunk content: %w", err)
			}
		} else {
			if err := tw.WriteHeader(header); err != nil {
				return fmt.Errorf("failed to write header: %w", err)
			}
			if _, err := io.Copy(tw, tr); err != nil { //nolint:gosec
				return fmt.Errorf("failed to copy tar entry: %w", err)
			}
		}
	}
	return nil
}

// Reads over ClickHouse chunks and returns content as byte array but converts all time values to a random time zone.
func readOverClickChunk(tr *tar.Reader, columnTypes []*sql.ColumnType) ([]byte, error) {
	content, err := io.ReadAll(tr)
	if err != nil {
		return nil, fmt.Errorf("failed to read chunk content: %w", err)
	}
	b := new(bytes.Buffer)
	readerTSV := tsv.NewReader(bytes.NewReader(content), columnTypes)
	writerTSV := tsv.NewWriter(b)
	for {
		records, err := readerTSV.Read()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("failed to read next tsv entry: %w", err)
		}
		records, err = convertTimeToRandomTimeZone(records)
		if err != nil {
			return nil, fmt.Errorf("failed to convert time to a random time zone: %w", err)
		}
		err = writerTSV.Write(toStringSlice(records))
		if err != nil {
			return nil, fmt.Errorf("failed to write tsv: %w", err)
		}
	}
	writerTSV.Flush()
	err = writerTSV.Error()
	if err != nil {
		return nil, fmt.Errorf("failed to flush tsv writer: %w", err)
	}
	content = b.Bytes()
	return content, nil
}

func convertTimeToRandomTimeZone(records []any) ([]any, error) {
	for i, record := range records {
		timeCh, ok := record.(time.Time)
		if !ok {
			continue
		}
		newYork, err := time.LoadLocation("America/New_York")
		if err != nil {
			return nil, err
		}
		los_angeles, err := time.LoadLocation("America/Los_Angeles")
		if err != nil {
			return nil, err
		}
		moscow, err := time.LoadLocation("Europe/Moscow")
		if err != nil {
			return nil, err
		}
		beijing, err := time.LoadLocation("Asia/Shanghai")
		if err != nil {
			return nil, err
		}
		tokyo, err := time.LoadLocation("Asia/Tokyo")
		if err != nil {
			return nil, err
		}
		locationMap := map[int]*time.Location{
			0: newYork,
			1: los_angeles,
			2: moscow,
			3: beijing,
			4: tokyo,
		}

		num, err := rand.Int(rand.Reader, big.NewInt(4))
		if err != nil {
			return nil, err
		}
		loc := locationMap[int(num.Int64())]
		records[i] = timeCh.In(loc)
	}
	return records, nil
}

func toStringSlice(iSlice []any) []string {
	values := make([]string, 0, cap(iSlice))
	for _, v := range iSlice {
		if v == nil {
			values = append(values, "")
			continue
		}
		values = append(values, fmt.Sprintf("%v", v))
	}
	return values
}
