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
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
	"github.com/hashicorp/go-version"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
	"github.com/valyala/fasthttp"

	"pmm-dump/internal/test/util"
	grafanaClient "pmm-dump/pkg/grafana/client"
	pkgUtil "pmm-dump/pkg/util"
)

const (
	PerconaLabel = "com.percona.pmm-dump.test"
)

type PMM struct {
	t        *testing.T
	testName string

	useExistingDeployment bool
	pmmURL                string

	dontCleanup bool

	pmmVersion     string
	pmmServerImage string
	pmmClientImage string
	mongoImage     string
	mongoTag       string

	// These fields will be populated during container creation
	httpPort             *string
	httpsPort            *string
	clickhousePort       *string
	clickhouseHTTPPort   *string
	mongoPort            *string
	pmmServerContainerID *string
	mongoContainerID     *string

	deployed   *bool
	deployedMu *sync.Mutex
}

func (pmm *PMM) setPorts(httpPort, httpsPort, clickhousePort, clickhouseHTTPPort string) {
	*pmm.httpPort = httpPort
	*pmm.httpsPort = httpsPort
	*pmm.clickhousePort = clickhousePort
	*pmm.clickhouseHTTPPort = clickhouseHTTPPort
}

func (pmm *PMM) setMongoPort(mongoPort string) {
	*pmm.mongoPort = mongoPort
}

func (pmm *PMM) setPMMServerContainerID(id string) {
	*pmm.pmmServerContainerID = id
}

func (pmm *PMM) setMongoContainerID(id string) {
	*pmm.mongoContainerID = id
}

func (pmm *PMM) Copy(t *testing.T) *PMM {
	t.Helper()

	newPMM := *pmm
	newPMM.t = t
	return &newPMM
}

func newPMM(deplName, configFile string) *PMM {
	if _, loaded := registeredDeployments.LoadOrStore(deplName, true); loaded {
		panic(deplName + " pmm deployment name is possibly created in multiple tests. Please use different name or use ReusablePMM method")
	}
	if configFile == "" {
		configFile = ".env.test"
	}
	envs, err := GetEnvFromFile(configFile)
	if err != nil {
		panic(err)
	}

	useExistingDeployment := false
	if v, ok := envs[envVarUseExistingPMM]; ok && (v == "true" || v == "1") {
		useExistingDeployment = true
	}

	pmm := &PMM{
		testName: deplName,

		pmmVersion:     envs[envVarPMMVersion],
		pmmServerImage: envs[envVarPMMServerImage],
		pmmClientImage: envs[envVarPMMClientImage],
		mongoImage:     envs[envVarMongoImage],
		mongoTag:       envs[envVarMongoTag],

		useExistingDeployment: useExistingDeployment,

		pmmURL: envs[envVarPMMURL],

		httpPort:             ptr(""),
		httpsPort:            ptr(""),
		clickhousePort:       ptr(""),
		clickhouseHTTPPort:   ptr(""),
		mongoPort:            ptr(""),
		pmmServerContainerID: ptr(""),
		mongoContainerID:     ptr(""),

		deployed:   ptr(false),
		deployedMu: new(sync.Mutex),
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
	u.Host += ":" + *p.httpPort

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

	u.User = pkgUtil.GetClickhouseUser(p.GetFullVersionString())

	u.Scheme = "clickhouse"
	u.Path = "pmm"
	if strings.Contains(u.Host, ":") {
		u.Host = u.Host[0:strings.Index(u.Host, ":")]
	}
	u.Host += ":" + *p.clickhousePort

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
	u.Host += ":" + *p.mongoPort

	return u.String()
}

func (pmm *PMM) Deploy(ctx context.Context) error {
	pmm.deployedMu.Lock()
	if pmm.deployed != nil && *pmm.deployed {
		pmm.deployedMu.Unlock()
		return nil
	}
	if err := pmm.deploy(ctx); err != nil {
		return errors.Wrap(err, "failed to deploy")
	}
	*pmm.deployed = true
	pmm.deployedMu.Unlock()
	if !pmm.dontCleanup {
		pmm.t.Cleanup(func() { //nolint:contextcheck
			pmm.Destroy(context.Background())
		})
	}
	return nil
}

func (pmm *PMM) DontCleanup() {
	pmm.dontCleanup = true
}

// Returns major version.
func (pmm *PMM) GetVersion() *version.Version {
	v1, err := version.NewVersion(pmm.pmmVersion)
	if err != nil {
		panic(fmt.Sprintf("cannot get version: %v", err))
	} else {
		return v1
	}
}

// Return the full version asking it to the PMM server itself
func (pmm *PMM) GetFullVersionString() string {

	type versionResp struct {
		Version string `json:"version"`
		Server  struct {
			Version string `json:"version"`
		}
	}

	client, err := pmm.NewClient()

	pmmVersionURL := pmm.PMMURL() + "/v1/version"

	if err != nil {
		pmm.t.Fatal(err)
	}

	response_code, response, err := client.Get(pmmVersionURL)

	if err != nil {
		pmm.t.Fatal(err)
	}

	if response_code != fasthttp.StatusOK {
		pmm.t.Fatal(fmt.Errorf("non-ok status: %d", response_code))
	}

	var versionInfo versionResp
	if err := json.Unmarshal(response, &versionInfo); err != nil {
		pmm.t.Fatal(err)
	}

	return versionInfo.Server.Version
}

func (pmm *PMM) SetVersion(version string) {
	pmm.pmmVersion = version
}

func (pmm *PMM) Log(args ...any) {
	pmm.t.Helper()

	args = append([]any{fmt.Sprintf("%s [%s]:", time.Now().UTC().Format(time.RFC3339), pmm.testName)}, args...)
	pmm.t.Log(args...)
}

func (pmm *PMM) Logf(f string, args ...any) {
	pmm.t.Helper()

	f = fmt.Sprintf("%s [%s]: ", time.Now().UTC().Format(time.RFC3339), pmm.testName) + f
	pmm.t.Logf(f, args...)
}

var checkImagesMu sync.Mutex

func (pmm *PMM) deploy(ctx context.Context) error {
	dockerCli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return errors.Wrap(err, "failed to create docker client")
	}
	defer dockerCli.Close() //nolint:errcheck

	pmm.Log("Destroying existing deployment")
	if err := destroy(ctx, filters.NewArgs(filters.Arg("label", PerconaLabel+"="+pmm.testName)), pmm); err != nil {
		return errors.Wrap(err, "failed to destroy existing deployment")
	}

	pmm.Log("Checking images")
	checkImagesMu.Lock()
	for _, image := range []string{pmm.ServerImage(), pmm.ClientImage(), pmm.MongoImage()} {
		exists, err := ImageExists(ctx, image)
		if err != nil {
			checkImagesMu.Unlock()
			return errors.Wrap(err, "failed to check image")
		}
		if exists {
			pmm.Log("Image", image, "exists")
			continue
		}
		pmm.Log("Pulling image", image)
		if err := PullImage(ctx, image); err != nil {
			checkImagesMu.Unlock()
			return errors.Wrap(err, "failed to pull image")
		}
	}
	checkImagesMu.Unlock()

	pmm.Log("Creating network")
	netresp, err := dockerCli.NetworkCreate(ctx, pmm.NetworkName(), network.CreateOptions{
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

	tCtx, cancel := context.WithTimeout(ctx, getTimeout)
	defer cancel()
	if err := util.RetryOnError(tCtx, func() error {
		_, err := dockerCli.NetworkInspect(ctx, netresp.ID, network.InspectOptions{})
		if err != nil {
			return errors.Wrapf(err, "failed to inspect network %s", netresp.ID)
		}
		return nil
	}); err != nil {
		return err
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

	tCtx, cancel = context.WithTimeout(ctx, execTimeout)
	defer cancel()
	err = util.RetryOnError(tCtx, func() error {
		return pmm.PingMongo(ctx)
	})
	if err != nil {
		return errors.Wrap(err, "failed to connect to mongo")
	}

	pmm.Log("Adding mongo to PMM")
	tCtx, cancel = context.WithTimeout(ctx, execTimeout)
	defer cancel()
	if err := util.RetryOnError(tCtx, func() error {
		return pmm.Exec(ctx, pmm.ClientContainerName(),
			"pmm-admin", "add", "mongodb",
			"--username", "admin",
			"--password", "admin",
			"mongo",
			pmm.MongoContainerName()+":27017")
	}); err != nil {
		return errors.Wrap(err, "failed to add mongo to PMM")
	}

	pmm.Log("Ping clickhouse with driver")
	tCtx, cancel = context.WithTimeout(ctx, execTimeout)
	defer cancel()
	if err := util.RetryOnError(tCtx, func() error {
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

	if err := dockerCli.ContainerRestart(ctx, *pmm.pmmServerContainerID, container.StopOptions{
		Timeout: nil, // 10 seconds
	}); err != nil {
		return errors.Wrap(err, "failed to restart pmm server")
	}
	if err := pmm.SetServerPublishedPorts(ctx, dockerCli); err != nil {
		return errors.Wrap(err, "failed to set server published ports")
	}

	tCtx, cancel := context.WithTimeout(ctx, getTimeout)
	defer cancel()
	if pkgUtil.CheckIsVer2(pmm.GetVersion()) {
		if err := getUntilOk(tCtx, pmm.PMMURL()+"/v1/version"); err != nil && !errors.Is(err, io.EOF) {
			return errors.Wrap(err, "failed to ping PMM")
		}
	} else {
		if err := getUntilOk(tCtx, pmm.PMMURL()+"/v1/server/version"); err != nil && !errors.Is(err, io.EOF) {
			return errors.Wrap(err, "failed to ping PMM")
		}
	}
	return nil
}

func (pmm *PMM) Destroy(ctx context.Context) {
	pmm.Log("Destroying deployment")
	if err := destroy(ctx, filters.NewArgs(filters.Arg("label", PerconaLabel+"="+pmm.testName)), pmm); err != nil {
		pmm.Log(err)
		pmm.t.FailNow()
	}
}

func getUntilOk(ctx context.Context, url string) error {
	return util.RetryOnError(ctx, func() error {
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

func DestroyAll(ctx context.Context) error {
	return destroy(ctx, filters.NewArgs(filters.Arg("label", PerconaLabel)), new(defaultLogger))
}

type logger interface {
	Log(args ...any)
}

type defaultLogger struct{}

func (l *defaultLogger) Log(args ...any) {
	log.Logger.Println(args...)
}

func destroy(ctx context.Context, filters filters.Args, log logger) error {
	dockerCli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return errors.Wrap(err, "failed to create docker client")
	}
	defer dockerCli.Close() //nolint:errcheck

	containers, err := dockerCli.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filters,
	})
	if err != nil {
		return errors.Wrap(err, "failed to list containers")
	}

	zero := 0
	for _, c := range containers {
		if err := dockerCli.ContainerStop(ctx, c.ID, container.StopOptions{Timeout: &zero}); err != nil {
			log.Log(err, "failed to stop container")
		}
		if err := dockerCli.ContainerRemove(ctx, c.ID, container.RemoveOptions{
			Force:         true,
			RemoveVolumes: true,
		}); err != nil {
			log.Log(err, "failed to remove container")
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
			log.Log(err, "failed to remove volume")
		}
	}

	networks, err := dockerCli.NetworkList(ctx, network.ListOptions{
		Filters: filters,
	})
	if err != nil {
		return errors.Wrap(err, "failed to list networks")
	}

	for _, n := range networks {
		if err := dockerCli.NetworkRemove(ctx, n.ID); err != nil {
			log.Log(err, "failed to remove network")
		}
	}

	return nil
}

func (p *PMM) NewClient() (*grafanaClient.Client, error) {
	httpC := &fasthttp.Client{
		MaxConnsPerHost:           2, //nolint:mnd
		MaxIdleConnDuration:       time.Minute,
		MaxIdemponentCallAttempts: 5, //nolint:mnd
		ReadTimeout:               time.Minute,
		WriteTimeout:              time.Minute,
		MaxConnWaitTimeout:        time.Second * 30, //nolint:mnd
		TLSConfig: &tls.Config{
			InsecureSkipVerify: true, //nolint:gosec
		},
	}
	authParams := grafanaClient.AuthParams{
		User:     "admin",
		Password: "admin",
	}
	grafanaClient, err := grafanaClient.NewClient(httpC, authParams)
	if err != nil {
		return nil, errors.Wrap(err, "new client")
	}
	return grafanaClient, nil
}

func ptr[T any](v T) *T {
	return &v
}
