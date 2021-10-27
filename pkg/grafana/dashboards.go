package grafana

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/promql/parser"
	"github.com/valyala/fasthttp"
	"strings"
)

func GetDashboardSelectors(pmmURL string, dashboards, serviceNames []string, c *fasthttp.Client) ([]string, error) {
	var selectors []string
	for _, d := range dashboards {
		sel, err := getSingleDashboardSelectors(pmmURL, d, serviceNames, c)
		if err != nil {
			return nil, fmt.Errorf("failed to retrieve selectors for dashboard \"%s\": %v", d, err)
		}
		selectors = append(selectors, sel...)
	}
	return selectors, nil
}

func getSingleDashboardSelectors(pmmURL, dashboardName string, serviceNames []string, c *fasthttp.Client) ([]string, error) {
	uid, err := findDashboardUID(pmmURL, dashboardName, c)
	if err != nil {
		return nil, err
	}
	link := fmt.Sprintf("%s/graph/api/dashboards/uid/%s", pmmURL, uid)
	status, data, err := c.Get(nil, link)
	if err != nil {
		return nil, err
	}
	if status != fasthttp.StatusOK {
		return nil, fmt.Errorf("non-ok status: %d", status)
	}

	exprResp := new(dashboardExprResp)
	if err = json.Unmarshal(data, exprResp); err != nil {
		return nil, err
	}
	selectors, err := exprResp.parseSelectors(serviceNames)
	if err != nil {
		return nil, err
	}

	return selectors, nil
}

type dashboardExprResp struct {
	Dashboard struct {
		Id     int `json:"id"`
		Panels []struct {
			Targets []struct {
				Expr string `json:"expr"`
			} `json:"targets"`
		} `json:"panels"`
	} `json:"dashboard"`
}

func (d *dashboardExprResp) parseSelectors(serviceNames []string) (selectors []string, err error) {
	selMap := make(map[string]struct{})
	for _, panel := range d.Dashboard.Panels {
		for _, target := range panel.Targets {
			if target.Expr == "" {
				continue
			}

			target.Expr = strings.ReplaceAll(target.Expr, "$interval", "1m")
			target.Expr = strings.ReplaceAll(target.Expr, "$node_name", "pmm-server")
			expr, err := parser.ParseExpr(target.Expr)
			if err != nil {
				return nil, err
			}
			extractedSelectors := parser.ExtractSelectors(expr)
			for _, sel := range extractedSelectors {
				s := "{"
				for i, v := range sel {
					if v.Value == "$service_name" {
						if len(serviceNames) == 0 {
							continue
						}
						serviceName := strings.Join(serviceNames, "|")
						v.Value = serviceName
						v.Type = labels.MatchRegexp
					}
					s += v.String()
					if i+1 < len(sel) {
						s += ", "
					}
				}
				s += "}"
				selMap[s] = struct{}{}
			}
		}
	}
	selectors = make([]string, 0, len(selMap))
	for v := range selMap {
		selectors = append(selectors, v)
	}
	return selectors, nil
}

func findDashboardUID(pmmURL, name string, c *fasthttp.Client) (string, error) {
	q := fasthttp.AcquireArgs()
	defer fasthttp.ReleaseArgs(q)

	q.Add("query", name)
	link := fmt.Sprintf("%s/graph/api/search?%s", pmmURL, q.String())
	status, data, err := c.Get(nil, link)
	if err != nil {
		return "", err
	}
	if status != fasthttp.StatusOK {
		return "", fmt.Errorf("non-ok status: %d", status)
	}

	var resp []dashboardSearchResp
	if err = json.Unmarshal(data, &resp); err != nil {
		return "", err
	}
	if len(resp) == 0 {
		return "", errors.New("dashboard not found")
	} else if len(resp) == 1 {
		return resp[0].Uid, nil
	}

	uid := ""
	for _, v := range resp {
		if v.Title == name {
			uid = v.Uid
			break
		}
	}
	if uid == "" {
		return "", errors.New("dashboard not found")
	}

	return uid, nil
}

type dashboardSearchResp struct {
	Id          int      `json:"id"`
	Uid         string   `json:"uid"`
	Title       string   `json:"title"`
	Uri         string   `json:"uri"`
	Url         string   `json:"url"`
	Slug        string   `json:"slug"`
	Type        string   `json:"type"`
	Tags        []string `json:"tags"`
	IsStarred   bool     `json:"isStarred"`
	FolderId    int      `json:"folderId"`
	FolderUid   string   `json:"folderUid"`
	FolderTitle string   `json:"folderTitle"`
	FolderUrl   string   `json:"folderUrl"`
}
