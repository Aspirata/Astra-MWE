#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
architecture="${1:-$(go env GOARCH)}"
case "$architecture" in amd64|arm64) ;; *) echo 'Expected amd64 or arm64' >&2; exit 1;; esac
if [[ "$architecture" != "$(go env GOHOSTARCH)" ]]; then
  echo 'Build the Linux GUI on a matching architecture (GTK/WebKitGTK and a C compiler are required).' >&2
  exit 1
fi
go run ./cmd/package-icon
(cd frontend && npm ci && npm run build)
mkdir -p build/bin
export GOARCH="$architecture"
go build -trimpath -tags desktop,production,webkit2_41 -ldflags '-s -w' -o "build/bin/astra-mwe-linux-$architecture" .
go build -trimpath -ldflags '-s -w' -o "build/bin/astra-cli-linux-$architecture" ./cmd/astra-cli
