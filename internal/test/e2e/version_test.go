package e2e

import (
	"os"
	"path/filepath"
	"testing"

	"pmm-dump/internal/test/util"

	"gopkg.in/yaml.v2"
)

func TestPMMCompatibility(t *testing.T) {
	t.Helper()

	pmmVersions := getVersions(t)
	if len(pmmVersions) < 2 {
		t.Fatal("not enough versions to test provided in ")
	}

	var b util.Binary
	for i := 0; i < len(pmmVersions); i++ {
		oldPMM := util.NewPMM(t, "compatibility", "")
		if oldPMM.UseExistingDeployment() {
			t.Skip("skipping test because existing deployment is used")
		}
		oldPMM.SetVersion(pmmVersions[i])
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
		if i == len(pmmVersions)-1 {
			break
		}
		newPMM := util.NewPMM(t, "compatibility", "")
		newPMM.SetVersion(pmmVersions[i+1])
		newPMM.Deploy()

		t.Log("Importing data from", filepath.Join(testDir, "dump.tar.gz"))
		stdout, stderr, err = b.Run("import", "-d", filepath.Join(testDir, "dump.tar.gz"), "--pmm-url", newPMM.PMMURL())
		if err != nil {
			t.Fatal("failed to import", err, stdout, stderr)
		}
		newPMM.Stop()
	}
}

func getVersions(t *testing.T) []string {
	t.Helper()

	type versionsConfig struct {
		Versions []string `yaml:"versions"`
	}
	data, err := os.ReadFile(filepath.Join(util.RepoPath, "internal", "test", "e2e", "data", "versions.yaml"))
	if err != nil {
		t.Fatal("failed to read test config", err)
	}
	cfg := versionsConfig{}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatal("failed to unmarshal test config", err)
	}
	return cfg.Versions
}
