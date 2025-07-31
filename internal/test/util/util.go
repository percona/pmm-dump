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

package util

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

var (
	_, b, _, _ = runtime.Caller(0)

	RepoPath = filepath.Join(filepath.Dir(b), "..", "..", "..")
	TestDir  = filepath.Join(RepoPath, "test")
)

// CreateTestDir creates a directory for a test in "./test/tmp/<dirName>/".
// It should be used in tests where it's necessary to save dumps at the end of the test for debugging purposes.
// If the "CI" env variable is set to true, this function will return t.TempDir().
func CreateTestDir(t *testing.T, dirName string) string {
	t.Helper()
	if os.Getenv("CI") == "true" {
		return t.TempDir()
	}
	dirName = fmt.Sprintf("%s-%d", dirName, time.Now().Unix())
	dirPath := filepath.Join(TestDir, "tmp", dirName)
	if err := os.MkdirAll(dirPath, os.ModePerm); err != nil { //nolint:gosec
		t.Fatal(err)
	}
	return dirPath
}

func VMURL(t *testing.T, pmm string) string {
	u, err := url.Parse(pmm)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/prometheus"
	u.RawQuery = ""
	return u.String()
}

