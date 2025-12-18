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

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type TimeRange struct {
	From time.Time
	To   time.Time
}

func FromMacro(inputString string, timeRange TimeRange) (string, error) {
	res, err := applyMacro("$$from", inputString, func(_ string, args []string) (string, error) {
		return expandTimeMacro(timeRange.From, args)
	})
	return res, err
}

func ToMacro(inputString string, timeRange TimeRange) (string, error) {
	res, err := applyMacro("$$to", inputString, func(_ string, args []string) (string, error) {
		return expandTimeMacro(timeRange.To, args)
	})
	return res, err
}

func expandTimeMacro(t time.Time, args []string) (string, error) {
	if len(args) < 1 || args[0] == "" {
		return strconv.FormatInt(t.UnixMilli(), 10), nil
	}
	if args[0] == "date" {
		if len(args) < 2 || args[1] == ":iso" {
			return t.Format("2006-01-02T15:04:05.999Z"), nil
		}
	}
	format := strings.TrimPrefix(strings.Join(args, ","), "date:")
	if format == "iso" {
		return t.Format("2006-01-02T15:04:05.999Z"), nil
	}
	format = strings.ReplaceAll(format, "YYYY", "2006")
	format = strings.ReplaceAll(format, "YY", "06")

	format = strings.ReplaceAll(format, "MMMM", "January")
	format = strings.ReplaceAll(format, "MMM", "Jan")
	format = strings.ReplaceAll(format, "MM", "01")
	format = strings.ReplaceAll(format, "M", "1")

	format = strings.ReplaceAll(format, "DD", "02")
	format = strings.ReplaceAll(format, "D", "2")

	format = strings.ReplaceAll(format, "hh", "03")
	format = strings.ReplaceAll(format, "h", "3")
	format = strings.ReplaceAll(format, "HH", "15")

	format = strings.ReplaceAll(format, "mm", "04")
	format = strings.ReplaceAll(format, "m", "4")

	format = strings.ReplaceAll(format, "ss", "05")
	format = strings.ReplaceAll(format, "s", "5")

	format = strings.ReplaceAll(format, "S", "0")

	format = strings.ReplaceAll(format, "A", "PM")

	format = strings.ReplaceAll(format, "zz", "MST")
	format = strings.ReplaceAll(format, "z", "MST")

	format = strings.ReplaceAll(format, "dddd", "Monday")
	format = strings.ReplaceAll(format, "ddd", "Mon")

	return t.Format(format), nil
}

func ApplyMacros(input string, timeRange TimeRange) (string, error) {
	input, err := FromMacro(input, timeRange)
	if err != nil {
		return input, err
	}
	input, err = ToMacro(input, timeRange)
	if err != nil {
		return input, err
	}
	return input, nil
}

type macroFunc func(string, []string) (string, error)

func getMatches(macroName, input string) ([][]string, error) {
	macroRegex := fmt.Sprintf("\\$__%s\\b(?:\\((.*?)\\))?", macroName) // regular macro syntax
	if strings.HasPrefix(macroName, "$$") {                            // prefix $$ is used to denote macro from frontend or grafana global variable
		macroRegex = fmt.Sprintf("\\${__%s:?(.*?)}", strings.TrimPrefix(macroName, "$$"))
	}
	rgx, err := regexp.Compile(macroRegex)
	if err != nil {
		return nil, err
	}
	return rgx.FindAllStringSubmatch(input, -1), nil
}

func applyMacro(macroKey string, queryString string, macro macroFunc) (string, error) {
	matches, err := getMatches(macroKey, queryString)
	if err != nil {
		return queryString, err
	}
	for _, match := range matches {
		if len(match) == 0 {
			continue
		}
		args := make([]string, 0)
		if len(match) > 1 {
			args = strings.Split(match[1], ",")
		}
		res, err := macro(queryString, args)
		if err != nil {
			return queryString, err
		}
		queryString = strings.ReplaceAll(queryString, match[0], res)
	}
	return strings.TrimSpace(queryString), nil
}
