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
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
	"github.com/pkg/errors"

	"pmm-dump/internal/test/util"
	pkgUtil "pmm-dump/pkg/util"
	"pmm-dump/pkg/victoriametrics"
)

const (
	defaultHTTPPortv2         = "80"
	defaultHTTPSPortv2        = "443"
	defaultHTTPPortv3         = "8080"
	defaultHTTPSPortv3        = "8443"
	defaultClickhousePort     = "9000"
	defaultClickhouseHTTPPort = "8123"

	defaultMongoPort = "27017"

	volumeSuffix = "-data"

	pmmClientMemoryLimit = 128 * 1024 * 1024
	pmmServerMemoryLimit = 2048 * 1024 * 1024
	mongoMemoryLimit     = 1024 * 1024 * 1024
)

const (
	execTimeout    = time.Second * 180
	getTimeout     = time.Second * 120
	inspectTimeout = time.Second * 20
)

func (pmm *PMM) CreatePMMServer(ctx context.Context, dockerCli *client.Client, networkID string) error {
	vol, err := dockerCli.VolumeCreate(ctx, volume.CreateOptions{
		Name: pmm.ServerContainerName() + volumeSuffix,
		Labels: map[string]string{
			PerconaLabel: pmm.testName,
		},
	})
	if err != nil {
		return errors.Wrap(err, "failed to create volume")
	}

	mounts := []mount.Mount{
		{
			Type:   mount.TypeVolume,
			Source: vol.Name,
			Target: "/srv",
		},
	}

	var ports []string
	var env []string
	if pkgUtil.CheckVer(pmm.GetVersion(), "< 3.0.0") {
		ports = []string{defaultHTTPPortv2, defaultHTTPSPortv2, defaultClickhousePort, defaultClickhouseHTTPPort}
	} else {
		ports = []string{defaultHTTPPortv3, defaultHTTPSPortv3, defaultClickhousePort, defaultClickhouseHTTPPort}
	}
	id, err := pmm.createContainer(ctx, dockerCli, pmm.ServerContainerName(), pmm.ServerImage(), ports, env, mounts, networkID, nil, pmmServerMemoryLimit)
	if err != nil {
		return errors.Wrap(err, "failed to create container")
	}

	pmm.setPMMServerContainerID(id)

	if err := pmm.SetServerPublishedPorts(ctx, dockerCli); err != nil {
		return errors.Wrap(err, "failed to set server published ports")
	}

	tCtx, cancel := context.WithTimeout(ctx, getTimeout)
	defer cancel()

	pmm.Log("Ping VictoriaMetrics")
	pmmConfig, err := pkgUtil.GetPMMConfig(pmm.PMMURL(), "", "")
	if err != nil {
		return errors.Wrap(err, "failed to get PMM config")
	}
	if err := getUntilOk(tCtx, pmmConfig.VictoriaMetricsURL+"/ready"); err != nil && !errors.Is(err, io.EOF) {
		return errors.Wrap(err, "failed to ping VM")
	}
	pmm.Log("VictoriaMetrics is ready")

	pmm.Log("Ping Clickhouse inside container before restart")
	if err := util.RetryOnError(tCtx, func() error {
		return pmm.Exec(ctx, pmm.ServerContainerName(), "curl", "-f", "http://127.0.0.1:8123/ping")
	}); err != nil {
		return errors.Wrap(err, "failed to ping clickhouse")
	}

	if err := pmm.Exec(ctx, pmm.ServerContainerName(), "sed", "-i", "s#<!-- <listen_host>0.0.0.0</listen_host> -->#<listen_host>0.0.0.0</listen_host>#g", "/etc/clickhouse-server/config.xml"); err != nil {
		return errors.Wrap(err, "failed to update clickhouse config")
	}

	pmm.Log("Restarting Clickhouse after config change")
	if err := util.RetryOnError(tCtx, func() error {
		return pmm.Exec(ctx, pmm.ServerContainerName(), "supervisorctl", "restart", "clickhouse")
	}); err != nil {
		return errors.Wrap(err, "failed to restart clickhouse")
	}

	pmm.Log("Ping Clickhouse inside container after restart")
	if err := util.RetryOnError(tCtx, func() error {
		return pmm.Exec(ctx, pmm.ServerContainerName(), "curl", "-f", "http://127.0.0.1:8123/ping")
	}); err != nil {
		return errors.Wrap(err, "failed to ping clickhouse")
	}

	pmm.Log("Ping Clickhouse with driver")
	tCtx, cancel = context.WithTimeout(ctx, getTimeout)
	defer cancel()
	if err := util.RetryOnError(tCtx, func() error {
		return pmm.PingClickhouse(ctx)
	}); err != nil {
		return errors.Wrap(err, "failed to ping clickhouse")
	}

	gc, err := pmm.NewClient()
	if err != nil {
		return errors.Wrap(err, "new client")
	}

	pmmConfig, err = pkgUtil.GetPMMConfig(pmm.PMMURL(), "", "")
	if err != nil {
		return errors.Wrap(err, "get pmm config")
	}
	tCtx, cancel = context.WithTimeout(ctx, execTimeout)
	defer cancel()
	if err := util.RetryOnError(tCtx, func() error {
		return victoriametrics.ExportTestRequest(gc, pmmConfig.VictoriaMetricsURL)
	}); err != nil {
		return errors.Wrap(err, "failed to check victoriametrics")
	}

	return nil
}

func (pmm *PMM) SetServerPublishedPorts(ctx context.Context, dockerCli *client.Client) error {
	var httpPort, httpsPort, defaultHTTPPort, defaultHTTPSPort, clickhousePort, clickhouseHTTPPort string
	if pkgUtil.CheckVer(pmm.GetVersion(), "< 3.0.0") {
		defaultHTTPPort = defaultHTTPPortv2
		defaultHTTPSPort = defaultHTTPSPortv2
	} else {
		defaultHTTPPort = defaultHTTPPortv3
		defaultHTTPSPort = defaultHTTPSPortv3
	}
	tCtx, cancel := context.WithTimeout(ctx, inspectTimeout)
	defer cancel()
	if err := util.RetryOnError(tCtx, func() error {
		container, err := dockerCli.ContainerInspect(ctx, *pmm.pmmServerContainerID)
		if err != nil {
			return errors.Wrap(err, "failed to inspect container")
		}
		httpPort, err = getPublishedPort(container, defaultHTTPPort)
		if err != nil {
			return errors.Wrap(err, "failed to get published http port")
		}
		httpsPort, err = getPublishedPort(container, defaultHTTPSPort)
		if err != nil {
			return errors.Wrap(err, "failed to get published https port")
		}
		clickhousePort, err = getPublishedPort(container, defaultClickhousePort)
		if err != nil {
			return errors.Wrap(err, "failed to get published clickhouse port")
		}
		clickhouseHTTPPort, err = getPublishedPort(container, defaultClickhouseHTTPPort)
		if err != nil {
			return errors.Wrap(err, "failed to get published clickhouse http port")
		}
		return nil
	}); err != nil {
		return err
	}
	pmm.setPorts(httpPort, httpsPort, clickhousePort, clickhouseHTTPPort)
	return nil
}

func getPublishedPort(container container.InspectResponse, port string) (string, error) {
	portMap := container.NetworkSettings.Ports
	natPort, err := nat.NewPort("tcp", port)
	if err != nil {
		return "", err
	}
	publishedPorts, ok := portMap[natPort]
	if !ok || len(publishedPorts) == 0 {
		return "", errors.New("port " + port + " is not published")
	}
	return publishedPorts[0].HostPort, nil
}

func (pmm *PMM) CreatePMMClient(ctx context.Context, dockerCli *client.Client, networkID string) error {
	var port string
	if pkgUtil.CheckVer(pmm.GetVersion(), "< 3.0.0") {
		port = "443"
	} else {
		port = "8443"
	}
	envs := []string{
		"PMM_AGENT_CONFIG_FILE=config/pmm-agent.yaml",
		"PMM_AGENT_SERVER_USERNAME=admin",
		"PMM_AGENT_SERVER_PASSWORD=admin",
		"PMM_AGENT_SERVER_ADDRESS=" + pmm.ServerContainerName() + ":" + port,
		"PMM_AGENT_SERVER_INSECURE_TLS=1",
		"PMM_AGENT_SETUP=1",
		"PMM_AGENT_SETUP_FORCE=true",
	}
	vol, err := dockerCli.VolumeCreate(ctx, volume.CreateOptions{
		Name: pmm.ClientContainerName() + volumeSuffix,
		Labels: map[string]string{
			PerconaLabel: pmm.testName,
		},
	})
	if err != nil {
		return errors.Wrap(err, "failed to create volume")
	}
	mounts := []mount.Mount{
		{
			Type:   mount.TypeVolume,
			Source: vol.Name,
			Target: "/srv",
		},
	}
	_, err = pmm.createContainer(ctx, dockerCli, pmm.ClientContainerName(), pmm.ClientImage(), nil, envs, mounts, networkID, nil, pmmClientMemoryLimit)
	if err != nil {
		return errors.Wrap(err, "failed to create container")
	}

	tCtx, cancel := context.WithTimeout(ctx, execTimeout)
	defer cancel()
	if err := util.RetryOnError(tCtx, func() error {
		err = pmm.Exec(ctx, pmm.ClientContainerName(), "pmm-admin", "status")
		if err != nil {
			if strings.Contains(err.Error(), "is not running") {
				time.Sleep(5 * time.Second) //nolint:mnd
				err := dockerCli.ContainerStart(ctx, pmm.ClientContainerName(), container.StartOptions{})
				if err != nil {
					pmm.Log("failed to start container", err)
				}
			}
			return errors.Wrap(err, "failed to exec")
		}
		return nil
	}); err != nil {
		return errors.Wrap(err, "failed to check pmm-admin status")
	}

	return nil
}

func (pmm *PMM) CreateMongo(ctx context.Context, dockerCli *client.Client, networkID string) error {
	const mongoPort = "27017"
	ports := []string{mongoPort}

	envs := []string{
		"MONGO_INITDB_DATABASE=admin",
		"MONGO_INITDB_ROOT_USERNAME=admin",
		"MONGO_INITDB_ROOT_PASSWORD=admin",
	}
	vol, err := dockerCli.VolumeCreate(ctx, volume.CreateOptions{
		Name: pmm.MongoContainerName() + volumeSuffix,
		Labels: map[string]string{
			PerconaLabel: pmm.testName,
		},
	})
	if err != nil {
		return errors.Wrap(err, "failed to create volume")
	}
	mounts := []mount.Mount{
		{
			Type:   mount.TypeVolume,
			Source: vol.Name,
			Target: "/data/db",
		},
		{
			Type:     mount.TypeBind,
			Source:   filepath.Join(util.RepoPath, "setup", "mongo", "init.js"),
			Target:   "/docker-entrypoint-initdb.d/init.js",
			ReadOnly: true,
		},
		{
			Type:   mount.TypeBind,
			Source: filepath.Join(util.RepoPath, "setup", "mongo", "mongod.conf"),
			Target: "/etc/mongod.conf",
		},
	}

	cmd := []string{"--config", "/etc/mongod.conf"}

	id, err := pmm.createContainer(ctx, dockerCli, pmm.MongoContainerName(), pmm.MongoImage(), ports, envs, mounts, networkID, cmd, mongoMemoryLimit)
	if err != nil {
		return errors.Wrap(err, "failed to create container")
	}
	pmm.setMongoContainerID(id)

	if err := pmm.SetMongoPublishedPorts(ctx, dockerCli); err != nil {
		return errors.Wrap(err, "failed to set mongo published ports")
	}
	return nil
}

func (pmm *PMM) SetMongoPublishedPorts(ctx context.Context, dockerCli *client.Client) error {
	var mongoPort string
	tCtx, cancel := context.WithTimeout(ctx, inspectTimeout)
	defer cancel()
	if err := util.RetryOnError(tCtx, func() error {
		container, err := dockerCli.ContainerInspect(ctx, *pmm.mongoContainerID)
		if err != nil {
			return errors.Wrap(err, "failed to inspect container")
		}

		mongoPort, err = getPublishedPort(container, defaultMongoPort)
		if err != nil {
			return errors.Wrap(err, "failed to get published mongo port")
		}
		return nil
	}); err != nil {
		return err
	}
	pmm.setMongoPort(mongoPort)

	return nil
}

func (pmm *PMM) createContainer(ctx context.Context,
	dockerCli *client.Client,
	name,
	image string,
	ports []string,
	env []string,
	mounts []mount.Mount,
	networkid string,
	cmd []string,
	memoryLimit int64,
) (string, error) {
	containerConfig := &container.Config{
		Cmd:   cmd,
		Image: image,
		Labels: map[string]string{
			PerconaLabel: pmm.testName,
		},
		ExposedPorts: make(map[nat.Port]struct{}),
		Env:          env,
		AttachStdout: true,
		AttachStderr: true,
	}

	s := nat.PortMap{}
	for _, port := range ports {
		containerPort, err := nat.NewPort("tcp", port)
		if err != nil {
			return "", err
		}
		containerConfig.ExposedPorts[containerPort] = struct{}{}
		s[containerPort] = []nat.PortBinding{{HostIP: "0.0.0.0"}}
	}

	hostConfig := &container.HostConfig{
		NetworkMode:     container.NetworkMode(pmm.NetworkName()),
		Mounts:          mounts,
		PublishAllPorts: true,
		Resources: container.Resources{
			Memory: memoryLimit,
		},
		PortBindings: s,
	}

	networkConfig := &network.NetworkingConfig{
		EndpointsConfig: map[string]*network.EndpointSettings{
			pmm.NetworkName(): {
				Aliases:   []string{name},
				NetworkID: networkid,
			},
		},
	}

	resp, err := dockerCli.ContainerCreate(ctx, containerConfig, hostConfig, networkConfig, nil, name)
	if err != nil {
		return "", errors.Wrap(err, "failed to create container")
	}

	if err := dockerCli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return "", errors.Wrap(err, "failed to start container")
	}

	return resp.ID, nil
}
