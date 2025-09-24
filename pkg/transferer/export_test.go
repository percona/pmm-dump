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
	"bytes"
	"testing"
	"time"

	"pmm-dump/pkg/dump"
	"pmm-dump/pkg/encryption"
)

func TestExport(t *testing.T) {
	ctx := t.Context()

	type lsOpts struct {
		status          LoadStatus
		waitCount       int
		statusAfterWait LoadStatus
	}

	tests := []struct {
		name            string
		workersCount    int
		loadStatus      lsOpts
		chunkTimeRange  time.Duration
		sourceType      dump.SourceType
		chunkSourceType dump.SourceType
		shouldErr       bool
	}{
		{
			name:           "normal",
			loadStatus:     lsOpts{status: LoadStatusOK},
			chunkTimeRange: time.Minute,
		},
		{
			name:           "terminate",
			loadStatus:     lsOpts{status: LoadStatusTerminate},
			chunkTimeRange: time.Minute,
			shouldErr:      true,
		},
		{
			name:           "wait",
			loadStatus:     lsOpts{status: LoadStatusWait},
			chunkTimeRange: time.Minute,
			shouldErr:      true,
		},
		{
			name:           "wait 5 seconds and pass",
			loadStatus:     lsOpts{status: LoadStatusWait, waitCount: 5, statusAfterWait: LoadStatusOK},
			chunkTimeRange: time.Minute,
		},
		{
			name:           "wait 5 seconds and terminate",
			loadStatus:     lsOpts{status: LoadStatusWait, waitCount: 5, statusAfterWait: LoadStatusTerminate},
			chunkTimeRange: time.Minute,
			shouldErr:      true,
		},
		{
			name:            "unknown source",
			loadStatus:      lsOpts{status: LoadStatusOK},
			chunkTimeRange:  time.Minute,
			chunkSourceType: dump.InvalidSource,
			shouldErr:       true,
		},
		{
			name:           "unknown load status",
			loadStatus:     lsOpts{status: LoadStatusNone},
			chunkTimeRange: time.Minute,
			shouldErr:      true,
		},
		{
			name:           "vm only",
			loadStatus:     lsOpts{status: LoadStatusOK},
			chunkTimeRange: time.Minute,
			sourceType:     dump.VictoriaMetrics,
		},
		{
			name:           "ch only",
			loadStatus:     lsOpts{status: LoadStatusOK},
			chunkTimeRange: time.Minute,
			sourceType:     dump.ClickHouse,
		},
	}
	options := []struct {
		suffix       string
		workersCount int
	}{
		{
			suffix:       "with 1 worker",
			workersCount: 1,
		},
		{
			suffix:       "with 4 workers",
			workersCount: 4,
		},
	}
	for _, opt := range options {
		for _, tt := range tests {
			t.Run(tt.name+" "+opt.suffix, func(t *testing.T) {
				if tt.chunkSourceType == dump.UndefinedSource {
					tt.chunkSourceType = tt.sourceType
				}
				sources := []dump.Source{
					&fakeSource{dump.VictoriaMetrics, false},
					&fakeSource{dump.ClickHouse, false},
				}
				if tt.sourceType != dump.UndefinedSource {
					sources = []dump.Source{
						&fakeSource{tt.sourceType, false},
					}
				}
				tr := Transferer{
					sources:      sources,
					workersCount: opt.workersCount,
					file:         bytes.NewBuffer(nil),
				}
				e := encryption.Options{
					Encryption: false,
					JustKey:    false,
					Pass:       "",
					Filepath:   "",
				}
				var meta dump.Meta
				var chunks []dump.ChunkMeta
				if tt.chunkSourceType != dump.UndefinedSource {
					chunks = prepareFakeChunks(time.Now().Add(-time.Hour), time.Now(), tt.chunkTimeRange, tt.chunkSourceType)
				} else {
					vmChunks := prepareFakeChunks(time.Now().Add(-time.Hour), time.Now(), tt.chunkTimeRange, dump.VictoriaMetrics)
					chChunks := prepareFakeChunks(time.Now().Add(-time.Hour), time.Now(), tt.chunkTimeRange, dump.ClickHouse)
					chunks = append(vmChunks, chChunks...) //nolint:gocritic
				}
				pool, err := dump.NewChunkPool(chunks)
				if err != nil {
					t.Fatal(err, "failed to create new chunk pool")
				}
				err = tr.Export(ctx, fakeStatusGetter{status: tt.loadStatus.status, waitCount: tt.loadStatus.waitCount, statusAfterWait: tt.loadStatus.statusAfterWait, count: new(int)}, meta, pool, new(bytes.Buffer), e)
				if err != nil {
					if tt.shouldErr {
						return
					}
					t.Fatal(err, "failed to export")
				} else if tt.shouldErr {
					t.Fatal("error is empty")
				}
			})
		}
	}
}

type fakeStatusGetter struct {
	status          LoadStatus
	count           *int
	waitCount       int
	statusAfterWait LoadStatus
}

func (g fakeStatusGetter) GetLatestStatus() (LoadStatus, int) {
	defer func() {
		*g.count++
	}()
	if g.waitCount > 0 && *g.count >= g.waitCount {
		return g.statusAfterWait, *g.count
	}
	return g.status, *g.count
}

func prepareFakeChunks(start, end time.Time, delta time.Duration, sourceType dump.SourceType) []dump.ChunkMeta {
	var chunks []dump.ChunkMeta
	chunkStart := start
	for {
		s, e := chunkStart, chunkStart.Add(delta)
		chunks = append(chunks, dump.ChunkMeta{
			Source: sourceType,
			Start:  &s,
			End:    &e,
		})

		chunkStart = e
		if chunkStart.After(end) {
			break
		}
	}
	return chunks
}
