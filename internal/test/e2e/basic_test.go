package e2e

import (
	"path/filepath"
	"testing"

	"pmm-dump/internal/testutil"
)

func TestExportImport(t *testing.T) {
	testutil.SetEnvFromDotEnv(t)

	b := new(testutil.Binary)
	pmm := testutil.NewPMM(t)
	pmm.Stop()
	pmm.Deploy()
	testDir := t.TempDir()

	args := []string{"-d", filepath.Join(testDir, "dump.tar.gz"), "--pmm-url", testutil.PMMURL(), "--dump-qan", "--click-house-url", testutil.ClickHouseURL()}

	t.Log("Exporting data to", filepath.Join(testDir, "dump.tar.gz"))
	stdout, stderr, err := b.Run(append([]string{"export", "--ignore-load"}, args...)...)
	if err != nil {
		t.Fatal("failed to export", err, stdout, stderr)
	}

	pmm.Stop()
	pmm.Deploy()

	t.Log("Importing data from", filepath.Join(testDir, "dump.tar.gz"))
	stdout, stderr, err = b.Run(append([]string{"import"}, args...)...)
	if err != nil {
		t.Fatal("failed to import", err, stdout, stderr)
	}
	pmm.Stop()
}

func TestShowMeta(t *testing.T) {
	testutil.SetEnvFromDotEnv(t)

	b := new(testutil.Binary)
	stdout, stderr, err := b.Run("show-meta", "-d", "data/onlymeta.tar.gz")
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
