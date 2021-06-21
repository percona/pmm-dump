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

	// TODO: configurable native/json formats
	url := fmt.Sprintf("%s/api/v1/export/native?%s", s.cfg.ConnectionURL, q.String())

	log.Debug().Msgf("Sending request to Victoria Metrics endpoint: %v", url)

	status, body, err := s.c.GetTimeout(nil, url, time.Second*30)
	if err != nil {
		return nil, errors.Wrap(err, "failed to send HTTP request to victoria metrics")
	}

	if status != fasthttp.StatusOK {
		return nil, errors.Errorf("non-OK response from victoria metrics: %d: %s", status, string(body))
	}

	chunk := &dump.Chunk{
		ChunkMeta: m,
		Content:   body,
		Filename:  m.String() + ".bin",
	}

	return chunk, nil
}

func (s Source) WriteChunk(_ string, r io.Reader) error {
	req := fasthttp.AcquireRequest()
	defer fasthttp.ReleaseRequest(req)

	chunkContent, err := ioutil.ReadAll(r)
	if err != nil {
		return errors.Wrap(err, "failed to read chunk content")
	}

	req.SetBody(chunkContent)
	req.Header.SetMethod(fasthttp.MethodPost)
	req.SetRequestURI(fmt.Sprintf("%s/api/v1/import/native", s.cfg.ConnectionURL))

	resp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseResponse(resp)

	if err = s.c.Do(req, resp); err != nil {
		return errors.Wrap(err, "failed to send HTTP request to victoria metrics")
	}

	if s := resp.StatusCode(); s != fasthttp.StatusOK && s != fasthttp.StatusNoContent {
		return errors.Errorf("non-OK response from victoria metrics: %d: %s", s, string(resp.Body()))
	}

	return nil
}

func (s Source) FinalizeWrites() error {
	url := fmt.Sprintf("%s/internal/resetRollupResultCache", s.cfg.ConnectionURL)

	status, body, err := s.c.GetTimeout(nil, url, time.Second*30)
	if err != nil {
		return errors.Wrap(err, "failed to send HTTP request to victoria metrics")
	}

	if status != fasthttp.StatusOK {
		return errors.Errorf("non-OK response from victoria metrics: %d: %s", status, string(body))
	}

	return nil
}
