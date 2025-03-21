# PMM Dump (pmm-import-export-tool)

PMM Dump is a tool that allows to transfer metrics and QAN data from one PMM Server instance to another. It helps Percona Services engineers troubleshoot issues.

## How to build?

You will need to have Go 1.21+ installed.

In the root directory: `make build`

## Using PMM Dump

The transfer process is split into two main parts: export and import.

In order to run either export or import, you have to specify PMM URL with credentials at least:
```
./pmm-dump export --pmm-url "http://USER:PASS@HOST"
./pmm-dump import --pmm-url "http://USER:PASS@HOST" --dump-path FILENAME.tar.gz
```
Also, you can use credentials flags or envars:
```
./pmm-dump export --pmm-url "http://HOST" --pmm-user USER --pmm-pass PASS
PMM_USER=USER PMM_PASS=PASS ./pmm-dump import --pmm-url "http://HOST" --dump-path FILENAME.tar.gz
```

Here are main commands/flags:


| Command   | Flag                 | Description                                                                                               | Example                                                                                                    |
|-----------|----------------------|-----------------------------------------------------------------------------------------------------------| ---------------------------------------------------------------------------------------------------------- |
| any       | pmm-url              | URL of PMM instance. Envar: `PMM_URL`                                                                     | `http://admin:admin@localhost`                                                                             |
| any       | pmm-host             | Host of PMM instance(with scheme). Envar: `PMM_HOST`                                                      | `http://localhost`                                                                                         |
| any       | pmm-port             | Port of PMM instance. Envar: `PMM_PORT`                                                                   | `80`                                                                                                       |
| any       | pmm-user             | PMM credentials user. Envar: `PMM_USER`                                                                   | -                                                                                                          |
| any       | pmm-pass             | PMM credentials password. Envar: `PMM_PASS`                                                               | -                                                                                                          |
| any       | pmm-token            | PMM API token. Envar: `PMM_TOKEN`                                                                         |                                                                                                            |
| any       | pmm-cookie           | PMM auth cookie value. Envar: `PMM_COOKIE`                                                                 |                                                                                                            |
| any       | dump-core            | Process core metrics                                                                                      | -                                                                                                          |
| any       | dump-qan             | Process QAN metrics                                                                                       | -                                                                                                          |
| any       | workers              | Set the number of import/export workers                                                                   | `4`                                                                                                        |
| export    | start-ts             | Start date-time to limit timeframe (in [RFC3339](https://www.ietf.org/rfc/rfc3339.txt) format)            | `2006-01-02T15:04:05Z` (please note that you can't use offset for UTC time)<br>`2006-01-02T15:04:05-07:00` |
| export    | end-ts               | End date-time to limit timeframe (in [RFC3339](https://www.ietf.org/rfc/rfc3339.txt) format)              | `2006-01-02T15:04:05Z` (please note that you can't use offset for UTC time)<br>`2006-01-02T15:04:05-07:00` |
| export    | ignore-load          | Disable checking for load values                                                                          | -                                                                                                          |
| export    | max-load             | Max value of a metric to postpone export                                                                  | `CPU=50,RAM=50,MYRAM=10`                                                                                   |
| export    | critical-load        | Max value of a metric to stop export                                                                      | `CPU=70,RAM=70,MYRAM=30`                                                                                   |
| export    | stdout               | Redirect output to STDOUT                                                                                 | -                                                                                                          |
| export    | vm-native-data       | Use VictoriaMetrics' native export format. Reduces dump size, but can be incompatible between PMM versions | -                                                                                                          |
| import    | vm-content-limit     | Limit the chunk content size for VictoriaMetrics (in bytes). Doesn't work with native format              | `1024`                                                                                                     |
| any       | dump-path, d         | Path to dump file                                                                                         | `/tmp/pmm-dumps/pmm-dump-1624342596.tar.gz`                                                                |
| any       | verbose, v           | Enable verbose (debug) mode                                                                               | -                                                                                                          |
| any       | allow-insecure-certs | For self-signed certificates                                                                              | -                                                                                                          |
| show-meta | -                    | Shows dump meta in human readable format                                                                  | -                                                                                                          |
| show-meta | no-prettify          | Shows raw dump meta                                                                                       | -                                                                                                          |
| version   | -                    | Shows binary version                                                                                      | -                                                                                                          |


For filtering you could use the following commands (will be improved in the future):

| Command | Flag        | Description                       | Example                      |
| ------- | ----------- | --------------------------------- | ---------------------------- |
| export  | ts-selector | Timeseries selector (for VM only) | `{service_name="mongo"}`     |
| export  | where       | WHERE statement (for CH only)     | `service_name='mongo'`       |
| export  | dashboard   | Dashboard name (for VM only)      | `MongoDB Instances Overview` |
| export  | instance    | Filter by service name            | `mongo`                      |

You could filter by instance using service name or id. For example, we have registered the following mongodb instance:

```
> pmm-admin add mongodb --username=pmm_mongodb --password=password mongo mongodb:27017
MongoDB Service added.
Service ID  : 6d7fbaa0-6b21-4c3f-a4a7-4be1e4f58b11
Service name: mongo
```

So the value of `ts-selector` would be: `{service_name="mongo"}` or `{service_id="6d7fbaa0-6b21-4c3f-a4a7-4be1e4f58b11"}`.
The same for `where` QAN filter: `service_name='mongo'` or `service_id='6d7fbaa0-6b21-4c3f-a4a7-4be1e4f58b11'`.
Note: On version 2 value of `ts-selector` would be: 
`{service_name="mongo"}` or `{service_id="/service_id/6d7fbaa0-6b21-4c3f-a4a7-4be1e4f58b11"}` 
and QAN filter: 
`service_name='mongo'` or `service_id='/service_id=6d7fbaa0-6b21-4c3f-a4a7-4be1e4f58b11'`.
Also, you can use `instance` option which filters QAN and core metrics by service name

```
> ./pmm-dump export --pmm-url="http://admin:admin@localhost:8282" --ts-selector=`{service_name="mongo"}` --dump-qan --where=`service_name='mongo'`
```
is same as
```
> ./pmm-dump export --pmm-url="http://admin:admin@localhost:8282" --instance="mongo" --dump-qan
```

To filter by multiple dashboards, you can use `dashboard` flag multiple times:
```
> ./pmm-dump export --pmm-url="http://admin:admin@localhost:8282" --dashboard='MongoDB Instances Overview' --dashboard='MySQL Instances Overview'`
```

In some cases you would need to override default configuration for VM/CH processing:

| Command | Flag                 | Description                                         | Example                                        |
| ------- | -------------------- | --------------------------------------------------- | ---------------------------------------------- |
| any     | victoria-metrics-url | URL of Victoria Metrics                             | `http://admin:admin@localhost:8282/prometheus` |
| any     | click-house-url      | URL of Click House                                  | `http://localhost:9000?database=pmm`           |
| export  | chunk-time-range     | Time range to be fit into a single chunk (VM only)  | `45s`, `5m`, `1h`                              |
| export  | chunk-rows           | Amount of rows to fit into a single chunk (CH only) | `1000`                                         |

### Using in pipelines
You can redirect output to STDOUT with --stdout option. It's useful to redirect output to another pmm-dump in a pipeline:
```
> ./pmm-dump export --pmm-url="http://admin:admin@localhost:8282" --dump-qan --stdout | ./pmm-dump import --pmm-url="http://admin:admin@localhost:8282" --dump-qan 
```

### Stop or postpone during export
You can set threshold values to stop or postpone pmm-dump during export using `max-load` and `critical-load` options.

The syntax for these options is following:

```
<threshold>=<percent_value>
```

You can provide multiple threshold values separated by commas. For example:

``` 
--max-load='CPU=100,RAM=30'
```
Available thresholds:
- `CPU` - CPU load of PMM instance in percents (0-100)
- `RAM` - RAM load of PMM instance in percents (0-100)
- `MYRAM` - RAM load of instance which uses pmm-dump in percents (0-100)

## About the dump file

Dump file is a `tar` archive compressed via `gzip`. Here is the shape of dump file:

* `dump.tar.gz/meta.json` - contains metadata about the dump (JSON object)
* `dump.tar.gz/vm/` - contains Victoria Metrics data chunks split by timeframe (in native VM format)
* `dump.tar.gz/ch/` - contains ClickHouse data chunks split by rows count (in TSV format)


## Using Makefile for local development environment

There is a Makefile that contains commands to build and test pmm-dump locally. It uses docker-compose to set up PMM Server, PMM Client and MongoDB.

You will need to have Go 1.21+ and Docker installed.

| Rule                | Description                  |
| ------------------- | ---------------------------- |
| make                | Shortcut for fast test       |
| make build          | Builds pmm-dump binary       |
| make up             | Sets up docker containers    |
| mongo-reg           | Registers MongoDB in PMM     |
| mongo-insert        | Executes MongoDB insert      |
| make down           | Shuts down docker containers |
| make re             | Shortcut for `down up`       |
| make export-all     | Runs export from local PMM   |
| make run-tests      | Runs all tests               |
| make run-e2e-tests  | Runs all e2e tests           |
| make run-unit-tests | Runs all unit tests          |

Read `Makefile` for more.

## Running End-to-End Tests

For detailed instructions on executing end-to-end tests, refer to [Executing e2e tests](./internal/test/README.md).
