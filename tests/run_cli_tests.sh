#set -x
set -e
cd $CL_HOME
go build ./cmd/clace
cd tests
rm -rf clace.db

export CL_HOME=.
unset CL_CONFIG_FILE

# Enabling verbose is useful for debugging but the commander command seems to
# return exit code of 0 when verbose is enabled, even if tests fails. So verbose
# is disabled by default.
#export CL_TEST_VERBOSE="--verbose"

trap "error_handler" ERR

error_handler () {
    echo "Error occurred, running cleanup"
    cleanup
    echo "Test failed"
    exit 1
}

cleanup() {
  rm -rf clace.db
  rm -rf logs/ clace.toml  server.stdout
  set +e
  ps -ax | grep "clace server start" | grep -v grep | cut -c1-6 | xargs kill -9

  # Github Actions does not seem to allow kill, the last echo is to allow the exit code to be zero
  echo "Done with cleanup"
}

# Test error messages
rm -f run/clace.sock
# Use password hash for "abcd"
cat <<EOF > config_error_test.toml
[security]
admin_password_bcrypt = "\$2a\$10\$Hk5/XcvwrN.JRFrjdG0vjuGZxa5JaILdir1qflIj5i9DUPUyvIK7C"
EOF
CL_CONFIG_FILE=config_error_test.toml ../clace server start  --http.port=9154 --https.port=9155 &
sleep 2

commander test $CL_TEST_VERBOSE test_errors.yaml
rm -rf clace.db run/clace.sock config_error_test.toml

# Test server prints a password when started without config
../clace server start --http.port=9156 --https.port=9157 > server.stdout &
sleep 2
grep "Admin password" server.stdout
rm -f run/clace.sock

# Run all other automated tests, use password hash for "qwerty"
export CL_CONFIG_FILE=clace.toml
cat <<EOF > $CL_CONFIG_FILE
[security]
admin_password_bcrypt = "\$2a\$10\$PMaPsOVMBfKuDG04RsqJbeKIOJjlYi1Ie1KQbPCZRQx38bqYfernm"
EOF

../clace server start  -l trace &
sleep 2
commander test $CL_TEST_VERBOSE --dir ./commander/

echo $?

cleanup
echo "All tests passed"
