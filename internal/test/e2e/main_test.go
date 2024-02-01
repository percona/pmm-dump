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

	"pmm-dump/internal/test/deployment"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"golang.org/x/sync/semaphore"
)

const (
	maxParallelTests = 4
)

var s *semaphore.Weighted

var failedTests []string

func startTest(t *testing.T) {
	t.Helper()
	t.Parallel()

	if err := s.Acquire(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		s.Release(1)
		if t.Failed() {
			failedTests = append(failedTests, t.Name())
		}
	})
}

func TestMain(m *testing.M) {
	ctx := context.Background()

	s = semaphore.NewWeighted(maxParallelTests)
	logConsoleWriter := zerolog.ConsoleWriter{
		Out:        os.Stderr,
		NoColor:    true,
		TimeFormat: time.RFC3339,
		PartsExclude: []string{
			zerolog.LevelFieldName,
		},
		FieldsExclude: []string{},
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

	if len(failedTests) > 0 {
		log.Print("Failed tests: " + strings.Join(failedTests, ","))
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
