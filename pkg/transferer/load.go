package transferer

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
	"github.com/valyala/fasthttp"
	"net/http"
	"strconv"
	"sync"
	"time"
)

type LoadStatus int

const (
	LoadStatusNone LoadStatus = iota
	LoadStatusOK
	LoadStatusWait
	LoadStatusTerminate
)

type LoadChecker struct {
	c             *fasthttp.Client
	connectionURL string

	thresholds []Threshold

	m            sync.RWMutex
	latestStatus LoadStatus
}

func NewLoadChecker(ctx context.Context, c *fasthttp.Client, url string) *LoadChecker {
	thresholds := []Threshold{
		{
			Key:          "cpu",
			Query:        `100 - (avg by (instance) (rate(node_cpu_seconds_total{mode="idle",node_name="pmm-server"}[1m])) * 100)`,
			MaxLoad:      50,
			CriticalLoad: 70,
		},
	}
	lc := &LoadChecker{
		c:             c,
		connectionURL: url,
		thresholds:    thresholds,
		latestStatus:  LoadStatusNone,
	}
	lc.runStatusUpdate(ctx)
	return lc
}

func (c *LoadChecker) GetLatestStatus() LoadStatus {
	c.m.RLock()
	defer c.m.RUnlock()
	return c.latestStatus
}

func (c *LoadChecker) setLatestStatus(s LoadStatus) {
	c.m.Lock()
	defer c.m.Unlock()
	c.latestStatus = s
}

func (c *LoadChecker) runStatusUpdate(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(time.Second) // TODO: make duration configurable
		defer ticker.Stop()
		for range ticker.C {
			select {
			case <-ctx.Done():
				return
			default:
			}
			status, err := c.checkMetricsLoad()
			if err != nil {
				log.Error().Err(err)
				continue
			}
			c.setLatestStatus(status)
		}
	}()
}

func (c *LoadChecker) checkMetricsLoad() (LoadStatus, error) {
	respStatus := LoadStatusOK
	for _, t := range c.thresholds {
		value, err := c.getMetricCurrentValue(t)
		if err != nil {
			return LoadStatusNone, fmt.Errorf("failed to retrieve threshold value for %s: ", t.Key)
		}
		switch {
		case value >= t.CriticalLoad:
			return LoadStatusTerminate, nil
		case value >= t.MaxLoad:
			respStatus = LoadStatusWait
		}
	}
	return respStatus, nil
}

func (c *LoadChecker) getMetricCurrentValue(m Threshold) (float64, error) {
	q := fasthttp.AcquireArgs()
	defer fasthttp.ReleaseArgs(q)

	q.Add("query", m.Query)

	url := fmt.Sprintf("%s/api/v1/query?%s", c.connectionURL, q.String())

	status, body, err := c.c.Get(nil, url)
	if status != http.StatusOK {
		return 0, fmt.Errorf("non-ok response: status %d: %s", status, string(body))
	}

	var resp metricResponse

	if err = json.Unmarshal(body, &resp); err != nil {
		return 0, fmt.Errorf("error parsing thresholds: %s", err)
	}

	return resp.getValidValue()
}

type Threshold struct {
	Key          string
	Query        string
	MaxLoad      float64
	CriticalLoad float64
}

type metricResponse struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Metric struct {
				Instance string `json:"instance"`
			} `json:"metric"`
			Value []interface{} `json:"value"`
		} `json:"result"`
	} `json:"data"`
}

func (r *metricResponse) getValidValue() (float64, error) {
	if r.Status != "success" {
		return 0, errors.New("status is not success")
	}
	if len(r.Data.Result) == 0 {
		return 0, errors.New("empty result")
	}
	if len(r.Data.Result[0].Value) != 2 {
		return 0, errors.New("unexpected number of values")
	}
	str, ok := r.Data.Result[0].Value[1].(string)
	if !ok {
		return 0, errors.New("value is not string")
	}
	val, err := strconv.ParseFloat(str, 64)
	if err != nil {
		return 0, fmt.Errorf("parsing value error: %s", err.Error())
	}
	return val, nil
}
