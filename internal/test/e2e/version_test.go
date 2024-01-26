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
	"os"
	"path/filepath"
	"testing"

	"pmm-dump/internal/test/deployment"
	"pmm-dump/internal/test/util"

	"github.com/pkg/errors"
	"golang.org/x/sync/errgroup"
	"gopkg.in/yaml.v2"
)

func TestPMMCompatibility(t *testing.T) {
	ctx := context.Background()

	startTest(t)

	pmmVersions, err := getVersions()
	if err != nil {
		t.Fatal(err)
	}
	if len(pmmVersions) < 2 {
		t.Fatal("not enough versions to test provided in ")
	}

	pmm := deployment.NewPMM(t, "compatibility", "")
	if pmm.UseExistingDeployment() {
		t.Skip("skipping test because existing deployment is used")
	}

	deployments := make(map[string]*deployment.PMM, len(pmmVersions))
	g, gCtx := errgroup.WithContext(ctx)
	for i := range pmmVersions {
		i := i
		g.Go(func() error {
			pmm := deployment.NewPMM(t, "compatibility-"+pmmVersions[i], "")
			pmm.SetVersion(pmmVersions[i])
			if err := pmm.Deploy(gCtx); err != nil {
				return err
			}
			deployments[pmmVersions[i]] = pmm
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		t.Fatal(err)
	}

	var b util.Binary
	dumpPath := ""
	for _, version := range pmmVersions {
		pmm := deployments[version]
		if dumpPath != "" {
			t.Log("Importing data from", dumpPath)
			stdout, stderr, err := b.Run("import", "-d", dumpPath, "--pmm-url", pmm.PMMURL())
			if err != nil {
				t.Fatal("failed to import", err, stdout, stderr)
			}
		}

		testDir := t.TempDir()
		dumpPath = filepath.Join(testDir, "dump.tar.gz")
		t.Log("Exporting data to", dumpPath)
		stdout, stderr, err := b.Run("export", "-d", dumpPath, "--pmm-url", pmm.PMMURL(), "--ignore-load")
		if err != nil {
			t.Fatal("failed to export", err, stdout, stderr)
		}

		t.Log("Importing data from", dumpPath)
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
	data, err := os.ReadFile(filepath.Join(util.RepoPath, "internal", "test", "e2e", "data", "versions.yaml"))
	if err != nil {
		return nil, errors.Wrap(err, "failed to read versions.yaml")
	}
	cfg := versionsConfig{}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal versions.yaml")
	}
	return cfg.Versions, nil
}
