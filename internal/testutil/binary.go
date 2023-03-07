package testutil

import (
	"bytes"
	"os"
	"os/exec"

	"github.com/pkg/errors"
)

type Binary struct {
}

func (b *Binary) Run(args ...string) (string, string, error) {
	return Exec("", "../../../pmm-dump", args...)
}

func Exec(wd string, name string, args ...string) (string, string, error) {
	var err error
	cmd := exec.Command(name, args...)
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
