#!/bin/sh
# Renders the tray menu icons from web/js/icons.js (one icon set for panel and menu) as 32 px black template PNGs.
set -eu
cd "$(dirname "$0")/../.."
CHROME="${PITWALL_CHROME:-/Applications/Google Chrome.app/Contents/MacOS/Google Chrome}"
OUT=internal/tray/assets/menu
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
NAMES="terminal app link activity gauge layers folder target eyeoff cycle restart image search settings power display shuffle agents cpu sun"

# One page, one 64 px cell per icon; Chrome renders at 2x the 32 px output so sips can downsample cleanly.
python3 - "$TMP/sheet.html" $NAMES <<'PY'
import re, sys
src = open("web/js/icons.js").read()
paths = dict(re.findall(r"^\s+(\w+): '([^']+)'", src, re.M))
out, names = sys.argv[1], sys.argv[2:]
cells = "".join(
    f'<svg width="64" height="64" viewBox="0 0 24 24" fill="none" stroke="#000" stroke-width="1.75" '
    f'stroke-linecap="round" stroke-linejoin="round"><path d="{paths[n]}"/></svg>' for n in names)
# a leading spacer cell: sips treats a 0,0 crop offset as "center", so no icon may start at x = 0
open(out, "w").write(f'<html><body style="margin:0;display:flex;background:transparent"><div style="width:64px;flex:none"></div>{cells}</body></html>')
PY
W=$(( ($(echo $NAMES | wc -w) + 1) * 64 ))
[ "$W" -lt 500 ] && W=500
"$CHROME" --headless=new --disable-gpu --hide-scrollbars --default-background-color=00000000 \
  --window-size="$W,64" --screenshot="$TMP/sheet.png" "file://$TMP/sheet.html" >/dev/null 2>&1
i=1
for n in $NAMES; do
  sips -c 64 64 --cropOffset 0 $((i * 64)) "$TMP/sheet.png" --out "$TMP/$n.png" >/dev/null
  sips -z 32 32 "$TMP/$n.png" --out "$OUT/$n.png" >/dev/null
  i=$((i + 1))
done

# Menu bar dials (template, connected, disconnected) from the Dial SVGs, same sheet-and-crop approach.
python3 scripts/brand/dial.py assets/brand >/dev/null
DIALS="template connected disconnected"
{ printf '<html><body style="margin:0;display:flex;background:transparent"><div style="width:64px;flex:none"></div>'
  for d in $DIALS; do printf '<img src="file://%s/assets/brand/dial-menu-%s.svg" width="64" height="64">' "$PWD" "$d"; done
  printf '</body></html>'; } > "$TMP/dials.html"
"$CHROME" --headless=new --disable-gpu --hide-scrollbars --default-background-color=00000000 --allow-file-access-from-files \
  --window-size=500,64 --screenshot="$TMP/dials.png" "file://$TMP/dials.html" >/dev/null 2>&1
i=1
for d in $DIALS; do
  sips -c 64 64 --cropOffset 0 $((i * 64)) "$TMP/dials.png" --out "$TMP/dial-$d.png" >/dev/null
  i=$((i + 1))
done
sips -z 32 32 "$TMP/dial-template.png" --out internal/tray/assets/menu-template-32.png >/dev/null
sips -z 16 16 "$TMP/dial-template.png" --out internal/tray/assets/menu-template-16.png >/dev/null
cp internal/tray/assets/menu-template-32.png internal/tray/assets/menu-template-16.png assets/icons/macos/
sips -z 32 32 "$TMP/dial-connected.png" --out internal/tray/assets/dial-connected-32.png >/dev/null
sips -z 32 32 "$TMP/dial-disconnected.png" --out internal/tray/assets/dial-disconnected-32.png >/dev/null
cp assets/brand/dial-menu-template.svg assets/icons/macos/menu-template.svg
echo "menu icons rendered to $OUT"
