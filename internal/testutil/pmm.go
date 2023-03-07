package testutil

import (
	"net/http"
	"testing"
	"time"

	"github.com/pkg/errors"
)

const (
	PMMURL = "http://admin:admin@localhost:8282"
)

type PMM struct {
	version string
	t       *testing.T
}

func NewPMM(t *testing.T, version string) *PMM {
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
	stdout, stderr, err := Exec("../../..", "make", "up")
	if err != nil {
		p.t.Fatal(errors.Wrapf(err, "failed to start PMM: stderr: %s, stdout: %s", stderr, stdout))
		return
	}
	if err := getUntilOk(PMMURL+"/v1/version", 30*time.Second); err != nil {
		p.t.Fatal(err, "failed to ping PMM")
		return
	}
}

func (p *PMM) Stop() {
	p.t.Log("Stopping PMM version", p.version)
	stdout, stderr, err := Exec("../../..", "make", "down")
	if err != nil {
		p.t.Fatal(errors.Wrapf(err, "failed to stop PMM: stderr: %s, stdout: %s", stderr, stdout))
		return
	}
}

func getUntilOk(url string, timeout time.Duration) error {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	timeoutTimer := time.NewTimer(30 * time.Second)
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
