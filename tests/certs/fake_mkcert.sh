#!/bin/sh
# mkcert wrapper for test_certs.yaml: checks the argument layout and logs the
# domain next to the cert files. Runs the real mkcert if installed (test_load_app.yaml
# calls apps without -k, CI runs mkcert -install), else creates a self signed cert
if [ "$#" -ne 6 ] || [ "$1" != "-cert-file" ] || [ "$3" != "-key-file" ] || [ "$5" != "--" ]; then
  echo "unexpected mkcert args: $*" >&2
  exit 1
fi
echo "$6" >> "$(dirname "$2")/mkcert_calls.log"
if command -v mkcert >/dev/null 2>&1; then
  exec mkcert "$@"
fi
openssl req -x509 -newkey rsa:2048 -nodes -days 1 -subj "/CN=$6" -addext "subjectAltName=DNS:$6" \
  -keyout "$4" -out "$2" >/dev/null 2>&1
