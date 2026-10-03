#!/bin/sh
# Builds bin/pitwall for this Mac, plus dist/: macOS arm64 binary and Pitwall.app, Windows amd64/arm64 (no cgo needed).
set -eu
cd "$(dirname "$0")/.."
export PKG_CONFIG="$PWD/scripts/pkg-config" # static libusb on macOS; no runtime dependency
VERSION="$(git describe --tags --always --dirty 2>/dev/null || echo dev)"
go build -trimpath -o bin/pitwall ./cmd/pitwall
mkdir -p dist
GOOS=darwin GOARCH=arm64 go build -trimpath -o dist/pitwall-darwin-arm64 ./cmd/pitwall
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -o dist/pitwall-windows-amd64.exe ./cmd/pitwall
GOOS=windows GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -o dist/pitwall-windows-arm64.exe ./cmd/pitwall

# Menu bar app bundle: LSUIElement keeps it out of the Dock; the icon comes from the brand kit.
APP=dist/Pitwall.app
rm -rf "$APP"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"
cp dist/pitwall-darwin-arm64 "$APP/Contents/MacOS/pitwall"
cp assets/icons/macos/Pitwall.icns "$APP/Contents/Resources/Pitwall.icns"
cat > "$APP/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>CFBundleName</key><string>Pitwall</string>
  <key>CFBundleDisplayName</key><string>Pitwall</string>
  <key>CFBundleIdentifier</key><string>com.goriparthi.pitwall</string>
  <key>CFBundleExecutable</key><string>pitwall</string>
  <key>CFBundleIconFile</key><string>Pitwall</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>CFBundleShortVersionString</key><string>${VERSION}</string>
  <key>LSMinimumSystemVersion</key><string>13.0</string>
  <key>LSUIElement</key><true/>
</dict></plist>
PLIST
ls -lh bin/pitwall dist/ | grep -v total
