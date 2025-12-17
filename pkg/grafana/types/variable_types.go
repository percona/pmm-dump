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

// VariableOption represents an option for a dashboard variable.
type VariableOption struct {
	Text  string      `json:"text"`
	Value interface{} `json:"value"`
}

// VariableSort represents sorting options for a variable.
type VariableSort struct {
	Type int  `json:"type"`
	Desc bool `json:"desc"`
}

// VariableHide represents hide options for a variable.
type VariableHide int

// VariableRefresh represents refresh options for a variable.
type VariableRefresh int
