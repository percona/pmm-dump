package victoriametrics

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"pmm-dump/pkg/dump"
	"pmm-dump/pkg/grafana"

	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
	"github.com/valyala/fasthttp"
)

type Source struct {
	c   *grafana.Client
	cfg Config
}

func NewSource(c *grafana.Client, cfg Config) *Source {
	if len(cfg.TimeSeriesSelectors) == 0 {
		cfg.TimeSeriesSelectors = []string{`{__name__=~".*"}`}
	}

	return &Source{
		c:   c,
		cfg: cfg,
	}
}

func (s Source) Type() dump.SourceType {
	return dump.VictoriaMetrics
}

const requestTimeout = time.Second * 30

func (s Source) ReadChunk(m dump.ChunkMeta) (*dump.Chunk, error) {
	q := fasthttp.AcquireArgs()
	defer fasthttp.ReleaseArgs(q)

	for _, v := range s.cfg.TimeSeriesSelectors {
		q.Add("match[]", v)
	}

	if m.Start != nil {
		q.Add("start", strconv.FormatInt(m.Start.Unix(), 10))
	}

	if m.End != nil {
		q.Add("end", strconv.FormatInt(m.End.Unix(), 10))
	}

	url := fmt.Sprintf("%s/api/v1/export?%s", s.cfg.ConnectionURL, q.String())
	if s.cfg.NativeData {
		url = fmt.Sprintf("%s/api/v1/export/native?%s", s.cfg.ConnectionURL, q.String())
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

	resp, err := s.c.DoWithTimeout(req, requestTimeout)
	defer fasthttp.ReleaseResponse(resp)
	if err != nil {
		return nil, errors.Wrap(err, "failed to send HTTP request to victoria metrics")
	}

	body := copyBytesArr(resp.Body())

	if status := resp.StatusCode(); status != fasthttp.StatusOK {
		return nil, errors.Errorf("non-OK response from victoria metrics: %d: %s", status, gzipDecode(body))
	}

	log.Debug().Msg("Got successful response from Victoria Metrics")

	chunk := &dump.Chunk{
		ChunkMeta: m,
		Content:   body,
		Filename:  m.String() + ".bin",
	}

	return chunk, nil
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

func decompressContent(content []byte) ([]byte, error) {
	r, err := gzip.NewReader(bytes.NewReader(content))
	if err != nil {
		return nil, errors.Wrap(err, "failed to create gzip reader")
	}
	defer r.Close()

	data, err := io.ReadAll(r)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read gzip data")
	}

	return data, nil
}

func decompressChunk(content []byte) ([]Metric, error) {
	r, err := gzip.NewReader(bytes.NewReader(content))
	if err != nil {
		return nil, errors.Wrap(err, "failed to create gzip reader")
	}
	defer r.Close()

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

func (s Source) splitChunkContent(chunkContent []byte, limit uint64) ([][]byte, error) {
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

func (s Source) splitMetrics(metricChunks [][]Metric, limit uint64) ([][]Metric, error) {
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
		if len(compressedData) > int(limit) {
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

	if s.cfg.ContentLimit > 0 && len(chunkContent) > int(s.cfg.ContentLimit) {
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

func (s *Source) sendChunk(content []byte) error {
	url := fmt.Sprintf("%s/api/v1/import", s.cfg.ConnectionURL)
	if s.cfg.NativeData {
		url = fmt.Sprintf("%s/api/v1/import/native", s.cfg.ConnectionURL)
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
	url := fmt.Sprintf("%s/internal/resetRollupResultCache", s.cfg.ConnectionURL)

	log.Debug().
		Str("url", url).
		Msg("Sending reset cache request to Victoria Metrics endpoint")

	status, body, err := s.c.GetWithTimeout(url, time.Second*30)
	if err != nil {
		return errors.Wrap(err, "failed to send HTTP request to victoria metrics")
	}

	if status != fasthttp.StatusOK {
		return errors.Errorf("non-OK response from victoria metrics: %d: %s", status, string(body))
	}

	log.Debug().Msg("Got successful response from Victoria Metrics")

	return nil
}

func SplitTimeRangeIntoChunks(start, end time.Time, delta time.Duration) (chunks []dump.ChunkMeta) {
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

	return
}
