package deployment

import (
	"context"
	"io"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"github.com/pkg/errors"
)

func (pmm *PMM) Exec(ctx context.Context, container string, cmd ...string) error {
	dockerCli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return errors.Wrap(err, "failed to create docker client")
	}
	defer dockerCli.Close()
	resp, err := dockerCli.ContainerExecCreate(ctx, container, types.ExecConfig{
		Tty:          true,
		AttachStdout: true,
		AttachStderr: true,
		Cmd:          cmd,
	})
	if err != nil {
		return errors.Wrap(err, "failed to create exec")
	}
	attach, err := dockerCli.ContainerExecAttach(ctx, resp.ID, types.ExecStartCheck{})
	if err != nil {
		return errors.Wrap(err, "failed to attach exec")
	}
	defer attach.Close()

	inspect, err := dockerCli.ContainerExecInspect(ctx, resp.ID)
	if err != nil {
		return errors.Wrap(err, "failed to inspect exec")
	}
	for inspect.Running {
		// TODO: timeout
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
