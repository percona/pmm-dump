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
	"bytes"
	"context"
	"os"
	"os/exec"
	"time"

	"github.com/pkg/errors"
)

const defaultTimeout = time.Minute * 5

type Binary struct {
	timeout time.Duration
}

func (b *Binary) Run(args ...string) (string, string, error) {
	if b.timeout == 0 {
		b.timeout = defaultTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), b.timeout)
	defer cancel()
	return Exec(ctx, RepoPath, "./pmm-dump", args...)
}

func Exec(ctx context.Context, wd string, name string, args ...string) (string, string, error) {
	var err error
	cmd := exec.CommandContext(ctx, name, args...)
	if wd == "" {
		cmd.Dir, err = os.Getwd()
		if err != nil {
			return "", "", errors.Wrap(err, "failed to get working directory")
		}
	} else {
		cmd.Dir = wd
	}
	cmd.Stdin = nil
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()

	return stdout.String(), stderr.String(), err
}

func (b *Binary) RunBash(args ...string) (string, string, error) {
	if b.timeout == 0 {
		b.timeout = defaultTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), b.timeout)
	defer cancel()
	var err error
	cmd := exec.CommandContext(ctx, "bash", args...)
	if RepoPath == "" {
		cmd.Dir, err = os.Getwd()
		if err != nil {
			return "", "", errors.Wrap(err, "failed to get working directory")
		}
	} else {
		cmd.Dir = RepoPath
	}
	cmd.Stdin = nil
	var output, outputerr bytes.Buffer
	cmd.Stderr = &outputerr
	cmd.Stdout = &output
	err = cmd.Run()
	return output.String(), outputerr.String(), err
}
