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

const defaultPMMURL = "http://admin:admin@localhost"

func getEnvFromDotEnv(filepath string) (map[string]string, error) {
	envs, err := dotenv.GetEnvFromFile(make(map[string]string), "", []string{filepath})
	if err != nil {
		return nil, errors.Wrap(err, "failed to get env from file")
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
	case envVarMongoTag, envVarPMMVersion:
		return "latest"
	case envVarUseExistingPMM:
		return "false"
	default:
		return ""
	}
}

func GetEnvFromFile(filename string) (map[string]string, error) {
	return getEnvFromDotEnv(filepath.Join(util.TestDir, filename))
}
