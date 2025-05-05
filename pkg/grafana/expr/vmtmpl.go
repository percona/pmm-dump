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

package expr

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/valyala/fasthttp"

	"pmm-dump/pkg/grafana/client"
	"pmm-dump/pkg/grafana/templating"
	"pmm-dump/pkg/grafana/types"
	"pmm-dump/pkg/victoriametrics"
)

func (p *VMExprParser) parseTemplatingQuery(v types.VariableModel) (templating.TemplatingVariable, error) {
	query, err := templating.GetQueryFromModel(v)
	if err != nil {
		return templating.TemplatingVariable{}, fmt.Errorf("get query from model: %w", err)
	}

	switch {
	case strings.HasPrefix(query, "metrics("):
		return templating.TemplatingVariable{}, errShouldIgnoreQuery
	case strings.HasPrefix(query, "query_result("):
		query, err = templating.InterpolateQuery(query, p.from, p.to, p.allVariables())
		if err != nil {
			return templating.TemplatingVariable{}, fmt.Errorf("interpolate query: %w", err)
		}
		query = strings.TrimPrefix(query, "query_result(")
		query = strings.TrimSuffix(query, ")")

		values, err := queryResult(p.c, p.pmmURL, query)
		if err != nil {
			return templating.TemplatingVariable{}, err
		}

		return templating.TemplatingVariable{
			Model:  v,
			Values: values,
		}, nil
	case strings.HasPrefix(query, "label_values("):
		query = strings.TrimPrefix(query, "label_values(")
		query = strings.TrimSuffix(query, ")")
		sep := strings.LastIndexByte(query, ',')
		var label string
		metric := strings.TrimSpace(query)
		if sep != -1 {
			metric = strings.TrimSpace(query[:sep])
			label = strings.TrimSpace(query[sep+1:])
		}

		_, ok := p.ignoredVars[strings.TrimPrefix(metric, "$")]
		if ok {
			label = ""
		}

		var values []string
		if label == "" {
			values, err = labelValuesSingleLabel(p.c, p.pmmURL, query, p.from, p.to)
			if err != nil {
				return templating.TemplatingVariable{}, err
			}
		} else {
			values, err = labelValues(p.c, p.pmmURL, label, metric, p.from, p.to, p.allVariables())
			if err != nil {
				return templating.TemplatingVariable{}, err
			}
		}

		return templating.TemplatingVariable{
			Model:  v,
			Values: values,
		}, nil
	}
	return templating.TemplatingVariable{}, fmt.Errorf("invalid query: %s", query)
}

func labelValuesSingleLabel(c *client.Client, pmmURL, label string, from, to time.Time) ([]string, error) {
	q := fasthttp.AcquireArgs()
	defer fasthttp.ReleaseArgs(q)
	q.Add("start", strconv.FormatInt(from.Unix(), 10))
	q.Add("end", strconv.FormatInt(to.Unix(), 10))

	status, data, err := c.Get(fmt.Sprintf("%s/prometheus/api/v1/label/%s/values?%s", pmmURL, label, q.String()))
	if err != nil {
		return nil, err
	}
	if status != fasthttp.StatusOK {
		return nil, fmt.Errorf("non-ok status: %d", status)
	}

	type VMQueryResp struct {
		Status string   `json:"status"`
		Data   []string `json:"data"`
	}
	resp := new(VMQueryResp)
	if err := json.Unmarshal(data, resp); err != nil {
		return nil, err
	}

	return resp.Data, nil
}

func labelValues(c *client.Client, pmmURL, label, metric string, from, to time.Time, vars []templating.TemplatingVariable) ([]string, error) {
	q := fasthttp.AcquireArgs()
	defer fasthttp.ReleaseArgs(q)
	metric, err := templating.InterpolateQuery(metric, from, to, vars)
	if err != nil {
		return nil, fmt.Errorf("interpolate query: %w", err)
	}
	label, err = templating.InterpolateQuery(label, from, to, vars)
	if err != nil {
		return nil, fmt.Errorf("interpolate query: %w", err)
	}

	q.Add("match[]", metric)
	q.Add("start", strconv.FormatInt(from.Unix(), 10))
	q.Add("end", strconv.FormatInt(to.Unix(), 10))

	status, data, err := c.Get(fmt.Sprintf("%s/prometheus/api/v1/series?%s", pmmURL, q.String()))
	if err != nil {
		return nil, err
	}
	if status != fasthttp.StatusOK {
		return nil, fmt.Errorf("non-ok status: %d", status)
	}
	type VMQueryResp struct {
		Status string              `json:"status"`
		Data   []map[string]string `json:"data"`
	}
	resp := new(VMQueryResp)
	if err := json.Unmarshal(data, resp); err != nil {
		return nil, err
	}
	values := make(map[string]struct{})
	for _, metric := range resp.Data {
		v, ok := metric[label]
		if !ok {
			continue
		}
		values[v] = struct{}{}
	}

	valuesSlice := make([]string, 0, len(values))
	for v := range values {
		valuesSlice = append(valuesSlice, v)
	}

	return valuesSlice, nil
}

func queryResult(c *client.Client, pmmURL, query string) ([]string, error) {
	q := fasthttp.AcquireArgs()
	defer fasthttp.ReleaseArgs(q)

	q.Add("query", query)
	q.Add("time", strconv.FormatInt(time.Now().Unix(), 10))

	status, data, err := c.Get(fmt.Sprintf("%s/prometheus/api/v1/query?%s", pmmURL, q.String()))
	if err != nil {
		return nil, err
	}
	if status != fasthttp.StatusOK {
		return nil, fmt.Errorf("non-ok status: %d", status)
	}
	resp := new(victoriametrics.MetricResponse)
	if err := json.Unmarshal(data, resp); err != nil {
		return nil, err
	}
	value, err := resp.GetValidValue()
	if err != nil {
		return nil, fmt.Errorf("get valid value: %w", err)
	}
	return []string{value}, nil
}
