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

package util

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/hashicorp/go-version"
)

type PMMConfig struct {
	PMMURL             string
	ClickHouseURL      string
	VictoriaMetricsURL string
}

func GetPMMConfig(pmmLink, vmLink, chLink string) (PMMConfig, error) {
	pmmURL, err := url.Parse(pmmLink)
	if err != nil {
		return PMMConfig{}, fmt.Errorf("failed to parse pmm-url: %w", err)
	}
	conf := PMMConfig{
		PMMURL:             pmmLink,
		ClickHouseURL:      chLink,
		VictoriaMetricsURL: vmLink,
	}

	if conf.ClickHouseURL == "" {
		conf.ClickHouseURL = composeClickHouseURL(*pmmURL)
	}
	if conf.VictoriaMetricsURL == "" {
		conf.VictoriaMetricsURL = composeVictoriaMetricsURL(*pmmURL)
	}
	return conf, nil
}

func composeVictoriaMetricsURL(u url.URL) string {
	u.Path = "/prometheus"
	u.RawQuery = ""
	return u.String()
}

func composeClickHouseURL(u url.URL) string {
	u.Scheme = "clickhouse"
	i := strings.LastIndex(u.Host, ":")
	if i != -1 {
		u.Host = u.Host[:i]
	}
	u.User = nil
	u.Host += ":9000"
	u.Path = "pmm"
	return u.String()
}

func CheckStructuredVersion(ver *version.Version) bool {
	constraints, err := version.NewConstraint("< 3.0.0")
	if err != nil {
		panic(fmt.Sprintf("cannot create constraint: %v", err))
	}
	resConst := ver
	return constraints.Check(resConst)
}
