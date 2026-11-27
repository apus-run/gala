#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
project="gala-dlock-faults-$$"
compose() { docker compose -p "$project" -f integration/faults.yaml "$@"; }
trap 'compose down --volumes' EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
compose up -d --wait --wait-timeout 120
export DLOCK_FAULT_PROJECT="$project"
for service in primary replica sentinel-1 sentinel-2 sentinel-3 a b c; do
    case "$service" in
        sentinel-*) port=26379 ;;
        primary|replica) port=6379 ;;
        *) port=2379 ;;
    esac
    address=$(compose port "$service" "$port")
    id=$(compose ps -q "$service")
    suffix=$(printf '%s' "$service" | tr 'a-z-' 'A-Z_')
    export "DLOCK_FAULT_${suffix}_ADDR=$address" "DLOCK_FAULT_${suffix}_ID=$id"
done
GOWORK=off go test -race -count=1 -tags=integration -run '^TestDockerFault' -timeout 4m -v .
