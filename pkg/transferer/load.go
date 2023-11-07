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

package transferer

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/valyala/fasthttp"

	"pmm-dump/pkg/grafana"
)

type LoadStatus int

const (
	LoadStatusNone LoadStatus = iota
	LoadStatusOK
	LoadStatusWait
	LoadStatusTerminate

	MaxWaitStatusInSequence int = 10
)

func (s LoadStatus) String() string {
	switch s {
	case LoadStatusNone:
		return "NONE"
	case LoadStatusOK:
		return "OK"
	case LoadStatusWait:
		return "WAIT"
	case LoadStatusTerminate:
		return "TERMINATE"
	default:
		return "UNDEFINED"
	}
}

const (
	MaxLoadWaitDuration = time.Second
)

type LoadChecker struct {
	c             grafana.Client
	connectionURL string

	thresholds []Threshold

	m            sync.RWMutex
	latestStatus LoadStatus

	latestStatusCount int
}

func NewLoadChecker(ctx context.Context, c grafana.Client, url string, thresholds []Threshold) *LoadChecker {
	lc := &LoadChecker{
		c:             c,
		connectionURL: url,
		thresholds:    thresholds,
		latestStatus:  LoadStatusWait,
	}

	lc.updateStatus()

	if len(thresholds) > 0 { // nothing to check so no status updates
		lc.runStatusUpdate(ctx)
	}

	return lc
}

func (c *LoadChecker) GetLatestStatus() (LoadStatus, int) {
	c.m.RLock()
	defer c.m.RUnlock()
	return c.latestStatus, c.latestStatusCount
}

func (c *LoadChecker) setLatestStatus(s LoadStatus, count int) {
	c.m.Lock()
	defer c.m.Unlock()
	c.latestStatus = s
	c.latestStatusCount = count
}

func (c *LoadChecker) runStatusUpdate(ctx context.Context) {
	go func() {
		log.Debug().Msg("Started load status update")
		ticker := time.NewTicker(MaxLoadWaitDuration)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				log.Debug().Msgf("Context is done: stopping load status update")
				return
			case <-ticker.C:
				c.updateStatus()
			}
		}
	}()
}

func (c *LoadChecker) updateStatus() {
	status, err := c.checkMetricsLoad()
	if err != nil {
		status = LoadStatusWait
		log.Warn().Err(err).Msgf("Error while checking metrics load")
	}
	latestStatus, count := c.GetLatestStatus()
	if latestStatus == status {
		count++
	} else {
		count = 0
	}

	c.setLatestStatus(status, count)
	log.Debug().Msgf("Load status now is %v", status)
}

func (c *LoadChecker) checkMetricsLoad() (LoadStatus, error) {
	log.Debug().Msg("Started check load status")
	loadStatus := LoadStatusOK
	for _, t := range c.thresholds {
		var value float64
		var err error

		switch t.Key {
		case ThresholdMYRAM:
			rms := runtime.MemStats{}
			runtime.ReadMemStats(&rms)
			var vm *mem.VirtualMemoryStat
			vm, err = mem.VirtualMemory()
			if err == nil {
				value = float64(rms.Alloc) * 100 / float64(vm.Total)
			}
		default:
			value, err = c.getMetricCurrentValue(t)
		}

		if err != nil {
			return LoadStatusNone, fmt.Errorf("failed to retrieve threshold value for %s: %w", t.Key, err)
		}
		switch {
		case value >= t.CriticalLoad:
			log.Debug().Msgf("Checked %s threshold: it exceeds critical load limit. Terminating", t.Key)
			return LoadStatusTerminate, nil
		case value >= t.MaxLoad:
			log.Debug().Msgf("Checked %s threshold: it exceeds max load limit. Continue checking", t.Key)
			loadStatus = LoadStatusWait
		default:
			log.Debug().Msgf("Checked %s threshold: it's ok. Continue checking", t.Key)
		}
	}

	log.Debug().Msgf("Checked all thresholds: final status is %v", loadStatus)

	return loadStatus, nil
}

func (c *LoadChecker) getMetricCurrentValue(m Threshold) (float64, error) {
	q := fasthttp.AcquireArgs()
	defer fasthttp.ReleaseArgs(q)

	q.Add("query", m.Query)

	url := fmt.Sprintf("%s/api/v1/query?%s", c.connectionURL, q.String())

	log.Debug().
		Str("url", url).
		Msgf("Sending HTTP request to load checker endpoint")
	status, body, err := c.c.Get(url)
	if err != nil {
		return 0, errors.Wrap(err, "failed to send req to load checker endpoint")
	}
	if status != http.StatusOK {
		return 0, fmt.Errorf("non-ok response: status %d: %s", status, string(body))
	}
	log.Debug().Msg("Got HTTP status OK from load checker endpoint")

	var resp metricResponse

	if err = json.Unmarshal(body, &resp); err != nil {
		return 0, fmt.Errorf("error parsing thresholds: %w", err)
	}

	value, err := resp.getValidValue()
	if err != nil {
		return 0, fmt.Errorf("error parsing threshold: %w", err)
	}
	log.Debug().Msgf("Got %f threshold value", value)
	return value, nil
}

type ThresholdKey = string

const (
	ThresholdCPU   ThresholdKey = "CPU"
	ThresholdRAM   ThresholdKey = "RAM"
	ThresholdMYRAM ThresholdKey = "MYRAM"
)

func AllThresholdKeys() []ThresholdKey {
	return []ThresholdKey{ThresholdCPU, ThresholdRAM, ThresholdMYRAM}
}

func IsValidThresholdKey(v string) bool {
	for _, k := range AllThresholdKeys() {
		if k == v {
			return true
		}
	}
	return false
}

func getQueryByThresholdKey(k ThresholdKey) string {
	switch k {
	case ThresholdCPU:
		return `100 - (avg by (instance) (rate(node_cpu_seconds_total{mode="idle",node_name="pmm-server"}[5s])) * 100)`
	case ThresholdRAM:
		return `100 * (1 - ((avg_over_time(node_memory_MemFree_bytes{node_name="pmm-server"}[5s]) + avg_over_time(node_memory_Cached_bytes{node_name="pmm-server"}[5s]) + avg_over_time(node_memory_Buffers_bytes{node_name="pmm-server"}[5s])) / avg_over_time(node_memory_MemTotal_bytes{node_name="pmm-server"}[5s])))`
	case ThresholdMYRAM:
		return ""
	default:
		panic("BUG: undefined threshold key")
	}
}

type Threshold struct {
	Key          ThresholdKey
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

func ParseThresholdList(max, critical string) ([]Threshold, error) {
	maxV, err := parseThresholdValues(max)
	if err != nil {
		return nil, errors.Wrap(err, "invalid max load list")
	}

	criticalV, err := parseThresholdValues(critical)
	if err != nil {
		return nil, errors.Wrap(err, "invalid critical load list")
	}

	keys := AllThresholdKeys()
	thresholds := make([]Threshold, 0, len(keys))
	for _, k := range keys {
		maxLoad, maxOk := maxV[k]
		criticalLoad, criticalOk := criticalV[k]

		if !maxOk && !criticalOk {
			continue
		}

		thresholds = append(thresholds, Threshold{
			Key:          k,
			Query:        getQueryByThresholdKey(k),
			MaxLoad:      maxLoad,
			CriticalLoad: criticalLoad,
		})
	}

	return thresholds, nil
}

func parseThresholdValues(v string) (map[string]float64, error) {
	if v = strings.TrimSpace(v); v == "" {
		return nil, nil
	}

	res := make(map[string]float64)

	pairs := strings.Split(v, ",")
	for _, p := range pairs {
		separator := "="
		if strings.Contains(p, ":") {
			separator = ":"
		}

		values := strings.Split(p, separator)
		if len(values) != 2 {
			return nil, errors.New("invalid syntax: must be K=V or K:V")
		}

		k := strings.TrimSpace(values[0])
		if !IsValidThresholdKey(k) {
			return nil, fmt.Errorf("undefined key: %s", k)
		}

		v, err := strconv.ParseFloat(strings.TrimSpace(values[1]), 64)
		if err != nil {
			return nil, errors.Wrap(err, "can't parse number: %w")
		}

		res[k] = v
	}

	return res, nil
}
