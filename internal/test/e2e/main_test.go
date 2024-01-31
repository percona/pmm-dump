//go:build e2e

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
