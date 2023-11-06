package victoriametrics

import (
	"encoding/json"
	"io"

	"github.com/pkg/errors"
)

type Metric struct {
	Metric     map[string]string `json:"metric"`
	Values     []float64         `json:"values"`
	Timestamps []int64           `json:"timestamps"`
}

func ParseMetrics(r io.Reader) ([]Metric, error) {
	var result []Metric
	decoder := json.NewDecoder(r)
	for {
		metric := Metric{}
		err := decoder.Decode(&metric)
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, errors.Wrap(err, "failed to decode JSON stream")
		}
		result = append(result, metric)
	}
	return result, nil
}
