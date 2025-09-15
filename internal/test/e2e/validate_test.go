//go:build e2e

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
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"pmm-dump/internal/test/deployment"
	"pmm-dump/internal/test/util"
	"pmm-dump/pkg/clickhouse/tsv"
	"pmm-dump/pkg/dump"
	"pmm-dump/pkg/victoriametrics"
)

const (
	chunkTimeRange       = time.Second * 30
	timeRange            = time.Second * 120
	precisionForRounding = 20
	bitsForRounding      = 64
)

func TestValidate(t *testing.T) {
	ctx := t.Context()

	c := deployment.NewController(t)
	pmm := c.NewPMM("validate", ".env.test")
	newPMM := c.NewPMM("validate-2", ".env2.test")

	var b util.Binary
	tmpDir := util.CreateTestDir(t, "validate-test")
	xDumpPath := filepath.Join(tmpDir, "dump.tar.gz")
	yDumpPath := filepath.Join(tmpDir, "dump2.tar.gz")

	if err := pmm.Deploy(ctx); err != nil {
		t.Fatal(err)
	}

	start := time.Now().UTC()
	pmm.Logf("Sleeping for %d seconds", int(timeRange.Seconds()))
	time.Sleep(timeRange)
	end := time.Now().UTC()

	pmm.Log("Exporting data to", xDumpPath, start, end)
	stdout, stderr, err := b.Run(
		"export",
		"--ignore-load",
		"-d", xDumpPath,
		"--pmm-url", pmm.PMMURL(),
		"--dump-qan",
		"--click-house-url", pmm.ClickhouseURL(),
		"--start-ts", start.Format(time.RFC3339),
		"--end-ts", end.Format(time.RFC3339),
		"--chunk-time-range", chunkTimeRange.String(),
		"--no-encryption",
		"-v")
	if err != nil {
		t.Fatal("failed to export", err, stdout, stderr)
	}

	pmm.Logf("Sleeping for %d seconds", int(chunkTimeRange.Seconds()))
	time.Sleep(chunkTimeRange)

	if err := newPMM.Deploy(ctx); err != nil {
		t.Fatal(err)
	}

	pmm.Log("Importing data from", xDumpPath)
	stdout, stderr, err = b.Run(
		"import",
		"-d", xDumpPath,
		"--pmm-url", newPMM.PMMURL(),
		"--dump-qan",
		"--click-house-url", newPMM.ClickhouseURL(),
		"--no-encryption",
		"-v")
	if err != nil {
		t.Fatal("failed to import", err, stdout, stderr)
	}

	pmm.Log("Sleeping for 10 seconds")
	time.Sleep(time.Second * 10)

	pmm.Log("Exporting data to", yDumpPath)
	stdout, stderr, err = b.Run(
		"export",
		"--ignore-load",
		"-d", yDumpPath,
		"--pmm-url", newPMM.PMMURL(),
		"--dump-qan",
		"--click-house-url", newPMM.ClickhouseURL(),
		"--start-ts", start.Format(time.RFC3339),
		"--end-ts", end.Format(time.RFC3339),
		"--chunk-time-range", chunkTimeRange.String(),
		"--no-encryption",
		"-v")
	if err != nil {
		t.Fatal("failed to import", err, stdout, stderr)
	}

	loss, missingChunks, err := validateChunks(t, pmm, xDumpPath, yDumpPath)
	if err != nil {
		t.Fatal("failed to validate chunks", err)
	}

	// TODO: This should be a removed and replaced with a better solution.
	// Investigate why, after PMM 3.3.0,
	// Postgres began creating duplicate metrics with a value of 0.
	dublicateLoss, notDuplicateLoss, err := countDuplicateLoss(t, pmm, xDumpPath, yDumpPath)
	if err != nil {
		t.Fatal("failed to count duplicate loss", err)
	}
	loss -= (dublicateLoss - notDuplicateLoss)

	if loss > 0.001 {
		t.Fatalf("too much data loss %f%%", loss*100)
	}
	if missingChunks > 5 {
		t.Fatalf("too many missing chunks: %d", missingChunks)
	}

	pmm.Log(fmt.Sprintf("Data loss in similar chunks is %f%%", loss*100))
	pmm.Log(fmt.Sprintf("Amount of missing chunks is %d", missingChunks))
}

func validateChunks(t *testing.T, pmm *deployment.PMM, xDump, yDump string) (float64, int, error) {
	t.Helper()

	xChunkMap, err := readChunks(xDump)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to read dump %s: %w", xDump, err)
	}
	yChunkMap, err := readChunks(yDump)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to read dump %s: %w", yDump, err)
	}

	xMissingChunks := make([]string, 0)
	yMissingChunks := make([]string, 0)
	if len(xChunkMap) != len(yChunkMap) {
		pmm.Log(fmt.Sprintf("number of chunks is different in %s = %d and %s = %d", xDump, len(xChunkMap), yDump, len(yChunkMap)))
		for xFilename := range xChunkMap {
			if _, ok := yChunkMap[xFilename]; !ok {
				xMissingChunks = append(xMissingChunks, xFilename)
				delete(xChunkMap, xFilename)
			}
		}
		for yFilename := range yChunkMap {
			if _, ok := xChunkMap[yFilename]; !ok {
				yMissingChunks = append(yMissingChunks, yFilename)
				delete(yChunkMap, yFilename)
			}
		}
	}
	var totalValues, totalMissingValues int

	for xFilename, xChunkData := range xChunkMap {
		yChunkData, ok := yChunkMap[xFilename]
		if !ok {
			return 0, 0, fmt.Errorf("chunk %s is missing in %s", xFilename, yDump)
		}
		dir, _ := path.Split(xFilename)
		st := dump.ParseSourceType(dir[:len(dir)-1])
		switch st {
		case dump.VictoriaMetrics:
			xChunk, err := vmParseChunk(xChunkData)
			if err != nil {
				return 0, 0, fmt.Errorf("failed to parse chunk %s: %w", xFilename, err)
			}
			yChunk, err := vmParseChunk(yChunkData)
			if err != nil {
				return 0, 0, fmt.Errorf("failed to parse chunk %s: %w", xFilename, err)
			}

			xValues := vmValuesCount(xChunk)
			yValues := vmValuesCount(yChunk)
			if xValues > yValues {
				totalValues += xValues
			} else {
				totalValues += yValues
			}

			missingValues, err := vmCompareChunkData(pmm, xChunk, yChunk)
			if err != nil {
				return 0, 0, fmt.Errorf("failed to compare chunk %s: %w", xFilename, err)
			}

			totalMissingValues += missingValues
		case dump.ClickHouse:
			chCompareChunks(t, pmm, xFilename, xDump, yDump, xChunkData, yChunkData)

			if !reflect.DeepEqual(xChunkData, yChunkData) {
				return 0, 0, fmt.Errorf("chunk %s is different", xFilename)
			}
		default:
			return 0, 0, fmt.Errorf("unknown source type %s", st)
		}
	}
	return float64(totalMissingValues) / float64(totalValues), len(xMissingChunks) + len(yMissingChunks), nil
}

func chCompareChunks(t *testing.T, pmm *deployment.PMM, filename string, xDump, yDump string, xChunkData, yChunkData []byte) {
	t.Helper()

	getHashMap := func(data []byte) map[string][]string {
		r := tsv.NewReader(bytes.NewBuffer(data), nil)
		records, err := r.ReadAll()
		if err != nil {
			t.Fatal(err)
		}
		recordsMap := make(map[string][]string)
		for _, r := range records {
			data, err := json.Marshal(r)
			if err != nil {
				t.Fatal(err)
			}
			hash := fmt.Sprintf("%x", sha256.Sum256(data))
			recordsMap[hash] = r
		}
		return recordsMap
	}

	xRecordsMap := getHashMap(xChunkData)
	yRecordsMap := getHashMap(yChunkData)

	for k := range xRecordsMap {
		_, ok := yRecordsMap[k]
		if !ok {
			continue
		}

		delete(xRecordsMap, k)
		delete(yRecordsMap, k)
	}
	if len(xRecordsMap) > 0 || len(yRecordsMap) > 0 {
		for _, r := range xRecordsMap {
			pmm.Log(fmt.Sprintf("Missing record in %s of %s dump: [%s]", filename, yDump, strings.Join(r, ";")))
		}
		for _, r := range yRecordsMap {
			pmm.Log(fmt.Sprintf("Missing record in %s of %s dump: [%s]", filename, xDump, strings.Join(r, ";")))
		}
		t.Fatal(fmt.Errorf("chunk %s is different", filename))
	}
}

func vmValuesCount(xChunk []vmMetric) int {
	total := 0
	for _, v := range xChunk {
		total += len(v.Values)
	}
	return total
}

func vmCompareChunkData(pmm *deployment.PMM, xChunk, yChunk []vmMetric) (int, error) {
	if len(xChunk) != len(yChunk) {
		pmm.Log(fmt.Sprintf("Size of chunks is different: len(x)=%d, len(y)=%d", len(xChunk), len(yChunk)))
	}

	xHashMap := make(map[string]vmMetric)
	for _, v := range xChunk {
		if _, ok := xHashMap[v.MetricHash()]; ok && v.Hash() != xHashMap[v.MetricHash()].Hash() {
			return 0, errors.New("duplicate metric but different values")
		}
		xHashMap[v.MetricHash()] = v
	}

	yHashMap := make(map[string]vmMetric)
	for _, v := range yChunk {
		if _, ok := yHashMap[v.MetricHash()]; ok && v.Hash() != yHashMap[v.MetricHash()].Hash() {
			return 0, errors.New("duplicate metric but different values")
		}
		yHashMap[v.MetricHash()] = v
	}

	loss := 0

	for k, xMetric := range xHashMap {
		yMetric, ok := yHashMap[k]
		if !ok {
			continue
		}

		currentLoss := xMetric.CompareTimestampValues(pmm, yMetric)
		if currentLoss > 0 {
			loss += currentLoss
		}

		delete(xHashMap, k)
		delete(yHashMap, k)
	}

	missingMetrics := make([]vmMetric, 0)
	for _, v := range xHashMap {
		missingMetrics = append(missingMetrics, v)
	}
	for _, v := range yHashMap {
		missingMetrics = append(missingMetrics, v)
	}

	return loss + len(missingMetrics), nil
}

type chunkMap map[string][]byte

func readChunks(filename string) (chunkMap, error) {
	f, err := os.Open(filename) //nolint:gosec
	if err != nil {
		return nil, err
	}
	defer f.Close() //nolint:errcheck

	gzr, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("failed to open as gzip: %w", err)
	}
	defer gzr.Close() //nolint:errcheck

	tr := tar.NewReader(gzr)
	chunkMap := make(chunkMap)

	for {
		header, err := tr.Next()

		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return nil, err
		}

		dir, filename := path.Split(header.Name)

		switch filename {
		case dump.MetaFilename, dump.LogFilename:
			continue
		}

		if len(dir) == 0 {
			return nil, fmt.Errorf("corrupted dump: found unknown file %s", filename)
		}

		st := dump.ParseSourceType(dir[:len(dir)-1])
		if st == dump.UndefinedSource {
			return nil, fmt.Errorf("corrupted dump: found undefined source: %s", dir)
		}

		content, err := io.ReadAll(tr)
		if err != nil {
			return nil, fmt.Errorf("failed to read chunk content: %w", err)
		}

		if len(content) == 0 {
			continue
		}

		chunkMap[header.Name] = content
	}
	return chunkMap, nil
}

func isGzip(data []byte) bool {
	reader := bytes.NewReader(data)
	r, err := gzip.NewReader(reader)
	if r != nil {
		err = r.Close()
		if err != nil {
			panic(err)
		}
	}
	return err == nil
}

func vmParseChunk(data []byte) ([]vmMetric, error) {
	var r io.Reader
	var err error
	r = bytes.NewBuffer(data)
	if isGzip(data) {
		gr, err := gzip.NewReader(bytes.NewBuffer(data))
		if err != nil {
			return nil, fmt.Errorf("failed to create reader: %w", err)
		}
		defer gr.Close() //nolint:errcheck
		r = gr
	}
	metrics, err := victoriametrics.ParseMetrics(r)
	if err != nil {
		return nil, fmt.Errorf("failed to parse metrics: %w", err)
	}
	result := make([]vmMetric, len(metrics))
	for i, v := range metrics {
		result[i] = vmMetric(v)
	}
	return result, nil
}

type vmMetric victoriametrics.Metric

func (vm vmMetric) MetricString() string {
	m := vm.Metric
	return fmt.Sprintf(`{"__name__": "%s", "job": "%s", "instance": "%s", "agent_id": "%s", "agent_type": "%s"} (%s)`, m["__name__"], m["job"], m["instance"], m["agent_id"], m["agent_type"], fmt.Sprint(m))
}

func (vm vmMetric) Hash() string {
	data, err := json.Marshal(vm)
	if err != nil {
		panic(err)
	}
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

func (vm vmMetric) MetricHash() string {
	data, err := json.Marshal(vm.Metric)
	if err != nil {
		panic(err)
	}
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

func (vm vmMetric) CompareTimestampValues(pmm *deployment.PMM, with vmMetric) int {
	xMap := make(map[int64]float64)
	for i, v := range vm.Timestamps {
		xMap[v] = vm.Values[i]
	}
	yMap := make(map[int64]float64)
	for i, v := range with.Timestamps {
		yMap[v] = with.Values[i]
	}

	for timestamp, xValue := range xMap {
		yValue, ok := yMap[timestamp]
		if !ok {
			switch vm.Metric["__name__"] {
			case "pg_stat_activity_count", "pg_stat_activity_max_tx_duration", "pg_stat_activity_max_state_duration":
				if vm.Metric["datname"] == "grafana" || vm.Metric["datname"] == "pmm-managed" {
					// Temporary fix for duplicates
					// See [countDuplicateLoss] for details about postgres and duplicates.
					continue
				}
			}
			pmm.Log(fmt.Sprintf("Value and timestamp not found for metric %s in second dump: wanted %v for %d", vm.MetricString(), xValue, timestamp))
			continue
		}
		if !roundFloatAndCheck(xValue, yValue) {
			pmm.Log(fmt.Sprintf("Values for timestamp %d in metric %s are not the same: %v and %v", timestamp, vm.MetricString(), xValue, yValue))
			continue
		}
		delete(xMap, timestamp)
		delete(yMap, timestamp)
	}

	for timestamp, yValue := range yMap {
		_, ok := xMap[timestamp]
		if !ok {
			pmm.Log(fmt.Sprintf("Value and timestamp not found for metric %s in first dump: wanted %v for %d", vm.MetricString(), yValue, timestamp))
			continue
		}
	}

	return int(math.Abs(float64(len(xMap) - len(yMap))))
}

// Go usually can't handle floats with 16+ digits.
// For example, when comparing them, the dump has the same numbers, but Go's float64 can interpret them as two different numbers:
// 1) 0.00999923005927 and 0.00999923005928.
// 2) 1.7540397181656854e+09 and 1.7540397181656857e+09.
// 3) 9.223372036854776e+18 and 9.223372036854775e+18.
// So, we convert them to Big.Float, round them, and then compare them.

func roundFloatAndCheck(f1, f2 float64) bool {
	// stop rounding and just compare if f1 has 1 digit only
	s := strconv.FormatFloat(f1, 'e', precisionForRounding, bitsForRounding)
	dotIndex := strings.Index(s, "e")
	if dotIndex == -1 {
		return f1 == f2
	}

	// round and compare
	n1Rounded := new(big.Float).SetPrec(precisionForRounding).SetMode(big.ToNearestAway).SetFloat64(f1)
	n2Rounded := new(big.Float).SetPrec(precisionForRounding).SetMode(big.ToNearestAway).SetFloat64(f2)

	return n1Rounded.Cmp(n2Rounded) == 0
}

// After version 3.3.0, PostgreSQL metrics (pg_stat_activity_count, pg_stat_activity_max_tx_duration,
// and pg_stat_activity_max_state_duration specifically from the Grafana and PMM-managed databases)
// started showing duplicate metrics with the same timestamp.
//
// The only difference is that the duplicate metrics have a value of 0, and an opposite usename.
// for example, we have two metrics from the first dump. Metrics with "##" will be missing after import.
//
// 1:
//
// Metric name: pg_stat_activity_count , value:0 timestamp:1, datname: pmm-managed, state:active, usename:
// ##Metric name: pg_stat_activity_count , value:0 timestamp:2, datname: pmm-managed, state:active, usename:
// ##Metric name: pg_stat_activity_count , value:0 timestamp:3, datname: pmm-managed, state:active, usename:
// Metric name: pg_stat_activity_count , value:0 timestamp:4, datname: pmm-managed, state:active, usename:
//
// 2:
//
// ##Metric name: pg_stat_activity_count , value:0 timestamp:1, datname: pmm-managed, state:active, usename: pmm-managed
// Metric name: pg_stat_activity_count , value:1 timestamp:2, datname: pmm-managed, state:active, usename: pmm-managed
// Metric name: pg_stat_activity_count , value:1 timestamp:3, datname: pmm-managed, state:active, usename: pmm-managed
// ##Metric name: pg_stat_activity_count , value:0 timestamp:4, datname: pmm-managed, state:active, usename: pmm-managed
//
// After importing the dump into another PMM, the metrics will be deduplicated:
//
// Metric name: pg_stat_activity_count , value:0 timestamp:1, datname: pmm-managed, state:active, usename:
// Metric name: pg_stat_activity_count , value:1 timestamp:2, datname: pmm-managed, state:active, usename: pmm-managed
// Metric name: pg_stat_activity_count , value:1 timestamp:3, datname: pmm-managed, state:active, usename: pmm-managed
// Metric name: pg_stat_activity_count , value:0 timestamp:4, datname: pmm-managed, state:active, usename:
//
// Although these results are two different metrics, they were merged in comment for demonstration.
// So, to prevent the test from failing, we need to go over two dumps again and calculate loss value of duplicates.
// And then subtract them from actual value.
func countDuplicateLoss(t *testing.T, pmm *deployment.PMM, xDump, yDump string) (float64, float64, error) {
	t.Helper()
	xChunkMap, err := readChunks(xDump)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to read dump %s: %w", xDump, err)
	}
	yChunkMap, err := readChunks(yDump)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to read dump %s: %w", yDump, err)
	}

	totalDuplicate, totalNotDuplicate, totalValues := 0, 0, 0

	duplicateMapX := make(map[string]float64)
	// only needed to compare metrics later to find the duplicates in the X dump.
	duplicateMapY := make(map[string]float64)

	checkIfPGMetric := func(vm vmMetric) bool {
		switch vm.Metric["__name__"] {
		case "pg_stat_activity_count", "pg_stat_activity_max_tx_duration", "pg_stat_activity_max_state_duration":
			if vm.Metric["datname"] == "grafana" || vm.Metric["datname"] == "pmm-managed" {
				return true
			}
		}
		return false
	}

	saveTimestampToMap := func(timestamp int64, value float64, vm vmMetric, destination map[string]float64) {
		destination[fmt.Sprintf("%s: Timestamp:%d, Datname:%s, State:%s", vm.Metric["__name__"], timestamp, vm.Metric["datname"], vm.Metric["state"])] = value
	}

	rangeOverChunk := func(xChunk, yChunk []vmMetric) {
		xHashMap := make(map[string]vmMetric)
		for _, v := range xChunk {
			xHashMap[v.MetricHash()] = v
		}

		yHashMap := make(map[string]vmMetric)
		for _, v := range yChunk {
			yHashMap[v.MetricHash()] = v
		}

		for i, vmX := range xHashMap {
			if !checkIfPGMetric(vmX) {
				continue
			}

			xMap := make(map[int64]float64)
			for i, v := range vmX.Timestamps {
				xMap[v] = vmX.Values[i]
			}

			vmY, ok := yHashMap[i]
			if !ok {
				for timestamp, valueX := range xMap {
					saveTimestampToMap(timestamp, valueX, vmX, duplicateMapX)
				}
			}

			yMap := make(map[int64]float64)
			for i, v := range vmY.Timestamps {
				yMap[v] = vmY.Values[i]
			}

			for timestamp, valueX := range xMap {
				valueY, ok := yMap[timestamp]
				if !ok {
					saveTimestampToMap(timestamp, valueX, vmX, duplicateMapX)
					continue
				}
				saveTimestampToMap(timestamp, valueY, vmY, duplicateMapY)
			}
		}
	}

	for xFilename, xChunkData := range xChunkMap {
		yChunkData := yChunkMap[xFilename]
		dir, _ := path.Split(xFilename)
		st := dump.ParseSourceType(dir[:len(dir)-1])
		if st == dump.VictoriaMetrics {
			xChunk, err := vmParseChunk(xChunkData)
			if err != nil {
				return 0, 0, fmt.Errorf("failed to parse chunk %s: %w", xFilename, err)
			}
			yChunk, err := vmParseChunk(yChunkData)
			if err != nil {
				return 0, 0, fmt.Errorf("failed to parse chunk %s: %w", xFilename, err)
			}

			xValues := vmValuesCount(xChunk)
			yValues := vmValuesCount(yChunk)
			if xValues > yValues {
				totalValues += xValues
			} else {
				totalValues += yValues
			}

			rangeOverChunk(xChunk, yChunk)
		}
	}

	for timestamp, value := range duplicateMapX {
		_, ok := duplicateMapY[timestamp]
		if ok {
			totalDuplicate++
			continue
		}
		pmm.Logf("Value and timestamp not found for metric %s in second dump, value:%f", timestamp, value)
		totalNotDuplicate++
	}

	pmm.Logf("Found %d dublicates for pg_stat_activity_count, pg_stat_activity_max_tx_duration, pg_stat_activity_max_state_duration in first dump and removed them", totalDuplicate)
	return float64(totalDuplicate) / float64(totalValues), float64(totalNotDuplicate) / float64(totalValues), nil
}
