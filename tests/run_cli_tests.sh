#set -x
set -e
cd $CL_HOME
go build ./cmd/clace
cd tests
rm -rf clace.db

export CL_HOME=.
unset CL_CONFIG_FILE

export VERBOSE="--verbose"

trap "error_handler" ERR

error_handler () {
    echo "Error occurred, running cleanup"
    cleanup
    echo "Test failed"
    exit 1
}

cleanup() {
  rm -rf clace.db
  #rm -rf logs/ clace.toml  server.stdout
  set +e
  ps -ax | grep "clace server start" | grep -v grep | cut -c1-6 | xargs kill -9

  # Github Actions does not seem to allow kill, the last echo is to allow the exit code to be zero
  echo "Done with cleanup"
}

# Test error messages
../clace server start --admin_password=abcd --http.port=9999 &
sleep 2
commander test test_errors.yaml
rm -rf clace.db

# Test server prints a password when started without config
../clace server start --http.port=9998 > server.stdout &
sleep 2
grep "Admin password" server.stdout

# Run all other automated tests
echo "admin_password = \"qwerty\"" > clace.toml

export CL_CONFIG_FILE=clace.toml

../clace server start -l trace &
sleep 2
commander test $VERBOSE --dir ./commander/

echo $?

cleanup
echo "All tests passed"
