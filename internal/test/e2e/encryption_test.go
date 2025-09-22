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
	"bytes"
	"errors"
	"fmt"
	"os"
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

	ctx := t.Context()
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
	baseArgsTemplate := []string{"-d", filepath.Join(testDir, "dump.tar.gz"), "--pmm-url", pmm.PMMURL(), "--dump-qan", "--click-house-url", pmm.ClickhouseURL(), "--ignore-load"}

	pmm.Log("Checking encryption flag `--no-encryption`")
	stderr, err := checkNoEcryption(testDir, baseArgsTemplate)
	if err != nil {
		t.Fatal("failed to check flag `--no-encryption`", stderr, err)
	}

	pmm.Log("Checking encryption flag `--no-just-key`")
	stderr, err = checkJustKey(baseArgsTemplate)
	if err != nil {
		t.Fatal("failed to check flag `--just-key`", stderr, err)
	}

	pmm.Log("Checking encryption flag `--pass`")
	stderr, err = checkPass(testDir, pmm, newPMM)
	if err != nil {
		t.Fatal("failed to check flag `--pass`", stderr, err)
	}

	// --pass-filepath and --force-pass-filepath
	pmm.Log("Checking encryption flag `--pass-filepath` and `--force-pass-filepath`")
	stderr, err = checkPassFilepath(testDir, baseArgsTemplate)
	if err != nil {
		t.Fatal("failed to check flag `--pass-filepath` and `--force-pass-filepath`", stderr, err)
	}

	pmm.Log("Exporting data to check pipe", filepath.Join(testDir, "dump.tar.gz"))
	exportArgs := "./pmm-dump export -d " + filepath.Join(testDir, "dump.tar.gz") + " --pmm-url " + newPMM.PMMURL() + " --dump-qan --click-house-url " + newPMM.ClickhouseURL() + " --pass somepass --stdout "
	importArgs := " | ./pmm-dump import -d " + filepath.Join(testDir, "dump.tar.gz.enc") + " --pmm-url " + newPMM.PMMURL() + " --dump-qan --click-house-url " + newPMM.ClickhouseURL() + " --pass somepass"
	output, outputerr, err := b.RunBash(append([]string{"-c"}, exportArgs+importArgs)...)
	if err != nil {
		t.Fatal("failed to pipe", err, output, outputerr)
	}

	pmm.Log("Exporting data to check pipe openssl", filepath.Join(testDir, "dump.tar.gz"))
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

func checkNoEcryption(testDir string, baseArgsTemplate []string) (string, error) {
	var b util.Binary
	_, stderr, err := b.Run(append([]string{
		"export", "--no-encryption", "--pass-filepath",
		filepath.Join(testDir, "pass.txt"), "--just-key", "--force-pass-filepath", "--pass", "somepass",
	}, baseArgsTemplate...)...)
	if err != nil {
		return stderr, fmt.Errorf("failed to check flag `--no-encryption`: %w", err)
	}

	if !strings.Contains(stderr, "no-encryption flag is set, disabling other encryption flags") {
		return stderr, errors.New("flag `no-encryption` is set but others encryption related flags is not disabled")
	}

	_, err = os.Stat(filepath.Join(testDir, "dump.tar.gz.enc"))
	if err == nil {
		return stderr, fmt.Errorf("--no-encryption flag is specified but encrypted dump was created: %w", err)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return stderr, err
	}
	return stderr, nil
}

func checkJustKey(baseArgsTemplate []string) (string, error) {
	var b util.Binary
	_, stderr, err := b.Run(append([]string{"export", "--just-key", "--pass", "test"}, baseArgsTemplate...)...)
	if err != nil {
		return stderr, fmt.Errorf("failed to check flag `--just-key`: %w", err)
	}
	if strings.TrimSpace(stderr) != "Password: test" {
		return stderr, errors.New("flag `--just-key` is set but logs has something other than password")
	}

	_, stderr, err = b.Run(append([]string{"export", "--just-key", "-v", "--pass", "test"}, baseArgsTemplate...)...)
	if err != nil {
		return stderr, fmt.Errorf("failed to check flag `--just-key`: %w", err)
	}
	if strings.TrimSpace(stderr) == "Password: test" {
		return stderr, errors.New("flag `--just-key` and flag `-v` is set but logs has only password")
	}

	_, stderr, _ = b.Run(append([]string{"failtest", "--just-key", "--pass", "test"}, baseArgsTemplate...)...)
	if strings.TrimSpace(stderr) == "" {
		return stderr, errors.New("flag `--just-key` is set and error is triggered but logs is empty")
	}

	return stderr, nil
}

func checkPass(testDir string, pmm, newPMM *deployment.PMM) (string, error) {
	var b util.Binary
	args := []string{"-d", filepath.Join(testDir, "dump.tar.gz"), "--pmm-url", pmm.PMMURL(), "--dump-qan", "--click-house-url", pmm.ClickhouseURL(), "--pass", "somepass"}
	_, stderr, err := b.Run(append([]string{"export", "--ignore-load"}, args...)...)
	if err != nil {
		return stderr, fmt.Errorf("failed to export: %w", err)
	}

	args = []string{"-d", filepath.Join(testDir, "dump.tar.gz.enc"), "--pmm-url", newPMM.PMMURL(), "--dump-qan", "--click-house-url", newPMM.ClickhouseURL(), "--pass", "somepass"}
	_, stderr, err = b.Run(append([]string{"import"}, args...)...)
	if err != nil {
		return stderr, fmt.Errorf("failed to import: %w", err)
	}
	return stderr, nil
}

func checkPassFilepath(testDir string, baseArgsTemplate []string) (string, error) {
	var b util.Binary
	_, stderr, err := b.Run(append([]string{"export", "--just-key", "--pass", "test", "--force-pass-filepath"}, baseArgsTemplate...)...)
	if err != nil {
		return stderr, fmt.Errorf("failed to check flag `--force-pass-filepath`: %w", err)
	}
	if !strings.Contains(stderr, "force-pass-filepath is set and pass-filepath is empty, disabling force-pass-filepath") {
		return stderr, errors.New("flag `force-pass-filepath` is set and pass-filepath is empty, but force-pass-filepath has not been disabled")
	}
	// password in existing file
	// paswword and force pass
	_, stderr, err = b.Run(append([]string{"export", "--just-key", "--pass", "test", "--pass-filepath", filepath.Join(testDir, "test.txt")}, baseArgsTemplate...)...)
	if err != nil {
		return stderr, fmt.Errorf("failed to check flag `--force-pass-filepath`: %w", err)
	}
	if strings.TrimSpace(stderr) != "" {
		return stderr, errors.New("flag `--just-key` is set and log is not empty")
	}
	file, err := os.Open(filepath.Join(testDir, "test.txt"))
	if err != nil {
		return "", fmt.Errorf("failed to open file: %w", err)
	}
	var buf bytes.Buffer
	_, err = buf.ReadFrom(file)
	if err != nil {
		return "", fmt.Errorf("failed to read from file: %w", err)
	}

	if buf.String() != "test" {
		return "", fmt.Errorf("password is not `test`, got: %s", buf.String())
	}
	buf.Reset()
	err = file.Close()
	if err != nil {
		return "", fmt.Errorf("failed to close file: %w", err)
	}

	_, stderr, err = b.Run(append([]string{"export", "--pass", "test", "--pass-filepath", filepath.Join(testDir, "test.txt")}, baseArgsTemplate...)...)
	if err != nil {
		if !strings.Contains(stderr, "file for exporting password exists: use flag --force-pass-filepath to overwrite file") {
			return stderr, fmt.Errorf("failed to check flag `--pass-filepath` with existing file: %w", err)
		}
	}

	_, stderr, err = b.Run(append([]string{"export", "--pass", "testforce", "--pass-filepath", filepath.Join(testDir, "test.txt"), "--force-pass-filepath"}, baseArgsTemplate...)...)
	if err != nil {
		return stderr, fmt.Errorf("failed to check flag `--pass-filepath` and `--force-pass-filepath` together: %w", err)
	}

	file, err = os.Open(filepath.Join(testDir, "test.txt"))
	if err != nil {
		return "", fmt.Errorf("failed to open file: %w", err)
	}
	_, err = buf.ReadFrom(file)
	if err != nil {
		return "", fmt.Errorf("failed to read from file: %w", err)
	}

	if buf.String() != "testforce" {
		return "", fmt.Errorf("password is not `test`, got: %s", buf.String())
	}

	return stderr, nil
}
