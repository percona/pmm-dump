# Exectuing e2e tests

## Preferred method

### Prerequisites

1. Ensure that Go is installed on your system.
2. Docker Compose v2 must also be installed.

### Execution Instructions

It's recommended to launch e2e tests by using `make run-e2e-tests` command.

Executing the command will perform the following actions:
1. Create Docker Compose configuration files, specifically `./test/.env.test` and `./test/.env2.test`.
2. Build the `pmm-dump` binary.
3. Launch the e2e tests.

## Manual method

### Prerequisites

1. Ensure that Go is installed on your system.
2. Docker Compose v2 must also be installed
3. Perform an initial setup by executing the `./test/init-test-configs.sh` script. This script creates Docker Compose configuration files: `./test/.env.test` and `./test/.env2.test`.
4. Build a `pmm-dump` binary using the `make build` command.


### Executing a single test

To execute a single e2e test, specify the `e2e` build tag to the `go test` command as follows:
```shell
go test -v -tags e2e -run ^TestExportImport$
```

### Executing multiple tests

To execute multiple e2e tests, provide the `-p 1` flag to prevent tests from running in parallel.
```shell
go test -v -p 1 -tags e2e ./...
```

## Troubleshooting

For troubleshooting, some e2e tests store dump files in the `./test/pmm/tmp` directory. These files can be used to investigate any issues that may arise during testing.

# Writing e2e tests

## Structure of e2e test
1. Tag your test file with `//go:build e2e`. This allows users to run basic tests using the `go test ./...` command without the need for Docker Compose or additional configuration files.
2. Creating a PMM test deployment:
	- Use the `util.NewPMM(t *testing.T, name string, dotEnvFilename string)` function to create a `util.PMM` object. The `name` parameter should be the test name. It is used in the `COMPOSE_PROJECT_NAME` environment variable during creation of the test deployment, which is necessary for differentiating test deployments from others. The `dotEnvFilename` should be either `.env.test` or `.env2.test`, as these files are created by the `./test/init-test-configs.sh` script.
	- Deploy the PMM using the `(*util.PMM) Deploy(ctx context.Context)` method.
	- Ensure to stop the PMM test deployments after the test finishes by calling `(*util.PMM) Stop`. In most tests, this should be used with `defer`.

	Example:
    ```go
    pmm := util.NewPMM(t, "test-name", ".env.test")
    pmm.Deploy(ctx)
    defer pmm.Stop()
    ```
3. Launching the binary:
   - Create an `util.Binary` structure.
   - Use the `(*util.Binary) Run(...string) (string, string, error)` method to launch `pmm-dump`.

## Test example:

```go
//go:build e2e

package e2e

...

func TestExample(t *testing.T) {
    ctx := context.Background()
	pmm := util.NewPMM(t, "test-name", ".env.test")
	pmm.Deploy(ctx)
	defer pmm.Stop()

	b := new(util.Binary)
	testDir := t.TempDir()

	args := []string{"-d", filepath.Join(testDir, "dump.tar.gz"), "--pmm-url", pmm.PMMURL(), "--dump-qan", "--click-house-url", pmm.ClickhouseURL()}

	t.Log("Exporting data to", filepath.Join(testDir, "dump.tar.gz"))
	stdout, stderr, err := b.Run(append([]string{"export", "--ignore-load"}, args...)...)
	if err != nil {
		t.Fatal("failed to export", err, stdout, stderr)
	}
}
```
