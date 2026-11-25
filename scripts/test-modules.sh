#!/usr/bin/env bash
# Run vet and tests for every go module under the given tree
# 对给定目录下的每个 go 模块执行 vet 与测试
set -euo pipefail

tree="${1:?usage: test-modules.sh <tree> [go test flags]}"
shift
extra="$*"
root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"

find "$tree" -name go.mod -not -path '*/.git/*' -print0 | sort -z |
while IFS= read -r -d '' mod; do
	dir="$(dirname "$mod")"
	echo "==> $dir"
	(cd "$dir" && go vet ./... && go test -race $extra ./...)
done
