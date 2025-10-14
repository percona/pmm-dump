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
	"path/filepath"

	"pmm-dump/internal/test/util"

	"github.com/compose-spec/compose-go/dotenv"
	"github.com/pkg/errors"
)

const (
	envVarPMMURL     = "PMM_URL"
	envVarPMMVersion = "PMM_VERSION"

	envVarPMMServerImage = "PMM_SERVER_IMAGE"
	envVarPMMClientImage = "PMM_CLIENT_IMAGE"
	envVarMongoImage     = "MONGO_IMAGE"
	envVarMongoTag       = "MONGO_TAG"
	envVarUseExistingPMM = "USE_EXISTING_PMM"
)

const defaultPMMURL = "http://admin:admin@localhost" //nolint:gosec

func getEnvFromDotEnv(filepath string) (map[string]string, error) {
	envs, err := dotenv.GetEnvFromFile(make(map[string]string), "", []string{filepath})
	if err != nil {
		return nil, errors.Wrap(err, "failed to get env from file")
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

	useExistingDeployment := false
	if v, ok := envs[envVarUseExistingPMM]; ok && (v == "true" || v == "1") {
		useExistingDeployment = true
	}
	if !useExistingDeployment {
		envs[envVarPMMURL] = setDefaultEnv(envVarPMMURL)
	}

	return envs, nil
}

func setDefaultEnv(key string) string {
	switch key {
	case envVarPMMURL:
		return defaultPMMURL
	case envVarPMMServerImage:
		return "percona/pmm-server"
	case envVarPMMClientImage:
		return "percona/pmm-client"
	case envVarMongoImage:
		return "mongo"
	case envVarMongoTag:
		return "latest"
	case envVarPMMVersion:
		return "3"
	case envVarUseExistingPMM:
		return "false"
	default:
		return ""
	}
}

func GetEnvFromFile(filename string) (map[string]string, error) {
	return getEnvFromDotEnv(filepath.Join(util.TestDir, filename))
}
