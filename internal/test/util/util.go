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
	repoPath   = filepath.Join(filepath.Dir(b), "..", "..", "..")
	testDir    = filepath.Join(repoPath, "test")
)

func TestDir(t *testing.T, dirName string) string {
	dirName = fmt.Sprintf("%s-%d", dirName, time.Now().Unix())
	dirPath := filepath.Join(testDir, "tmp", dirName)
	if err := os.MkdirAll(dirPath, os.ModePerm); err != nil {
		t.Fatal(err)
	}
	return dirPath
}
