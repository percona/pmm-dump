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
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

var (
	_, b, _, _ = runtime.Caller(0)

	RepoPath = filepath.Join(filepath.Dir(b), "..", "..", "..")
	testDir  = filepath.Join(RepoPath, "test")
)

func TestDir(t *testing.T, dirName string) string {
	dirName = fmt.Sprintf("%s-%d", dirName, time.Now().Unix())
	dirPath := filepath.Join(testDir, "tmp", dirName)
	if err := os.MkdirAll(dirPath, os.ModePerm); err != nil {
		t.Fatal(err)
	}
	return dirPath
}
