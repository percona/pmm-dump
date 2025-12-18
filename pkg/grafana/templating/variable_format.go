// Copyright 2023 Percona LLC
//
// Portions of this file are derived from the Grafana project (https://github.com/grafana/grafana)
// and are licensed under the Apache License, Version 2.0.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package templating

type VariableFormat string

const (
	FormatCSV             VariableFormat = "csv"
	FormatJSON            VariableFormat = "json"
	FormatDoubleQuote     VariableFormat = "doublequote"
	FormatSingleQuote     VariableFormat = "singlequote"
	FormatPipe            VariableFormat = "pipe"
	FormatRaw             VariableFormat = "raw"
	FormatDistributed     VariableFormat = "distributed"
	FormatGlob            VariableFormat = "glob"
	FormatLucene          VariableFormat = "lucene"
	FormatPercentencode   VariableFormat = "percentencode"
	FormatRegex           VariableFormat = "regex"
	FormatSqlstring       VariableFormat = "sqlstring"
	FormatText            VariableFormat = "text"
	FormatQueryparameters VariableFormat = "queryparam"
)

func FormatVariables(format VariableFormat, input []string) string {
	switch format {
	case FormatCSV:
		return joinWithSep(input, ",")
	case FormatJSON:
		return joinWithSep(input, ",") // Simplified for now
	case FormatDoubleQuote:
		return joinWithSep(input, "\"")
	case FormatSingleQuote:
		return joinWithSep(input, "'")
	case FormatPipe:
		return joinWithSep(input, "|")
	case FormatRaw:
		return joinWithSep(input, " ")
	default:
		return joinWithSep(input, ",")
	}
}

func joinWithSep(input []string, sep string) string {
	if len(input) == 0 {
		return ""
	}
	result := input[0]
	for _, s := range input[1:] {
		result += sep + s
	}
	return result
}
