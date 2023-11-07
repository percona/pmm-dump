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

package transferer

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"path"
	"testing"
	"time"

	"pmm-dump/pkg/dump"
)

func TestImport(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name          string
		dumpPath      string
		shouldErr     bool
		finalizerFail bool
	}{
		{
			name: "basic test",
		},
		{
			name:      "invalid data",
			dumpPath:  "invalidfile.tar.gz",
			shouldErr: true,
		},
		{
			name:      "invalid chunk",
			shouldErr: true,
			dumpPath:  "dumpwithinvalidchunk.tar.gz",
		},
		{
			name:     "empty chunk",
			dumpPath: "dumpwithemptychunk.tar.gz",
		},
		{
			name:      "invalid tar",
			shouldErr: true,
			dumpPath:  "dumpwithinvalidtar.tar.gz",
		},
		{
			name:      "invalid file",
			shouldErr: true,
			dumpPath:  "dumpwithinvalidfile.tar.gz",
		},
		{
			name:      "undefined source",
			shouldErr: true,
			dumpPath:  "dumpwithundefinedsource.tar.gz",
		},
		{
			name:          "failed finalizer",
			shouldErr:     true,
			finalizerFail: true,
		},
	}
	options := []struct {
		suffix       string
		workersCount int
		sourceType   dump.SourceType
	}{
		{
			suffix:       "with 1 worker",
			workersCount: 1,
		},
		{
			suffix:       "with 4 workers",
			workersCount: 4,
		},
		{
			suffix:       "vm only",
			workersCount: 4,
			sourceType:   dump.VictoriaMetrics,
		},
		{
			suffix:       "ch only",
			workersCount: 4,
			sourceType:   dump.ClickHouse,
		},
	}
	fs := map[string][]byte{
		"dumpfile.tar.gz":                fakeFileData(t, fakeFileOpts{withoutMetafile: true}),
		"invalidfile.tar.gz":             []byte("invalid data"),
		"dumpwithinvalidchunk.tar.gz":    fakeFileData(t, fakeFileOpts{withInvalidChunk: true}),
		"dumpwithemptychunk.tar.gz":      fakeFileData(t, fakeFileOpts{withEmptyChunk: true}),
		"dumpwithinvalidtar.tar.gz":      fakeFileData(t, fakeFileOpts{withInvalidTar: true}),
		"dumpwithinvalidfile.tar.gz":     fakeFileData(t, fakeFileOpts{withInvalidFile: true}),
		"dumpwithundefinedsource.tar.gz": fakeFileData(t, fakeFileOpts{withUndefinedSource: true}),
	}
	for _, opt := range options {
		for _, tt := range tests {
			t.Run(fmt.Sprintf("%s %s", tt.name, opt.suffix), func(t *testing.T) {
				if tt.dumpPath == "" {
					tt.dumpPath = "dumpfile.tar.gz"
				}
				buf := bytes.NewBuffer(fs[tt.dumpPath])
				sources := []dump.Source{
					&fakeSource{dump.VictoriaMetrics, tt.finalizerFail},
					&fakeSource{dump.ClickHouse, tt.finalizerFail},
				}
				if opt.sourceType != dump.SourceType(0) {
					sources = []dump.Source{&fakeSource{opt.sourceType, tt.finalizerFail}}
				}
				tr := Transferer{
					sources:      sources,
					workersCount: opt.workersCount,
					file:         buf,
				}
				meta := dump.Meta{}
				err := tr.Import(ctx, meta)
				if err != nil {
					if tt.shouldErr {
						return
					}
					t.Fatal(err, "failed to import")
				}
				if tt.shouldErr && err == nil {
					t.Fatal("there was no err")
				}
			})
		}
	}
}

func fakeFileData(t *testing.T, opts fakeFileOpts) []byte {
	t.Helper()

	var buf bytes.Buffer
	writeFakeFile(t, &buf, opts)
	return buf.Bytes()
}

func writeFakeFile(t *testing.T, w io.Writer, opts fakeFileOpts) {
	t.Helper()

	gzw, err := gzip.NewWriterLevel(w, gzip.BestCompression)
	if err != nil {
		t.Fatal(err, "failed to create gzip writer")
	}
	defer gzw.Close() //nolint:errcheck

	tw := tar.NewWriter(gzw)
	defer tw.Close() //nolint:errcheck
	if opts.withInvalidTar {
		var content bytes.Buffer
		_, err := io.CopyN(&content, rand.Reader, 1024)
		if err != nil {
			t.Fatal(err, "failed to fill content")
		}
		_, err = gzw.Write(content.Bytes())
		if err != nil {
			t.Fatal(err, "failed to write invalid tar")
		}
		return
	}

	if opts.withUndefinedSource {
		var content bytes.Buffer
		_, err := io.CopyN(&content, rand.Reader, 1024)
		if err != nil {
			t.Fatal(err, "failed to fill content")
		}
		err = tw.WriteHeader(&tar.Header{
			Typeflag: tar.TypeReg,
			Name:     path.Join("unknownsource", "chunk.bin"),
			Size:     int64(content.Len()),
			Mode:     0o600,
			ModTime:  time.Now(),
		})
		if err != nil {
			t.Fatal(err, "failed to write file header")
		}
		if _, err = tw.Write(content.Bytes()); err != nil {
			t.Fatal(err, "failed to write chunk content")
		}
	}
	if !opts.withoutMetafile {
		if err := writeMetafile(tw, dump.Meta{}); err != nil {
			t.Fatal(err)
		}
	}

	if err = writeLog(tw, bytes.NewBufferString("logs")); err != nil {
		t.Fatal(err)
	}

	if opts.withInvalidFile {
		var content bytes.Buffer
		_, err := io.CopyN(&content, rand.Reader, 1024)
		if err != nil {
			t.Fatal(err, "failed to fill content")
		}
		err = tw.WriteHeader(&tar.Header{
			Typeflag: tar.TypeReg,
			Name:     "invalidfile.bin",
			Size:     int64(content.Len()),
			Mode:     0o600,
			ModTime:  time.Now(),
		})
		if err != nil {
			t.Fatal(err, "failed to write file header")
		}
		if _, err = tw.Write(content.Bytes()); err != nil {
			t.Fatal(err, "failed to write chunk content")
		}
	}

	for i := 0; i < 10; i++ {
		var content bytes.Buffer
		switch {
		case opts.withEmptyChunk && i == 5:
		case opts.withInvalidChunk && i == 6:
			_, err := content.WriteString("invalid")
			if err != nil {
				t.Fatal(err, "failed to fill invalid content")
			}
		default:
			_, err := io.CopyN(&content, rand.Reader, 1024)
			if err != nil {
				t.Fatal(err, "failed to fill content")
			}
		}
		chunkSize := int64(len(content.Bytes()))

		err = tw.WriteHeader(&tar.Header{
			Typeflag: tar.TypeReg,
			Name:     path.Join("vm", fmt.Sprintf("chunk-%d.bin", i)),
			Size:     chunkSize,
			Mode:     0o600,
			ModTime:  time.Now(),
			Uid:      1,
		})
		if err != nil {
			t.Fatal(err, "failed to write file header")
		}
		if _, err = tw.Write(content.Bytes()); err != nil {
			t.Fatal(err, "failed to write chunk content")
		}

		err = tw.WriteHeader(&tar.Header{
			Typeflag: tar.TypeReg,
			Name:     path.Join("ch", fmt.Sprintf("chunk-%d.bin", i)),
			Size:     chunkSize,
			Mode:     0o600,
			ModTime:  time.Now(),
		})
		if err != nil {
			t.Fatal(err, "failed to write file header")
		}
		if _, err = tw.Write(content.Bytes()); err != nil {
			t.Fatal(err, "failed to write chunk content")
		}
	}
}

type fakeFileOpts struct {
	withInvalidChunk    bool
	withEmptyChunk      bool
	withInvalidTar      bool
	withInvalidFile     bool
	withUndefinedSource bool
	withoutMetafile     bool
}
