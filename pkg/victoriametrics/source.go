package victoriametrics

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"pmm-dump/pkg/dump"
	"pmm-dump/pkg/grafana"
	"strconv"
	"time"

	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
	"github.com/valyala/fasthttp"
)

type Source struct {
	c   grafana.Client
	cfg Config
}

func NewSource(c grafana.Client, cfg Config) *Source {
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

func (s Source) WriteChunk(_ string, r io.Reader) error {
	chunkContent, err := ioutil.ReadAll(r)
	if err != nil {
		return errors.Wrap(err, "failed to read chunk content")
	}

	url := fmt.Sprintf("%s/api/v1/import", s.cfg.ConnectionURL)
	if s.cfg.NativeData {
		url = fmt.Sprintf("%s/api/v1/import/native", s.cfg.ConnectionURL)
	}

	req := fasthttp.AcquireRequest()
	defer fasthttp.ReleaseRequest(req)

	req.SetBody(chunkContent)
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
