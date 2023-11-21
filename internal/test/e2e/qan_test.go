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
	"database/sql"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"testing"
	"time"

	"github.com/pkg/errors"

	"pmm-dump/internal/test/util"
	"pmm-dump/pkg/clickhouse"
	"pmm-dump/pkg/clickhouse/tsv"
	"pmm-dump/pkg/dump"
)

func TestQANWhere(t *testing.T) {
	ctx := context.Background()
	pmm := util.NewPMM(t, "qan-where", ".env.test")
	pmm.Stop()
	pmm.Deploy()
	defer pmm.Stop()

	var b util.Binary
	testDir := util.TestDir(t, "qan-where")

	t.Log("Waiting for QAN data for 2 minutes")
	time.Sleep(time.Minute * 2)

	cSource, err := clickhouse.NewSource(ctx, clickhouse.Config{
		ConnectionURL: pmm.ClickhouseURL(),
	})
	if err != nil {
		t.Fatal("failed to create clickhouse source", err)
	}

	columnTypes := cSource.ColumnTypes()

	tests := []struct {
		name      string
		instances []string
		query     string
		equalMap  map[string]string
	}{
		{
			name:     "no filter",
			query:    "",
			equalMap: map[string]string{},
		},
		{
			name:      "one instance was specified",
			instances: []string{"mongo"},
			equalMap: map[string]string{
				"service_name": "mongo",
			},
		},
		{
			name:      "two instances were specified",
			instances: []string{"mongo", "some_other_service"},
			equalMap: map[string]string{
				"service_name": "mongo",
			},
		},
		{
			name:  "filter by service name",
			query: "service_name='mongo'",
			equalMap: map[string]string{
				"service_name": "mongo",
			},
		},
		{
			name:  "filter by service type and service name",
			query: "service_name='mongo' AND service_type='mongodb'",
			equalMap: map[string]string{
				"service_type": "mongodb",
				"service_name": "mongo",
			},
		},
	}

	for i, tt := range tests {
		i := i
		t.Run(tt.name, func(t *testing.T) {
			dumpName := fmt.Sprintf("dump-%d.tar.gz", i)
			dumpPath := filepath.Join(testDir, dumpName)

			args := []string{
				"-d", dumpPath,
				"--pmm-url", pmm.PMMURL(),
				"--dump-qan",
				"--click-house-url", pmm.ClickhouseURL(),
				"--where", tt.query,
			}

			for _, instance := range tt.instances {
				args = append(args, fmt.Sprintf("--instance=%s", instance))
			}

			t.Log("Exporting data to", filepath.Join(testDir, "dump.tar.gz"))
			stdout, stderr, err := b.Run(append([]string{"export", "--ignore-load"}, args...)...)
			if err != nil {
				t.Fatal("failed to export", err, stdout, stderr)
			}
			chunkMap, err := getQANChunks(dumpPath)
			if err != nil {
				t.Fatal("failed to get qan chunks", err)
			}

			if len(chunkMap) == 0 {
				t.Fatal("qan chunks not found", err)
			}
			for chunkName, chunkData := range chunkMap {
				err := validateQAN(chunkData, columnTypes, tt.equalMap)
				if err != nil {
					t.Fatal("failed to validate qan chunk", chunkName, err)
				}
			}
		})
	}
}

func validateQAN(data []byte, columnTypes []*sql.ColumnType, equalMap map[string]string) error {
	tr := tsv.NewReader(bytes.NewReader(data), columnTypes)
	for {
		values, err := tr.Read()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return errors.Wrap(err, "failed to read tsv")
		}
		if len(values) != len(columnTypes) {
			return errors.Errorf("invalid number of values: expected %d, got %d", len(columnTypes), len(values))
		}

		for k, v := range equalMap {
			found := false
			for i, ct := range columnTypes {
				if ct.Name() == k {
					if values[i] != v {
						return errors.Errorf("invalid value in column %s: expected %s, got %s", ct.Name(), v, values[i])
					}
					found = true
				}
			}
			if !found {
				return errors.Errorf("column %s not found", k)
			}
		}
	}
	return nil
}

func getQANChunks(filename string) (map[string][]byte, error) {
	f, err := os.Open(filename) //nolint:gosec
	if err != nil {
		return nil, err
	}
	defer f.Close() //nolint:errcheck

	gzr, err := gzip.NewReader(f)
	if err != nil {
		return nil, errors.Wrap(err, "failed to open as gzip")
	}
	defer gzr.Close() //nolint:errcheck

	tr := tar.NewReader(gzr)
	chunkMap := make(chunkMap)

	for {
		header, err := tr.Next()
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}

		dir, filename := path.Split(header.Name)

		switch filename {
		case dump.MetaFilename, dump.LogFilename:
			continue
		}

		if len(dir) == 0 {
			return nil, errors.Errorf("corrupted dump: found unknown file %s", filename)
		}

		st := dump.ParseSourceType(dir[:len(dir)-1])
		if st == dump.UndefinedSource {
			return nil, errors.Errorf("corrupted dump: found undefined source: %s", dir)
		}
		if st == dump.ClickHouse {
			content, err := io.ReadAll(tr)
			if err != nil {
				return nil, errors.Wrap(err, "failed to read chunk content")
			}

			chunkMap[header.Name] = content
		}
	}
	return chunkMap, nil
}

func TestQANEmptyChunks(t *testing.T) {
	pmm := util.NewPMM(t, "qan-empty-chunks", ".env.test")
	pmm.Stop()
	pmm.Deploy()
	defer pmm.Stop()

	var b util.Binary
	testDir := util.TestDir(t, "qan-empty-chunks")

	startTime := time.Now()
	t.Log("Waiting for QAN data for 3 minutes")
	time.Sleep(time.Minute * 3)

	endTime := time.Now().Add(-time.Minute)

	dumpPath := filepath.Join(testDir, "dump.tar.gz")
	args := []string{
		"-d", dumpPath,
		"--pmm-url", pmm.PMMURL(),
		"--dump-qan",
		"--no-dump-core",
		"--click-house-url", pmm.ClickhouseURL(),
		"--instance", "pmm-server-postgresql",
		"--start-ts", startTime.Format(time.RFC3339),
		"--end-ts", endTime.Format(time.RFC3339),
		"--chunk-rows", "1",
	}

	t.Log("Exporting data to", dumpPath)
	stdout, stderr, err := b.Run(append([]string{"export", "--ignore-load"}, args...)...)
	if err != nil {
		t.Fatal("failed to export", err, stdout, stderr)
	}

	// We shouldn't have any empty chunks in the dump
	chunks, err := getQANChunks(dumpPath)
	if err != nil {
		t.Fatal(err)
	}

	for name, data := range chunks {
		if len(data) == 0 {
			t.Fatalf("Empty chunk %s found in the dump %s", name, dumpPath)
		}
	}
}
