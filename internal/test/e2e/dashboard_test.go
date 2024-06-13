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
	"crypto/tls"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/valyala/fasthttp"

	"pmm-dump/internal/test/deployment"
	"pmm-dump/internal/test/util"
	"pmm-dump/pkg/grafana/client"
)

func TestDashboard(t *testing.T) {
	ctx := context.Background()

	c := deployment.NewController(t)
	pmm := c.NewPMM("content-limit", ".env.test")
	if err := pmm.Deploy(ctx); err != nil {
		t.Fatal(err)
	}

	importCustomDashboards(t, pmm.PMMURL())
	names := getAllDashbaordNames(t, pmm.PMMURL())

	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			testDir := t.TempDir()

			var b util.Binary

			dashboardDumpPath := filepath.Join(testDir, "dump.tar.gz")
			args := []string{"-d", dashboardDumpPath, "--pmm-url", pmm.PMMURL(), "--pmm-user", "admin", "--pmm-pass", "admin", "--dashboard", name}

			t.Log("Exporting data with `--dashboard` flag to", dashboardDumpPath)
			stdout, stderr, err := b.Run(append([]string{"export", "--ignore-load"}, args...)...)
			if err != nil {
				t.Fatal("failed to export", err, stdout, stderr)
			}
		})
	}
}

func importCustomDashboards(t *testing.T, pmmURL string) {
	grafanaClient := newClient(t)

	entries, err := os.ReadDir(filepath.Join(util.RepoPath, "internal", "test", "e2e", "testdata", "dashboards"))
	if err != nil {
		t.Fatal(err)
	}

	for _, v := range entries {
		data, err := os.ReadFile(filepath.Join(util.RepoPath, "internal", "test", "e2e", "testdata", "dashboards", v.Name()))
		if err != nil {
			t.Fatal(err)
		}
		dashboard := make(map[string]any)
		if err := json.Unmarshal(data, &dashboard); err != nil {
			t.Fatal(err)
		}
		importReq := map[string]any{
			"dashboard": dashboard,
			"folderId":  0,
			"inputs":    []any{},
		}
		status, data, err := grafanaClient.PostJSON(pmmURL+"/graph/api/dashboards/import", importReq)
		if err != nil {
			t.Fatal(err)
		}
		if status != fasthttp.StatusOK {
			t.Fatalf("non-ok status: %d: %s", status, string(data))
		}
	}
}

func newClient(t *testing.T) *client.Client {
	httpC := &fasthttp.Client{
		MaxConnsPerHost:           2,
		MaxIdleConnDuration:       time.Minute,
		MaxIdemponentCallAttempts: 5,
		ReadTimeout:               time.Minute,
		WriteTimeout:              time.Minute,
		MaxConnWaitTimeout:        time.Second * 30,
		TLSConfig: &tls.Config{
			InsecureSkipVerify: true, //nolint:gosec
		},
	}
	authParams := client.AuthParams{
		User:     "admin",
		Password: "admin",
	}
	grafanaClient, err := client.NewClient(httpC, authParams)
	if err != nil {
		t.Fatal(err)
	}
	return grafanaClient
}

func getAllDashbaordNames(t *testing.T, pmmURL string) []string {
	t.Helper()

	grafanaClient := newClient(t)

	q := fasthttp.AcquireArgs()
	defer fasthttp.ReleaseArgs(q)

	q.Add("query", "")
	status, data, err := grafanaClient.Get(pmmURL + "/graph/api/search?" + q.String())
	if err != nil {
		t.Fatal(err)
	}
	if status != fasthttp.StatusOK {
		t.Fatalf("non-ok status: %d", status)
	}

	type dashboardResp struct {
		Title string `json:"title"`
	}
	var s []dashboardResp
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(s))
	for _, v := range s {
		names = append(names, v.Title)
	}
	return names
}
