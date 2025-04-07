package e2e

import (
	"archive/tar"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	//"os"

	"strings"
	"testing"

	"pmm-dump/internal/test/deployment"
	"pmm-dump/internal/test/util"
)

// /srv/victoriametrics/data/indexdb/
// https://docs.victoriametrics.com/#storage
// https://github.com/VictoriaMetrics/VictoriaMetrics/issues/2691#issuecomment-1178442457

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

	ctx := context.Background()
	c := deployment.NewController(t)

	pmm := c.NewPMM("max-samples", ".env.test")
	if pmm.UseExistingDeployment() {
		t.Skip("skipping test because existing deployment is used")
	}

	b := new(util.Binary)
	tmpDir := util.CreateTestDir(t, "max-samples-test")
	dumpPath := filepath.Join(tmpDir, "dump.tar.gz")

	pmm.Deploy(ctx)

	var stdout, stderr string
	var err error

	time.Sleep(time.Second * 20)

	mounth := fmt.Sprintf("%02d", time.Now().Month())
	year := fmt.Sprint(time.Now().Year())
	part := parts{}
	reader, err := pmm.DockerCopy(ctx, pmm.ServerContainerName(), "/srv/victoriametrics/data/data/small/"+year+"_"+mounth+"/parts.json")
	if err != nil {
		t.Fatal("failed to get file from container", err)
	}
	defer reader.Close()

	tr := tar.NewReader(reader)
	if _, err := tr.Next(); err != nil {
		panic(err)
	}
	decoder := json.NewDecoder(tr)
	err = decoder.Decode(&part)
	if err != nil {
		t.Fatal("failed to decode json", err)
	}

	metaD := metadata{}
	var rows int
	for _, n := range part.Small {
		meta, err := pmm.DockerCopy(ctx, pmm.ServerContainerName(), "/srv/victoriametrics/data/data/small/"+year+"_"+mounth+"/"+n+"/metadata.json")
		if err != nil {
			t.Fatal("failed to get file from container", err)
		}
		defer meta.Close()
		ta := tar.NewReader(meta)
		if _, err := ta.Next(); err != nil {
			panic(err)
		}
		metaDecoder := json.NewDecoder(ta)
		err = metaDecoder.Decode(&metaD)
		if err != nil {
			t.Fatal("failed to decode json", err)
		}
		rows += metaD.RowsCount
	}
	fmt.Printf("Number of rows in metadata: %v ", rows)
	rows = rows - 10
	fmt.Printf("Number of rows after conversion: %v ", rows)
	from := "1500000000"
	to := fmt.Sprint(rows)

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

	stdout, stderr, err = b.Run(
		"export",
		"-v",
		"--dump-path", dumpPath,
		"--pmm-url", pmm.PMMURL(),
		"--dump-core",
		"--dump-qan",
		"--ignore-load",
	)
	if err != nil {
		t.Fatal("failed to export", err, stdout, stderr)
	}

	if !strings.Contains(stderr, "Chunk was split into several parts") {
		t.Fatal("No chunk was split", err, stdout, stderr)
	} else {
		pmm.Log(stdout)
		pmm.Log(stderr)
		pmm.Log("Number of rows after test:", rows)
		pmm.Log("Succesfuly splited big chunks into smaller one")
	}
}
