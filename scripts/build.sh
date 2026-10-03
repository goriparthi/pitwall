#!/bin/sh
# Builds bin/pitwall for this Mac and dist/ binaries for every target. Windows builds need no cgo.
set -eu
cd "$(dirname "$0")/.."
export PKG_CONFIG="$PWD/scripts/pkg-config" # static libusb on macOS; no runtime dependency
go build -trimpath -o bin/pitwall ./cmd/pitwall
mkdir -p dist
GOOS=darwin GOARCH=arm64 go build -trimpath -o dist/pitwall-darwin-arm64 ./cmd/pitwall
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -o dist/pitwall-windows-amd64.exe ./cmd/pitwall
GOOS=windows GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -o dist/pitwall-windows-arm64.exe ./cmd/pitwall
ls -lh bin/pitwall dist/
