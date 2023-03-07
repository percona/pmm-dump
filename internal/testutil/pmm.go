package testutil

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/compose-spec/compose-go/cli"
	"github.com/pkg/errors"
)

var (
	_, b, _, _ = runtime.Caller(0)
	repoPath   = filepath.Join(filepath.Dir(b), "..", "..")
)

const (
	timeout = time.Second * 120
)

type PMM struct {
	version string
	t       *testing.T
}

func ClickHouseURL() string {
	return fmt.Sprintf("http://localhost:%s?database=pmm", os.Getenv("CLICKHOUSE_PORT"))
}

func PMMURL() string {
	return fmt.Sprintf("http://admin:admin@localhost:%s", os.Getenv("PMM_HTTP_PORT"))
}

func SetEnvFromDotEnv(t *testing.T) {
	envs, err := cli.GetEnvFromFile(map[string]string{}, "", []string{filepath.Join(repoPath, ".env.test")})
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range envs {
		t.Setenv(k, v)
	}
}

func NewPMM(t *testing.T) *PMM {
	version := os.Getenv("PMM_VERSION")
	if version == "" {
		version = "latest"
	}
	return &PMM{
		version: version,
		t:       t,
	}
}

func (p *PMM) Deploy() {
	p.t.Setenv("PMM_VERSION", p.version)
	p.t.Log("Starting PMM version", p.version)
	stdout, stderr, err := Exec("../../..", "make", "up-test")
	if err != nil {
		p.t.Fatal(errors.Wrapf(err, "failed to start PMM: stderr: %s, stdout: %s", stderr, stdout))
		return
	}
	if err := getUntilOk(PMMURL()+"/v1/version", timeout); err != nil {
		p.t.Fatal(err, "failed to ping PMM")
		return
	}
	time.Sleep(15 * time.Second)
	stdout, stderr, err = Exec("../../..", "make", "mongo-reg-test")
	if err != nil {
		p.t.Fatal(errors.Wrapf(err, "failed to add mongo: stderr: %s, stdout: %s", stderr, stdout))
		return
	}
}

func (p *PMM) Stop() {
	p.t.Log("Stopping PMM version", p.version)
	stdout, stderr, err := Exec("../../..", "make", "down-test")
	if err != nil {
		p.t.Fatal(errors.Wrapf(err, "failed to stop PMM: stderr: %s, stdout: %s", stderr, stdout))
		return
	}
}

func getUntilOk(url string, timeout time.Duration) error {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	timeoutTimer := time.NewTimer(timeout)
	defer timeoutTimer.Stop()
	for {
		select {
		case <-ticker.C:
			err := func() error {
				resp, err := http.Get(url)
				if err != nil {
					return err
				}
				defer resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					return nil
				}
				return errors.New("not ok")
			}()
			if err == nil {
				return nil
			}
		case <-timeoutTimer.C:
			return errors.New("timeout")
		}
	}
}
