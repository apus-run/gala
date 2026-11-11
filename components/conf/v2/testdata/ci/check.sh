#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."
export GOWORK=off
check='go build ./... && go vet ./... && go test -race -count=1 ./... && go mod tidy -diff'
bash -c "$check"
for adapter in etcd consul filewatch; do
  testdata/ci/adapter.sh "$adapter" bash -c "$check"
done
unformatted=$(gofmt -l .)
if [[ -n "$unformatted" ]]; then echo "$unformatted" >&2; exit 1; fi
deps=$(go list -deps -f '{{if not .Standard}}{{if .Module}}{{if not .Module.Main}}{{.ImportPath}}{{end}}{{else}}{{.ImportPath}}{{end}}{{end}}' ./...)
if [[ -n "$deps" ]]; then echo "$deps" >&2; exit 1; fi
go list -m -f '{{.Path}}' all | awk '
  /^(go\.etcd\.io\/etcd|github\.com\/hashicorp\/consul|github\.com\/fsnotify\/fsnotify|google\.golang\.org\/(grpc|protobuf)|github\.com\/(golang|gogo)\/protobuf)(\/|$)/ {
    print "unexpected core dependency:", $0
    forbidden = 1
  }
  END { exit forbidden }
'
