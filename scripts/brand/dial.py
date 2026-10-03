#!/usr/bin/env python3
"""Generates the Pitwall Dial mark (ring split along the needle axis, tapered needle) as SVG variants."""
import math
import os
import re
import sys

OUT = sys.argv[1] if len(sys.argv) > 1 else "assets/brand"
ORANGE = "#F26B2A"   # logo needle only; the UI accent stays Pit Lime
INK = "#1F2226"      # ring on light backgrounds
PAPER = "#F3F5EF"    # ring on dark backgrounds
TILE = "#10171C"     # app icon tile (brand accentText token)

C, R, r, CUT, PHI = 100.0, 86.0, 38.0, 14.0, math.radians(36)
u = (math.sin(PHI), -math.cos(PHI))   # needle direction (up and right), SVG y grows down
p = (math.cos(PHI), math.sin(PHI))    # perpendicular, 90 degrees clockwise from u


def pt(d, t):
    return (C + p[0] * d + u[0] * t, C + p[1] * d + u[1] * t)


def f(x):
    return f"{x:.2f}"


def half(side):
    """One ring half: the cut runs along the needle axis, offset by CUT on each side."""
    d = side * CUT
    to, ti = math.sqrt(R * R - d * d), math.sqrt(r * r - d * d)
    s_out, s_in = (1, 0) if side > 0 else (0, 1)
    a, b = pt(d, to), pt(d, -to)
    c, e = pt(d, -ti), pt(d, ti)
    return (f"M{f(a[0])} {f(a[1])}A{R:g} {R:g} 0 0 {s_out} {f(b[0])} {f(b[1])}"
            f"L{f(c[0])} {f(c[1])}A{r:g} {r:g} 0 0 {s_in} {f(e[0])} {f(e[1])}Z")


def needle():
    tip_t, base_t, hw = R + 4, 4.0, 17.0
    tip = pt(0, tip_t)
    b1, b2 = pt(hw, base_t), pt(-hw, base_t)
    return f"M{f(tip[0])} {f(tip[1])}L{f(b1[0])} {f(b1[1])}L{f(b2[0])} {f(b2[1])}Z"


RING = half(1) + half(-1)
NEEDLE = needle()


def symbol(ring, needle_color, size=200, pad=0):
    vb = f"{-pad:g} {-pad:g} {200 + 2 * pad:g} {200 + 2 * pad:g}"
    return (f'<svg xmlns="http://www.w3.org/2000/svg" width="{size}" height="{size}" viewBox="{vb}">'
            f'<path d="{RING}" fill="{ring}"/><path d="{NEEDLE}" fill="{needle_color}"/></svg>\n')


def app_icon(size=1024):
    # rounded tile with the light ring; geometry scaled to ~66% of the tile like the kit's icons
    return (f'<svg xmlns="http://www.w3.org/2000/svg" width="{size}" height="{size}" viewBox="0 0 512 512">'
            f'<rect width="512" height="512" rx="112" fill="{TILE}"/>'
            f'<g transform="translate(86 86) scale(1.7)"><path d="{RING}" fill="{PAPER}"/><path d="{NEEDLE}" fill="{ORANGE}"/></g></svg>\n')


def wordmark_paths():
    # the kit's drawn wordmark (font-independent paths), reused so the lockup keeps the brand letterforms
    src = open(os.path.join(os.path.dirname(__file__), "..", "..", "assets", "brand", "wordmark-light.svg")).read()
    inner = re.search(r"<g[^>]*>(.*)</g>", src, re.S).group(1)
    return inner


def lockup(ring, word):
    # symbol (155 tall) + 32 gap + wordmark (525x155), heavier strokes to match the Dial board's weight
    return (f'<svg xmlns="http://www.w3.org/2000/svg" width="740" height="155" viewBox="0 0 740 155">'
            f'<g transform="translate(0 0) scale(.775)"><path d="{RING}" fill="{ring}"/><path d="{NEEDLE}" fill="{ORANGE}"/></g>'
            f'<g transform="translate(205 0)" fill="{word}" stroke="{word}" stroke-width="9" stroke-linejoin="round">{wordmark_paths()}</g></svg>\n')


files = {
    "dial-symbol-light.svg": symbol(PAPER, ORANGE),   # for dark backgrounds
    "dial-symbol-dark.svg": symbol(INK, ORANGE),      # for light backgrounds
    "dial-menu-template.svg": symbol("#000000", "#000000", size=22, pad=6),
    "dial-app-icon.svg": app_icon(),
    "dial-lockup-light.svg": lockup(PAPER, PAPER),
    "dial-lockup-dark.svg": lockup(INK, INK),
}
os.makedirs(OUT, exist_ok=True)
for name, svg in files.items():
    with open(os.path.join(OUT, name), "w") as fh:
        fh.write(svg)
    print(os.path.join(OUT, name))
