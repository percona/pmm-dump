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

package templating

import (
	"github.com/pkg/errors"

	"pmm-dump/pkg/grafana/types"
)

type TemplatingVariable struct {
	Model  types.VariableModel
	Values []string
}

func (v TemplatingVariable) Name() string {
	return v.Model.Name
}

func (v TemplatingVariable) Query() (string, error) {
	return GetQueryFromModel(v.Model)
}

func GetQueryFromModel(m types.VariableModel) (string, error) {
	if m.Query == nil {
		return "", nil
	}
	switch s := (*m.Query).(type) {
	case string:
		return s, nil
	case map[string]interface{}:
		q, ok := s["query"].(string)
		if !ok {
			return "", errors.New("query is not a string type")
		}
		return q, nil
	}
	return "", errors.New("failed to get query")
}
