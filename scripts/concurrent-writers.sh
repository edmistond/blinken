#!/usr/bin/env bash
# Phase 0 proof: many separate blinken processes writing to one database at
# once, the way several agent sessions would. Expects every insert to succeed.
set -euo pipefail
BIN=${BIN:-$(go env GOPATH)/bin/blinken}
[ -x "$BIN" ] || { go build -o /tmp/blinken-cw ./cmd/blinken; BIN=/tmp/blinken-cw; }
WRITERS=${WRITERS:-12}
PER=${PER:-25}
DIR=$(mktemp -d)
export BLINKEN_DB="$DIR/blinken.db"
trap 'rm -rf "$DIR"' EXIT

start=$(date +%s.%N)
fail=0
for w in $(seq 1 "$WRITERS"); do
  (
    for i in $(seq 1 "$PER"); do
      "$BIN" guess -q --project "$DIR" "writer $w guess $i" || echo "writer $w insert $i FAILED" >&2
    done
  ) &
done
wait
end=$(date +%s.%N)
count=$("$BIN" guesses --compact --project "$DIR" | wc -l | tr -d ' ')
want=$((WRITERS * PER))
printf 'processes=%d inserts=%d rows=%d elapsed=%.2fs\n' "$WRITERS" "$want" "$count" "$(echo "$end - $start" | bc)"
[ "$count" -eq "$want" ] && echo OK || { echo "MISMATCH" >&2; exit 1; }
