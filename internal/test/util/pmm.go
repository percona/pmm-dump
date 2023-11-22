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

package util

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/compose-spec/compose-go/dotenv"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"github.com/pkg/errors"
)

const (
	envVarPMMURL                 = "PMM_URL"
	envVarPMMHTTPPort            = "PMM_HTTP_PORT"
	envVarPMMVersion             = "PMM_VERSION"
	envVarClickhousePort         = "CLICKHOUSE_PORT"
	envVarUseExistingPMM         = "USE_EXISTING_PMM"
	envVarPMMAgentConfigPath     = "PMM_AGENT_CONFIG_FILE"
	envVarContainerNamePMMServer = "PMM_SERVER_NAME"
	envVarContainerNamePMMClient = "PMM_CLIENT_NAME"
	envVarContainerNameMongo     = "PMM_MONGO_NAME"

	envVarComposeProjectName = "COMPOSE_PROJECT_NAME"
)

const (
	defaultPMMURL = "http://localhost"

	composeProjectPrefix = "pmm-dump-test-"
)

type PMM struct {
	t                     *testing.T
	dotEnvFilename        string
	envMap                map[string]string
	name                  string
	useExistingDeployment bool
	timeout               time.Duration
}

func (p *PMM) UseExistingDeployment() bool {
	return p.useExistingDeployment
}

func (p *PMM) PMMURL() string {
	m := p.envMap

	u, err := url.Parse(m[envVarPMMURL])
	if err != nil {
		p.t.Fatal(err)
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

func (p *PMM) ClickhouseURL() string {
	m := p.envMap

	u, err := url.Parse(m[envVarPMMURL])
	if err != nil {
		p.t.Fatal(err)
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
	envs, err := dotenv.GetEnvFromFile(make(map[string]string), "", []string{filepath})
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
	t.Helper()

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
	if name == "" {
		name = "test"
	}
	envs[envVarComposeProjectName] = composeProjectPrefix + name
	agentConfigFilepath := filepath.Join(testDir, "pmm", fmt.Sprintf("agent_%s.yaml", name))
	d := filepath.Dir(agentConfigFilepath)
	if err := os.MkdirAll(d, os.ModePerm); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(agentConfigFilepath) //nolint:gosec
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	if err := os.Chmod(agentConfigFilepath, 0o666); err != nil { //nolint:gosec
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
		timeout:               time.Minute * 8,
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

func (p *PMM) Deploy(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	if p.useExistingDeployment {
		p.t.Log("Using existing PMM deployment")
		return
	}
	p.SetEnv()
	p.t.Log("Starting PMM deployment", p.name, "version:", p.envMap[envVarPMMVersion])
	if err := p.removeExistingTestDeployments(ctx); err != nil {
		p.t.Fatal(errors.Wrap(err, "remove existing test deployments"))
	}
	stdout, stderr, err := Exec(ctx, RepoPath, "make", "up")
	if err != nil {
		p.t.Fatal(errors.Wrapf(err, "failed to start PMM: stderr: %s, stdout: %s", stderr, stdout))
		return
	}
	if err := getUntilOk(p.PMMURL()+"/v1/version", p.timeout); err != nil {
		p.t.Fatal(err, "failed to ping PMM")
		return
	}
	time.Sleep(15 * time.Second)
	stdout, stderr, err = Exec(ctx, RepoPath, "make", "mongo-reg")
	if err != nil {
		p.t.Fatal(errors.Wrapf(err, "failed to add mongo: stderr: %s, stdout: %s", stderr, stdout))
		return
	}
	for i := 0; i < 10; i++ {
		stdout, stderr, err = Exec(ctx, RepoPath, "make", "mongo-insert")
		if err != nil {
			p.t.Fatal(errors.Wrapf(err, "failed to add mongo: stderr: %s, stdout: %s", stderr, stdout))
			return
		}
	}
}

func (p *PMM) removeExistingTestDeployments(ctx context.Context) error {
	t := p.t

	dockerCli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return err
	}
	defer dockerCli.Close() //nolint:errcheck

	containers, err := dockerCli.ContainerList(ctx, types.ContainerListOptions{
		All: true,
	})
	if err != nil {
		return err
	}

	getContainer := func(containers []types.Container, containerName string) (types.Container, bool) {
		for _, container := range containers {
			for _, name := range container.Names {
				if containerName == strings.TrimPrefix(name, "/") {
					return container, true
				}
			}
		}
		return types.Container{}, false
	}

	containerNameEnvs := []string{
		envVarContainerNamePMMServer,
		envVarContainerNamePMMClient,
		envVarContainerNameMongo,
	}
	for _, envName := range containerNameEnvs {
		containerName := p.envMap[envName]
		existingContainer, ok := getContainer(containers, containerName)
		if !ok {
			continue
		}

		projectName := existingContainer.Labels["com.docker.compose.project"]
		if !strings.HasPrefix(projectName, composeProjectPrefix) {
			t.Fatal("container", containerName, "is already running and doesn't belong to any test PMM deployment.",
				"Please stop it manually or edit", envName, "env variable in", p.dotEnvFilename, "file")
		}

		t.Setenv(envVarComposeProjectName, projectName)
		t.Log("PMM test deployment", projectName, "is already running. Trying to stop it...")
		stdout, stderr, err := Exec(ctx, RepoPath, "make", "down")
		if err != nil {
			return errors.Wrapf(err, "failed to stop PMM deployment %s: stderr: %s, stdout: %s", projectName, stderr, stdout)
		}
		t.Log("PMM test deployment", projectName, "is stopped.")
		p.SetEnv()

		return p.removeExistingTestDeployments(ctx)
	}
	return nil
}

func (p *PMM) Stop() {
	if p.useExistingDeployment {
		return
	}
	timeout := 2 * time.Minute
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	p.SetEnv()
	p.t.Log("Stopping PMM deployment", p.name, "version:", p.envMap[envVarPMMVersion])
	stdout, stderr, err := Exec(ctx, RepoPath, "make", "down")
	if err != nil {
		p.t.Fatal(errors.Wrapf(err, "failed to stop PMM: stderr: %s, stdout: %s", stderr, stdout))
		return
	}
}

func (p *PMM) Restart() {
	if p.useExistingDeployment {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), p.timeout)
	defer cancel()
	p.SetEnv()
	p.t.Log("Restarting PMM deployment", p.name, "version:", p.envMap[envVarPMMVersion])

	stdout, stderr, err := Exec(ctx, "", "docker", "compose", "restart")
	if err != nil {
		p.t.Fatal("failed to change nginx settings", err, stdout, stderr)
	}

	if err := getUntilOk(p.PMMURL()+"/v1/version", p.timeout); err != nil {
		p.t.Fatal(err, "failed to ping PMM")
		return
	}
}

func getUntilOk(url string, timeout time.Duration) error {
	return doUntilSuccess(timeout, func() error {
		resp, err := http.Get(url) //nolint:gosec,noctx
		if err != nil {
			return err
		}
		defer resp.Body.Close() //nolint:errcheck
		if resp.StatusCode == http.StatusOK {
			return nil
		}
		return errors.New("not ok")
	})
}

func doUntilSuccess(timeout time.Duration, f func() error) error {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	timeoutTimer := time.NewTimer(timeout)
	defer timeoutTimer.Stop()
	var err error
	for {
		select {
		case <-ticker.C:
			err = f()
			if err == nil {
				return nil
			}
		case <-timeoutTimer.C:
			return errors.Wrap(err, "timeout")
		}
	}
}
