#!/bin/sh
# 只检查本 Skill 的随包示例；不批量修改目标库。
set -eu

race=0
case "${1-}" in
    "") ;;
    --race) race=1 ;;
    *) printf 'Usage: %s [--race]\n' "$0" >&2; exit 2 ;;
esac
if [ "$#" -gt 1 ]; then
    printf 'Usage: %s [--race]\n' "$0" >&2
    exit 2
fi

for tool in go gofmt; do
    if ! command -v "$tool" >/dev/null 2>&1; then
        printf 'Required tool not found: %s\n' "$tool" >&2
        exit 127
    fi
done

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$root/assets/example"

# 隔离父目录 go.work，并避免自动下载/切换 Go 工具链。
export GOWORK=off
export GOTOOLCHAIN=local

go version
unformatted=$(gofmt -l .)
if [ -n "$unformatted" ]; then
    printf 'Run gofmt on the following example files:\n%s\n' "$unformatted" >&2
    exit 1
fi
printf '\n== go test ==\n'
go test -count=1 ./...
printf '\n== go vet ==\n'
go vet ./...
if [ "$race" -eq 1 ]; then
    printf '\n== go test -race ==\n'
    go test -race -count=1 ./...
fi
printf '\nAll requested example checks passed.\n'
