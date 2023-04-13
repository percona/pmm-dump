package util

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/compose-spec/compose-go/cli"
	"github.com/pkg/errors"
)

const (
	timeout = time.Second * 120
)

type PMM struct {
	version        string
	t              *testing.T
	dotEnvFilename string
	envMap         map[string]string
	name           string
}

func (pmm *PMM) PMMURL() string {
	return fmt.Sprintf("http://admin:admin@localhost:%s", pmm.envMap["PMM_HTTP_PORT"])
}
func (pmm *PMM) ClickhouseURL() string {
	return fmt.Sprintf("http://localhost:%s?database=pmm", pmm.envMap["CLICKHOUSE_PORT"])
}

func GetEnvFromDotEnv(filepath string) (map[string]string, error) {
	envs, err := cli.GetEnvFromFile(map[string]string{}, "", []string{filepath})
	if err != nil {
		return nil, err
	}
	return envs, nil
}

func NewPMM(t *testing.T, name string, version string, dotEnvFilename string) *PMM {
	envs, err := GetEnvFromDotEnv(filepath.Join(testDir, dotEnvFilename))
	if err != nil {
		t.Fatal(err)
	}
	if version == "" {
		version = envs["PMM_VERSION"]
		if version == "" {
			version = "latest"
		}
	}
	if dotEnvFilename == "" {
		dotEnvFilename = ".env.test"
	}
	if name == "" {
		name = "test"
	}
	envs["COMPOSE_PROJECT_NAME"] = name
	agentConfigFilepath := filepath.Join(testDir, "pmm", fmt.Sprintf("agent_%s.yaml", name))
	d := filepath.Dir(agentConfigFilepath)
	if err := os.MkdirAll(d, os.ModePerm); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(agentConfigFilepath)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	if err := os.Chmod(agentConfigFilepath, 0666); err != nil {
		t.Fatal(err)
	}
	if _, ok := envs["PMM_AGENT_CONFIG_FILE"]; !ok {
		envs["PMM_AGENT_CONFIG_FILE"] = agentConfigFilepath
	}
	return &PMM{
		version:        version,
		t:              t,
		dotEnvFilename: dotEnvFilename,
		envMap:         envs,
		name:           name,
	}
}

func (p *PMM) SetEnv() {
	for k, v := range p.envMap {
		p.t.Setenv(k, v)
	}
}

func (p *PMM) Deploy() {
	p.SetEnv()
	p.t.Setenv("PMM_VERSION", p.version)
	p.t.Log("Starting PMM deployment", p.name, "version:", p.version)
	stdout, stderr, err := Exec(repoPath, "make", "up")
	if err != nil {
		p.t.Fatal(errors.Wrapf(err, "failed to start PMM: stderr: %s, stdout: %s", stderr, stdout))
		return
	}
	if err := getUntilOk(p.PMMURL()+"/v1/version", timeout); err != nil {
		p.t.Fatal(err, "failed to ping PMM")
		return
	}
	time.Sleep(15 * time.Second)
	stdout, stderr, err = Exec(repoPath, "make", "mongo-reg")
	if err != nil {
		p.t.Fatal(errors.Wrapf(err, "failed to add mongo: stderr: %s, stdout: %s", stderr, stdout))
		return
	}
	for i := 0; i < 10; i++ {
		stdout, stderr, err = Exec(repoPath, "make", "mongo-insert")
		if err != nil {
			p.t.Fatal(errors.Wrapf(err, "failed to add mongo: stderr: %s, stdout: %s", stderr, stdout))
			return
		}
	}
}

func (p *PMM) Stop() {
	p.SetEnv()
	p.t.Log("Stopping PMM deployment", p.name, "version:", p.version)
	stdout, stderr, err := Exec(repoPath, "make", "down")
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
