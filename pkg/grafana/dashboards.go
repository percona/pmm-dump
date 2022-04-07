package grafana

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/valyala/fasthttp"
	"pmm-dump/pkg/network"
)

func GetDashboardSelectors(pmmURL string, dashboards, serviceNames []string, c network.Client) ([]string, error) {
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

func getSingleDashboardSelectors(pmmURL, dashboardName string, serviceNames []string, c network.Client) ([]string, error) {
	uid, err := findDashboardUID(pmmURL, dashboardName, c)
	if err != nil {
		return nil, err
	}
	link := fmt.Sprintf("%s/graph/api/dashboards/uid/%s", pmmURL, uid)
	status, data, err := c.Get(link)
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
	Dashboard panel `json:"dashboard"`
}

type templateElement struct {
	Name  string `json:"name"`
	Query string `json:"query"`
}

func (t *templateElement) UnmarshalJSON(data []byte) error {
	var v map[string]interface{}
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	if name, ok := v["name"]; ok {
		t.Name, _ = name.(string)
	}
	switch v["query"].(type) {
	case string:
		t.Query = v["query"].(string)
	case map[string]interface{}:
		t.Query, _ = v["query"].(map[string]interface{})["query"].(string)
	}
	return nil
}

type panel struct {
	Id      int     `json:"id"`
	Panels  []panel `json:"panels"`
	Targets []struct {
		Expr string `json:"expr"`
	} `json:"targets"`
	Templating struct {
		List []templateElement `json:"list"`
	} `json:"templating"`
}

func findDashboardUID(pmmURL, name string, c network.Client) (string, error) {
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
