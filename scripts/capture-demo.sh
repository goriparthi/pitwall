#!/bin/sh
# Records the demo story and renders site/img/pitwall-demo.gif (README) and .mp4/.jpg poster (website).
set -eu
cd "$(dirname "$0")/.."
export PKG_CONFIG="$PWD/scripts/pkg-config"
go build -o bin/pitwall ./cmd/pitwall
FRAMES="$(mktemp -d)"
trap 'rm -rf "$FRAMES"' EXIT
go run ./tools/capture -bin bin/pitwall -out "$FRAMES" -fps 10 -secs 20

# GIF: 1280 wide, one palette tuned on the whole clip, rectangle diffs keep unchanged areas cheap
ffmpeg -v error -y -framerate 10 -i "$FRAMES/frame_%04d.png" \
  -vf "scale=1280:-1:flags=lanczos,split[a][b];[a]palettegen=max_colors=192:stats_mode=diff[p];[b][p]paletteuse=dither=bayer:bayer_scale=4:diff_mode=rectangle" \
  -loop 0 site/img/pitwall-demo.gif
# MP4 at native resolution for the website
ffmpeg -v error -y -framerate 10 -i "$FRAMES/frame_%04d.png" -c:v libx264 -preset slow -crf 20 -pix_fmt yuv420p -movflags +faststart -r 30 site/img/pitwall-demo.mp4
# poster: the approval-alert moment
cp "$FRAMES/frame_0080.png" "$FRAMES/poster.png"
sips -s format jpeg -s formatOptions 88 "$FRAMES/poster.png" --out site/img/pitwall-demo-poster.jpg >/dev/null
ls -lh site/img/pitwall-demo.*
