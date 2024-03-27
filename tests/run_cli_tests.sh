#set -x
set -e
cd $CL_HOME
go build ./cmd/clace
cd tests
rm -rf clace.db

export CL_HOME=.
unset CL_CONFIG_FILE
unset SSH_AUTH_SOCK

# Enabling verbose is useful for debugging but the commander command seems to
# return exit code of 0 when verbose is enabled, even if tests fails. So verbose
# is disabled by default.
# export CL_TEST_VERBOSE="--verbose"

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

# Test basic functionality
rm -f run/clace.sock
# Use password hash for "abcd"
cat <<EOF > config_basic_test.toml
[security]
admin_password_bcrypt = "\$2a\$10\$Hk5/XcvwrN.JRFrjdG0vjuGZxa5JaILdir1qflIj5i9DUPUyvIK7C"
EOF
CL_CONFIG_FILE=config_basic_test.toml ../clace server start  --http.port=9154 --https.port=9155 &
sleep 2

commander test $CL_TEST_VERBOSE test_basics.yaml
rm -rf clace.db* run/clace.sock config_basic_test.toml

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
callback_url = "https://localhost:25223"
EOF

if [[ -n "$CL_INFOCLACE_SSH" ]]; then
  # CL_INFOCLACE_SSH env is set, test authenticated git access with ssh key
  # infoclace user has only read access to clace repo, which is anyway public
  echo "$CL_INFOCLACE_SSH" > ./infoclace_ssh

  cat <<EOF >> $CL_CONFIG_FILE
  [git_auth.infoclace]
  key_file_path = "./infoclace_ssh"
EOF
fi

if [[ -n "$CL_GITHUB_SECRET" ]]; then
  # CL_GITHUB_SECRET env is set, test github oauth login redirect

  cat <<EOF >> $CL_CONFIG_FILE

[auth.github_test]
key = "02507afb0ad9056fab09"
secret = "$CL_GITHUB_SECRET"

EOF
fi

../clace server start  -l trace &
sleep 2
commander test $CL_TEST_VERBOSE --dir ./commander/

echo $?

if [[ -n "$CL_INFOCLACE_SSH" ]]; then
  # test git ssh key access
  commander test $CL_TEST_VERBOSE test_github_auth.yaml
  rm ./infoclace_ssh
fi

if [[ -n "$CL_GITHUB_SECRET" ]]; then
  # test git oauth access are tested 
  commander test $CL_TEST_VERBOSE test_oauth.yaml
fi

cleanup
echo "All tests passed"
