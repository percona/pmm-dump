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
	"strconv"
	"time"

	"github.com/grafana/grafana/pkg/kinds/dashboard"
	"github.com/pkg/errors"

	"pmm-dump/pkg/grafana/client"
	"pmm-dump/pkg/grafana/templating"
	"pmm-dump/pkg/grafana/types"
)

type VMExprParser struct {
	ignoredVars  map[string]struct{}
	serviceNames []string

	c        *client.Client
	pmmURL   string
	from     time.Time
	to       time.Time
	varOrder []string
	vars     map[string]templating.TemplatingVariable
}

func (p *VMExprParser) allVariables() []templating.TemplatingVariable {
	s := make([]templating.TemplatingVariable, 0, len(p.vars))
	for _, k := range p.varOrder {
		s = append(s, p.vars[k])
	}
	for name := range p.ignoredVars {
		s = append(s, templating.TemplatingVariable{
			Model: types.VariableModel{
				Name: name,
			},
			Values: []string{},
		})
	}
	return s
}

const (
	defaultRate     = time.Second * 10
	defaultInterval = time.Minute
)

func durationToStr(dur time.Duration, suffix string) string {
	switch suffix {
	case "s":
		return strconv.Itoa(int(dur.Seconds())) + suffix
	case "ms":
		return strconv.Itoa(int(dur.Milliseconds())) + suffix
	case "m":
		return strconv.Itoa(int(dur.Minutes())) + suffix
	}
	return ""
}

func NewVMParser(dashboard types.DashboardPanel, serviceNames []string, c *client.Client, vmURL string, from time.Time, to time.Time) *VMExprParser {
	newVar := func(name string, values ...string) templating.TemplatingVariable {
		return templating.TemplatingVariable{
			Model: types.VariableModel{
				Name: name,
			},
			Values: values,
		}
	}
	vars := []templating.TemplatingVariable{
		newVar(globalVarIntervalMs, durationToStr(defaultInterval, "ms")),
		newVar(globalVarInterval, durationToStr(defaultInterval, "m")),

		newVar(globalVarRangeS, durationToStr(to.Sub(from), "s")),
		newVar(globalVarRangeMs, durationToStr(to.Sub(from), "ms")),
		newVar(globalVarRange, durationToStr(to.Sub(from), "m")),
		newVar(globalVarRateIntervalMs, durationToStr(defaultRate, "ms")),
		newVar(globalVarRateInterval, durationToStr(defaultRate, "s")),

		newVar(globalVarTimezone, "utc"),

		newVar(globalVarDashboard, dashboard.Title),
	}
	ignoredVars := []string{
		string(globalVarOrg),
		string(globalVarUser),
		string(globalVarUserLogin),
		string(globalVarUserEmail),
		string(globalVarTimeFilter),
		string(globalVarTimeFilterUnderscore),
	}
	ivM := make(map[string]struct{}, len(ignoredVars))
	for _, v := range ignoredVars {
		ivM[v] = struct{}{}
	}
	vM := make(map[string]templating.TemplatingVariable, len(vars))
	vOrder := make([]string, 0, len(vars))
	for _, v := range vars {
		vOrder = append(vOrder, v.Name())
		vM[v.Name()] = v
	}
	return &VMExprParser{
		ignoredVars:  ivM,
		serviceNames: serviceNames,
		c:            c,
		from:         from,
		to:           to,
		vars:         vM,
		pmmURL:       vmURL,
		varOrder:     vOrder,
	}
}

const VMDatasourceName = "Metrics"

func (p *VMExprParser) GetSelectors(dashboard types.DashboardPanel) ([]string, error) {
	selectorMap := make(map[string]struct{})

	err := p.parseTemplatingVars(dashboard.Templating.List)
	if err != nil {
		return nil, errors.Wrap(err, "parse templating vars")
	}

	for _, target := range dashboard.Targets {
		if target.Datasource.Name != VMDatasourceName || target.Expr == "" {
			continue
		}

		query := target.Expr
		query, err := templating.InterpolateQuery(query, p.from, p.to, p.allVariables())
		if err != nil {
			return nil, errors.Wrapf(err, "failed to interpolate query")
		}
		s, err := p.parseQuery(query)
		if err != nil {
			return nil, errors.Wrapf(err, "parse query: %s", s)
		}
		for _, v := range s {
			selectorMap[v] = struct{}{}
		}
	}

	for _, panel := range dashboard.Panels {
		s, err := p.GetSelectors(panel)
		if err != nil {
			return nil, errors.Wrapf(err, "get selectors from dashboard")
		}
		for _, v := range s {
			selectorMap[v] = struct{}{}
		}
	}

	selectors := make([]string, 0, len(selectorMap))
	for k := range selectorMap {
		selectors = append(selectors, k)
	}

	return selectors, nil
}

var errShouldIgnoreQuery = errors.New("should ignore query")

func (p *VMExprParser) parseTemplatingVar(v types.VariableModel) (templating.TemplatingVariable, error) {
	switch v.Type {
	case dashboard.VariableTypeQuery:
		if v.Datasource != nil {
			uid, err := templating.InterpolateQuery(v.Datasource.UID, p.from, p.to, p.allVariables())
			if err != nil {
				return templating.TemplatingVariable{}, errors.Wrap(err, "interpolate query")
			}

			if v.Datasource.Name != VMDatasourceName && uid != VMDatasourceName && v.Datasource.Type != "prometheus" {
				return templating.TemplatingVariable{}, errShouldIgnoreQuery
			}
		}
		pv, err := p.parseTemplatingQuery(v)
		if err != nil {
			return templating.TemplatingVariable{}, errors.Wrap(err, "parse templating query")
		}
		return pv, nil
	case dashboard.VariableTypeCustom:
		vals := make([]string, 0, len(v.Options))
		for _, opt := range v.Options {
			s, ok := opt.Value.(string)
			if !ok {
				return templating.TemplatingVariable{}, errors.Errorf("variable option %s is not string", opt.Text)
			}
			vals = append(vals, s)
		}
		return templating.TemplatingVariable{
			Model:  v,
			Values: vals,
		}, nil
	case dashboard.VariableTypeConstant:
		val, err := templating.GetQueryFromModel(v)
		if err != nil {
			return templating.TemplatingVariable{}, errors.Wrap(err, "get query from model")
		}
		return templating.TemplatingVariable{
			Model:  v,
			Values: []string{val},
		}, nil
	case dashboard.VariableTypeAdhoc:
		return templating.TemplatingVariable{}, errShouldIgnoreQuery
	case dashboard.VariableTypeDatasource:
		query, err := templating.GetQueryFromModel(v)
		if err != nil {
			return templating.TemplatingVariable{}, errors.Wrap(err, "get query from model")
		}
		if query != "prometheus" {
			return templating.TemplatingVariable{}, errShouldIgnoreQuery
		}
		return templating.TemplatingVariable{
			Model:  v,
			Values: []string{VMDatasourceName},
		}, nil
	case dashboard.VariableTypeInterval:
		return templating.TemplatingVariable{
			Model:  v,
			Values: []string{durationToStr(defaultInterval, "m")},
		}, nil
	}
	return templating.TemplatingVariable{}, errors.Errorf("not supported type by pmm-dump: %s", string(v.Type))
}

func (p *VMExprParser) parseTemplatingVars(list []types.VariableModel) error {
	for _, templVar := range list {
		pv, err := p.parseTemplatingVar(templVar)
		if err != nil {
			if errors.Is(err, errShouldIgnoreQuery) {
				p.ignoredVars[templVar.Name] = struct{}{}
				continue
			}
			return errors.Wrap(err, "parse templating var")
		}
		p.varOrder = append(p.varOrder, pv.Name())
		p.vars[pv.Name()] = pv
	}
	return nil
}
