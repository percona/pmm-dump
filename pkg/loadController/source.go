package loadController

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/rs/zerolog/log"
	"github.com/valyala/fasthttp"
	"net/http"
	"strconv"
	"time"
)

// TODO: Make configurable
const (
	CriticalLoad percent = 70
	MaxLoad      percent = 50
)

func New(c *fasthttp.Client, cfg Config) (*Controller, error) {
	return &Controller{
		c:   c,
		cfg: cfg,
	}, cfg.Validate()
}

func (lc *Controller) Start(ctx context.Context, workersCount int) error {
	lm, err := lc.initialLoadMapVal(lc.cfg.LoadTypes)
	if err != nil {
		return fmt.Errorf("failed to get initial load metrics: %s", err.Error())
	}
	lc.lm.initialVal = lm
	lc.lm.val = lm

	updateLoad := make(chan struct{}, 0)
	go lc.workerPool(ctx, updateLoad)
	go lc.loadChecker(ctx, updateLoad, workersCount)
	log.Debug().Msgf("LoadController started")
	return nil
}

func (lc *Controller) loadChecker(ctx context.Context, update <-chan struct{}, workerCount int) {
	lc.ls = make(chan Signal, workerCount)
	for {
		select {
		case <-ctx.Done():
			log.Debug().Msgf("Context is done, stopping loadChecker")
			return
		case <-update:
			log.Debug().Msgf("loadChecker update started")
			for k, initialVal := range lc.lm.initialVal {
				val, err := lc.lm.read(k)
				if err != nil {
					log.Error().Msgf("failed to retrieve %s from loadMap: %s", k, err.Error())
					continue
				}
				currentLoad := (val - initialVal) * 100 / (100 - initialVal)
				lc.sendSignal(Max)
				switch {
				case CriticalLoad <= currentLoad:
					log.Debug().Msgf("Sending critical load signal")
					lc.sendSignal(Critical)
				case MaxLoad <= currentLoad:
					log.Debug().Msgf("Sending max load signal")
					lc.sendSignal(Max)
				}
			}
		default:
		}
	}
}

func (lc *Controller) workerPool(ctx context.Context, update chan<- struct{}) {
	ticker := time.NewTicker(lc.cfg.LoadInfoDelay)
	for range ticker.C {
		select {
		case <-ctx.Done():
			log.Debug().Msgf("Context is done, stopping workerPool")
			close(update)
			return
		default:
			ticker.Reset(lc.cfg.LoadInfoDelay)
		}
		lc.lm.wg.Add(len(lc.cfg.LoadTypes))
		for _, lt := range lc.cfg.LoadTypes {
			go lc.worker(lt)
		}

		lc.lm.wg.Wait()
		log.Debug().Msgf("WorkerPool sends signal to loadChecker")
		update <- struct{}{}
	}
}

func (lc *Controller) worker(lt LoadType) {
	defer lc.lm.wg.Done()

	lp, err := lc.loadValue(lt)
	if err != nil {
		log.Error().Msgf("failed to get load value of %s: %s", lt, lp)
		return
	}
	lc.lm.write(lt, lp)
}

func (lc *Controller) Signal() <-chan Signal {
	return lc.ls
}

func (lc *Controller) sendSignal(ls Signal) {
	for i := 0; i < cap(lc.ls); i++ {
		lc.ls <- ls
	}
}

func (lm *loadMap) read(lt LoadType) (percent, error) {
	lm.lock.RLock()
	val, ok := lm.val[lt]
	lm.lock.RUnlock()
	if !ok {
		return 0, errors.New("invalid LoadType")
	}
	return val, nil
}

func (lm *loadMap) write(lt LoadType, lp percent) {
	lm.lock.Lock()
	lm.val[lt] = lp
	lm.lock.Unlock()
}

func (lm *loadMap) writeNewMap(newVal loadMapVal) {
	lm.lock.Lock()
	lm.val = newVal
	lm.lock.Unlock()
}

func (lc *Controller) initialLoadMapVal(loadTypes []LoadType) (loadMapVal, error) {
	lm := make(loadMapVal)
	for _, lt := range loadTypes {
		val, err := lc.loadValue(lt)
		if err != nil {
			return nil, err
		}
		lm[lt] = val
	}
	return lm, nil
}

func (lc *Controller) loadValue(lt LoadType) (percent, error) {
	q := fasthttp.AcquireArgs()
	defer fasthttp.ReleaseArgs(q)
	var dst []byte
	switch lt {
	case CPU:
		q.Add("query", `100 - (avg by (instance) (rate(node_cpu_seconds_total{mode="idle",node_name="pmm-server"}[1m])) * 100)`)
	default:
		return 0, errors.New("invalid LoadType")
	}
	newUrl := fmt.Sprintf("%s/api/v1/query?%s", lc.cfg.ConnectionURL, q.String())
	status, body, err := lc.c.Get(dst, newUrl)
	if status != http.StatusOK {
		return 0, fmt.Errorf("non-ok response: status %d", status)
	}
	newData := new(loadData)
	err = json.Unmarshal(body, newData)
	if err != nil {
		return 0, fmt.Errorf("error parsing metrics: %s", err)
	}
	return newData.value()
}

func (ld *loadData) value() (percent, error) {
	if ld.Status != "success" {
		return 0, errors.New("status is not success")
	}
	if len(ld.Data.Result) == 0 {
		return 0, errors.New("empty result")
	}
	if len(ld.Data.Result[0].Value) < 2 {
		return 0, errors.New("not enough values")
	}
	str, ok := ld.Data.Result[0].Value[1].(string)
	if !ok {
		return 0, errors.New("value is not string")
	}
	val, err := strconv.ParseFloat(str, 64)
	if err != nil {
		return 0, fmt.Errorf("parsing value error: %s", err.Error())
	}
	return percent(val), nil
}
