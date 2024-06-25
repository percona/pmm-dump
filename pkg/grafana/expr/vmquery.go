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
	"fmt"
	"strings"

	"github.com/VictoriaMetrics/metricsql"
)

func (p *VMExprParser) parseQuery(query string) ([]string, error) {
	if query == "" {
		return nil, nil
	}

	expr, err := metricsql.Parse(query)
	if err != nil {
		return nil, err
	}
	var selectors []string
	metricsql.VisitAll(expr, func(expr metricsql.Expr) {
		m, ok := expr.(*metricsql.MetricExpr)
		if !ok {
			return
		}
		var filters []string
		for _, labelFilter := range m.LabelFilterss {
			for _, f := range labelFilter {
				var s string
				if _, ok := p.ignoredVars[f.Value]; ok {
					continue
				}
				
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
				
				filters = append(filters, s)
			}
		}
		if len(p.serviceNames) == 1 {
			filters = append(filters, fmt.Sprintf("%s=~\"%s\"", "service_name", "^"+p.serviceNames[0]+"$"))
		} else if len(p.serviceNames) > 1 {
			filters = append(filters, fmt.Sprintf("%s=~\"%s\"", "service_name", "^("+strings.Join(p.serviceNames, "|")+")$"))
		}
		if len(filters) == 0 {
			return
		}
		s := fmt.Sprintf("{%s}", strings.Join(filters, ","))
		selectors = append(selectors, s)
	})
	return selectors, nil
}
