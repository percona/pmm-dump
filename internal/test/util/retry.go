package util

import (
	"context"
	"time"

	"github.com/pkg/errors"
)

func RetryOnError(ctx context.Context, f func() error) error {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	var err error
	for {
		select {
		case <-ticker.C:
			err = f()
			if err == nil {
				return nil
			}
		case <-ctx.Done():
			return errors.Wrap(err, "timeout")
		}
	}
}
