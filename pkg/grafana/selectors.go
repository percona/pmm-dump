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

package grafana

import (
	"fmt"
	"strings"

	"github.com/VictoriaMetrics/metricsql"
)

func parseQuery(query string, serviceNames []string, templateVars map[string]struct{}, existingSelectors map[string]struct{}) error {
	if query == "" {
		return nil
	}

	query = strings.ReplaceAll(query, "$interval", "1m")
	query = strings.ReplaceAll(query, "$node_name", "pmm-server")
	expr, err := metricsql.Parse(query)
	if err != nil {
		return err
	}
	metricsql.VisitAll(expr, func(expr metricsql.Expr) {
		m, ok := expr.(*metricsql.MetricExpr)
		if !ok {
			return
		}
		var filters []string
		for _, labelFilter := range m.LabelFilterss {
			for _, f := range labelFilter {
				var s string
				if f.Value == "$service_name" {
					continue
				} else if _, ok := templateVars[f.Value]; ok {
					continue
				} else {
					s += f.Label
					switch {
					case f.IsNegative && f.IsRegexp:
						s += "!~"
					case f.IsNegative && !f.IsRegexp:
						s += "!="
					case !f.IsNegative && f.IsRegexp:
						s += "=~"
					case !f.IsNegative && !f.IsRegexp:
						s += "="
					}
					s += fmt.Sprintf(`"%s"`, f.Value)
				}
				filters = append(filters, s)
			}
		}
		if len(serviceNames) == 1 {
			filters = append(filters, fmt.Sprintf("%s=~\"%s\"", "service_name", "^"+serviceNames[0]+"$"))
		} else if len(serviceNames) > 1 {
			filters = append(filters, fmt.Sprintf("%s=~\"%s\"", "service_name", "^("+strings.Join(serviceNames, "|")+")$"))
		}
		if len(filters) == 0 {
			return
		}
		s := fmt.Sprintf("{%s}", strings.Join(filters, ","))
		existingSelectors[s] = struct{}{}
	})
	return nil
}

func removeTemplatingFuncs(query string) string {
	funcNames := []string{
		"label_names",
		"label_values",
		"metrics",
		"query_result",
	}
	for _, f := range funcNames {
		if strings.HasPrefix(query, f) {
			query = strings.TrimPrefix(query, f)
			query = query[1 : len(query)-1]
			if f == "label_values" {
				_, err := metricsql.Parse(query)
				if err != nil {
					idx := strings.LastIndex(query, ",")
					query = query[:idx]
				}
			}
			return removeTemplatingFuncs(query)
		}
	}
	return query
}

func (p *panel) selectors(serviceNames []string, templateVars map[string]struct{}, existingSelectors map[string]struct{}) error {
	for _, v := range p.Templating.List {
		if v.Name == "interval" {
			continue
		}

		templateVars["$"+v.Name] = struct{}{}

		if err := parseQuery(removeTemplatingFuncs(v.Query), serviceNames, templateVars, existingSelectors); err != nil {
			return err
		}
	}

	for _, target := range p.Targets {
		if err := parseQuery(target.Expr, serviceNames, templateVars, existingSelectors); err != nil {
			return fmt.Errorf("failed to parse query \"%s\": %w", target.Expr, err)
		}
	}

	for _, panel := range p.Panels {
		if err := panel.selectors(serviceNames, templateVars, existingSelectors); err != nil {
			return err
		}
	}
	return nil
}

func (d *dashboardExprResp) parseSelectors(serviceNames []string) ([]string, error) {
	existingSelectors := make(map[string]struct{})
	templateVars := make(map[string]struct{})
	if err := d.Dashboard.selectors(serviceNames, templateVars, existingSelectors); err != nil {
		return nil, err
	}
	selectors := make([]string, 0, len(existingSelectors))
	for v := range existingSelectors {
		selectors = append(selectors, v)
	}
	return selectors, nil
}
