package main

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/valyala/fasthttp"
)

func newClientHTTP(insecureSkipVerify bool) *fasthttp.Client {
	return &fasthttp.Client{
		MaxConnsPerHost:           2,
		MaxIdleConnDuration:       time.Minute,
		MaxIdemponentCallAttempts: 5,
		ReadTimeout:               time.Minute,
		WriteTimeout:              time.Minute,
		MaxConnWaitTimeout:        time.Second * 30,
		TLSConfig: &tls.Config{
			InsecureSkipVerify: insecureSkipVerify,
		},
	}
}

type goroutineLoggingHook struct{}

func (h goroutineLoggingHook) Run(e *zerolog.Event, level zerolog.Level, msg string) {
	e.Int("goroutine_id", getGoroutineID())
}

func getGoroutineID() int {
	var buf [64]byte
	n := runtime.Stack(buf[:], false)
	idField := strings.Fields(strings.TrimPrefix(string(buf[:n]), "goroutine "))[0]
	id, err := strconv.Atoi(idField)
	if err != nil {
		panic(fmt.Sprintf("cannot get goroutine id: %v", err))
	}
	return id
}

func getPMMVersion(pmmURL string, c *fasthttp.Client) (string, error) {
	type updatesResp struct {
		Installed struct {
			FullVersion string `json:"full_version"`
		} `json:"installed"`
	}

	statusCode, body, err := c.Post(nil, fmt.Sprintf("%s/v1/Updates/Check", pmmURL), nil)
	if err != nil {
		return "", err
	}
	if statusCode != fasthttp.StatusOK {
		return "", fmt.Errorf("non-ok status: %d", statusCode)
	}
	resp := new(updatesResp)
	if err = json.Unmarshal(body, resp); err != nil {
		return "", fmt.Errorf("failed to unmarshal response: %s", err)
	}
	return resp.Installed.FullVersion, nil
}
