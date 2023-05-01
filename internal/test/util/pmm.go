package util

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/compose-spec/compose-go/cli"
	"github.com/pkg/errors"
)

const (
	timeout = time.Second * 120
)

const (
	envVarPMMURL             = "PMM_URL"
	envVarPMMHTTPPort        = "PMM_HTTP_PORT"
	envVarPMMVersion         = "PMM_VERSION"
	envVarClickhousePort     = "CLICKHOUSE_PORT"
	envVarUseExistingPMM     = "USE_EXISTING_PMM"
	envVarPMMAgentConfigPath = "PMM_AGENT_CONFIG_FILE"
)

const (
	defaultPMMURL = "http://localhost"
)

type PMM struct {
	t                     *testing.T
	dotEnvFilename        string
	envMap                map[string]string
	name                  string
	useExistingDeployment bool
}

func (pmm *PMM) UseExistingDeployment() bool {
	return pmm.useExistingDeployment
}

func (pmm *PMM) PMMURL() string {
	m := pmm.envMap

	u, err := url.Parse(m[envVarPMMURL])
	if err != nil {
		pmm.t.Fatal(err)
	}
	if u.User.Username() == "" {
		u.User = url.UserPassword("admin", "admin")
	}
	if u.Scheme == "" {
		u.Scheme = "http"
	}
	if strings.Contains(u.Host, ":") {
		u.Host = u.Host[0:strings.Index(u.Host, ":")]
	}
	u.Host += ":" + m[envVarPMMHTTPPort]

	a := u.String()
	return a
}
func (pmm *PMM) ClickhouseURL() string {
	m := pmm.envMap

	u, err := url.Parse(m[envVarPMMURL])
	if err != nil {
		pmm.t.Fatal(err)
	}
	if u.Scheme == "" {
		u.Scheme = "http"
	}
	u.RawQuery = "database=pmm"
	if strings.Contains(u.Host, ":") {
		u.Host = u.Host[0:strings.Index(u.Host, ":")]
	}
	u.Host += ":" + m[envVarClickhousePort]

	return u.String()
}

func getEnvFromDotEnv(filepath string) (map[string]string, error) {
	envs, err := cli.GetEnvFromFile(map[string]string{}, "", []string{filepath})
	if err != nil {
		return nil, err
	}
	if v, ok := envs[envVarPMMURL]; !ok && v == "" {
		envs[envVarPMMURL] = defaultPMMURL
	}
	for _, env := range []string{envVarPMMHTTPPort, envVarClickhousePort} {
		if v, ok := envs[env]; !ok && v == "" {
			return nil, errors.Errorf("env %s is not set in %s", env, filepath)
		}
	}
	return envs, nil
}

func NewPMM(t *testing.T, name string, dotEnvFilename string) *PMM {
	if dotEnvFilename == "" {
		dotEnvFilename = ".env.test"
	}
	envs, err := getEnvFromDotEnv(filepath.Join(testDir, dotEnvFilename))
	if err != nil {
		t.Fatal(err)
	}
	useExistingDeployment := false
	if v, ok := envs[envVarUseExistingPMM]; ok && (v == "true" || v == "1") {
		useExistingDeployment = true
	}
	if !useExistingDeployment {
		envs[envVarPMMURL] = defaultPMMURL
	}
	version := envs[envVarPMMVersion]
	if version == "" {
		version = "latest"
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
	if _, ok := envs[envVarPMMAgentConfigPath]; !ok {
		envs[envVarPMMAgentConfigPath] = agentConfigFilepath
	}
	return &PMM{
		t:                     t,
		dotEnvFilename:        dotEnvFilename,
		envMap:                envs,
		name:                  name,
		useExistingDeployment: useExistingDeployment,
	}
}

func (p *PMM) SetVersion(version string) {
	p.envMap[envVarPMMVersion] = version
}

func (p *PMM) SetEnv() {
	for k, v := range p.envMap {
		p.t.Setenv(k, v)
	}
}

func (p *PMM) Deploy() {
	if p.useExistingDeployment {
		p.t.Log("Using existing PMM deployment")
		return
	}
	p.SetEnv()
	p.t.Log("Starting PMM deployment", p.name, "version:", p.envMap[envVarPMMVersion])
	stdout, stderr, err := Exec(RepoPath, "make", "up")
	if err != nil {
		p.t.Fatal(errors.Wrapf(err, "failed to start PMM: stderr: %s, stdout: %s", stderr, stdout))
		return
	}
	if err := getUntilOk(p.PMMURL()+"/v1/version", timeout); err != nil {
		p.t.Fatal(err, "failed to ping PMM")
		return
	}
	time.Sleep(15 * time.Second)
	stdout, stderr, err = Exec(RepoPath, "make", "mongo-reg")
	if err != nil {
		p.t.Fatal(errors.Wrapf(err, "failed to add mongo: stderr: %s, stdout: %s", stderr, stdout))
		return
	}
	for i := 0; i < 10; i++ {
		stdout, stderr, err = Exec(RepoPath, "make", "mongo-insert")
		if err != nil {
			p.t.Fatal(errors.Wrapf(err, "failed to add mongo: stderr: %s, stdout: %s", stderr, stdout))
			return
		}
	}
}

func (p *PMM) Stop() {
	if p.useExistingDeployment {
		return
	}
	p.SetEnv()
	p.t.Log("Stopping PMM deployment", p.name, "version:", p.envMap[envVarPMMVersion])
	stdout, stderr, err := Exec(RepoPath, "make", "down")
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
