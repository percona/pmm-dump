package main

import (
	"encoding/json"
	"fmt"
	"github.com/valyala/fasthttp"
	"net/http"
	"net/url"
	"strings"
)

type dataSourceResp struct {
	Id          int         `json:"id"`
	OrgId       int         `json:"orgId"`
	Name        string      `json:"name"`
	Type        string      `json:"type"`
	TypeLogoUrl string      `json:"typeLogoUrl"`
	Access      string      `json:"access"`
	URL         string      `json:"url"`
	Password    string      `json:"password"`
	User        string      `json:"user"`
	Database    string      `json:"database"`
	BasicAuth   bool        `json:"basicAuth"`
	IsDefault   bool        `json:"isDefault"`
	JsonData    interface{} `json:"jsonData"`
	ReadOnly    bool        `json:"readOnly"`
}

type DateSources struct {
	ClickHouse      string
	VictoriaMetrics string
}

func getDataSources(c *fasthttp.Client, pmmLink string) (DateSources, error) {
	var result DateSources
	pmmUrl, err := url.Parse(pmmLink)
	if err != nil {
		return result, fmt.Errorf("failed to parse pmm_url: %s", err)
	}

	dsUrl := fmt.Sprintf("%s/graph/api/datasources", pmmLink)
	status, body, err := c.Get(nil, dsUrl)
	if err != nil {
		return result, err
	}
	if status != http.StatusOK {
		return result, fmt.Errorf("non-ok response status: status %d", status)
	}
	var dataSources []dataSourceResp
	if err := json.Unmarshal(body, &dataSources); err != nil {
		return result, err
	}
	result.VictoriaMetrics = dataSourceUrl{
		url:      pmmLink,
		path:     "/prometheus",
		userInfo: pmmUrl.User,
	}.String()
	for _, source := range dataSources {
		switch source.Type {
		case "vertamedia-clickhouse-datasource":
			result.ClickHouse = dataSourceUrl{
				url:   source.URL,
				port:  "9000",
				query: "database=pmm",
			}.String()
		}
	}
	return result, nil
}

type dataSourceUrl struct {
	url      string
	path     string
	userInfo *url.Userinfo
	port     string
	query    string
}

func (s dataSourceUrl) String() string {
	u, err := url.Parse(s.url)
	if err != nil {
		return ""
	}
	if s.userInfo != nil {
		u.User = s.userInfo
	}
	u.Path = s.path
	if s.port != "" {
		i := strings.LastIndex(u.Host, ":")
		if i != -1 {
			u.Host = u.Host[:i] + ":" + s.port
		}
	}
	u.RawQuery = s.query
	return u.String()
}
