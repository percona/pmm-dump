package victoriametrics

import (
	"fmt"
	"io"
	"io/ioutil"
	"pmm-transferer/pkg/dump"
	"strconv"
	"time"

	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
	"github.com/valyala/fasthttp"
)

type Source struct {
	c   *fasthttp.Client
	cfg Config
}

func NewSource(c *fasthttp.Client, cfg Config) *Source {
	if cfg.TimeSeriesSelector == "" {
		cfg.TimeSeriesSelector = `{__name__=~".*"}`
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

	q.Add("match[]", s.cfg.TimeSeriesSelector)

	if m.Start != nil {
		q.Add("start", strconv.FormatInt(m.Start.Unix(), 10))
	}

	if m.End != nil {
		q.Add("end", strconv.FormatInt(m.End.Unix(), 10))
	}

	url := fmt.Sprintf("%s/api/v1/export/native?%s", s.cfg.ConnectionURL, q.String())

	log.Debug().
		Stringer("timeout", requestTimeout).
		Str("url", url).
		Msg("Sending GET chunk request to Victoria Metrics endpoint")

	req := fasthttp.AcquireRequest()
	defer fasthttp.ReleaseRequest(req)

	req.Header.SetMethod(fasthttp.MethodGet)
	req.SetRequestURI(url)
	req.Header.Set(fasthttp.HeaderAcceptEncoding, "gzip")

	resp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseResponse(resp)

	if err := s.c.DoTimeout(req, resp, requestTimeout); err != nil {
		return nil, errors.Wrap(err, "failed to send HTTP request to victoria metrics")
	}

	body := copyBytesArr(resp.Body())

	if status := resp.StatusCode(); status != fasthttp.StatusOK {
		return nil, errors.Errorf("non-OK response from victoria metrics: %d: %s", status, body)
	}

	log.Debug().Msg("Got successful response from Victoria Metrics")

	chunk := &dump.Chunk{
		ChunkMeta: m,
		Content:   body,
		Filename:  m.String() + ".bin",
	}

	return chunk, nil
}

func copyBytesArr(a []byte) []byte {
	c := make([]byte, len(a))
	copy(c, a)
	return c
}

func (s Source) WriteChunk(_ string, r io.Reader) error {
	chunkContent, err := ioutil.ReadAll(r)
	if err != nil {
		return errors.Wrap(err, "failed to read chunk content")
	}

	url := fmt.Sprintf("%s/api/v1/import/native", s.cfg.ConnectionURL)

	req := fasthttp.AcquireRequest()
	defer fasthttp.ReleaseRequest(req)

	req.SetBody(chunkContent)
	req.Header.SetMethod(fasthttp.MethodPost)
	req.Header.Set(fasthttp.HeaderContentEncoding, "gzip")
	req.SetRequestURI(url)

	resp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseResponse(resp)

	log.Debug().
		Str("url", url).
		Msg("Sending POST chunk request to Victoria Metrics endpoint")

	if err = s.c.DoTimeout(req, resp, requestTimeout); err != nil {
		return errors.Wrap(err, "failed to send HTTP request to victoria metrics")
	}

	if s := resp.StatusCode(); s != fasthttp.StatusOK && s != fasthttp.StatusNoContent {
		return errors.Errorf("non-OK response from victoria metrics: %d: %s", s, string(resp.Body()))
	}

	log.Debug().Msg("Got successful response from Victoria Metrics")

	return nil
}

func (s Source) FinalizeWrites() error {
	url := fmt.Sprintf("%s/internal/resetRollupResultCache", s.cfg.ConnectionURL)

	log.Debug().
		Str("url", url).
		Msg("Sending reset cache request to Victoria Metrics endpoint")

	status, body, err := s.c.GetTimeout(nil, url, time.Second*30)
	if err != nil {
		return errors.Wrap(err, "failed to send HTTP request to victoria metrics")
	}

	if status != fasthttp.StatusOK {
		return errors.Errorf("non-OK response from victoria metrics: %d: %s", status, string(body))
	}

	log.Debug().Msg("Got successful response from Victoria Metrics")

	return nil
}

func SplitTimeRangeIntoChunks(start, end time.Time) (chunks []dump.ChunkMeta) {
	const delta = 15 * time.Minute

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
