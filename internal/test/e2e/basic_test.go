//go:build e2e

package e2e

import (
	"context"
	"path/filepath"
	"testing"

	"pmm-dump/internal/test/util"
)

func TestExportImport(t *testing.T) {
	ctx := context.Background()
	pmm := util.NewPMM(t, "export-import", ".env.test")
	pmm.Deploy(ctx)
	defer pmm.Stop()

	newPMM := util.NewPMM(t, "export-import-2", ".env2.test")
	newPMM.Deploy(ctx)
	defer newPMM.Stop()

	b := new(util.Binary)
	testDir := t.TempDir()

	args := []string{"-d", filepath.Join(testDir, "dump.tar.gz"), "--pmm-url", pmm.PMMURL(), "--dump-qan", "--click-house-url", pmm.ClickhouseURL()}

	t.Log("Exporting data to", filepath.Join(testDir, "dump.tar.gz"))
	stdout, stderr, err := b.Run(append([]string{"export", "--ignore-load"}, args...)...)
	if err != nil {
		t.Fatal("failed to export", err, stdout, stderr)
	}

	args = []string{"-d", filepath.Join(testDir, "dump.tar.gz"), "--pmm-url", newPMM.PMMURL(), "--dump-qan", "--click-house-url", newPMM.ClickhouseURL()}
	t.Log("Importing data from", filepath.Join(testDir, "dump.tar.gz"))
	stdout, stderr, err = b.Run(append([]string{"import"}, args...)...)
	if err != nil {
		t.Fatal("failed to import", err, stdout, stderr)
	}
}

func TestShowMeta(t *testing.T) {
	b := new(util.Binary)
	stdout, stderr, err := b.Run("show-meta", "-d", filepath.Join(util.RepoPath, "internal", "test", "e2e", "data", "onlymeta.tar.gz"))
	if err != nil {
		t.Fatal(err, stdout, stderr)
	}
	want := `Build: 8f26789
PMM Version: 2.34.0-20.2301131343.a7f5d22.el7
Max Chunk Size: 617.2 kB (602.7 KiB)
Arguments: export --verbose=true --dump-path=dump.tar.gz --pmm-url=http://localhost:8282 --dump-core=true --vm-native-data=true
`
	if stdout != want {
		t.Fatalf("want %s, got %s", want, stdout)
	}
}
