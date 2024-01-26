package deployment

import (
	"context"
	"io"
	"os"
	"path/filepath"

	"pmm-dump/internal/test/util"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"github.com/pkg/errors"
)

func PullImage(ctx context.Context, image string) error {
	dockerCli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return errors.Wrap(err, "failed to create docker client")
	}
	defer dockerCli.Close()

	out, err := dockerCli.ImagePull(ctx, image, types.ImagePullOptions{
		Platform: "linux/amd64", // TODO: get from env
	})
	if err != nil {
		return errors.Wrap(err, "failed to pull image")
	}

	// Read the output to make sure the image is fully pulled
	_, err = io.Copy(io.Discard, out)
	if err != nil {
		return errors.Wrap(err, "failed to read image pull output")
	}
	return nil
}

func ImageExists(ctx context.Context, image string) (bool, error) {
	dockerCli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return false, errors.Wrap(err, "failed to create docker client")
	}
	defer dockerCli.Close()

	images, err := dockerCli.ImageList(ctx, types.ImageListOptions{})
	if err != nil {
		return false, errors.Wrap(err, "failed to list images")
	}

	for _, img := range images {
		for _, tag := range img.RepoTags {
			if tag == image {
				return true, nil
			}
		}
	}

	return false, nil
}

func PullNecessaryImages(ctx context.Context) error {
	files, err := os.ReadDir(util.TestDir)
	if err != nil {
		return errors.Wrap(err, "failed to read test dir")
	}

	configFiles := []string{}
	for _, file := range files {
		if !file.IsDir() && filepath.Ext(file.Name()) == ".test" {
			configFiles = append(configFiles, file.Name())
		}
	}

	for _, configFile := range configFiles {
		envs, err := GetEnvFromFile(configFile)
		if err != nil {
			return errors.Wrapf(err, "failed to get env from %s", configFile)
		}

		imagesWithTagsEnvs := map[string]string{
			envVarPMMClientImage: envVarPMMVersion,
			envVarPMMServerImage: envVarPMMVersion,
			envVarMongoImage:     envVarMongoTag,
		}

		for imageEnv, tagEnv := range imagesWithTagsEnvs {
			image := envs[imageEnv] + ":" + envs[tagEnv]
			exists, err := ImageExists(ctx, image)
			if err != nil {
				return errors.Wrap(err, "failed to check image")
			}
			if exists {
				continue
			}
			if err := PullImage(ctx, image); err != nil {
				return errors.Wrapf(err, "failed to pull image %s", image)
			}
		}
	}
	return nil
}
