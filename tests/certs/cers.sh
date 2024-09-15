#!/bin/bash
# Based on https://github.com/haoel/mTLS/blob/main/key.sh
# run twice, first mv certs testcerts1 and then mv certs testcerts2

set -e

pushd `dirname $0` > /dev/null
SCRIPTPATH=`pwd -P`
popd > /dev/null
SCRIPTFILE=`basename $0`

mkdir -p ${SCRIPTPATH}/certs

cd ${SCRIPTPATH}/certs

DAYS=3650

# generate a self-signed rootCA file that would be used to sign both the server and client cert.
# Alternatively, we can use different CA files to sign the server and client, but for our use case, we would use a single CA.
openssl req -newkey rsa:2048 \
  -new -nodes -x509 \
  -days ${DAYS} \
  -out ca.crt \
  -keyout ca.key \
  -subj "/C=SO/ST=Earth/L=Mountain/O=IntTest/OU=IntCloud/CN=localhost"

function generate_client() {
  CLIENT=$1
  O=$2
  OU=$3
  openssl genrsa -out ${CLIENT}.key 2048
  openssl req -new -key ${CLIENT}.key -out ${CLIENT}.csr \
     -subj "/C=SO/ST=Earth/L=Mountain/O=$O/OU=$OU/CN=localhost"
  openssl x509  -req -in ${CLIENT}.csr \
    -extfile <(printf "subjectAltName=DNS:localhost") \
    -CA ca.crt -CAkey ca.key -out ${CLIENT}.crt -days ${DAYS} -sha256 -CAcreateserial
}

generate_client client.a Client-A Client-A-OU
generate_client client.b Client-B Client-B-OU

rm *.csr *.srl
