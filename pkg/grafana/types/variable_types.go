// Copyright 2023 Percona LLC
//
// Local equivalents of Grafana dashboard variable types and related types.
// This file replaces the need for github.com/grafana/grafana/pkg/kinds/dashboard imports.

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
