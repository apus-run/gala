#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
project="gala-dlock-test-$$"
compose() { docker compose -p "$project" -f integration/compose.yaml "$@"; }
trap 'compose down --volumes' EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
compose up -d --wait --wait-timeout 180
export DLOCK_REDIS_ADDR="$(compose port redis 6379)"
export DLOCK_MYSQL_ADDR="$(compose port mysql 3306)"
export DLOCK_POSTGRES_ADDR="$(compose port postgres 5432)"
export DLOCK_ETCD_ADDR="$(compose port etcd 2379)"
export DLOCK_CONSUL_ADDR="$(compose port consul 8500)"
export DLOCK_TEST_MONGODB_URI="mongodb://$(compose port mongo 27017)"
GOWORK=off go test -race -count=1 -tags=integration -run 'TestDockerLeases|TestMongoLeases' -v ./...
