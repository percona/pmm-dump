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

package deployment

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
	"github.com/pkg/errors"
)

const (
	PerconaLabel = "com.percona.pmm-dump.test"
)

type PMM struct {
	testName string

	dontCleanup bool

	// These fields will be populated during container creation
	httpPort           string
	httpsPort          string
	clickhousePort     string
	clickhouseHTTPPort string
	mongoPort          string

	pmmServerContainerID string
	mongoContainerID     string

	t                     *testing.T
	useExistingDeployment bool
	pmmURL                string

	pmmVersion     string
	pmmServerImage string
	pmmClientImage string
	mongoImage     string
	mongoTag       string
}

func newPMM(t *testing.T, testName, configFile string) *PMM {
	t.Helper()
	if configFile == "" {
		configFile = ".env.test"
	}
	envs, err := GetEnvFromFile(configFile)
	if err != nil {
		t.Fatal(err)
	}

	useExistingDeployment := false
	if v, ok := envs[envVarUseExistingPMM]; ok && (v == "true" || v == "1") {
		useExistingDeployment = true
	}
	if !useExistingDeployment {
		envs[envVarPMMURL] = setDefaultEnv(envVarPMMURL)
	}

	requiredEnvs := []string{
		envVarPMMVersion,
		envVarPMMServerImage,
		envVarPMMClientImage,
		envVarMongoImage,
		envVarMongoTag,
	}
	for _, re := range requiredEnvs {
		if _, ok := envs[re]; !ok {
			envs[re] = setDefaultEnv(re)
		}
	}

	pmm := &PMM{
		testName: testName,
		t:        t,

		pmmVersion:     envs[envVarPMMVersion],
		pmmServerImage: envs[envVarPMMServerImage],
		pmmClientImage: envs[envVarPMMClientImage],
		mongoImage:     envs[envVarMongoImage],
		mongoTag:       envs[envVarMongoTag],

		pmmURL: envs[envVarPMMURL],
	}

	return pmm
}

func (p *PMM) PMMURL() string {
	u, err := url.Parse(p.pmmURL)
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
	u.Host += ":" + p.httpPort

	return u.String()
}

func (p *PMM) UseExistingDeployment() bool {
	return p.useExistingDeployment
}

func (p *PMM) ClickhouseURL() string {
	u, err := url.Parse(p.PMMURL())
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
	u.Host += ":" + p.clickhousePort

	return u.String()
}

func (p *PMM) MongoURL() string {
	u, err := url.Parse(p.PMMURL())
	if err != nil {
		p.t.Fatal(err)
	}
	u.Scheme = "mongodb"
	if strings.Contains(u.Host, ":") {
		u.Host = u.Host[0:strings.Index(u.Host, ":")]
	}
	u.Host += ":" + p.mongoPort

	return u.String()
}

func (pmm *PMM) Deploy(ctx context.Context) error {
	if err := pmm.deploy(ctx); err != nil {
		return errors.Wrap(err, "failed to deploy")
	}
	if !pmm.dontCleanup {
		pmm.t.Cleanup(func() {
			pmm.Destroy(context.Background())
		})
	}
	return nil
}

func (pmm *PMM) DontCleanup() {
	pmm.dontCleanup = true
}

func (pmm *PMM) SetVersion(version string) {
	pmm.pmmVersion = version
}

func (pmm *PMM) Log(args ...any) {
	pmm.t.Helper()

	args = append([]any{fmt.Sprintf("[pmm] %s:", pmm.testName)}, args...)
	pmm.t.Log(args...)
}

var checkImagesMu sync.Mutex

func (pmm *PMM) deploy(ctx context.Context) error {
	dockerCli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return errors.Wrap(err, "failed to create docker client")
	}
	defer dockerCli.Close() //nolint:errcheck

	pmm.Log("Destroying existing deployment")
	if err := destroy(ctx, filters.NewArgs(filters.Arg("label", PerconaLabel+"="+pmm.testName))); err != nil {
		return errors.Wrap(err, "failed to destroy existing deployment")
	}

	pmm.Log("Checking images")
	checkImagesMu.Lock()
	for _, image := range []string{pmm.ServerImage(), pmm.ClientImage(), pmm.MongoImage()} {
		exists, err := ImageExists(ctx, image)
		if err != nil {
			return errors.Wrap(err, "failed to check image")
		}
		if exists {
			pmm.Log("Image", image, "exists")
			continue
		}
		pmm.Log("Pulling image", image)
		if err := PullImage(ctx, image); err != nil {
			return errors.Wrap(err, "failed to pull image")
		}
	}
	checkImagesMu.Unlock()

	pmm.Log("Creating network")
	netresp, err := dockerCli.NetworkCreate(ctx, pmm.NetworkName(), types.NetworkCreate{
		Labels: map[string]string{
			PerconaLabel: pmm.testName,
		},
	})
	if err != nil {
		return err
	}
	if len(netresp.Warning) > 0 {
		return errors.New("got warnings during network creation:" + netresp.Warning)
	}

	pmm.Log("Creating PMM server")
	if err := pmm.CreatePMMServer(ctx, dockerCli, netresp.ID); err != nil {
		return errors.Wrap(err, "failed to create pmm server")
	}

	pmm.Log("Creating PMM client")
	if err := pmm.CreatePMMClient(ctx, dockerCli, netresp.ID); err != nil {
		return errors.Wrap(err, "failed to create pmm client")
	}

	pmm.Log("Creating mongo")
	if err := pmm.CreateMongo(ctx, dockerCli, netresp.ID); err != nil {
		return errors.Wrap(err, "failed to create mongo container")
	}

	pmm.Log("Waiting for mongo to be ready")
	err = doUntilSuccess(30*time.Second, func() error {
		return pmm.PingMongo(ctx)
	})
	if err != nil {
		return errors.Wrap(err, "failed to connect to mongo")
	}

	pmm.Log("Adding mongo to PMM")
	if err := pmm.Exec(ctx, pmm.ClientContainerName(),
		"pmm-admin", "add", "mongodb",
		"--username", "admin",
		"--password", "admin",
		"mongo",
		pmm.MongoContainerName()+":27017",
	); err != nil {
		return errors.Wrap(err, "failed to exec")
	}

	if err := doUntilSuccess(30*time.Second, func() error {
		return pmm.PingClickhouse(ctx)
	}); err != nil {
		return errors.Wrap(err, "failed to ping clickhouse")
	}

	return nil
}

func (pmm *PMM) Restart(ctx context.Context) error {
	dockerCli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return errors.Wrap(err, "failed to create docker client")
	}
	defer dockerCli.Close() //nolint:errcheck

	if err := dockerCli.ContainerRestart(ctx, pmm.pmmServerContainerID, container.StopOptions{
		Timeout: nil, // 10 seconds
	}); err != nil {
		return errors.Wrap(err, "failed to restart pmm server")
	}
	if err := pmm.SetServerPublishedPorts(ctx, dockerCli); err != nil {
		return errors.Wrap(err, "failed to set server published ports")
	}

	if err := getUntilOk(pmm.PMMURL()+"/v1/version", time.Second*30); err != nil {
		return errors.Wrap(err, "failed to ping PMM")
	}
	return nil
}

func (pmm *PMM) Destroy(ctx context.Context) {
	pmm.Log("Destroying deployment")
	if err := destroy(ctx, filters.NewArgs(filters.Arg("label", PerconaLabel+"="+pmm.testName))); err != nil {
		pmm.Log(err)
		pmm.t.FailNow()
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

func DestroyAll(ctx context.Context) error {
	return destroy(ctx, filters.NewArgs(filters.Arg("label", PerconaLabel)))
}

func destroy(ctx context.Context, filters filters.Args) error {
	dockerCli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return errors.Wrap(err, "failed to create docker client")
	}
	defer dockerCli.Close() //nolint:errcheck

	containers, err := dockerCli.ContainerList(ctx, types.ContainerListOptions{
		All:     true,
		Filters: filters,
	})
	if err != nil {
		return errors.Wrap(err, "failed to list containers")
	}

	zero := 0
	for _, c := range containers {
		if err := dockerCli.ContainerStop(ctx, c.ID, container.StopOptions{Timeout: &zero}); err != nil {
			return errors.Wrap(err, "failed to stop container")
		}
		if err := dockerCli.ContainerRemove(ctx, c.ID, types.ContainerRemoveOptions{
			Force:         true,
			RemoveVolumes: true,
		}); err != nil {
			return errors.Wrap(err, "failed to remove container")
		}
	}

	volumes, err := dockerCli.VolumeList(ctx, volume.ListOptions{
		Filters: filters,
	})
	if err != nil {
		return errors.Wrap(err, "failed to list volumes")
	}
	for _, vol := range volumes.Volumes {
		if err := dockerCli.VolumeRemove(ctx, vol.Name, true); err != nil {
			return errors.Wrap(err, "failed to remove volume")
		}
	}

	networks, err := dockerCli.NetworkList(ctx, types.NetworkListOptions{
		Filters: filters,
	})
	if err != nil {
		return errors.Wrap(err, "failed to list networks")
	}

	for _, n := range networks {
		if err := dockerCli.NetworkRemove(ctx, n.ID); err != nil {
			return errors.Wrap(err, "failed to remove network")
		}
	}

	return nil
}
