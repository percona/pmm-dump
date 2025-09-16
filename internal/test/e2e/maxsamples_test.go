//go:build e2e

// Copyright 2023 Percona LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package e2e

import (
	"archive/tar"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"pmm-dump/internal/test/deployment"
	"pmm-dump/internal/test/util"
	pkgUtil "pmm-dump/pkg/util"
)

const (
	getTimeout     = 60
	rowsToSubtract = 10
)

func TestMaxSamples(t *testing.T) {
	type metadata struct {
		RowsCount        int `json:"RowsCount"`
		BlocksCount      int `json:"BlocksCount"`
		MinTimestamp     int `json:"MinTimestamp"`
		MaxTimestamp     int `json:"MaxTimestamp"`
		MinDedupInterval int `json:"MinDedupInterval"`
	}

	type parts struct {
		Small []string `json:"Small"`
		Big   []string `json:"Big"`
	}

	ctx := t.Context()
	c := deployment.NewController(t)

	pmm := c.NewPMM("max-samples", ".env.test")
	if pmm.UseExistingDeployment() {
		t.Skip("skipping test because existing deployment is used")
	}

	b := new(util.Binary)
	tmpDir := util.CreateTestDir(t, "max-samples-test")
	dumpPath := filepath.Join(tmpDir, "dump.tar.gz")

	err := pmm.Deploy(ctx)
	if err != nil {
		t.Fatal("Failde to deploy pmm", err)
	}

	var stdout, stderr string

	// Test will pass if we successfully export dump and at least 1 chunk was split.
	// For this we want to get the 	number of rows currently situated in the VM.
	// To do this we need to go into the container and check each metadata.json of each part.

	// The Metadata.json file has the following fields:
	// RowsCount - the number of raw samples stored in the part
	// BlocksCount - the number of blocks stored in the part
	// MinTimestamp and MaxTimestamp - minimum and maximum timestamps across raw samples stored in the part
	// MinDedupInterval - the deduplication interval applied to the given part.
	// More information: https://docs.victoriametrics.com/#storage

	// metadata.json looks like this
	// {"RowsCount":763025,"BlocksCount":9391,"MinTimestamp":1743756839550,"MaxTimestamp":1743757562317,"MinDedupInterval":0}

	// To get the correct path for parts we use parts.json which contains all the names for big and small parts
	// parts.json looks like this
	// {"Small":["1833119B552B7720","1833119B552B7735","1833119B552B7753","1833119B552B7771","1833119B552B7788","1833119B552B779C",
	// "1833119B552B779D","1833119B552B779E","1833119B552B779F","1833119B552B77A0","1833119B552B77A1"],"Big":[]}

	// So first we read the parts.json file to get paths for parts. Then we iterate over all parts to get the RowsCount value
	// from metadata.json , adding these values together gives us total number of rows when we export.
	// Then we subtract 10 from this number and set this value as the limit for the query.
	// When we start the export we get the error "cannot select more than -search.maxSamplesPerQuery= '*' samples",
	// because the number of rows is greater than the query limit.
	// When this specific error is triggered pmm-dump will try to split chunk by
	// making 2 different queries with time range split into two.

	// This is done recursively until time range is less than 1 millisecond.
	// If this happens, export will fail, and so will test.
	// But if the export is successful, the test will pass.

	month := fmt.Sprintf("%02d", time.Now().Month())
	year := strconv.Itoa(time.Now().Year())
	part := parts{}
	pmm.Log("Getting parts names from /srv/victoriametrics/data/data/small/" + year + "_" + month + "/parts.json")
	reader, err := pmm.FileReader(ctx, pmm.ServerContainerName(), "/srv/victoriametrics/data/data/small/"+year+"_"+month+"/parts.json")
	if err != nil {
		t.Fatal("failed to get file from container", err)
	}
	defer reader.Close() //nolint:errcheck

	tr := tar.NewReader(reader)
	if _, err := tr.Next(); err != nil {
		t.Fatal("failed to read from json", err)
	}
	decoder := json.NewDecoder(tr)
	err = decoder.Decode(&part)
	if err != nil {
		t.Fatal("failed to decode json", err)
	}

	metaD := metadata{}
	var rows int

	pmm.Log("Getting number of rows in each small part")
	for _, n := range part.Small {
		meta, err := pmm.FileReader(ctx, pmm.ServerContainerName(), "/srv/victoriametrics/data/data/small/"+year+"_"+month+"/"+n+"/metadata.json")
		if err != nil {
			t.Fatal("failed to get file from container", err)
		}
		ta := tar.NewReader(meta)
		if _, err := ta.Next(); err != nil {
			t.Fatal("failed to read from reader", err)
		}
		metaDecoder := json.NewDecoder(ta)
		err = metaDecoder.Decode(&metaD)
		if err != nil {
			t.Fatal("failed to decode json", err)
		}
		rows += metaD.RowsCount
		err = meta.Close()
		if err != nil {
			t.Fatal("failed to close meta", err)
		}
	}

	pmm.Log("Number of rows in metadata: " + strconv.Itoa(rows))
	rows -= rowsToSubtract
	pmm.Log("Subtracting 10 from number of rows and updating Victoria Metrics with: search.maxSamplesPerQuery = " + strconv.Itoa(rows))
	from := "1500000000"
	to := strconv.Itoa(rows)

	err = pmm.Exec(ctx, pmm.ServerContainerName(), "bash", "-c", "sed -i -e 's/--search.maxSamplesPerQuery="+from+"/--search.maxSamplesPerQuery="+to+"/g' /etc/supervisord.d/victoriametrics.ini")
	if err != nil {
		t.Fatal("failed to change victoriametrics settings", err)
	}
	err = pmm.Exec(ctx, pmm.ServerContainerName(), "bash", "-c", "supervisorctl reread")
	if err != nil {
		t.Fatal("failed to reread supervisorctl", err)
	}
	err = pmm.Exec(ctx, pmm.ServerContainerName(), "bash", "-c", "supervisorctl update victoriametrics")
	if err != nil {
		t.Fatal("failed to update supervisorctl for victoriametrics", err)
	}

	pmm.Log("Waiting for VictoriaMetrics to restart")
	tCtx, cancel := context.WithTimeout(ctx, getTimeout)
	defer cancel()

	pmmConfig, err := pkgUtil.GetPMMConfig(pmm.PMMURL(), "", "", nil)
	if err != nil {
		t.Fatal("failed to get config for pmm", err)
	}

	if err := util.RetryOnError(tCtx, func() error {
		resp, err := http.Get(pmmConfig.VictoriaMetricsURL + "/ready") //nolint:noctx
		if err != nil {
			return err
		}
		defer resp.Body.Close() //nolint:errcheck
		if resp.StatusCode == http.StatusOK {
			return nil
		}
		return errors.New("not ok")
	}); err != nil && !errors.Is(err, io.EOF) {
		t.Fatal("failed to ping victoriametrics: ", err)
	}

	pmm.Log("Exporting dump to check split")
	stdout, stderr, err = b.Run(
		"export",
		"-v",
		"--dump-path", dumpPath,
		"--pmm-url", pmm.PMMURL(),
		"--click-house-url", pmm.ClickhouseURL(),
		"--dump-core",
		"--dump-qan",
		"--ignore-load")
	if err != nil {
		t.Fatal("failed to export", err, stdout, stderr)
	}

	if !strings.Contains(stderr, "VM chunk was split into several parts") {
		t.Fatal("No chunk was split", err, stdout, stderr)
	}

	pmm.Log("Succesfuly splited big chunks into smaller one")
}
