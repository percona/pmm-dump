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
	"os"
	"path/filepath"

	"pmm-dump/internal/test/util"

	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
	"github.com/pkg/errors"
)

func PullImage(ctx context.Context, imageName string) error {
	dockerCli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return errors.Wrap(err, "failed to create docker client")
	}
	defer dockerCli.Close() //nolint:errcheck

	out, err := dockerCli.ImagePull(ctx, imageName, image.PullOptions{})
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

func ImageExists(ctx context.Context, imageName string) (bool, error) {
	dockerCli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return false, errors.Wrap(err, "failed to create docker client")
	}
	defer dockerCli.Close() //nolint:errcheck

	images, err := dockerCli.ImageList(ctx, image.ListOptions{})
	if err != nil {
		return false, errors.Wrap(err, "failed to list images")
	}

	for _, img := range images {
		for _, tag := range img.RepoTags {
			if tag == imageName {
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

	configFiles := make([]string, 0)
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
