package e2e

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/pkg/errors"

	"pmm-dump/internal/test/util"
	"pmm-dump/pkg/dump"
	"pmm-dump/pkg/victoriametrics"
)

func TestValidate(t *testing.T) {
	pmm := util.NewPMM(t, "validate", ".env.test")
	newPMM := util.NewPMM(t, "validate-2", ".env2.test")
	pmm.Stop()
	newPMM.Stop()

	var b util.Binary
	tmpDir := util.TestDir(t, "validate-test")
	xDumpPath := filepath.Join(tmpDir, "dump.tar.gz")
	yDumpPath := filepath.Join(tmpDir, "dump2.tar.gz")
	chunkTimeRange := time.Second * 30

	pmm.Deploy()

	start := time.Now().UTC()
	t.Log("Sleeping for 120 seconds")
	time.Sleep(time.Second * 120)
	end := time.Now().UTC()

	t.Log("Exporting data to", xDumpPath, start, end)
	stdout, stderr, err := b.Run(
		"export",
		"--ignore-load",
		"-d", xDumpPath,
		"--pmm-url", pmm.PMMURL(),
		"--dump-qan",
		"--click-house-url", pmm.ClickhouseURL(),
		"--start-ts", start.Format(time.RFC3339),
		"--end-ts", end.Format(time.RFC3339),
		"--chunk-time-range", chunkTimeRange.String())
	if err != nil {
		t.Fatal("failed to export", err, stdout, stderr)
	}

	t.Logf("Sleeping for %d seconds", int(chunkTimeRange.Seconds()))
	time.Sleep(chunkTimeRange)

	newPMM.Deploy()

	t.Log("Importing data from", xDumpPath)
	stdout, stderr, err = b.Run(
		"import",
		"-d", xDumpPath,
		"--pmm-url", newPMM.PMMURL(),
		"--dump-qan",
		"--click-house-url", newPMM.ClickhouseURL())
	if err != nil {
		t.Fatal("failed to import", err, stdout, stderr)
	}

	t.Log("Sleeping for 10 seconds")
	time.Sleep(time.Second * 10)

	t.Log("Exporting data to", yDumpPath)
	stdout, stderr, err = b.Run(
		"export",
		"--ignore-load",
		"-d", yDumpPath,
		"--pmm-url", newPMM.PMMURL(),
		"--dump-qan",
		"--click-house-url", newPMM.ClickhouseURL(),
		"--start-ts", start.Format(time.RFC3339), "--end-ts", end.Format(time.RFC3339),
		"--chunk-time-range", chunkTimeRange.String())
	if err != nil {
		t.Fatal("failed to import", err, stdout, stderr)
	}

	loss, err := validateChunks(t, xDumpPath, yDumpPath)
	if err != nil {
		t.Fatal("failed to validate chunks", err)
	}
	if loss > 0.001 {
		t.Fatalf("too much data loss %f%%", loss*100)
	}
	t.Logf("data loss is %f%%", loss*100)

	pmm.Stop()
	newPMM.Stop()
}

func validateChunks(t *testing.T, xDump, yDump string) (float64, error) {
	t.Helper()

	xChunkMap, err := readChunks(xDump)
	if err != nil {
		return 0, errors.Wrapf(err, "failed to read dump %s", xDump)
	}
	yChunkMap, err := readChunks(yDump)
	if err != nil {
		return 0, errors.Wrapf(err, "failed to read dump %s", yDump)
	}

	if len(xChunkMap) != len(yChunkMap) {
		return 0, errors.Wrapf(err, "number of chunks is different in %s = %d and %s = %d", xDump, len(xChunkMap), yDump, len(yChunkMap))
	}
	var totalValues, totalMissingValues int

	for xFilename, xChunkData := range xChunkMap {
		yChunkData, ok := yChunkMap[xFilename]
		if !ok {
			return 0, errors.Errorf("chunk %s is missing in %s", xFilename, yDump)
		}
		dir, _ := path.Split(xFilename)
		st := dump.ParseSourceType(dir[:len(dir)-1])
		switch st {
		case dump.VictoriaMetrics:
			xChunk, err := vmParseChunk(xChunkData)
			if err != nil {
				return 0, errors.Wrapf(err, "failed to parse chunk %s", xFilename)
			}
			yChunk, err := vmParseChunk(yChunkData)
			if err != nil {
				return 0, errors.Wrapf(err, "failed to parse chunk %s", xFilename)
			}

			xValues := vmValuesCount(xChunk)
			yValues := vmValuesCount(yChunk)
			if xValues > yValues {
				totalValues += xValues
			} else {
				totalValues += yValues
			}

			missingValues, err := vmCompareChunkData(t, xChunk, yChunk)
			if err != nil {
				return 0, errors.Wrapf(err, "failed to compare chunk %s", xFilename)
			}

			totalMissingValues += missingValues
		case dump.ClickHouse:
			if !reflect.DeepEqual(xChunkData, yChunkData) {
				return 0, errors.Errorf("chunk %s is different", xFilename)
			}
		default:
			return 0, errors.Errorf("unknown source type %s", st)
		}
	}
	return float64(totalMissingValues) / float64(totalValues), nil
}

func vmValuesCount(xChunk []vmMetric) int {
	total := 0
	for _, v := range xChunk {
		total += len(v.Values)
	}
	return total
}

func vmCompareChunkData(t *testing.T, xChunk, yChunk []vmMetric) (int, error) {
	t.Helper()

	if len(xChunk) != len(yChunk) {
		return 0, errors.Errorf("len(x)=%d, len(y)=%d", len(xChunk), len(yChunk))
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

		currentLoss := xMetric.CompareTimestampValues(t, yMetric)
		if currentLoss > 0 {
			loss += currentLoss
			continue
		}

		delete(xHashMap, k)
		delete(yHashMap, k)
	}

	return loss, nil
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
		return nil, errors.Wrap(err, "failed to open as gzip")
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
			return nil, errors.Errorf("corrupted dump: found unknown file %s", filename)
		}

		st := dump.ParseSourceType(dir[:len(dir)-1])
		if st == dump.UndefinedSource {
			return nil, errors.Errorf("corrupted dump: found undefined source: %s", dir)
		}

		content, err := io.ReadAll(tr)
		if err != nil {
			return nil, errors.Wrap(err, "failed to read chunk content")
		}

		if len(content) == 0 {
			continue
		}

		chunkMap[header.Name] = content
	}
	return chunkMap, nil
}

func vmParseChunk(data []byte) ([]vmMetric, error) {
	r, err := gzip.NewReader(bytes.NewBuffer(data))
	if err != nil {
		return nil, errors.Wrap(err, "failed to create reader")
	}
	defer r.Close() //nolint:errcheck
	metrics, err := victoriametrics.ParseMetrics(r)
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse metrics")
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
	return fmt.Sprintf(`{"__name__": "%s", "job": "%s", "instance": "%s", "agent_id": "%s", "agent_type": "%s"}`, m["__name__"], m["job"], m["instance"], m["agent_id"], m["agent_type"])
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

func (vm vmMetric) CompareTimestampValues(t *testing.T, with vmMetric) int {
	t.Helper()

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
			if t != nil {
				t.Logf("Value and timestamp not found for metric %s in second dump: wanted %v for %d", vm.MetricString(), xValue, timestamp)
			}
			continue
		}
		if xValue != yValue {
			if t != nil {
				t.Logf("Values for timestamp %d in metric %s are not the same: %v and %v", timestamp, vm.MetricString(), xValue, yValue)
			}
			continue
		}
		delete(xMap, timestamp)
		delete(yMap, timestamp)
	}

	for timestamp, yValue := range yMap {
		_, ok := xMap[timestamp]
		if !ok {
			if t != nil {
				t.Logf("Value and timestamp not found for metric %s in first dump: wanted %v for %d", vm.MetricString(), yValue, timestamp)
			}
			continue
		}
	}

	return int(math.Abs(float64(len(xMap) - len(yMap))))
}
