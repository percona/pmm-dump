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
	"errors"
	"fmt"
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
	execTimeout = time.Second * 180
	getTimeout  = time.Second * 120
)

func (pmm *PMM) CreatePMMServer(ctx context.Context, dockerCli *client.Client, networkID string) error {
	vol, err := dockerCli.VolumeCreate(ctx, volume.CreateOptions{
		Name: pmm.ServerContainerName() + volumeSuffix,
		Labels: map[string]string{
			PerconaLabel: pmm.testName,
		},
	})
	if err != nil {
		return fmt.Errorf("failed to create volume: %w", err)
	}

	mounts := []mount.Mount{
		{
			Type:   mount.TypeVolume,
			Source: vol.Name,
			Target: "/srv",
		},
	}

	var ports []string
	if pkgUtil.CheckIsVer2(pmm.GetVersion()) {
		ports = []string{defaultHTTPPortv2, defaultHTTPSPortv2, defaultClickhousePort, defaultClickhouseHTTPPort}
	} else {
		ports = []string{defaultHTTPPortv3, defaultHTTPSPortv3, defaultClickhousePort, defaultClickhouseHTTPPort}
	}
	id, err := pmm.createContainer(ctx, dockerCli, pmm.ServerContainerName(), pmm.ServerImage(), ports, nil, mounts, networkID, nil, pmmServerMemoryLimit)
	if err != nil {
		return fmt.Errorf("failed to create container: %w", err)
	}
	pmm.setPMMServerContainerID(id)

	if err := pmm.SetServerPublishedPorts(ctx, dockerCli); err != nil {
		return fmt.Errorf("failed to set server published ports: %w", err)
	}

	if err := pmm.Exec(ctx, pmm.ServerContainerName(), "sed", "-i", "s#<!-- <listen_host>0.0.0.0</listen_host> -->#<listen_host>0.0.0.0</listen_host>#g", "/etc/clickhouse-server/config.xml"); err != nil {
		return fmt.Errorf("failed to update clickhouse config: %w", err)
	}

	tCtx, cancel := context.WithTimeout(ctx, execTimeout)
	defer cancel()
	if err := util.RetryOnError(tCtx, func() error {
		return pmm.Exec(ctx, pmm.ServerContainerName(), "supervisorctl", "restart", "clickhouse")
	}); err != nil {
		return fmt.Errorf("failed to restart clickhouse: %w", err)
	}

	tCtx, cancel = context.WithTimeout(ctx, getTimeout)
	defer cancel()

	if pkgUtil.CheckIsVer2(pmm.GetVersion()) {
		if err := getUntilOk(tCtx, pmm.PMMURL()+"/v1/version"); err != nil && !errors.Is(err, io.EOF) {
			return fmt.Errorf("failed to ping PMM: %w", err)
		}
	} else {
		if err := getUntilOk(tCtx, pmm.PMMURL()+"/v1/server/version"); err != nil && !errors.Is(err, io.EOF) {
			return fmt.Errorf("failed to ping PMM: %w", err)
		}
	}

	gc, err := pmm.NewClient()
	if err != nil {
		return fmt.Errorf("new client: %w", err)
	}

	pmmConfig, err := pkgUtil.GetPMMConfig(pmm.PMMURL(), "", "")
	if err != nil {
		return fmt.Errorf("get pmm config: %w", err)
	}
	tCtx, cancel = context.WithTimeout(ctx, execTimeout)
	defer cancel()
	if err := util.RetryOnError(tCtx, func() error {
		return victoriametrics.ExportTestRequest(gc, pmmConfig.VictoriaMetricsURL)
	}); err != nil {
		return fmt.Errorf("failed to check victoriametrics: %w", err)
	}

	return nil
}

func (pmm *PMM) SetServerPublishedPorts(ctx context.Context, dockerCli *client.Client) error {
	container, err := dockerCli.ContainerInspect(ctx, *pmm.pmmServerContainerID)
	if err != nil {
		return fmt.Errorf("failed to inspect container: %w", err)
	}

	var httpPort, httpsPort, defaultHTTPPort, defaultHTTPSPort string
	if pkgUtil.CheckIsVer2(pmm.GetVersion()) {
		defaultHTTPPort = defaultHTTPPortv2
		defaultHTTPSPort = defaultHTTPSPortv2
	} else {
		defaultHTTPPort = defaultHTTPPortv3
		defaultHTTPSPort = defaultHTTPSPortv3
	}
	httpPort, err = getPublishedPort(container, defaultHTTPPort)
	if err != nil {
		return fmt.Errorf("failed to get published http port: %w", err)
	}
	httpsPort, err = getPublishedPort(container, defaultHTTPSPort)
	if err != nil {
		return fmt.Errorf("failed to get published https port: %w", err)
	}
	clickhousePort, err := getPublishedPort(container, defaultClickhousePort)
	if err != nil {
		return fmt.Errorf("failed to get published clickhouse port: %w", err)
	}
	clickhouseHTTPPort, err := getPublishedPort(container, defaultClickhouseHTTPPort)
	if err != nil {
		return fmt.Errorf("failed to get published clickhouse http port: %w", err)
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
	if pkgUtil.CheckIsVer2(pmm.GetVersion()) {
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
		return fmt.Errorf("failed to create volume: %w", err)
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
		return fmt.Errorf("failed to create container: %w", err)
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
			return fmt.Errorf("failed to exec: %w", err)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("failed to check pmm-admin status: %w", err)
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
		return fmt.Errorf("failed to create volume: %w", err)
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
		return fmt.Errorf("failed to create container: %w", err)
	}
	pmm.setMongoContainerID(id)

	if err := pmm.SetMongoPublishedPorts(ctx, dockerCli); err != nil {
		return fmt.Errorf("failed to set mongo published ports: %w", err)
	}
	return nil
}

func (pmm *PMM) SetMongoPublishedPorts(ctx context.Context, dockerCli *client.Client) error {
	container, err := dockerCli.ContainerInspect(ctx, *pmm.mongoContainerID)
	if err != nil {
		return fmt.Errorf("failed to inspect container: %w", err)
	}

	mongoPort, err := getPublishedPort(container, defaultMongoPort)
	if err != nil {
		return fmt.Errorf("failed to get published mongo port: %w", err)
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
	hostConfig := &container.HostConfig{
		NetworkMode:     container.NetworkMode(pmm.NetworkName()),
		Mounts:          mounts,
		PublishAllPorts: true,
		Resources: container.Resources{
			Memory: memoryLimit,
		},
	}

	for _, port := range ports {
		containerPort, err := nat.NewPort("tcp", port)
		if err != nil {
			return "", err
		}
		containerConfig.ExposedPorts[containerPort] = struct{}{}
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
		return "", fmt.Errorf("failed to create container: %w", err)
	}

	if err := dockerCli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return "", fmt.Errorf("failed to start container: %w", err)
	}
	return resp.ID, nil
}
