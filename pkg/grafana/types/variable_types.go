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

// VariableType is the type of a dashboard variable.
type VariableType string

const (
	VariableTypeQuery      VariableType = "query"
	VariableTypeCustom     VariableType = "custom"
	VariableTypeConstant   VariableType = "constant"
	VariableTypeAdhoc      VariableType = "adhoc"
	VariableTypeDatasource VariableType = "datasource"
	VariableTypeInterval   VariableType = "interval"
)

// VariableText can be a string or an array of strings in JSON.
type VariableText []string

func (t *VariableText) UnmarshalJSON(data []byte) error {
       // Try to unmarshal as a string
       var s string
       if err := json.Unmarshal(data, &s); err == nil {
	       *t = []string{s}
	       return nil
       }
       // Try to unmarshal as an array of strings
       var arr []string
       if err := json.Unmarshal(data, &arr); err == nil {
	       *t = arr
	       return nil
       }
       return json.Unmarshal(data, (*[]string)(t))
}

// VariableOption represents an option for a dashboard variable.
type VariableOption struct {
       Text  VariableText `json:"text"`
       Value interface{}  `json:"value"`
}

// VariableSort represents sorting options for a variable.
type VariableSort struct {
	Type int  `json:"type"`
	Desc bool `json:"desc"`
}

// UnmarshalJSON allows VariableSort to be unmarshaled from either a number or an object.
func (s *VariableSort) UnmarshalJSON(data []byte) error {
	// Try to unmarshal as a number (Grafana sometimes encodes as int)
	var asInt int
	if err := json.Unmarshal(data, &asInt); err == nil {
		s.Type = asInt
		s.Desc = false
		return nil
	}
	// Try to unmarshal as a struct
	var asStruct struct {
		Type int  `json:"type"`
		Desc bool `json:"desc"`
	}
	if err := json.Unmarshal(data, &asStruct); err != nil {
		return err
	}
	s.Type = asStruct.Type
	s.Desc = asStruct.Desc
	return nil
}

// VariableHide represents hide options for a variable.
type VariableHide int

// VariableRefresh represents refresh options for a variable.
type VariableRefresh int
