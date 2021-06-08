package main

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"os"
	"path"
	"pmm-transferer/pkg/clickhouse"
	"pmm-transferer/pkg/victoriametrics"
	"time"

	"github.com/pkg/errors"
	"github.com/valyala/fasthttp"
)

type exportParams struct {
	clickHouse      *clickhouse.Config
	victoriaMetrics *victoriametrics.Config
	outPath         string
}

func runExport(p exportParams) error {
	exportTS := time.Now().UTC()

	filepath := fmt.Sprintf("pmm-dump-%v.gz", exportTS.Unix())
	if p.outPath != "" {
		filepath = path.Join(p.outPath, filepath)
	}

	file, err := os.Create(filepath)
	if err != nil {
		return errors.Wrapf(err, "failed to create %s", filepath)
	}
	defer file.Close()

	// TODO: configurable compression level
	w, err := gzip.NewWriterLevel(file, gzip.BestCompression)
	if err != nil {
		return errors.Wrap(err, "failed to create gzip writer")
	}
	defer w.Close()

	// TODO: configurable http client
	c := fasthttp.Client{
		MaxConnsPerHost:           2,
		MaxIdleConnDuration:       time.Minute,
		MaxIdemponentCallAttempts: 5,
		ReadTimeout:               time.Minute,
		WriteTimeout:              time.Minute,
		MaxConnWaitTimeout:        time.Second * 30,
	}

	if p.victoriaMetrics != nil {
		// TODO: ts validation (recheck other input validations)
		if p.victoriaMetrics.TimeSeriesSelector == "" {
			p.victoriaMetrics.TimeSeriesSelector = `{__name__=~".*"}`
		}

		var q = fasthttp.Args{}

		q.Add("match[]", p.victoriaMetrics.TimeSeriesSelector)

		// TODO: configurable native/json formats
		url := fmt.Sprintf("%s/api/v1/export/native?%s", p.victoriaMetrics.ConnectionURL, q.String())

		// TODO: configurable timeout
		status, body, err := c.GetTimeout(nil, url, time.Second*30)
		if err != nil {
			return errors.Wrap(err, "failed to send HTTP request to victoria metrics")
		}

		if status != fasthttp.StatusOK {
			return errors.Errorf("non-OK response from victoria metrics: %d: %s", status, string(body))
		}

		tw := tar.NewWriter(w)
		defer tw.Close()

		err = tw.WriteHeader(&tar.Header{
			Name: "name", // path.Join("/vm", "native.bin"),
			Size: int64(len(body)),
			Mode: 0600,
		})
		if err != nil {
			return errors.Wrap(err, "failed to write victoria metrics file header")
		}

		_, err = tw.Write(body)
		if err != nil {
			return errors.Wrap(err, "failed to write victoria metrics to gzip")
		}
	}

	// TODO:
	//  1. open/create file with gzip
	//  2. create a folder for VM
	//  3. pass folder writer to exporter
	//  4. implement exporter so it writes file to disk (best in chunks)
	//  5. close the file

	// TODO: clickhouse export

	return nil
}
