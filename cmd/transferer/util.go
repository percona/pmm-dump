package main

import (
	"time"

	"github.com/valyala/fasthttp"
)

func newClientHTTP() *fasthttp.Client {
	return &fasthttp.Client{
		MaxConnsPerHost:           2,
		MaxIdleConnDuration:       time.Minute,
		MaxIdemponentCallAttempts: 5,
		ReadTimeout:               time.Minute,
		WriteTimeout:              time.Minute,
		MaxConnWaitTimeout:        time.Second * 30,
	}
}
