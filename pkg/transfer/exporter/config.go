package exporter

import "time"

type Config struct {
	OutPath string
	Start   *time.Time
	End     *time.Time
}
