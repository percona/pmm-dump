package dump

import (
	"encoding/json"
	"fmt"
	"github.com/valyala/fasthttp"
	"time"
)

var (
	version string
)

const (
	MetaFilename = "meta.json"
)

type Meta struct {
	Version          string `json:"version"`
	PMMServerVersion string `json:"pmm-server_version"`
}

func NewMeta(pmmURL string, c *fasthttp.Client) (Meta, error) {
	statusCode, body, err := c.Post(nil, fmt.Sprintf("%s/v1/Updates/Check", pmmURL), nil)
	if err != nil {
		return Meta{}, err
	}
	if statusCode != fasthttp.StatusOK {
		return Meta{}, fmt.Errorf("non-ok status: %d", statusCode)
	}
	resp := new(pmmUpdatesResponse)
	if err := json.Unmarshal(body, resp); err != nil {
		return Meta{}, fmt.Errorf("failed to unmarshal response: %s", err)
	}
	meta := Meta{
		Version:          version,
		PMMServerVersion: resp.Installed.FullVersion,
	}
	return meta, nil
}

type pmmUpdatesResponse struct {
	Installed struct {
		Version     string    `json:"version"`
		FullVersion string    `json:"full_version"`
		Timestamp   time.Time `json:"timestamp"`
	} `json:"installed"`
	Latest struct {
		Version     string    `json:"version"`
		FullVersion string    `json:"full_version"`
		Timestamp   time.Time `json:"timestamp"`
	} `json:"latest"`
	UpdateAvailable bool      `json:"update_available"`
	LatestNewsUrl   string    `json:"latest_news_url"`
	LastCheck       time.Time `json:"last_check"`
}
