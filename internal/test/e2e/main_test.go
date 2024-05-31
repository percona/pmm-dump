//go:build e2e

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

package e2e

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"pmm-dump/internal/test/deployment"
)

func TestMain(m *testing.M) {
	ctx := context.Background()

	logConsoleWriter := zerolog.ConsoleWriter{
		Out:        os.Stderr,
		NoColor:    true,
		TimeFormat: time.RFC3339,
		PartsExclude: []string{
			zerolog.LevelFieldName,
		},
		FieldsExclude: make([]string, 0),
	}

	log.Logger = log.Output(logConsoleWriter)

	if err := deployment.DestroyAll(ctx); err != nil {
		log.Err(err).Msg("failed to destroy all deployments")
		os.Exit(1)
	}

	log.Print("Pulling necessary images")

	if err := deployment.PullNecessaryImages(ctx); err != nil {
		log.Err(err).Msg("failed to pull images")
		os.Exit(1)
	}

	log.Print("Images pulled")

	log.Print("Running tests")
	exitcode := m.Run()

	if len(deployment.GetFailedTests()) > 0 {
		log.Print("Failed tests: " + strings.Join(deployment.GetFailedTests(), ","))
		os.Exit(1)
	}

	log.Print("Tests ran successfully")
	log.Print("Destroying all deployments")
	if err := deployment.DestroyAll(ctx); err != nil {
		log.Err(err).Msg("failed to destroy all deployments")
		os.Exit(1)
	}
	os.Exit(exitcode)
}
