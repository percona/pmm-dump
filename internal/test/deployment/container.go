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
	"path/filepath"
	"strings"
	"time"

	"pmm-dump/internal/test/util"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
	"github.com/pkg/errors"
)

const (
	defaultHTTPPort           = "80"
	defaultHTTPSPort          = "443"
	defaultClickhousePort     = "9000"
	defaultClickhouseHTTPPort = "8123"

	defaultMongoPort = "27017"

	volumeSuffix = "-data"

	pmmClientMemoryLimit = 128 * 1024 * 1024
	pmmServerMemoryLimit = 2048 * 1024 * 1024
	mongoMemoryLimit     = 256 * 1024 * 1024
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

	ports := []string{defaultHTTPPort, defaultHTTPSPort, defaultClickhousePort, defaultClickhouseHTTPPort}

	id, err := pmm.createContainer(ctx, dockerCli, pmm.ServerContainerName(), pmm.ServerImage(), ports, nil, mounts, networkID, nil, pmmServerMemoryLimit)
	if err != nil {
		return errors.Wrap(err, "failed to create container")
	}
	pmm.pmmServerContainerID = id

	if err := pmm.SetServerPublishedPorts(ctx, dockerCli); err != nil {
		return errors.Wrap(err, "failed to set server published ports")
	}

	if err := pmm.Exec(ctx, pmm.ServerContainerName(), "sed", "-i", "s#<!-- <listen_host>0.0.0.0</listen_host> -->#<listen_host>0.0.0.0</listen_host>#g", "/etc/clickhouse-server/config.xml"); err != nil {
		return errors.Wrap(err, "failed to update clickhouse config")
	}

	if err := doUntilSuccess(60*time.Second, func() error {
		return pmm.Exec(ctx, pmm.ServerContainerName(), "supervisorctl", "restart", "clickhouse")
	}); err != nil {
		return errors.Wrap(err, "failed to restart clickhouse")
	}

	if err := getUntilOk(pmm.PMMURL()+"/v1/version", time.Second*120); err != nil {
		return errors.Wrap(err, "failed to ping PMM")
	}

	return nil
}

func (pmm *PMM) SetServerPublishedPorts(ctx context.Context, dockerCli *client.Client) error {
	container, err := dockerCli.ContainerInspect(ctx, pmm.pmmServerContainerID)
	if err != nil {
		return errors.Wrap(err, "failed to inspect container")
	}

	pmm.httpPort, err = getPublishedPort(container, defaultHTTPPort)
	if err != nil {
		return errors.Wrap(err, "failed to get published http port")
	}
	pmm.httpsPort, err = getPublishedPort(container, defaultHTTPSPort)
	if err != nil {
		return errors.Wrap(err, "failed to get published https port")
	}
	pmm.clickhousePort, err = getPublishedPort(container, defaultClickhousePort)
	if err != nil {
		return errors.Wrap(err, "failed to get published clickhouse port")
	}
	pmm.clickhouseHTTPPort, err = getPublishedPort(container, defaultClickhouseHTTPPort)
	if err != nil {
		return errors.Wrap(err, "failed to get published clickhouse http port")
	}
	return nil
}

func getPublishedPort(container types.ContainerJSON, port string) (string, error) {
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
	envs := []string{
		"PMM_AGENT_CONFIG_FILE=config/pmm-agent.yaml",
		"PMM_AGENT_SERVER_USERNAME=admin",
		"PMM_AGENT_SERVER_PASSWORD=admin",
		"PMM_AGENT_SERVER_ADDRESS=" + pmm.ServerContainerName() + ":443",
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
	if err := doUntilSuccess(30*time.Second, func() error {
		err = pmm.Exec(ctx, pmm.ClientContainerName(), "pmm-admin", "status")
		if err != nil {
			if strings.Contains(err.Error(), "is not running") {
				time.Sleep(5 * time.Second)
				err := dockerCli.ContainerStart(ctx, pmm.ClientContainerName(), types.ContainerStartOptions{})
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
	pmm.mongoContainerID = id

	if err := pmm.SetMongoPublishedPorts(ctx, dockerCli); err != nil {
		return errors.Wrap(err, "failed to set mongo published ports")
	}
	return nil
}

func (pmm *PMM) SetMongoPublishedPorts(ctx context.Context, dockerCli *client.Client) error {
	container, err := dockerCli.ContainerInspect(ctx, pmm.mongoContainerID)
	if err != nil {
		return errors.Wrap(err, "failed to inspect container")
	}

	pmm.mongoPort, err = getPublishedPort(container, defaultMongoPort)
	if err != nil {
		return errors.Wrap(err, "failed to get published mongo port")
	}
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
		return "", errors.Wrap(err, "failed to create container")
	}

	if err := dockerCli.ContainerStart(ctx, resp.ID, types.ContainerStartOptions{}); err != nil {
		return "", errors.Wrap(err, "failed to start container")
	}
	return resp.ID, nil
}
