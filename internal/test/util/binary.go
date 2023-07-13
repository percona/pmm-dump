package util

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"time"

	"github.com/pkg/errors"
)

type Binary struct {
	timeout time.Duration
}

func (b *Binary) Run(args ...string) (string, string, error) {
	if b.timeout == 0 {
		b.timeout = time.Minute * 5
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
	cmd.Stdout = new(bytes.Buffer)
	cmd.Stderr = new(bytes.Buffer)
	err = cmd.Run()
	stdout := cmd.Stdout.(*bytes.Buffer).String()
	stderr := cmd.Stderr.(*bytes.Buffer).String()
	return stdout, stderr, err
}
