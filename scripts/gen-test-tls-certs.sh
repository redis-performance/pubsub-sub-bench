#!/bin/bash
# Generates a throwaway CA + server + client certificate set for TLS integration
# testing. Mirrors the approach used by Redis's own utils/gen-test-certs.sh.
# Output goes to testdata/tls/certs/ (gitignored, regenerated on demand).
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
CERTS_DIR="$REPO_ROOT/testdata/tls/certs"

mkdir -p "$CERTS_DIR"
cd "$CERTS_DIR"

echo "[gen-test-tls-certs] generating certs in $CERTS_DIR"

# CA
openssl genrsa -out ca.key 2048 >/dev/null 2>&1
openssl req -x509 -new -nodes -sha256 -days 3650 \
    -key ca.key -out ca.crt \
    -subj "/O=pubsub-sub-bench/CN=pubsub-sub-bench Test CA" >/dev/null 2>&1

# Server cert (used by redis-server), SAN covers localhost/127.0.0.1
openssl genrsa -out redis.key 2048 >/dev/null 2>&1
openssl req -new -sha256 \
    -key redis.key -out redis.csr \
    -subj "/O=pubsub-sub-bench/CN=redis-tls-test-server" >/dev/null 2>&1
openssl x509 -req -sha256 -days 3650 \
    -in redis.csr -CA ca.crt -CAkey ca.key -CAcreateserial \
    -out redis.crt \
    -extfile <(printf "subjectAltName=DNS:localhost,IP:127.0.0.1") >/dev/null 2>&1

# Client cert (used by the go-redis client / pubsub-sub-bench itself for mTLS)
openssl genrsa -out client.key 2048 >/dev/null 2>&1
openssl req -new -sha256 \
    -key client.key -out client.csr \
    -subj "/O=pubsub-sub-bench/CN=redis-tls-test-client" >/dev/null 2>&1
openssl x509 -req -sha256 -days 3650 \
    -in client.csr -CA ca.crt -CAkey ca.key -CAcreateserial \
    -out client.crt >/dev/null 2>&1

rm -f redis.csr client.csr ca.srl

# World-readable: these are throwaway test-only certs, and the Redis container
# runs as a different (non-root) uid than the host user that generated them.
chmod 0644 ca.key redis.key client.key

echo "[gen-test-tls-certs] done: ca.crt, redis.{crt,key}, client.{crt,key}"
