#!/bin/sh
# Build from any working directory, with Go only. No frontend install/build step.
set -eu
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$root/control-plane"
go test -race ./...
go vet ./...
mkdir -p "$root/.local/bin"
CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o "$root/.local/bin/pulse-ops" .
printf 'Verified binary: %s/.local/bin/pulse-ops\n' "$root"
