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
	"database/sql"
	"errors"
	"fmt"
	"io"
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

const qanWaitTimeout = time.Minute * 5

var qanPMM = deployment.NewReusablePMM("qan", ".env.test")

func TestQANWhere(t *testing.T) {
	c := deployment.NewController(t)
	ctx := t.Context()
	pmm := c.ReusablePMM(qanPMM)
	if err := pmm.Deploy(ctx); err != nil {
		t.Fatal(err)
	}

	var b util.Binary
	testDir := util.CreateTestDir(t, "qan-where")

	cSource, err := clickhouse.NewSource(ctx, clickhouse.Config{
		ConnectionURL: pmm.ClickhouseURL(),
	})
	if err != nil {
		t.Fatal(err)
	}

	pmm.Log("Waiting for QAN data for", qanWaitTimeout, "minutes")
	tCtx, cancel := context.WithTimeout(ctx, qanWaitTimeout)
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
		pmm.Log("Error: ", err)
		pmm.Log("Clickhouse status: ")
		reader, err := pmm.FileReader(ctx, pmm.ServerContainerName(), "/srv/clickhouse/status")
		if err != nil {
			t.Fatal("failed to get file from container", err)
		}
		defer reader.Close() //nolint:errcheck
		tr := tar.NewReader(reader)
		if _, err := tr.Next(); err != nil {
			t.Fatal("failed to read from tar file", err)
		}
		buf := new(bytes.Buffer)
		_, err = buf.ReadFrom(tr)
		if err != nil {
			t.Fatal("failed to read from file", err)
		}
		status := buf.Bytes()
		pmm.Log(string(status))

		pmm.Log("Clickhouse logs: ")
		logs, err := pmm.FileReader(ctx, pmm.ServerContainerName(), "/srv/logs/clickhouse-server.log")
		if err != nil {
			t.Fatal("failed to get file from container", err)
		}
		defer logs.Close() //nolint:errcheck
		trL := tar.NewReader(logs)
		if _, err := trL.Next(); err != nil {
			t.Fatal("failed to read from file", err)
		}
		bufs := new(bytes.Buffer)
		_, err = bufs.ReadFrom(trL)
		if err != nil {
			t.Fatal("failed to read from file", err)
		}
		lo := bufs.Bytes()
		pmm.Log(string(lo))

		t.Fatal(err)
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
			equalMap: nil,
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
		t.Run(tt.name, func(t *testing.T) {
			dumpName := fmt.Sprintf("dump-%d.tar.gz", i)
			dumpPath := filepath.Join(testDir, dumpName)

			args := []string{
				"-d", dumpPath,
				"--pmm-url", pmm.PMMURL(),
				"--dump-qan",
				"--click-house-url", pmm.ClickhouseURL(),
				"--where", tt.query,
				"-v",
			}
			tCtx, cancel := context.WithTimeout(ctx, qanWaitTimeout)
			for _, instance := range tt.instances {
				args = append(args, "--instance="+instance)
			}

			defer cancel()
			if err := util.RetryOnError(tCtx, func() error {
				pmm.Log("Exporting data to", filepath.Join(testDir, "dump.tar.gz"))
				stdout, stderr, err := b.Run(append([]string{"export", "--ignore-load", "--no-encryption"}, args...)...)
				if err != nil {
					return fmt.Errorf("failed to export: stdout %s; stderr %s: %w", stdout, stderr, err)
				}
				chunkMap, err := getQANChunks(dumpPath)
				if err != nil {
					return fmt.Errorf("failed to get qan chunks: %w", err)
				}
				if len(chunkMap) == 0 {
					return fmt.Errorf("qan chunks not found: %w", err)
				}
				for chunkName, chunkData := range chunkMap {
					err := validateQAN(chunkData, columnTypes, tt.equalMap)
					if err != nil {
						return fmt.Errorf("failed to validate qan chunk %s: %w", chunkName, err)
					}
				}
				return nil
			}); err != nil {
				t.Fatal(err)
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
			return fmt.Errorf("failed to read tsv: %w", err)
		}
		if len(values) != len(columnTypes) {
			return fmt.Errorf("invalid number of values: expected %d, got %d", len(columnTypes), len(values))
		}

		for k, v := range equalMap {
			found := false
			for i, ct := range columnTypes {
				if ct.Name() == k {
					if values[i] != v {
						return fmt.Errorf("invalid value in column %s: expected %s, got %s", ct.Name(), v, values[i])
					}
					found = true
				}
			}
			if !found {
				return fmt.Errorf("column %s not found", k)
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
		return nil, fmt.Errorf("failed to open as gzip: %w", err)
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
			return nil, fmt.Errorf("corrupted dump: found unknown file %s", filename)
		}

		st := dump.ParseSourceType(dir[:len(dir)-1])
		if st == dump.UndefinedSource {
			return nil, fmt.Errorf("corrupted dump: found undefined source: %s", dir)
		}
		if st == dump.ClickHouse {
			content, err := io.ReadAll(tr)
			if err != nil {
				return nil, fmt.Errorf("failed to read chunk content: %w", err)
			}

			chunkMap[header.Name] = content
		}
	}
	return chunkMap, nil
}

func TestQANEmptyChunks(t *testing.T) {
	ctx := t.Context()

	c := deployment.NewController(t)
	pmm := c.ReusablePMM(qanPMM)
	if err := pmm.Deploy(ctx); err != nil {
		t.Fatal(err)
	}

	var b util.Binary
	testDir := util.CreateTestDir(t, "qan-empty-chunks")

	startTime := time.Now()

	cSource, err := clickhouse.NewSource(ctx, clickhouse.Config{
		ConnectionURL: pmm.ClickhouseURL(),
	})
	if err != nil {
		t.Fatal("failed to create clickhouse source", err)
	}

	pmm.Log("Waiting for QAN data for", qanWaitTimeout, "minutes")
	tCtx, cancel := context.WithTimeout(ctx, qanWaitTimeout)
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
		pmm.Log("Clickhouse status: ")
		reader, err := pmm.FileReader(ctx, pmm.ServerContainerName(), "/srv/clickhouse/status")
		if err != nil {
			t.Fatal("failed to get file from container", err)
		}
		defer reader.Close() //nolint:errcheck
		tr := tar.NewReader(reader)
		if _, err := tr.Next(); err != nil {
			t.Fatal("failed to read from file", err)
		}
		buf := new(bytes.Buffer)
		_, err = buf.ReadFrom(tr)
		if err != nil {
			t.Fatal("failed to read from file", err)
		}
		status := buf.Bytes()
		pmm.Log(string(status))

		pmm.Log("Clickhouse logs: ")
		logs, err := pmm.FileReader(ctx, pmm.ServerContainerName(), "/srv/logs/clickhouse-server.log")
		if err != nil {
			t.Fatal("failed to get file from container", err)
		}
		defer logs.Close() //nolint:errcheck
		trL := tar.NewReader(logs)
		if _, err := trL.Next(); err != nil {
			t.Fatal("failed to read from file", err)
		}
		bufs := new(bytes.Buffer)
		_, err = bufs.ReadFrom(trL)
		if err != nil {
			t.Fatal("failed to read from file", err)
		}
		lo := bufs.Bytes()
		pmm.Log(string(lo))
		t.Fatal(err)
	}

	pmm.Log("Waiting for QAN data about instance \"pmm-server-postgresql\" for", qanWaitTimeout, "minutes")
	tCtx, cancel = context.WithTimeout(ctx, qanWaitTimeout)
	defer cancel()
	if err := util.RetryOnError(tCtx, func() error {
		tn := time.Now()
		rowsCount, err := cSource.Count("service_name='pmm-server-postgresql'", &startTime, &tn)
		if err != nil {
			return err
		}
		if rowsCount == 0 {
			pmm.Log("QAN doesn't have data about instance \"pmm-server-postgresql\". Waiting...")
			return errors.New("no qan data")
		}
		return nil
	}); err != nil {
		t.Fatal(err, "failed to get qan data")
	}

	dumpPath := filepath.Join(testDir, "dump.tar.gz")
	args := []string{
		"-d", dumpPath,
		"--pmm-url", pmm.PMMURL(),
		"--dump-qan",
		"--no-dump-core",
		"--click-house-url", pmm.ClickhouseURL(),
		"--instance", "pmm-server-postgresql",
		"--start-ts", startTime.Format(time.RFC3339),
		"--end-ts", time.Now().Format(time.RFC3339),
		"--chunk-rows", "1",
		"-v",
	}

	pmm.Log("Exporting data to", dumpPath)
	stdout, stderr, err := b.Run(append([]string{"export", "--ignore-load", "--no-encryption"}, args...)...)
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
