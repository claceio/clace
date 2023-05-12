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
ps -ax | grep clace | grep 9999 | grep -v grep | cut -c1-6
ps -ax | grep clace | grep 9999 | grep -v grep | cut -c1-6 | xargs kill -9
rm -rf clace.db

# Test server prints a password when started without config
../clace server start --http.port=9999 > server.stdout &
sleep 2
ps -ax | grep clace | grep 9999 | grep -v grep | cut -c1-6 | xargs kill -9
grep "Admin password" server.stdout


# Run all other automated tests
echo "admin_password = \"qwerty\"" > clace.toml

export CL_CONFIG_FILE=clace.toml

../clace server start &
sleep 2
commander test --dir ./commander/
ps -ax | grep "server start" | grep -v grep | cut -c1-6 | xargs kill -9
rm -rf clace.db
rm -rf logs/ clace.toml  server.stdout
