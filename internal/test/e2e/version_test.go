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
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v2"

	"pmm-dump/internal/test/deployment"
	"pmm-dump/internal/test/util"
)

func TestPMMCompatibility(t *testing.T) {
	ctx := context.Background()

	pmmVersions, err := getVersions()
	if err != nil {
		t.Fatal(err)
	}
	if len(pmmVersions) < 2 {
		t.Skip("Not enough versions to test provided in versions.yaml. skip")
	}

	c := deployment.NewController(t)

	var b util.Binary
	dumpPath := ""
	for _, version := range pmmVersions {
		pmm := c.NewPMM("compatibility-"+version, "")
		if pmm.UseExistingDeployment() {
			t.Skip("skipping test because existing deployment is used")
		}
		pmm.SetVersion(version)
		if err := pmm.Deploy(ctx); err != nil {
			t.Fatal(err)
		}
		if dumpPath != "" {
			pmm.Log("Importing data from", dumpPath)
			stdout, stderr, err := b.Run("import", "-d", dumpPath, "--pmm-url", pmm.PMMURL())
			if err != nil {
				t.Fatal("failed to import", err, stdout, stderr)
			}
		}

		testDir := t.TempDir()
		dumpPath = filepath.Join(testDir, "dump.tar.gz")
		pmm.Log("Exporting data to", dumpPath)
		stdout, stderr, err := b.Run("export", "-d", dumpPath, "--pmm-url", pmm.PMMURL(), "--ignore-load")
		if err != nil {
			t.Fatal("failed to export", err, stdout, stderr)
		}

		pmm.Log("Importing data from", dumpPath)
		stdout, stderr, err = b.Run("import", "-d", dumpPath, "--pmm-url", pmm.PMMURL())
		if err != nil {
			t.Fatal("failed to import", err, stdout, stderr)
		}
		pmm.Destroy(ctx)
	}
}

func getVersions() ([]string, error) {
	type versionsConfig struct {
		Versions []string `yaml:"versions"`
	}
	data, err := os.ReadFile(filepath.Join(util.RepoPath, "internal", "test", "e2e", "testdata", "versions.yaml"))
	if err != nil {
		return nil, fmt.Errorf("failed to read versions.yaml: %w", err)
	}
	cfg := versionsConfig{}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal versions.yaml: %w", err)
	}
	return cfg.Versions, nil
}
