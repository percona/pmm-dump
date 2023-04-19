package e2e

import (
	"path/filepath"
	"testing"

	"pmm-dump/internal/test/util"
)

func TestPMMCompatibility(t *testing.T) {
	var PMMVersions = []string{
		"2.12.0",
		"2.13.0",
		"2.14.0",
		"2.15.0",
		"2.16.0",
		"2.17.0",
		"2.18.0",
		"2.19.0",
		"2.20.0",
		"2.21.0",
		"2.22.0",
		"2.23.0",
		"2.24.0",
		"2.25.0",
		"2.26.0",
		"2.27.0",
		"2.28.0",
		"2.29.0",
		"2.30.0",
		"2.31.0",
		"2.32.0",
		"2.33.0",
		"2.34.0",
		"2.35.0",
	}

	b := new(util.Binary)
	for i := 0; i < len(PMMVersions); i++ {
		oldPMM := util.NewPMM(t, "compatibility", "")
		if oldPMM.UseExistingDeployment() {
			t.Skip("skipping test because existing deployment is used")
		}
		oldPMM.SetVersion(PMMVersions[i])
		oldPMM.Stop()
		oldPMM.Deploy()

		testDir := t.TempDir()
		t.Log("Exporting data to", filepath.Join(testDir, "dump.tar.gz"))
		stdout, stderr, err := b.Run("export", "-d", filepath.Join(testDir, "dump.tar.gz"), "--pmm-url", oldPMM.PMMURL(), "--ignore-load")
		if err != nil {
			t.Fatal("failed to export", err, stdout, stderr)
		}

		t.Log("Importing data from", filepath.Join(testDir, "dump.tar.gz"))
		stdout, stderr, err = b.Run("import", "-d", filepath.Join(testDir, "dump.tar.gz"), "--pmm-url", oldPMM.PMMURL())
		if err != nil {
			t.Fatal("failed to import", err, stdout, stderr)
		}

		oldPMM.Stop()
		if i == len(PMMVersions)-1 {
			break
		}
		newPMM := util.NewPMM(t, "compatibility", "")
		newPMM.SetVersion(PMMVersions[i+1])
		newPMM.Deploy()

		t.Log("Importing data from", filepath.Join(testDir, "dump.tar.gz"))
		stdout, stderr, err = b.Run("import", "-d", filepath.Join(testDir, "dump.tar.gz"), "--pmm-url", newPMM.PMMURL())
		if err != nil {
			t.Fatal("failed to import", err, stdout, stderr)
		}
		newPMM.Stop()
	}
}
