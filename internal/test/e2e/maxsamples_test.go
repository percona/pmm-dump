package e2e

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"pmm-dump/internal/test/deployment"
	"pmm-dump/internal/test/util"
)

// /srv/victoriametrics/data/indexdb/
// https://docs.victoriametrics.com/#storage
// https://github.com/VictoriaMetrics/VictoriaMetrics/issues/2691#issuecomment-1178442457

func TestMaxSamples(t *testing.T) {
	ctx := context.Background()
	c := deployment.NewController(t)

	pmm := c.NewPMM("max-samples", ".env.test")
	if pmm.UseExistingDeployment() {
		t.Skip("skipping test because existing deployment is used")
	}

	//pmm.SetEnv()
	//pmm.Stop() // TODO: it stops very fast need to check if it is ok

	b := new(util.Binary)
	tmpDir := util.CreateTestDir(t, "max-samples-test")
	fmt.Println("haha")
	dumpPath := filepath.Join(tmpDir, "dump.tar.gz")

	pmm.Deploy(ctx)
	//defer pmm.Stop()

	//`stdout, stderr, err := util.Exec(ctx, "", "docker", "compose", "exec", "pmm-server", "bash", "-c", "sed -i -e 's/--search.maxSamplesPerQuery=1500000000/--search.maxSamplesPerQuery=170000/g' /etc/supervisord.d/victoriametrics.ini")
	from := "1500000000"
	to := "6000"
	//from = to
	//to = "1500000000"
	//from := "1000"
	//to := "170000"

	var stdout, stderr string
	var err error

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

	//stdout, stderr, err = util.Exec(ctx, "", "docker", "compose", "exec", "pmm-server", "bash", "-c", "sed -i -e 's/--search.maxSamplesPerQuery="+from+"/--search.maxSamplesPerQuery="+to+"/g' /etc/supervisord.d/victoriametrics.ini")
	//if err != nil {
	//	t.Fatal("failed to change victoriametrics settings", err, stdout, stderr)
	//}
	// stdout, stderr, err = util.Exec(ctx, "", "docker", "compose", "exec", "pmm-server", "bash", "-c", "supervisorctl reread")
	// if err != nil {
	// 	t.Fatal("supervisorctl reread", err, stdout, stderr)
	// }
	// stdout, stderr, err = util.Exec(ctx, "", "docker", "compose", "exec", "pmm-server", "bash", "-c", "supervisorctl update victoriametrics")
	// if err != nil {
	// 	t.Fatal("supervisorctl update victoriametrics", err, stdout, stderr)
	// }
	time.Sleep(time.Second * 5)
	time.Sleep(time.Second * 1)

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
	fmt.Println(stdout)
	fmt.Println(stderr)
}
