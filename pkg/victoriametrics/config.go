package victoriametrics

type Config struct {
	ConnectionURL       string
	TimeSeriesSelectors []string
	NativeData          bool
	ContentLimit        uint64
}
