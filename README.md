# PMM Transferer (pmm-import-export-tool)

A tool that will fetch data from PMM and import it into local instance. Will help Percona Services engineers to resolve issues when the customer cannot provide access to their PMM instance.

## How to build?

You will need to have Go 1.16+ installed.

In the root directory: `go build -o pmm-transferer pmm-transferer/cmd/transferer`

## Using Transferer

The transfer process is split into two main parts: export and import.

In order to run either export or import, you have to specify PMM URL at least:
```
./pmm-transferer export --pmm-url "http://USER:PASS@HOST"
./pmm-transferer import --pmm-url "http://USER:PASS@HOST" --dump-path FILENAME.tar.gz
```

Here are main commands/flags:

| Command | Flag | Description | Example |
|---------|------|-------------|---------|
| any | pmm-url | URL of PMM instance | `http://admin:admin@localhost` |
| any | dump-core | Process core metrics | - |
| any | dump-qan | Process QAN metrics | - |
| export | start-ts | Start date-time to limit timeframe (in RFC3339 format) | `2006-01-02T15:04:05Z`<br>`2006-01-02T15:04:05-07:00` |
| export | end-ts | End date-time to limit timeframe (in RFC3339 format) | `2006-01-02T15:04:05Z`<br>`2006-01-02T15:04:05-07:00` |
| export | max-load | Max value of a metric to postpone export | `CPU=50,RAM=50` |
| export | critical-load | Max value of a metric to stop export | `CPU=70,RAM=70` |
| export | stdout | Redirect output to STDOUT | - |
| export | workers | Set the number of reading workers | `4` |
| any | dump-path, d | Path to dump file | `/tmp/pmm-dumps/pmm-dump-1624342596.tar.gz` |
| any | verbose, v | Enable verbose (debug) mode | - |
| any | allow-insecure-certs | For self-signed certificates | - |
| show-meta | - | Shows dump meta in human readable format | - |
| show-meta | no-prettify | Shows raw dump meta | - |
| version | - | Shows binary version | - |

For filtering you could use the following commands (will be improved in the future):

| Command | Flag | Description | Example |
|---------|------|-------------|---------|
| export | ts-selector | Timeseries selector (for VM only) | `{service_name="mongo"}` |
| export | where | WHERE statement (for CH only) | `service_name='mongo'` |
| export | dashboard | Dashboard name (for VM only) | `MongoDB Instances Overview` |
| export | service-name | Filter by service name | `mongo` |

You could filter by instance using service name or id. For example, we have registered the following mongodb instance:

```
> pmm-admin add mongodb --username=pmm_mongodb --password=password mongo mongodb:27017
MongoDB Service added.
Service ID  : /service_id/6d7fbaa0-6b21-4c3f-a4a7-4be1e4f58b11
Service name: mongo
```

So the value of `ts-selector` would be: `{service_name="mongo"}` or `{service_id="/service_id/6d7fbaa0-6b21-4c3f-a4a7-4be1e4f58b11"}`.
The same for `where` QAN filter: `service_name='mongo'` or `service_id='/service_id/6d7fbaa0-6b21-4c3f-a4a7-4be1e4f58b11'`.

```
> ./pmm-transferer export --pmm-url="http://admin:admin@localhost:8282" --ts-selector=`{service_name="mongo"}` --dump-qan --where=`service_name='mongo'`
```

To filter by multiple dashboards, you can use `dashboard` flag multiple times:
```
> ./pmm-transferer export --pmm-url="http://admin:admin@localhost:8282" --dashboard='MongoDB Instances Overview' --dashboard='MySQL Instances Overview'`
```

In some cases you would need to override default configuration for VM/CH processing:

| Command | Flag | Description | Example |
|---------|------|-------------|---------|
| any | victoria-metrics-url | URL of Victoria Metrics | `http://admin:admin@localhost:8282/prometheus` |
| any | click-house-url | URL of Click House | `http://localhost:9000?database=pmm` |
| export | chunk-time-range | Time range to be fit into a single chunk (VM only) | `45s`, `5m`, `1h` |
| export | chunk-rows | Amount of rows to fit into a single chunk (CH only) | `1000` |

### Using in pipelines
You can redirect output to STDOUT with --stdout option. It's useful to redirect output to another pmm-transferer in a pipeline:
```
> ./pmm-transferer export --pmm-url="http://admin:admin@localhost:8282" --dump-qan --stdout | ./pmm-transferer import --pmm-url="http://admin:admin@localhost:8282" --dump-qan 
```

## About the dump file

Dump file is a `tar` archive compressed via `gzip`. Here is the shape of dump file:

* `dump.tar.gz/meta.json` - contains metadata about the dump (JSON object)
* `dump.tar.gz/vm/` - contains Victoria Metrics data chunks split by timeframe (in native VM format)
* `dump.tar.gz/ch/` - contains ClickHouse data chunks split by rows count (in TSV format)


## Using Makefile - local dev env

There is a Makefile for easier testing locally. It uses docker-compose to set up PMM Server, Client and MongoDB.

You will need to have Go 1.16+ and Docker installed.

| Rule | Description |
|------|-------------|
| make | Shortcut for fast test |
| make build | Builds transferer binary |
| make up | Sets up docker containers |
| mongo-reg | Registers MongoDB in PMM |
| mongo-insert | Executes MongoDB insert |
| make down | Shuts down docker containers |
| make re | Shortcut for `down up` |
| make export-all | Runs export from local PMM |

For more rules, please see `Makefile`
