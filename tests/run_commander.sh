set -xe
cd $CL_ROOT
go build ./cmd/clace
cd tests
rm -rf clace.db

export CL_ROOT=.
unset CL_CONFIG_FILE

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

../clace server start &
sleep 2
commander test --dir ./commander/
rm -rf clace.db
rm -rf logs/ clace.toml  server.stdout

set +e
ps -ax | grep "clace server start" | grep -v grep | cut -c1-6 | xargs kill -9

echo "Done with CLI test"
