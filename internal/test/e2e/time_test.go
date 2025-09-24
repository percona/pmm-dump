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

package e2e

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"math/big"
	"testing"
	"time"

	"pmm-dump/internal/test/deployment"

	"pmm-dump/pkg/clickhouse"
	"pmm-dump/pkg/clickhouse/tsv"
	"pmm-dump/pkg/transferer"
)

const (
	trytime     = 30
	chunkRowLen = 10000
)

func TestClickHouseTime(t *testing.T) {
	c := deployment.NewController(t)
	ctx := t.Context()
	pmm := c.NewPMM("time", ".env.test")
	if err := pmm.Deploy(ctx); err != nil {
		t.Fatal(err)
	}

	cSource, err := clickhouse.NewSource(ctx, clickhouse.Config{
		ConnectionURL: pmm.ClickhouseURL(),
	})
	if err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	for {
		time.Sleep(time.Second * trytime)
		end := time.Now()
		chunkMetas, err := cSource.SplitIntoChunks(start, end, chunkRowLen)
		if err != nil {
			t.Fatal(err)
		}

		var buf bytes.Buffer
		tsvWriter := tsv.NewWriter(&buf)

		for _, meta := range chunkMetas {
			chunks, err := cSource.ReadChunks(meta)
			if err != nil {
				t.Fatal(err)
			}

			for j, chunk := range chunks {
				if len(chunk.Content) == 0 {
					pmm.Log(fmt.Sprintf("Clickhouse chunk content is empty, waiting another %d seconds", trytime))
					continue
				}

				r := tsv.NewReader(bytes.NewBuffer(chunk.Content), cSource.ColumnTypes())
				values, err := r.Read()
				if err != nil {
					if errors.Is(err, io.EOF) {
						break
					}
					t.Fatal(err)
				}

				for i := range values {
					time := convertTimeToRandomTimeZone(t, pmm, values[i])
					if time != nil {
						values[i] = time
					}
				}

				err = tsvWriter.Write(toStringSlice(values))
				if err != nil {
					t.Fatal(err)
				}

				tsvWriter.Flush()
				if tsvWriter.Error() != nil {
					t.Fatal(tsvWriter.Error())
				}

				pmm.Log("Testing the ability to convert clickhouse chunks time")
				chunks[j].Content = buf.Bytes()

				err = cSource.WriteChunk("", bytes.NewBuffer(chunks[j].Content))
				if err == nil {
					t.Fatal("no error when parsing bad time zone")
				}
				pmm.Log("Got an intentional time conversion error", err)
				err = transferer.ConvertTimeToUTC(cSource, chunks[j])
				if err != nil {
					t.Fatal(err)
				}

				err = cSource.WriteChunk("", bytes.NewBuffer(chunks[j].Content))
				if err != nil {
					t.Fatal(err)
				}
				pmm.Log("Succesfully parsed chunk as UTC")
				break
			}
		}
	}
}

func convertTimeToRandomTimeZone(t *testing.T, pmm *deployment.PMM, cont any) any {
	t.Helper()
	timeCh, ok := cont.(time.Time)
	if !ok {
		return nil
	}

	newYork, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal("failed to load location", err)
	}
	los_angeles, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatal("failed to load location", err)
	}
	moscow, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		t.Fatal("failed to load location", err)
	}
	beijing, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal("failed to load location", err)
	}
	tokyo, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		t.Fatal("failed to load location", err)
	}
	locationMap := map[int]*time.Location{
		0: newYork,
		1: los_angeles,
		2: moscow,
		3: beijing,
		4: tokyo,
	}

	num, err := rand.Int(rand.Reader, big.NewInt(4))
	if err != nil {
		t.Fatal("failed to generate random number")
	}
	loc := locationMap[int(num.Int64())]
	pmm.Log("Changed Clickhouse time to:", loc)
	return timeCh.In(loc)
}

func toStringSlice(iSlice []any) []string {
	values := make([]string, 0, cap(iSlice))
	for _, v := range iSlice {
		if v == nil {
			values = append(values, "")
			continue
		}
		values = append(values, fmt.Sprintf("%v", v))
	}
	return values
}
