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
	"context"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sync/errgroup"

	"pmm-dump/internal/test/deployment"
	"pmm-dump/internal/test/util"
)

func TestEncryptionExportImport(t *testing.T) {
	c := deployment.NewController(t)
	pmm := c.NewPMM("encryption-export-import", ".env.test")
	newPMM := c.NewPMM("encryption-export-import-2", ".env2.test")

	ctx := context.Background()
	g, gCtx := errgroup.WithContext(ctx)
	g.Go(func() error {
		return pmm.Deploy(gCtx)
	})

	g.Go(func() error {
		return newPMM.Deploy(gCtx)
	})
	if err := g.Wait(); err != nil {
		t.Fatal(err)
	}

	var b util.Binary
	testDir := t.TempDir()
	pmm.Log("Exporting data to", filepath.Join(testDir, "dump.tar.gz.enc"))
	args := []string{"-d", filepath.Join(testDir, "dump.tar.gz.enc"), "--pmm-url", pmm.PMMURL(), "--dump-qan", "--click-house-url", pmm.ClickhouseURL(), "--pass", "somepass"}
	stdout, stderr, err := b.Run(append([]string{"export", "--ignore-load"}, args...)...)
	if err != nil {
		t.Fatal("failed to export", err, stdout, stderr)
	}

	args = []string{"-d", filepath.Join(testDir, "dump.tar.gz.enc"), "--pmm-url", newPMM.PMMURL(), "--dump-qan", "--click-house-url", newPMM.ClickhouseURL(), "--pass", "somepass"}
	pmm.Log("Importing data from", filepath.Join(testDir, "dump.tar.gz.enc"))
	stdout, stderr, err = b.Run(append([]string{"import"}, args...)...)
	if err != nil {
		t.Fatal("failed to import", err, stdout, stderr)
	}

	pmm.Log("Exporting data to check keys", filepath.Join(testDir, "dump-just-key.tar.gz.enc"))
	args = []string{"-d", filepath.Join(testDir, "dump-just-key.tar.gz.enc"), "--pmm-url", pmm.PMMURL(), "--dump-qan", "--click-house-url", pmm.ClickhouseURL(), "--pass", "somepass", "--just-key"}
	stdout, stderr, err = b.Run(append([]string{"export", "--ignore-load"}, args...)...)
	if err != nil {
		t.Fatal("failed to export", err, stdout, stderr)
	}

	want := `Pass: somepass`
	stderr = strings.TrimSpace(stderr)
	if stderr != want {
		t.Fatalf("want %s, got %s", want, stderr)
	}

	pmm.Log("Exporting data to check pipe", filepath.Join(testDir, "dump.tar.gz.enc"))
	exportArgs := "./pmm-dump export -d " + filepath.Join(testDir, "dump.tar.gz.enc") + " --pmm-url " + newPMM.PMMURL() + " --dump-qan --click-house-url " + newPMM.ClickhouseURL() + " --pass somepass --stdout "
	importArgs := " | ./pmm-dump import -d " + filepath.Join(testDir, "dump.tar.gz.enc") + " --pmm-url " + newPMM.PMMURL() + " --dump-qan --click-house-url " + newPMM.ClickhouseURL() + " --pass somepass"
	output, outputerr, err := b.RunBash(append([]string{"-c"}, exportArgs+importArgs)...)
	if err != nil {
		t.Fatal("failed to pipe", err, output, outputerr)
	}

	pmm.Log("Exporting data to check pipe openssl", filepath.Join(testDir, "dump.tar.gz.enc"))
	opensslArgs := " | openssl enc -d -aes-256-ctr -pbkdf2 -out " + filepath.Join(testDir, "dump.tar.gz") + " -pass pass:somepass"
	output, outputerr, err = b.RunBash(append([]string{"-c"}, exportArgs+opensslArgs)...)
	if err != nil {
		t.Fatal("failed to pipe to openssl", err, output, outputerr)
	}

	pmm.Log("Importing data to check openssl decryption", filepath.Join(testDir, "dump.tar.gz.enc"))
	argsImport := []string{"-d", filepath.Join(testDir, "dump.tar.gz"), "--pmm-url", newPMM.PMMURL(), "--dump-qan", "--click-house-url", newPMM.ClickhouseURL(), "--no-encryption"}
	pmm.Log("Importing data from", filepath.Join(testDir, "dump.tar.gz"))
	output, outputerr, err = b.Run(append([]string{"import"}, argsImport...)...)
	if err != nil {
		t.Fatal("failed to import", err, output, outputerr)
	}
}
