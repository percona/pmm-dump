package loadController

import (
	"github.com/valyala/fasthttp"
	"sync"
)

type LoadType string
type percent float64
type loadMapVal map[LoadType]percent
type Signal int

const (
	CPU LoadType = "cpu"

	Critical Signal = iota
	Max
)

type Controller struct {
	lm  loadMap
	c   *fasthttp.Client
	cfg Config
	ls  chan Signal
}

type loadMap struct {
	initialVal loadMapVal
	val        loadMapVal
	lock       sync.RWMutex
	wg         sync.WaitGroup
}

type loadData struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Metric struct {
				Instance string `json:"instance"`
			} `json:"metric"`
			Value []interface{} `json:"value"`
		} `json:"result"`
	} `json:"data"`
}
