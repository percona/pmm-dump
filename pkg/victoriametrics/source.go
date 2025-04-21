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

package victoriametrics

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
	"github.com/valyala/fasthttp"

	"pmm-dump/pkg/dump"
	"pmm-dump/pkg/grafana/client"
)

type Source struct {
	c   *client.Client
	cfg Config
}

func NewSource(c *client.Client, cfg *Config) *Source {
	if len(cfg.TimeSeriesSelectors) == 0 {
		cfg.TimeSeriesSelectors = []string{`{__name__=~".*"}`}
	}

	return &Source{
		c:   c,
		cfg: *cfg,
	}
}

func (s Source) Type() dump.SourceType {
	return dump.VictoriaMetrics
}

const requestTimeout = time.Second * 30

func (s Source) splitChunk(m dump.ChunkMeta) ([]*dump.Chunk, error) {
	if m.End.UnixMilli()-m.Start.UnixMilli() <= 1 {
		return nil, errors.New("Time range is smaller than millisecond, split is impossible, can only be fixed by increasing -search.maxSamplesPerQuery")
	}

	dur := m.End.Sub(*m.Start) / 2 //nolint:mnd
	t := m.Start.Add(dur)

	log.Info().Msg("Splitting chunk in to two parts")

	firstMeta := dump.ChunkMeta{
		Source: dump.VictoriaMetrics,
		Start:  m.Start,
		End:    &t,
	}

	secondMeta := dump.ChunkMeta{
		Source: dump.VictoriaMetrics,
		Start:  &t,
		End:    m.End,
	}

	firstPart, err := s.ReadChunks(firstMeta)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to read first chunk. Start time:"+m.Start.String()+"End time: "+t.String())
	}
	secondPart, err := s.ReadChunks(secondMeta)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to read second chunk. Start time:"+t.String()+"End time: "+m.End.String())
	}
	firstPart = append(firstPart, secondPart...)
	return firstPart, nil
}

func (s Source) ReadChunks(m dump.ChunkMeta) ([]*dump.Chunk, error) {
	body, status, err := ReadChunk(s.c, m.Start, m.End, s.cfg.NativeData, s.cfg.ConnectionURL, s.cfg.TimeSeriesSelectors)
	if status == fasthttp.StatusBadRequest && strings.Contains(gzipDecode(body), "cannot select more than -search.maxSamplesPerQuery") {
		c, err := s.splitChunk(m)
		if err != nil {
			return nil, errors.Wrap(err, "Failed to split VM chunk")
		}
		log.Debug().Msg("VM chunk was split into several parts")
		return c, nil
	}

	if err != nil {
		return nil, errors.Wrap(err, "Error getting responce from Victoria Metrics")
	}

	log.Debug().Msg("Got successful response from Victoria Metrics")

	metrics, err := ParseMetrics(bytes.NewReader([]byte(gzipDecode(body))))
	if err != nil {
		return nil, errors.Wrap(err, "Failed to parse metrics from body")
	}
	samples := 0
	for _, m := range metrics {
		samples += len(m.Timestamps)
	}
	if samples != 0 {
		log.Debug().Msg(fmt.Sprintln("Got samples from reading: ", samples))
	}

	chunk := &dump.Chunk{
		ChunkMeta: m,
		Content:   body,
		Filename:  m.String() + ".bin",
	}

	return []*dump.Chunk{chunk}, nil
}

func ReadChunk(c *client.Client, startTime, endTime *time.Time, nativeData bool, host string, selectors []string) ([]byte, int, error) {
	q := fasthttp.AcquireArgs()
	defer fasthttp.ReleaseArgs(q)

	for _, v := range selectors {
		q.Add("match[]", v)
	}

	const RFC3339Milli = "2006-01-02T15:04:05.000Z07:00"

	if startTime != nil {
		q.Add("start", startTime.UTC().Format(RFC3339Milli))
	}

	if endTime != nil {
		q.Add("end", endTime.UTC().Format(RFC3339Milli))
	}
	log.Info().Msg("start: " + startTime.UTC().Format(RFC3339Milli))
	log.Info().Msg("end: " + endTime.UTC().Format(RFC3339Milli))

	url := fmt.Sprintf("%s/api/v1/export?%s", host, q.String())
	if nativeData {
		url = fmt.Sprintf("%s/api/v1/export/native?%s", host, q.String())
	}

	log.Debug().
		Stringer("timeout", requestTimeout).
		Str("url", url).
		Msg("Sending GET chunk request to Victoria Metrics endpoint")

	req := fasthttp.AcquireRequest()
	defer fasthttp.ReleaseRequest(req)

	req.Header.SetMethod(fasthttp.MethodGet)
	req.SetRequestURI(url)
	req.Header.Set(fasthttp.HeaderAcceptEncoding, "gzip")

	resp, err := c.DoWithTimeout(req, requestTimeout)
	defer fasthttp.ReleaseResponse(resp)
	if err != nil {
		return nil, 0, errors.Wrap(err, "failed to send HTTP request to victoria metrics")
	}

	body := copyBytesArr(resp.Body())

	return body, resp.StatusCode(), nil
}

func gzipDecode(data []byte) string {
	r, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return string(data)
	}
	result, err := io.ReadAll(r)
	if err != nil {
		return string(data)
	}
	return string(result)
}

func copyBytesArr(a []byte) []byte {
	c := make([]byte, len(a))
	copy(c, a)
	return c
}

const (
	errRequestEntityTooLarge = `received "413 Request Entity Too Large" error from PMM`
)

func decompressChunk(content []byte) ([]Metric, error) {
	r, err := gzip.NewReader(bytes.NewReader(content))
	if err != nil {
		return nil, errors.Wrap(err, "failed to create gzip reader")
	}
	defer r.Close() //nolint:errcheck

	metrics, err := ParseMetrics(r)
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse chunk content")
	}
	return metrics, nil
}

func compressChunk(chunk []Metric) ([]byte, error) {
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	for _, metric := range chunk {
		metricData, err := json.Marshal(metric)
		if err != nil {
			return nil, errors.Wrap(err, "failed to marshal metric")
		}
		if _, err := w.Write(metricData); err != nil {
			return nil, errors.Wrap(err, "failed to write gzip data")
		}
	}
	if err := w.Close(); err != nil {
		return nil, errors.Wrap(err, "failed to close gzip writer")
	}
	return buf.Bytes(), nil
}

func (s Source) splitChunkContent(chunkContent []byte, limit int) ([][]byte, error) {
	metrics, err := decompressChunk(chunkContent)
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse chunk content")
	}

	chunks, err := s.splitMetrics([][]Metric{metrics}, limit)
	if err != nil {
		return nil, errors.Wrap(err, "failed to split metrics")
	}

	data := make([][]byte, 0, len(chunks))
	for _, chunk := range chunks {
		compressedContent, err := compressChunk(chunk)
		if err != nil {
			return nil, errors.Wrap(err, "failed to compress chunk content")
		}
		data = append(data, compressedContent)
	}

	return data, nil
}

func (s Source) splitMetrics(metricChunks [][]Metric, limit int) ([][]Metric, error) {
	newMetricChunks := make([][]Metric, 0, len(metricChunks))

	for _, chunk := range metricChunks {
		if len(chunk) <= 1 {
			newMetricChunks = append(newMetricChunks, chunk)
			continue
		}
		newMetricChunks = append(newMetricChunks, chunk[:len(chunk)/2])
		newMetricChunks = append(newMetricChunks, chunk[len(chunk)/2:])
	}

	if len(newMetricChunks) == len(metricChunks) {
		return nil, errors.New("unable to split metrics: content limit is too small")
	}

	for _, chunk := range newMetricChunks {
		compressedData, err := compressChunk(chunk)
		if err != nil {
			return nil, errors.Wrap(err, "failed to compress metrics")
		}
		if len(compressedData) > limit {
			return s.splitMetrics(newMetricChunks, limit)
		}
	}
	return newMetricChunks, nil
}

func (s Source) WriteChunk(filename string, r io.Reader) error {
	if s.cfg.ContentLimit != 0 && s.cfg.NativeData {
		return errors.New("content limit is not supported for native data")
	}
	chunkContent, err := io.ReadAll(r)
	if err != nil {
		return errors.Wrap(err, "failed to read chunk content")
	}

	if s.cfg.ContentLimit > 0 && len(chunkContent) > s.cfg.ContentLimit {
		chunks, err := s.splitChunkContent(chunkContent, s.cfg.ContentLimit)
		if err != nil {
			return errors.Wrap(err, "failed to split chunk content")
		}
		for i, chunk := range chunks {
			if err := s.sendChunk(chunk); err != nil {
				return errors.Wrapf(err, "failed to send splitted chunk %s/%d", filename, i+1)
			}
		}

		return nil
	}

	if err := s.sendChunk(chunkContent); err != nil {
		return errors.Wrapf(err, "failed to send chunk %s", filename)
	}

	return nil
}

func (s Source) sendChunk(content []byte) error {
	url := s.cfg.ConnectionURL + "/api/v1/import"
	if s.cfg.NativeData {
		url = s.cfg.ConnectionURL + "/api/v1/import/native"
	}

	req := fasthttp.AcquireRequest()
	defer fasthttp.ReleaseRequest(req)

	req.SetBody(content)
	req.Header.SetMethod(fasthttp.MethodPost)
	req.Header.Set(fasthttp.HeaderContentEncoding, "gzip")
	req.SetRequestURI(url)

	log.Debug().
		Str("url", url).
		Msg("Sending POST chunk request to Victoria Metrics endpoint")

	resp, err := s.c.DoWithTimeout(req, requestTimeout)
	defer fasthttp.ReleaseResponse(resp)
	if err != nil {
		return errors.Wrap(err, "failed to send HTTP request to victoria metrics")
	}

	if s := resp.StatusCode(); s != fasthttp.StatusOK && s != fasthttp.StatusNoContent {
		if s == http.StatusRequestEntityTooLarge {
			return errors.New(errRequestEntityTooLarge)
		}
		return errors.Errorf("non-OK response from victoria metrics: %d: %s", s, gzipDecode(resp.Body()))
	}

	log.Debug().Msg("Got successful response from Victoria Metrics")
	return nil
}

func ErrIsRequestEntityTooLarge(err error) bool {
	if err.Error() == errRequestEntityTooLarge || errors.Cause(err).Error() == errRequestEntityTooLarge {
		return true
	}
	return false
}

func (s Source) FinalizeWrites() error {
	url := s.cfg.ConnectionURL + "/internal/resetRollupResultCache"

	log.Debug().
		Str("url", url).
		Msg("Sending reset cache request to Victoria Metrics endpoint")

	status, body, err := s.c.GetWithTimeout(url, requestTimeout)
	if err != nil {
		return errors.Wrap(err, "failed to send HTTP request to victoria metrics")
	}

	if status != fasthttp.StatusOK {
		return errors.Errorf("non-OK response from victoria metrics: %d: %s", status, string(body))
	}

	log.Debug().Msg("Got successful response from Victoria Metrics")

	return nil
}

func (s Source) HasMetrics(start, end time.Time) (bool, error) {
	q := fasthttp.AcquireArgs()
	defer fasthttp.ReleaseArgs(q)
	req := fasthttp.AcquireRequest()
	defer fasthttp.ReleaseRequest(req)
	query := ""
	for i, v := range s.cfg.TimeSeriesSelectors {
		if i != 0 {
			query += " and "
		}
		query += "absent(" + v + ")"
	}
	q.Add("time", strconv.FormatInt(end.Unix(), 10))
	q.Add("step", strconv.Itoa(int(end.Sub(start).Seconds())+1)+"s")
	q.Add("query", query)

	url := fmt.Sprintf("%s/api/v1/query?%s", s.cfg.ConnectionURL, q.String())

	log.Debug().
		Stringer("timeout", requestTimeout).
		Str("url", url).
		Msg("Sending GET query request to Victoria Metrics endpoint")

	req.Header.SetMethod(fasthttp.MethodGet)
	req.SetRequestURI(url)
	req.Header.Set(fasthttp.HeaderAcceptEncoding, "gzip")

	resp, err := s.c.DoWithTimeout(req, requestTimeout)
	defer fasthttp.ReleaseResponse(resp)

	if err != nil {
		return false, errors.Wrap(err, "failed to send HTTP request to victoria metrics")
	}

	body := gzipDecode(copyBytesArr(resp.Body()))

	if status := resp.StatusCode(); status != fasthttp.StatusOK {
		if strings.Contains(body, "cannot select more than -search.maxSamplesPerQuery") {
			return s.splitContainsMetrics(start, end, body)
		} else {
			return false, errors.Errorf("non-OK response from victoria metrics: %d: %s", status, body)
		}
	}

	log.Debug().Msg("Got successful response from Victoria Metrics")

	metricsResp := new(MetricResponse)
	if err := json.Unmarshal([]byte(body), metricsResp); err != nil {
		return false, errors.Wrap(err, "failed to unmarshal metrics response")
	}

	if metricsResp.Stats.SeriesFetched == "0" {
		log.Debug().Msg("Series fetched 0")
		return false, nil
	}
	return true, nil
}

func (s Source) splitContainsMetrics(start, end time.Time, body string) (bool, error) {
	log.Debug().Msg("Too big samples size, trying to split")

	diff := end.Sub(start) / 2 //nolint:mnd
	middle := start.Add(diff)
	log.Debug().Msg("Time range for first part: Start:" + start.String() + " End:" + middle.String() + " ")
	firstPart, err := s.HasMetrics(start, middle)
	if err != nil && !strings.Contains(body, "cannot select more than -search.maxSamplesPerQuery") {
		return false, errors.Errorf("non-OK response from victoria metrics: %s", body)
	}

	if firstPart {
		log.Debug().Msg("First part has metrics")
		return true, nil
	}
	log.Debug().Msg("Time range for first part: Start:" + middle.String() + "End:" + end.String() + " ")
	secondPart, err := s.HasMetrics(middle, end)
	if err != nil && !strings.Contains(body, "cannot select more than -search.maxSamplesPerQuery") {
		return false, errors.Errorf("non-OK response from victoria metrics: %s", body)
	}
	if secondPart {
		log.Debug().Msg("Second part has metrics")
		return true, nil
	}
	return false, errors.New("Can't find metrics")
}

func SplitTimeRangeIntoChunks(start, end time.Time, delta time.Duration) []dump.ChunkMeta {
	var chunks []dump.ChunkMeta
	chunkStart := start
	for {
		s, e := chunkStart, chunkStart.Add(delta)
		chunks = append(chunks, dump.ChunkMeta{
			Source: dump.VictoriaMetrics,
			Start:  &s,
			End:    &e,
		})

		chunkStart = e
		if chunkStart.After(end) {
			break
		}
	}

	log.Debug().
		Time("start", start).
		Time("end", end).
		Stringer("chunk_size", delta).
		Int("chunks", len(chunks)).
		Msg("Split Victoria Metrics timerange into chunks")

	return chunks
}
