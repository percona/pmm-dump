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
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/pkg/errors"
)

func (pmm *PMM) Exec(ctx context.Context, containerName string, cmd ...string) error {
	dockerCli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return errors.Wrap(err, "failed to create docker client")
	}
	defer dockerCli.Close() //nolint:errcheck
	resp, err := dockerCli.ContainerExecCreate(ctx, containerName, container.ExecOptions{
		Tty:          true,
		AttachStdout: true,
		AttachStderr: true,
		Cmd:          cmd,
	})
	if err != nil {
		return errors.Wrap(err, "failed to create exec")
	}
	attach, err := dockerCli.ContainerExecAttach(ctx, resp.ID, container.ExecAttachOptions{})
	if err != nil {
		return errors.Wrap(err, "failed to attach exec")
	}
	defer attach.Close()

	ctx, cancel := context.WithTimeout(ctx, 180*time.Second) //nolint:mnd
	defer cancel()
	inspect, err := dockerCli.ContainerExecInspect(ctx, resp.ID)
	if err != nil {
		return errors.Wrap(err, "failed to inspect exec")
	}
	for inspect.Running {
		time.Sleep(1 * time.Second)
		inspect, err = dockerCli.ContainerExecInspect(ctx, resp.ID)
		if err != nil {
			return errors.Wrap(err, "failed to inspect exec")
		}
	}
	if inspect.ExitCode != 0 {
		output, err := io.ReadAll(attach.Reader)
		if err != nil {
			return errors.Wrap(err, "failed to read exec output")
		}
		return errors.New("exit code is not 0:" + string(output))
	}
	return nil
}

func (pmm *PMM) FileReader(ctx context.Context, containerName string, path string) (io.ReadCloser, error) {
	dockerCli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, errors.Wrap(err, "failed to create docker client")
	}
	defer dockerCli.Close() //nolint:errcheck
	reader, _, err := dockerCli.CopyFromContainer(ctx, containerName, path)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get file from container")
	}
	return reader, nil
}
