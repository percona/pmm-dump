# PMM Transferer

PMM Transferer is a tool for export/import PMM Server data (Victoria Metrics and ClickHouse).

The work is in progress, so some things could change.

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
| export | out | Path to output directory | `/tmp/pmm-dumps` |
| export | ts_selector | Timeseries selector (for VM only) | `{__name__=~".*mongo.*"}` |
| export | start | Start date-time to limit timeframe | `2006-01-02T15:04:05Z07:00` |
| export | end | End date-time to limit timeframe | `2006-01-02T15:04:05Z07:00` |
| import | dump_path | Path to dump file | `/tmp/pmm-dumps/pmm-dump-1624342596.tar.gz` |

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
| make vm-export | Runs Victoria Metrics export from local PMM |
