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
	"os"
	"strconv"
	"sync"
	"testing"

	"github.com/pkg/errors"
	"golang.org/x/sync/semaphore"
)

const (
	defaultMaxParallelTests = 4
	envMaxParallel          = "PMM_DUMP_MAX_PARALLEL_TESTS"
)

var testLimitSemaphore *semaphore.Weighted

var registeredDeployments = new(sync.Map)

var failedTests []string

type Controller struct {
	t           *testing.T
	deployments []*PMM
}

func init() { //nolint:gochecknoinits
	maxParallelTests := defaultMaxParallelTests
	maxParallelTestsStr, ok := os.LookupEnv(envMaxParallel)
	if ok {
		var err error
		maxParallelTests, err = strconv.Atoi(maxParallelTestsStr)
		if err != nil {
			panic(errors.Wrapf(err, "failed to parse %s", envMaxParallel))
		}
	}

	testLimitSemaphore = semaphore.NewWeighted(int64(maxParallelTests))
}

func NewController(t *testing.T) *Controller {
	t.Helper()
	t.Parallel()

	if err := testLimitSemaphore.Acquire(t.Context(), 1); err != nil {
		t.Fatal(err)
	}
	c := &Controller{t: t}
	t.Cleanup(func() {
		testLimitSemaphore.Release(1)
		if t.Failed() {
			failedTests = append(failedTests, t.Name())
		} else {
			for _, pmm := range c.deployments {
				if !pmm.dontCleanup {
					pmm.Destroy(t.Context())
				}
			}
		}
	})
	return &Controller{t: t}
}

func (c *Controller) NewPMM(name, configFile string) *PMM {
	pmm := newPMM(name, configFile)
	pmm.t = c.t
	c.deployments = append(c.deployments, pmm)
	return pmm
}

// NewReusablePMM creates a PMM object which is not cleaned up on the finish of the test.
// It allows to reuse PMM deployment in multiple test.
// Should be used with (*Controller) ReusablePMM.
func NewReusablePMM(name, configFile string) *PMM {
	pmm := newPMM(name, configFile)
	pmm.DontCleanup()
	return pmm
}

// ReusablePMM creates a copy of reusablePMM object with adjusted fields to be used in test.
func (c *Controller) ReusablePMM(reusablePMM *PMM) *PMM {
	return reusablePMM.Copy(c.t)
}

func GetFailedTests() []string {
	return failedTests
}
