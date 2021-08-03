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

const (
	LoadStatusWaitSleepDuration = time.Second // TODO: make duration configurable
)

type TrLoadChecker struct {
	c             *fasthttp.Client
	connectionURL string

	thresholds []Threshold

	m            sync.RWMutex
	latestStatus LoadStatus
}

func NewLoadChecker(ctx context.Context, c *fasthttp.Client, url string) *TrLoadChecker {
	thresholds := []Threshold{
		{
			Key:          "cpu",
			Query:        `100 - (avg by (instance) (rate(node_cpu_seconds_total{mode="idle",node_name="pmm-server"}[1m])) * 100)`,
			MaxLoad:      50,
			CriticalLoad: 70,
		},
	}
	lc := &TrLoadChecker{
		c:             c,
		connectionURL: url,
		thresholds:    thresholds,
		latestStatus:  LoadStatusWait,
	}
	lc.runStatusUpdate(ctx)
	return lc
}

func (c *TrLoadChecker) GetLatestStatus() LoadStatus {
	c.m.RLock()
	defer c.m.RUnlock()
	return c.latestStatus
}

func (c *TrLoadChecker) setLatestStatus(s LoadStatus) {
	c.m.Lock()
	defer c.m.Unlock()
	c.latestStatus = s
}

func (c *TrLoadChecker) runStatusUpdate(ctx context.Context) {
	go func() {
		log.Debug().Msg("Started load status update")
		ticker := time.NewTicker(LoadStatusWaitSleepDuration)
		defer ticker.Stop()
		for range ticker.C {
			log.Debug().Msg("New load status update iteration started")
			select {
			case <-ctx.Done():
				log.Debug().Msg("Context is done, stopping to update load status")
				return
			default:
			}
			status, err := c.checkMetricsLoad()
			if err != nil {
				log.Error().Err(err)
				log.Debug().Msgf("Error while checking metrics load: %s. Skipping status update iteration", err)
				continue
			}
			c.setLatestStatus(status)
			log.Debug().Msg("Load status updated")
		}
	}()
}

func (c *TrLoadChecker) checkMetricsLoad() (LoadStatus, error) {
	log.Debug().Msg("Started check load status")
	respStatus := LoadStatusOK
	for _, t := range c.thresholds {
		value, err := c.getMetricCurrentValue(t)
		if err != nil {
			return LoadStatusNone, fmt.Errorf("failed to retrieve threshold value for %s: ", t.Key)
		}
		switch {
		case value >= t.CriticalLoad:
			log.Debug().Msgf("checked %s threshold: it's exceeds critical load limit. Stopping to check other thresholds", t.Key)
			return LoadStatusTerminate, nil
		case value >= t.MaxLoad:
			log.Debug().Msgf("checked %s threshold: it's exceeds max load limit", t.Key)
			respStatus = LoadStatusWait
		default:
			log.Debug().Msgf("checked %s threshold: it's ok", t.Key)
		}
	}
	switch respStatus {
	case LoadStatusWait:
		log.Debug().Msg("checked all thresholds: final result is wait load status")
	case LoadStatusOK:
		log.Debug().Msg("checked all thresholds: final result is ok load status")
	}
	return respStatus, nil
}

func (c *TrLoadChecker) getMetricCurrentValue(m Threshold) (float64, error) {
	q := fasthttp.AcquireArgs()
	defer fasthttp.ReleaseArgs(q)

	q.Add("query", m.Query)

	url := fmt.Sprintf("%s/api/v1/query?%s", c.connectionURL, q.String())

	log.Debug().Msgf("sending request to %s", url)
	status, body, err := c.c.Get(nil, url)
	if status != http.StatusOK {
		return 0, fmt.Errorf("non-ok response: status %d: %s", status, string(body))
	}
	log.Debug().Msg("got status OK")

	var resp metricResponse

	if err = json.Unmarshal(body, &resp); err != nil {
		return 0, fmt.Errorf("error parsing thresholds: %s", err)
	}

	value, err := resp.getValidValue()
	if err != nil {
		return 0, fmt.Errorf("error parsing threshold: %s", err)
	}
	log.Debug().Msgf("got %f threshold value", value)
	return value, nil
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
