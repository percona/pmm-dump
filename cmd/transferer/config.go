package main

import (
	"fmt"
	"net/url"
	"strings"
)

type PMMConfig struct {
	PMMURL             string
	ClickHouseURL      string
	VictoriaMetricsURL string
}

func getPMMConfig(pmmLink, vmLink, chLink string) (PMMConfig, error) {
	pmmURL, err := url.Parse(pmmLink)
	if err != nil {
		return PMMConfig{}, fmt.Errorf("failed to parse pmm_url: %s", err)
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
	i := strings.LastIndex(u.Host, ":")
	if i != -1 {
		u.Host = u.Host[:i]
	}
	u.Host += ":9000"
	u.RawQuery = "database=pmm"
	return u.String()
}
