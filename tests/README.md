# Running Clace CLI tests

This folder has the tests for the Clace CLI. The tests use the [Commander](https://github.com/commander-cli/commander) CLI test framework.

To run the tests, ensure that CL_HOME env variable points to the Clace code base location. Install Commander by running

`go install github.com/commander-cli/commander/v2/cmd/commander@latest`

and then run

`$CL_HOME/tests/run_cli_tests.sh`

The CLI tests are run as part of the Github Actions [workflow](https://github.com/claceio/clace/blob/4ac5958977dc03134eb4e8dae6ad419440c6b5c0/.github/workflows/test.yml#L44)


