#!/usr/bin/env bash
# Run a command in an adapter copy using the core from this checkout.
set -euo pipefail
core=$(cd "$(dirname "$0")/../.." && pwd)
adapter=$1
shift
export GOWORK=off
scratch=$(mktemp -d)
trap 'rm -rf "$scratch"' EXIT
cp -R "$core/sources/$adapter/." "$scratch/"
cd "$scratch"
module=$(cd "$core" && go list -m)
go mod edit -replace="$module=$core"
awk -v core="$module" '$1 != core' "$core/sources/$adapter/go.sum" > go.sum
"$@"
