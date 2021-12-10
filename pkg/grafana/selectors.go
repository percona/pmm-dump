package grafana

import (
	"fmt"
	"github.com/VictoriaMetrics/metricsql"
	"strings"
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
		if m, ok := expr.(*metricsql.MetricExpr); ok {
			var filters []string
			for _, f := range m.LabelFilters {
				var s string
				if f.Value == "$service_name" {
					if len(serviceNames) == 0 {
						continue
					}
					serviceName := strings.Join(serviceNames, "|")
					s += fmt.Sprintf("%s=~\"%s\"", f.Label, serviceName)
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
			if len(filters) == 0 {
				return
			}
			s := fmt.Sprintf("{%s}", strings.Join(filters, ","))
			existingSelectors[s] = struct{}{}
		}
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
			return fmt.Errorf("failed to parse query \"%s\": %v", target.Expr, err)
		}
	}

	for _, panel := range p.Panels {
		if err := panel.selectors(serviceNames, templateVars, existingSelectors); err != nil {
			return err
		}
	}
	return nil
}

func (d *dashboardExprResp) parseSelectors(serviceNames []string) (selectors []string, err error) {
	existingSelectors := make(map[string]struct{})
	templateVars := make(map[string]struct{})
	if err := d.Dashboard.selectors(serviceNames, templateVars, existingSelectors); err != nil {
		return nil, err
	}
	selectors = make([]string, 0, len(existingSelectors))
	for v := range existingSelectors {
		selectors = append(selectors, v)
	}
	return selectors, nil
}
