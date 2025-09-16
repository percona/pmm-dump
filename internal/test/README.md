# Executing e2e tests

## Preferred method

### Prerequisites

1. Ensure that Go is installed on your system.
2. Docker Compose v2 must also be installed.

### Execution Instructions

It's recommended to launch e2e tests by using `make run-e2e-tests` command.

Executing the command will perform the following actions:
1. Create configuration files, specifically `./test/.env.test` and `./test/.env2.test`.
2. Build the `pmm-dump` binary.
3. Launch the e2e tests.

### Adjusting the Number of Concurrent Tests
End-to-end tests are executed concurrently. The maximum number of tests that can run simultaneously is determined by the `PMM_DUMP_MAX_PARALLEL_TESTS` environment variable. By default, this is set to 4. To modify this value, assign your desired number to this environment variable as shown below:
```shell
export PMM_DUMP_MAX_PARALLEL_TESTS="2"

make run-e2e-tests
```
This example sets the maximum number of concurrent tests to 2. Adjust the value as needed for your testing environment.

## Manual method

### Prerequisites

1. Ensure that Go is installed on your system.
2. Docker must also be installed.
3. Perform an initial setup by executing the `./test/init-test-configs.sh` script. This script creates configuration files: `./test/.env.test` and `./test/.env2.test`.
4. Build a `pmm-dump` binary using the `make build` command.


### Executing a single test

To execute a single e2e test, specify the `e2e` build tag to the `go test` command as follows:
```shell
go test -v -tags e2e -run ^TestExportImport$
```

### Executing multiple tests

To execute multiple e2e tests, use the following command:
```shell
go test -v -tags e2e ./...
```

## Troubleshooting

For troubleshooting, some e2e tests store dump files in the `./test/pmm/tmp` directory. These files can be used to investigate any issues that may arise during testing.

## Execution Flow
Upon initiation of the end-to-end (e2e) tests, the following sequence of actions will occur:
1. Any pre-existing e2e test deployments will be removed
2. The testing framework will automatically pull the required Docker images
3. The e2e tests will be launched. If they pass, we'll delete the test deployments. But if a test fails, we'll keep those deployments

# Writing e2e tests

## Structure of e2e test
1. Tag your test file with `//go:build e2e`. This allows users to run basic tests using the `go test ./...` command without the need for Docker or additional configuration files.
2. Creating a PMM test deployment:
	- Create a deployment controller using `deployment.NewController(t *testing.T)` function.
	- Use the `(*deployment.Controller) NewPMM(name, configFile string) *deployment.PMM` method to create a `deployment.PMM` object. The `name` parameter should be the test name. It is necessary for differentiating test deployments from others. The `dotEnvFilename` should be either `.env.test` or `.env2.test`, as these files are created by the `./test/init-test-configs.sh` script.
	- Deploy the PMM using the `(*deployment.PMM) Deploy(ctx context.Context)` method.

	Example:
    ```go
	c := deployment.NewController(t)
	pmm := c.NewPMM("test-name", ".env.test")
	pmm.Deploy(ctx)
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
	c := deployment.NewController(t)
	pmm := c.NewPMM("test-name", ".env.test")
	pmm.Deploy(ctx)

	b := new(util.Binary)
	testDir := t.TempDir()

	args := []string{"-d", filepath.Join(testDir, "dump.tar.gz"), "--pmm-url", pmm.PMMURL(), "--dump-qan", "--click-house-url", pmm.ClickhouseURL()}

	pmm.Log("Exporting data to", filepath.Join(testDir, "dump.tar.gz"))
	stdout, stderr, err := b.Run(append([]string{"export", "--ignore-load"}, args...)...)
	if err != nil {
		t.Fatal("failed to export", err, stdout, stderr)
	}
}
```
