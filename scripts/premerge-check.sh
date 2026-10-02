#!/usr/bin/env sh
set -eu

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
API="$ROOT/relive-api"

printf '%s
' "== ReLive pre-merge check =="
cd "$API"

printf '%s
' "[1/4] gofmt"
UNFORMATTED="$(gofmt -l .)"
if [ -n "$UNFORMATTED" ]; then
  printf '%s
' "Files need gofmt:"
  printf '%s
' "$UNFORMATTED"
  exit 1
fi

printf '%s
' "[2/4] go test"
go test ./...

printf '%s
' "[3/4] go vet"
go vet ./...

printf '%s
' "[4/4] go build"
go build ./...

printf '%s
' "ReLive pre-merge check passed."
