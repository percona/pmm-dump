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
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/pkg/errors"

	"pmm-dump/internal/test/deployment"
	"pmm-dump/internal/test/util"
	"pmm-dump/pkg/dump"
	"pmm-dump/pkg/victoriametrics"
)

func TestContentLimit(t *testing.T) {
	c := deployment.NewController(t)
	pmm := c.NewPMM("content-limit", ".env.test")
	if pmm.UseExistingDeployment() {
		t.Skip("skipping test because existing deployment is used")
	}

	ctx := context.Background()

	var b util.Binary
	tmpDir := util.CreateTestDir(t, "content-limit-test")
	dumpPath := filepath.Join(tmpDir, "dump.tar.gz")
	err := generateFakeDump(dumpPath)
	if err != nil {
		t.Fatal(err)
	}

	if err := pmm.Deploy(ctx); err != nil {
		t.Fatal(err)
	}

	err = pmm.Exec(ctx, pmm.ServerContainerName(), "bash", "-c", "sed -i -e 's/client_max_body_size 10m/client_max_body_size 1m/g' /etc/nginx/conf.d/pmm.conf")
	if err != nil {
		t.Fatal("failed to change nginx settings", err)
	}

	if err := pmm.Restart(ctx); err != nil {
		t.Fatal("failed to restart pmm", err)
	}

	stdout, stderr, err := b.Run(
		"import",
		"-d", dumpPath,
		"--pmm-url", pmm.PMMURL())
	if err != nil {
		if !strings.Contains(stderr, "413 Request Entity Too Large") {
			t.Fatal("expected `413 Request Entity Too Large` error, got", err, stdout, stderr)
		}
	} else {
		t.Fatal("expected `413 Request Entity Too Large` error but import didn't fail")
	}

	pmm.Log("Importing with 10KB limit")

	stdout, stderr, err = b.Run(
		"import",
		"-d", dumpPath,
		"--pmm-url", pmm.PMMURL(),
		"--vm-content-limit", "10024")
	if err != nil {
		t.Fatal("failed to import", err, stdout, stderr)
	}
}

const (
	filePermission = 0o600
)

func generateFakeDump(filepath string) error {
	file, err := os.Create(filepath) //nolint:gosec
	if err != nil {
		return errors.Wrap(err, "failed to open file")
	}
	defer file.Close() //nolint:errcheck
	gzw, err := gzip.NewWriterLevel(file, gzip.BestCompression)
	if err != nil {
		return errors.Wrap(err, "failed to create gzip writer")
	}
	defer gzw.Close() //nolint:errcheck

	tw := tar.NewWriter(gzw)
	defer tw.Close() //nolint:errcheck

	meta := &dump.Meta{
		VMDataFormat: "json",
	}

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
		return errors.Wrap(err, "failed to write dump meta")
	}

	if _, err = tw.Write(metaContent); err != nil {
		return errors.Wrap(err, "failed to write dump meta content")
	}

	for i := 0; i < 10; i++ {
		content, err := generateFakeChunk(100000)
		if err != nil {
			return errors.Wrap(err, "failed to generate fake chunk")
		}

		chunkSize := int64(len(content))

		err = tw.WriteHeader(&tar.Header{
			Typeflag: tar.TypeReg,
			Name:     path.Join("vm", fmt.Sprintf("chunk-%d.bin", i)),
			Size:     chunkSize,
			Mode:     filePermission,
			ModTime:  time.Now(),
			Uid:      1,
		})
		if err != nil {
			return errors.Wrap(err, "failed to write file header")
		}
		if _, err = tw.Write(content); err != nil {
			return errors.Wrap(err, "failed to write chunk content")
		}
	}
	return nil
}

func generateFakeChunk(size int) ([]byte, error) {
	r := rand.New(rand.NewSource(time.Now().Unix())) //nolint:gosec
	var data []byte
	for i := 0; i < size; i++ {
		metricsData, err := json.Marshal(victoriametrics.Metric{
			Metric: map[string]string{
				"__name__": "test",
				"job":      "test",
				"instance": "test-" + strconv.Itoa(i),
				"test":     strconv.Itoa(int(time.Now().UnixNano())),
			},
			Values:     []float64{r.NormFloat64()},
			Timestamps: []int64{time.Now().UnixNano()},
		})
		if err != nil {
			return nil, errors.Wrap(err, "marshal metrics")
		}
		data = append(data, metricsData...)
	}
	return compressData(data)
}

func compressData(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	if _, err := gw.Write(data); err != nil {
		return nil, errors.Wrap(err, "write gzip")
	}
	if err := gw.Close(); err != nil {
		return nil, errors.Wrap(err, "close gzip")
	}
	return buf.Bytes(), nil
}
