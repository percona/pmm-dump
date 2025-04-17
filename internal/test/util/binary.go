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
	"io"
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

func (b *Binary) RunPipe(exportP []string, importP []string, nameOut string, nameIn string) (string, string, error) {
	if b.timeout == 0 {
		b.timeout = defaultTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), b.timeout)
	defer cancel()
	c1 := exec.CommandContext(ctx, nameOut, exportP...)
	c2 := exec.CommandContext(ctx, nameIn, importP...)
	var err error
	if RepoPath == "" {
		c1.Dir, err = os.Getwd()
		if err != nil {
			return "", "", errors.Wrap(err, "failed to get working directory")
		}
		c2.Dir, err = os.Getwd()
		if err != nil {
			return "", "", errors.Wrap(err, "failed to get working directory")
		}
	} else {
		c1.Dir = RepoPath
		c2.Dir = RepoPath
	}
	pr, pw := io.Pipe()
	c1.Stdout = pw
	c2.Stdin = pr
	var stderr1, stderr2 bytes.Buffer
	c1.Stderr = &stderr1
	c2.Stderr = &stderr2

	err = c1.Start()
	if err != nil {
		return "", "", errors.Wrap(err, "failed to export")
	}
	err = c2.Start()
	if err != nil {
		return "", "", errors.Wrap(err, "failed to import")
	}
	go func() {
		defer pw.Close() //nolint:errcheck
		err = c1.Wait()
	}()

	err = c2.Wait()
	if err != nil {
		return "", "", errors.Wrap(err, "failed to import")
	}
	return stderr1.String(), stderr2.String(), err
}
