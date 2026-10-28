#!/usr/bin/env python3
"""Generates a chrome/gold 80s-style logo 'CHIPTUNE PLAYER' as SVG (+ PNG via inkscape)."""
import subprocess
from pathlib import Path

S = 20  # stroke width (glyph box height = 100)

# Glyph center-line polylines in a 100-high box. (width, [polylines])
G = {
    "C": (70, [[(62, 10), (25, 10), (10, 25), (10, 75), (25, 90), (62, 90)]]),
    "H": (70, [[(10, 10), (10, 90)], [(60, 10), (60, 90)], [(10, 50), (60, 50)]]),
    "I": (20, [[(10, 10), (10, 90)]]),
    "P": (70, [[(10, 90), (10, 10), (45, 10), (60, 25), (60, 42), (45, 57), (10, 57)]]),
    "T": (70, [[(10, 10), (60, 10)], [(35, 10), (35, 90)]]),
    "U": (70, [[(10, 10), (10, 75), (25, 90), (45, 90), (60, 75), (60, 10)]]),
    "N": (70, [[(10, 10), (10, 90)], [(60, 10), (60, 90)], [(17, 20), (53, 80)]]),
    "E": (70, [[(60, 10), (10, 10), (10, 90), (60, 90)], [(10, 50), (48, 50)]]),
    "L": (70, [[(10, 10), (10, 90), (60, 90)]]),
    "A": (70, [[(10, 90), (10, 30), (30, 10), (40, 10), (60, 30), (60, 90)], [(10, 60), (60, 60)]]),
    "Y": (70, [[(10, 10), (10, 35), (25, 50), (45, 50), (60, 35), (60, 10)], [(35, 50), (35, 90)]]),
    "R": (70, [[(10, 90), (10, 10), (45, 10), (60, 25), (60, 40), (45, 55), (10, 55)],
               [(44, 62), (60, 76), (60, 90)]]),
}


def word_paths(text, gap):
    x, d = 0, []
    for ch in text:
        w, lines = G[ch]
        for pl in lines:
            d.append("M" + " L".join(f"{x + px},{py}" for px, py in pl))
        x += w + gap
    return " ".join(d), x - gap


def layers(d, gid):
    """Stacked strokes: glow, gold rim, dark separator, chrome face, top shine."""
    common = 'fill="none" stroke-linejoin="miter" stroke-miterlimit="10" stroke-linecap="square"'
    return f"""
    <path d="{d}" {common} stroke="#ff8a00" stroke-width="{S + 16}" opacity=".55" filter="url(#glow)"/>
    <path d="{d}" {common} stroke="url(#rim{gid})" stroke-width="{S + 8}"/>
    <path d="{d}" {common} stroke="#2a1404" stroke-width="{S + 2}"/>
    <path d="{d}" {common} stroke="url(#chrome{gid})" stroke-width="{S}"/>
    <path d="{d}" {common} stroke="url(#shine{gid})" stroke-width="{S}" opacity=".9"/>"""


def grads(gid, y0, h):
    y1 = y0 + h
    return f"""
    <linearGradient id="chrome{gid}" gradientUnits="userSpaceOnUse" x1="0" y1="{y0}" x2="0" y2="{y1}">
      <stop offset="0" stop-color="#ffffff"/>
      <stop offset=".22" stop-color="#e6ecf5"/>
      <stop offset=".44" stop-color="#bcc4d2"/>
      <stop offset=".49" stop-color="#868ea0"/>
      <stop offset=".50" stop-color="#7a4210"/>
      <stop offset=".54" stop-color="#d08424"/>
      <stop offset=".70" stop-color="#ffb840"/>
      <stop offset=".86" stop-color="#ffe2a0"/>
      <stop offset="1" stop-color="#fff3d0"/>
    </linearGradient>
    <linearGradient id="rim{gid}" gradientUnits="userSpaceOnUse" x1="0" y1="{y0 - 6}" x2="0" y2="{y1 + 6}">
      <stop offset="0" stop-color="#fff6c8"/>
      <stop offset=".35" stop-color="#f0b040"/>
      <stop offset=".5" stop-color="#c47a26"/>
      <stop offset=".7" stop-color="#ffb030"/>
      <stop offset="1" stop-color="#a85a14"/>
    </linearGradient>
    <linearGradient id="shine{gid}" gradientUnits="userSpaceOnUse" x1="0" y1="{y0}" x2="0" y2="{y1}">
      <stop offset="0" stop-color="#fff" stop-opacity=".9"/>
      <stop offset=".08" stop-color="#fff" stop-opacity="0"/>
      <stop offset="1" stop-color="#fff" stop-opacity="0"/>
    </linearGradient>"""


def sparkle(x, y, r):
    return f"""
    <g transform="translate({x},{y})" filter="url(#soft)">
      <circle r="{r * .35}" fill="url(#flare)"/>
      <path d="M0,{-r} L{r * .08},0 L0,{r} L{-r * .08},0 Z M{-r},0 L0,{r * .08} L{r},0 L0,{-r * .08} Z" fill="#fff"/>
    </g>"""


def build(inline=False):
    d1, w1 = word_paths("CHIPTUNE", 14)
    if inline:
        W, Hc, s2, sep = 1000, 190, 0.5, 34
        d2, w2 = word_paths("PLAYER", 22)
        x1, y1 = (W - w1 - sep - w2 * s2) / 2, 45
        x2, y2 = x1 + w1 + sep, y1 + 100 * (1 - s2)
        bars = f"""<rect x="{x2 + 4}" y="{y1 + 6}" width="{w2 * s2 - 4}" height="4" fill="url(#barR)"/>
  <rect x="{x2 + 2}" y="{y1 + 22}" width="{w2 * s2 * .6}" height="3" fill="url(#barR)" opacity=".7"/>"""
        sparkles = sparkle(x1 + 22, y1 + 8, 26) + sparkle(x1 + w1 - 30, y1 + 92, 20) + sparkle(x2 + w2 * s2, y1 + 8, 16)
        name = "chiptune_player_logo_inline.svg"
    else:
        W, Hc, s2 = 820, 290, 0.58
        d2, w2 = word_paths("PLAYER", 30)
        x1, y1 = (W - w1) / 2, 45
        x2, y2 = (W - w2 * s2) / 2, 190
        lw = 110  # decorative line length
        ly = y2 + 50 * s2
        bars = f"""<rect x="{x2 - lw - 25}" y="{ly - 2}" width="{lw}" height="4" fill="url(#bar)"/>
  <rect x="{x2 + w2 * s2 + 25}" y="{ly - 2}" width="{lw}" height="4" fill="url(#barR)"/>"""
        sparkles = sparkle(x1 + 22, y1 + 8, 26) + sparkle(x1 + w1 - 30, y1 + 92, 20) + sparkle(x2 + w2 * s2 - 8, y2 + 4, 14)
        name = "chiptune_player_logo.svg"
    svg = f"""<svg xmlns="http://www.w3.org/2000/svg" width="{W}" height="{Hc}" viewBox="0 0 {W} {Hc}">
  <defs>
    <filter id="glow" x="-20%" y="-50%" width="140%" height="200%"><feGaussianBlur stdDeviation="7"/></filter>
    <filter id="soft"><feGaussianBlur stdDeviation=".6"/></filter>
    <radialGradient id="flare">
      <stop offset="0" stop-color="#fff"/><stop offset=".4" stop-color="#ffe9b0" stop-opacity=".8"/>
      <stop offset="1" stop-color="#ff9a20" stop-opacity="0"/>
    </radialGradient>
    <linearGradient id="bar" x1="0" x2="1">
      <stop offset="0" stop-color="#ff9a20" stop-opacity="0"/><stop offset="1" stop-color="#ffd27a"/>
    </linearGradient>
    <linearGradient id="barR" x1="1" x2="0">
      <stop offset="0" stop-color="#ff9a20" stop-opacity="0"/><stop offset="1" stop-color="#ffd27a"/>
    </linearGradient>
    {grads("A", 0, 100)}
    {grads("B", 0, 100)}
  </defs>
  <rect width="100%" height="100%" fill="#000"/>
  <g transform="translate({x1},{y1}) skewX(-6)">{layers(d1, "A")}
  </g>
  <g transform="translate({x2},{y2}) scale({s2}) skewX(-6)">{layers(d2, "B")}
  </g>
  {bars}
  {sparkles}
</svg>
"""
    out = Path(__file__).with_name(name)
    out.write_text(svg)
    png = out.with_suffix(".png")
    subprocess.run(["inkscape", str(out), "--export-type=png", f"--export-filename={png}", "--export-dpi=192"],
                   check=True, capture_output=True)
    print(out, png)


if __name__ == "__main__":
    build()
    build(inline=True)
