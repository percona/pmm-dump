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

package expr

// https://grafana.com/docs/grafana/latest/dashboards/variables/add-template-variables/#global-variables

const (
	globalVarDashboard            = "__dashboard"
	globalVarInterval             = "__interval"
	globalVarIntervalMs           = "__interval_ms"
	globalVarOrg                  = "__org"
	globalVarUser                 = "__user"
	globalVarUserLogin            = "__user.login"
	globalVarUserEmail            = "__user.email"
	globalVarRange                = "__range"
	globalVarRangeMs              = "__range_ms"
	globalVarRangeS               = "__range_s"
	globalVarRateInterval         = "__rate_interval"
	globalVarRateIntervalMs       = "__rate_interval_ms"
	globalVarTimeFilterUnderscore = "__timeFilter"
	globalVarTimeFilter           = "timeFilter"
	globalVarTimezone             = "__timezone"
)
