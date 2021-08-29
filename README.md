# PMM Transferer (pmm-import-export-tool)

A tool that will fetch data from PMM and import it into local instance. Will help Percona Services engineers to resolve issues when the customer cannot provide access to their PMM instance.

## How to build?

You will need to have Go 1.16+ installed.

In the root directory: `go build -o pmm-transferer pmm-transferer/cmd/transferer`

## Using Transferer

The transfer process is split into two main parts: export and import.

In order to run either export or import, you have to specify data source URLs (Victoria Metrics and/or ClickHouse).

Here are main commands/flags:

| Command | Flag | Description | Example |
|---------|------|-------------|---------|
| export | victoria_metrics_url | URL of Victoria Metrics | `http://admin:admin@localhost:8282/prometheus` |
| export | click_house_url | URL of Click House | TBD |
| export | ts_selector | Timeseries selector (for VM only) | `{__name__=~".*mongo.*"}` |
| export | where | WHERE statement (for CH only) | TBD |
| export | start-ts | Start date-time to limit timeframe | `2006-01-02T15:04:05Z07:00` |
| export | end-ts | End date-time to limit timeframe | `2006-01-02T15:04:05Z07:00` |
| any | dump_path, d | Path to dump file | `/tmp/pmm-dumps/pmm-dump-1624342596.tar.gz` |
| any | verbose, v | Enable verbose (debug) mode | - |
| any | allow-insecure-certs | For self-signed certificates | - |

## About the dump file

Dump file is a `tar` archive compressed via `gzip`. Here is the shape of dump file:

* `dump.tar.gz/meta.json` - contains metadata about the dump (TBD)
* `dump.tar.gz/vm/` - contains Victoria Metrics data chunks split by timeframe (in native VM format)
* `dump.tar.gz/ch/` - contains ClickHouse data chunks (TBD)


## Using Makefile - local dev env

There is a Makefile for easier testing locally. It uses docker-compose to set up PMM Server, Client and MongoDB.

You will need to have Go 1.16+ and Docker installed.

| Rule | Description |
|------|-------------|
| make | Shortcut for `build up mongo-reg mongo-insert` |
| make build | Builds transferer binary |
| make up | Sets up docker containers |
| mongo-reg | Registers MongoDB in PMM |
| mongo-insert | Executes MongoDB insert |
| make down | Shuts down docker containers |
| make re | Shortcut for `down up` |
| make export-all | Runs export from local PMM |


## Transfer Example

In this example:
* Victoria Metrics is available at `http://admin:admin@localhost:8282/prometheus`
* transferer binary name - `pmm-transferer`
* monitored service - MongoDB

Running export with filter:
```
./pmm-transferer export \
    --victoria_metrics_url="http://admin:admin@localhost:8282/prometheus" \
    --ts_selector='{__name__=~".*mongo.*"}'
```

You should see the following (**outdated**):
```
3:28PM INF Parsing cli params...
3:28PM INF Setting up HTTP client...
3:28PM INF Got Victoria Metrics URL: http://admin:admin@localhost:8282/prometheus
3:28PM INF Processing export...
3:28PM INF Preparing dump file: pmm-dump-1624451309.tar.gz
3:28PM INF Reading metrics from vm...
3:28PM INF Sending request to Victoria Metrics endpoint: http://admin:admin@localhost:8282/prometheus/api/v1/export/native?match%5B%5D=%7B__name__%3D~%22.%2Amongo.%2A%22%7D
3:28PM INF Got successful response from Victoria Metrics
3:28PM INF Writing retrieved metrics to the dump...
3:28PM INF Processed vm data source...
3:28PM INF Successfully exported!
```

Running import:
```
./pmm-transferer import \
    --dump_path pmm-dump-1624451309.tar.gz \
    --victoria_metrics_url="http://admin:admin@localhost:8282/prometheus"
```

You should see the following (**outdated**):
```
3:30PM INF Parsing cli params...
3:30PM INF Setting up HTTP client...
3:30PM INF Got Victoria Metrics URL: http://admin:admin@localhost:8282/prometheus
3:30PM INF Processing import...
3:30PM INF Opening dump file: pmm-dump-1624451309.tar.gz
3:30PM INF Reading file from dump...
3:30PM INF Processing chunk 'vm/0-0.bin'
3:30PM INF Writing chunk to vm
3:30PM INF Successfully processed vm/0-0.bin
3:30PM INF Reading file from dump...
3:30PM INF Processed complete dump
3:30PM INF Finalizing writes...
3:30PM INF Successfully imported!
```
