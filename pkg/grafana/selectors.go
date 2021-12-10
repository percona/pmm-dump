package grafana

import (
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/promql/parser"
	"github.com/rs/zerolog/log"
	"strings"
)

func parseQuery(query string, serviceNames []string, templateVars map[string]struct{}, existingSelectors map[string]struct{}) error {
	if query == "" {
		return nil
	}

	query = strings.ReplaceAll(query, "$interval", "1m")
	query = strings.ReplaceAll(query, "$node_name", "pmm-server")
	expr, err := parser.ParseExpr(query)
	if err != nil {
		return err
	}
	extractedSelectors := parser.ExtractSelectors(expr)
	for _, sel := range extractedSelectors {
		s := "{"
		for i, v := range sel {
			if v.Value == "$service_name" {
				if len(serviceNames) == 0 {
					continue
				}
				serviceName := strings.Join(serviceNames, "|")
				v.Value = serviceName
				v.Type = labels.MatchRegexp
			} else if _, ok := templateVars[v.Value]; ok {
				continue
			}
			s += v.String()
			if i+1 < len(sel) {
				s += ", "
			}
		}
		s += "}"
		if s != "{}" {
			existingSelectors[s] = struct{}{}
		}
	}
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
				_, err := parser.ParseExpr(query)
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
			log.Err(err).Msgf("failed to parse query \"%s\": %v", target.Expr, err)
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
