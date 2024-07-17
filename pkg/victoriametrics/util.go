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

package victoriametrics

import (
	"net/http"

	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"

	"pmm-dump/pkg/grafana/client"
)

var ErrNotFound = errors.New("not found")

func ExportTestRequest(c *client.Client, victoriaMetricsURL string) error {
	checkUrls := []string{victoriaMetricsURL + "/api/v1/export"}

	for _, v := range checkUrls {
		code, _, err := c.Get(v)
		if err == nil {
			if code == http.StatusNotFound {
				log.Debug().Msgf("404 error by %s", v)
				return ErrNotFound
			}
		} else {
			return err
		}
	}
	return nil
}
