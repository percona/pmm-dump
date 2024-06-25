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

package grafana

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/pkg/errors"
	"github.com/valyala/fasthttp"

	"pmm-dump/pkg/grafana/client"
	"pmm-dump/pkg/grafana/expr"
	"pmm-dump/pkg/grafana/types"
)

func GetSelectorsFromDashboards(c *client.Client, pmmURL string, dashboardNames, serviceNames []string, from, to time.Time) ([]string, error) {
	selectorMap := make(map[string]struct{})

	for _, name := range dashboardNames {
		dashboard, err := getDashboard(c, pmmURL, name)
		if err != nil {
			return nil, errors.Wrapf(err, "get dashboard: %s", name)
		}
		sel, err := getSelectorsFromDashboard(c, pmmURL, dashboard, serviceNames, from, to)
		if err != nil {
			return nil, errors.Wrap(err, "get selectors from dashboard")
		}
		for _, s := range sel {
			selectorMap[s] = struct{}{}
		}
	}

	selectors := make([]string, 0, len(selectorMap))
	for k := range selectorMap {
		selectors = append(selectors, k)
	}
	return selectors, nil
}

func getSelectorsFromDashboard(c *client.Client, pmmURL string, dashboard types.DashboardPanel, serviceNames []string, from, to time.Time) ([]string, error) {
	parser := expr.NewVMParser(dashboard, serviceNames, c, pmmURL, from, to)
	selectors, err := parser.GetSelectors(dashboard)
	if err != nil {
		return nil, errors.Wrap(err, "get selectors")
	}

	return selectors, nil
}

func getDashboard(c *client.Client, pmmURL, dashboardName string) (types.DashboardPanel, error) {
	uid, err := findDashboardUID(c, pmmURL, dashboardName)
	if err != nil {
		return types.DashboardPanel{}, err
	}
	link := fmt.Sprintf("%s/graph/api/dashboards/uid/%s", pmmURL, uid)
	status, data, err := c.Get(link)
	if err != nil {
		return types.DashboardPanel{}, err
	}
	if status != fasthttp.StatusOK {
		return types.DashboardPanel{}, fmt.Errorf("non-ok status: %d", status)
	}

	resp := struct {
		Dashboard types.DashboardPanel `json:"dashboard"`
	}{}

	if err = json.Unmarshal(data, &resp); err != nil {
		return types.DashboardPanel{}, err
	}
	return resp.Dashboard, nil
}

func findDashboardUID(c *client.Client, pmmURL, name string) (string, error) {
	q := fasthttp.AcquireArgs()
	defer fasthttp.ReleaseArgs(q)

	q.Add("query", name)
	link := fmt.Sprintf("%s/graph/api/search?%s", pmmURL, q.String())
	status, data, err := c.Get(link)
	if err != nil {
		return "", err
	}
	if status != fasthttp.StatusOK {
		return "", fmt.Errorf("non-ok status: %d", status)
	}

	var resp []struct {
		Type        string   `json:"type"`
		UID         string   `json:"uid"`
		Title       string   `json:"title"`
		URI         string   `json:"uri"`
		URL         string   `json:"url"`
		Slug        string   `json:"slug"`
		FolderUID   string   `json:"folderUid"`
		FolderTitle string   `json:"folderTitle"`
		FolderURL   string   `json:"folderUrl"`
		Tags        []string `json:"tags"`
		ID          int      `json:"id"`
		FolderID    int      `json:"folderId"`
		IsStarred   bool     `json:"isStarred"`
	}

	if err = json.Unmarshal(data, &resp); err != nil {
		return "", err
	}
	if len(resp) == 0 {
		return "", errors.New("dashboard not found")
	} else if len(resp) == 1 {
		return resp[0].UID, nil
	}

	uid := ""
	for _, v := range resp {
		if v.Title == name {
			uid = v.UID
			break
		}
	}
	if uid == "" {
		return "", errors.New("dashboard not found")
	}

	return uid, nil
}
