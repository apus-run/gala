#!/bin/sh
set -eu
cd "$(dirname "$0")"
unset DLOCK_TEST_MONGODB_URI
profile=$(mktemp)
trap 'rm -f "$profile"' EXIT
GOWORK=off go test -race -count=1 -covermode=atomic -coverpkg=./... -coverprofile="$profile" ./...

# Each test package can report the same source block. Count its statements
# once, and consider the block covered if any package exercised it.
awk '
    NR > 1 { statements[$1] = $2; if ($3 > 0) covered[$1] = 1 }
    END {
        for (block in statements) {
            total += statements[block]
            if (covered[block]) hit += statements[block]
        }
        if (!total) exit 1
        printf "Unit statement coverage: %d/%d (%.2f%%), minimum 99%%\n", hit, total, 100 * hit / total
        exit (100 * hit < 99 * total)
    }
' "$profile"
