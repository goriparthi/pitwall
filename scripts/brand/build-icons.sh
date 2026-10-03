#!/bin/sh
# Rebuilds every raster icon from the Dial SVGs (scripts/brand/dial.py) using headless Chrome, sips and iconutil.
set -eu
cd "$(dirname "$0")/../.."
CHROME="${PITWALL_CHROME:-/Applications/Google Chrome.app/Contents/MacOS/Google Chrome}"
B=assets/brand
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

python3 scripts/brand/dial.py "$B" >/dev/null

render() { # svg, px, out: renders on a transparent background at px x px
  sed -E "s/width=\"[0-9]+\" height=\"[0-9]+\"/width=\"$2\" height=\"$2\"/" "$1" > "$TMP/in.svg"
  "$CHROME" --headless=new --disable-gpu --hide-scrollbars --default-background-color=00000000 \
    --window-size="$2,$2" --screenshot="$3" "file://$TMP/in.svg" >/dev/null 2>&1
}

render "$B/dial-app-icon.svg" 1024 "$TMP/app-1024.png"
render "$B/dial-menu-template.svg" 512 "$TMP/menu-512.png"
size() { sips -z "$2" "$2" "$1" --out "$3" >/dev/null; }

# macOS: app icon set -> .icns, plus black template glyphs for the menu bar
mkdir -p assets/icons/macos "$TMP/Pitwall.iconset"
for s in 16 32 128 256 512; do
  size "$TMP/app-1024.png" "$s" "$TMP/Pitwall.iconset/icon_${s}x${s}.png"
  size "$TMP/app-1024.png" $((s * 2)) "$TMP/Pitwall.iconset/icon_${s}x${s}@2x.png"
done
iconutil -c icns "$TMP/Pitwall.iconset" -o assets/icons/macos/Pitwall.icns
cp "$TMP/app-1024.png" assets/icons/macos/icon-1024.png
size "$TMP/menu-512.png" 16 assets/icons/macos/menu-template-16.png
size "$TMP/menu-512.png" 32 assets/icons/macos/menu-template-32.png
cp "$B/dial-menu-template.svg" assets/icons/macos/menu-template.svg

# Windows: PNG sizes and a multi-size .ico
mkdir -p assets/icons/windows
for s in 16 20 24 32 40 48 64 128 256 512 1024; do size "$TMP/app-1024.png" "$s" "assets/icons/windows/icon-$s.png"; done
python3 - <<'PY'
import struct
sizes = [16, 20, 24, 32, 40, 48, 64, 128, 256]
data = [open(f"assets/icons/windows/icon-{s}.png", "rb").read() for s in sizes]
out = struct.pack("<HHH", 0, 1, len(sizes))
offset = 6 + 16 * len(sizes)
for s, d in zip(sizes, data):
    out += struct.pack("<BBBBHHII", s % 256, s % 256, 0, 0, 1, 32, len(d), offset)
    offset += len(d)
open("assets/icons/windows/Pitwall.ico", "wb").write(out + b"".join(data))
PY

# Web
mkdir -p assets/icons/web
cp "$B/dial-app-icon.svg" assets/icons/web/favicon.svg
for s in 32 192 512; do size "$TMP/app-1024.png" "$s" "assets/icons/web/icon-$s.png"; done
cp assets/icons/windows/Pitwall.ico assets/icons/web/favicon.ico

# Copies the app embeds (web UI and tray)
cp "$B/dial-lockup-light.svg" web/brand/lockup-light.svg
cp "$B/dial-app-icon.svg" web/brand/favicon.svg
cp assets/icons/macos/menu-template-16.png assets/icons/macos/menu-template-32.png assets/icons/windows/icon-32.png internal/tray/assets/
echo "icons rebuilt"
