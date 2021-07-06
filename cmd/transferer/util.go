package main

import (
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog"
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
