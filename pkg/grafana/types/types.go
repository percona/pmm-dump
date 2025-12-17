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

package types

import "encoding/json"

// TODO: use https://github.com/grafana/grok cli for generating these types

type DashboardPanel struct {
	Title   string           `json:"title"`
	Panels  []DashboardPanel `json:"panels"`
	Targets []struct {
		Expr       string        `json:"expr"`
		Datasource DataSourceRef `json:"datasource"`
	} `json:"targets"`
	Templating struct {
		List []VariableModel `json:"list"`
	} `json:"templating"`
	ID int `json:"id"`
}

// https://github.com/grafana/grok/blob/3e8026f90a59baa4e80dd88ac558142aafd7190e/go/kinds/core/dashboard/x/dashboard_types_gen.go#L752
type VariableModel struct {
	Regex       *string          `json:"regex,omitempty"`
	Query       *any             `json:"query,omitempty"`
	Datasource  *DataSourceRef   `json:"datasource,omitempty"`
	Description *string          `json:"description,omitempty"`
	Sort        *VariableSort    `json:"sort,omitempty"`
	IncludeAll  *bool            `json:"includeAll,omitempty"`
	Current     *VariableOption  `json:"current,omitempty"`
	Label       *string          `json:"label,omitempty"`
	Hide        *VariableHide    `json:"hide,omitempty"`
	SkipURLSync *bool            `json:"skipUrlSync,omitempty"`
	Multi       *bool            `json:"multi,omitempty"`
	Refresh     *VariableRefresh `json:"refresh,omitempty"`
	AllValue    *string          `json:"allValue,omitempty"`
	Name        string           `json:"name"`
	Type        VariableType     `json:"type"`
	Options     []VariableOption `json:"options,omitempty"`
}

// Custom unmarshal to handle Sort as number or struct
func (v *VariableModel) UnmarshalJSON(data []byte) error {
	type Alias VariableModel
	aux := &struct {
		Sort json.RawMessage `json:"sort,omitempty"`
		*Alias
	}{
		Alias: (*Alias)(v),
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	if len(aux.Sort) > 0 && string(aux.Sort) != "null" {
		var sort VariableSort
		if err := json.Unmarshal(aux.Sort, &sort); err != nil {
			// Try as int
			var sortInt int
			if err2 := json.Unmarshal(aux.Sort, &sortInt); err2 == nil {
				sort.Type = sortInt
				sort.Desc = false
			} else {
				return err
			}
		}
		v.Sort = &sort
	}
	return nil
}

type DataSourceRef struct {
	Name string
	Type string
	UID  string
}

func (r *DataSourceRef) UnmarshalJSON(data []byte) error {
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	switch s := v.(type) {
	case string:
		*r = DataSourceRef{Name: s}
	case map[string]interface{}:
		valType, _ := s["type"].(string)
		valUID, _ := s["uid"].(string)
		tds := DataSourceRef{}
		tds.Type = valType
		tds.UID = valUID
		*r = tds
	}

	return nil
}
