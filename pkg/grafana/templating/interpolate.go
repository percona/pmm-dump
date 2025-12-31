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
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/experimental/macros"
	"github.com/pkg/errors"
)

// VariableFormat constants are now in variable_format.go

func FormatVar(format VariableFormat, input []string) (string, error) {
       switch format {
       case FormatCSV, FormatJSON, FormatDoubleQuote, FormatSingleQuote, FormatPipe, FormatRaw:
	       return FormatVariables(format, input), nil
       case FormatDistributed, FormatGlob, FormatLucene, FormatPercentencode, FormatRegex, FormatSqlstring, FormatText, FormatQueryparameters:
	       return "", errors.Errorf("unsupported format by pmm-dump: %s", format)
       }
       return "", errors.Errorf("unsupported format by pmm-dump: %s", format)
}

func InterpolateQuery(query string, from time.Time, to time.Time, vars []TemplatingVariable) (string, error) {
       if query == "" {
	       return "", nil
       }
       query, err := macros.ApplyMacros(query, backend.TimeRange{
	       From: from,
	       To:   to,
       }, backend.PluginContext{})
       if err != nil {
	       return "", errors.Wrap(err, "failed to apply macros")
       }

	for _, v := range vars {
		str, err := v.Interpolate("")
		if err != nil {
			return "", err
		}
		for _, template := range []string{"$" + v.Model.Name, "${" + v.Model.Name + "}"} {
			query = strings.ReplaceAll(query, template, str)
		}
	}

	// Interpolating variables in ${variable:format} format
	currIdx := 0
	for currIdx >= 0 {
		currIdx = strings.Index(query[currIdx:], "${")
		if currIdx < 0 {
			break
		}
		currIdx += 2
		closingIdx := strings.Index(query[currIdx:], "}")
		closingIdx += currIdx

		spl := strings.Split(query[currIdx:closingIdx], ":")
		if len(spl) != 2 { //nolint:mnd
			return "", errors.New("failed to interpolate query")
		}

		varName := spl[0]
		varFormat := spl[1]
		v, ok := findVariable(varName, vars)
		if !ok {
			continue
		}

			   str, err := v.Interpolate(VariableFormat(varFormat))
		if err != nil {
			return "", err
		}
		query = strings.ReplaceAll(query, "${"+varName+":"+varFormat+"}", str)
	}

	return query, nil
}

func findVariable(name string, vars []TemplatingVariable) (TemplatingVariable, bool) {
	for _, v := range vars {
		if v.Model.Name == name {
			return v, true
		}
	}
	return TemplatingVariable{}, false
}

func (v TemplatingVariable) Interpolate(format VariableFormat) (string, error) {
       if format == "" {
	       format = FormatPipe
       }
	values := v.Values
	if v.Model.Regex != nil && *v.Model.Regex != "" {
		pattern := *v.Model.Regex
		firstSlash := strings.IndexByte(pattern, '/')
		lastSlash := strings.LastIndexByte(pattern, '/')
		pattern = strings.TrimSpace(pattern[firstSlash+1 : lastSlash])
		r, err := regexp.Compile(pattern)
		if err != nil {
			return "", fmt.Errorf("failed to compile regexp: %s", *v.Model.Regex)
		}

		var filteredValues []string
		for _, v := range values {
			if r.FindString(v) == "" {
				continue
			}
			filteredValues = append(filteredValues, r.FindString(v))
		}

		values = filteredValues
	}
       if len(values) > 0 {
	       return FormatVar(format, values)
       }

       s, _ := FormatVar(format, values) // TODO: regex escape
       return "(" + s + ")", nil
}
