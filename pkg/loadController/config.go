package loadController

import (
	"errors"
	"time"
)

type Config struct {
	ConnectionURL string
	LoadTypes     []LoadType
	LoadInfoDelay time.Duration
}

func (cfg *Config) Validate() error {
	if len(cfg.LoadTypes) == 0 {
		return errors.New("no LoadTypes provided")
	}
	if cfg.ConnectionURL == "" {
		return errors.New("url is empty")
	}
	return nil
}
