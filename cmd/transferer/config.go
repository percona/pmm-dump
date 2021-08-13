package main

import (
	"fmt"
	"net/url"
	"strings"
)

type PMMConfig struct {
	PmmURL             string
	ClickHouseURL      string
	VictoriaMetricsURL string
}

func getPMMConfig(pmmLink, vmLink, chLink string) (PMMConfig, error) {
	var result PMMConfig
	pmmURL, err := url.Parse(pmmLink)
	if err != nil {
		return result, fmt.Errorf("failed to parse pmm_url: %s", err)
	}

	result.PmmURL = pmmLink
	if vmLink != "" {
		result.VictoriaMetricsURL = vmLink
	} else {
		result.VictoriaMetricsURL = modifyURL(*pmmURL, "/prometheus", pmmURL.User, "", "")
	}
	if chLink != "" {
		result.ClickHouseURL = chLink
	} else {
		result.ClickHouseURL = modifyURL(*pmmURL, "", nil, "9000", "database=pmm")
	}

	return result, nil
}

func modifyURL(u url.URL, path string, userinfo *url.Userinfo, port, query string) string {
	if userinfo != nil {
		u.User = userinfo
	}
	u.Path = path
	if port != "" {
		i := strings.LastIndex(u.Host, ":")
		if i != -1 {
			u.Host = u.Host[:i] + ":" + port
		}
	}
	u.RawQuery = query
	return u.String()
}
