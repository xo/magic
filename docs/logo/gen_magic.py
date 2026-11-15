#!/usr/bin/env python3
"""Generate magic.svg: the logo for magic, a pure Go port of libmagic.

The white letters ("ma" and "ic") are outlines traced with potrace from the
original raster logo and stored in magic_letters.json; the bear "g" and the
rest of the artwork are drawn here. The binary digits are converted to
outlines from URW Gothic Book (ships with ghostscript's gsfonts). The SVG
does not depend on any installed font.
"""
import json
import math

from fontTools.pens.svgPathPen import SVGPathPen
from fontTools.pens.transformPen import TransformPen
from fontTools.ttLib import TTFont

FONT = "/usr/share/fonts/gsfonts/URWGothic-Book.otf"

W, H = 2000, 1116
LETTER = "#8a6cf0"   # violet: ~3.8:1 contrast on both dark and white
BEAR = "#0e93bd"     # ~3.5:1 on white, ~4.2:1 on dark
CYAN = "#5ee9fa"
INDIGO = "#6a6fe0"
PURPLE = "#6b4fd8"
SKY = "#9fdcff"

LETTERS = json.load(open("magic_letters.json"))

font = TTFont(FONT)
glyphs = font.getGlyphSet()
cmap = font.getBestCmap()
upm = font["head"].unitsPerEm
cap_h = font["OS/2"].sCapHeight or 740


def text_path(s, x, baseline, size, tracking=0.0):
    """Return (path d, advance) for s set at size px with extra tracking px."""
    pen = SVGPathPen(glyphs)
    k = size / upm
    cx = x
    for ch in s:
        g = cmap[ord(ch)]
        tp = TransformPen(pen, (k, 0, 0, -k, cx, baseline))
        glyphs[g].draw(tp)
        cx += glyphs[g].width * k + tracking
    return pen.getCommands(), cx - x - tracking



def star(cx, cy, r, pinch=0.16, rx=None):
    """Four-point sparkle."""
    rx = rx or r
    p = pinch
    return (f"M{cx},{cy-r} "
            f"C{cx+rx*p},{cy-r*p} {cx+rx*p},{cy-r*p} {cx+rx},{cy} "
            f"C{cx+rx*p},{cy+r*p} {cx+rx*p},{cy+r*p} {cx},{cy+r} "
            f"C{cx-rx*p},{cy+r*p} {cx-rx*p},{cy+r*p} {cx-rx},{cy} "
            f"C{cx-rx*p},{cy-r*p} {cx-rx*p},{cy-r*p} {cx},{cy-r}Z")


def f(v):
    return f"{v:.2f}".rstrip("0").rstrip(".")


# ---------------------------------------------------------------- wordmark
# g as a bear
gx, gy, gr = 1178, 562.5, 86
g_head = f"M{gx-gr},{gy} a{gr},{gr} 0 1 0 {2*gr},0 a{gr},{gr} 0 1 0 -{2*gr},0Z"
g_ears = (f'<circle cx="1108" cy="497" r="19"/>'
          f'<circle cx="1252" cy="497" r="19"/>')
g_stem = (f"M1249,505 V660 C1249,694 1220,{725-17} 1180,{725-17} "
          f"C1150,{725-17} 1124,697 1115,669")
eyes = ((1144, 544, 1154), (1217, 544, 1207))
g_face = "".join(f'<circle cx="{ex}" cy="{ey}" r="27"/>' for ex, ey, _ in eyes)
g_nose = ("M1158,576 C1158,566 1170,565 1181,565 C1192,565 1204,566 1204,576 "
          "C1204,586 1197,592 1190,592 C1186,592 1183,589 1181,586 "
          "C1179,589 1176,592 1172,592 C1165,592 1158,586 1158,576Z")
pupils = "".join(f'<circle cx="{px}" cy="{ey-1}" r="7"/>' for _, ey, px in eyes)

# ---------------------------------------------------------------- document
doc = ("M543,357 H718 Q730,357 730,369 V671 Q730,683 718,683 "
       "H487 Q475,683 475,671 V425 Z")
doc_fold = "M543,357 V407 Q543,425 525,425 H475"

# ---------------------------------------------------------------- wand
wb, wt = (490, 681), (772, 401)          # bottom and top end centres
wr = 11
ux, uy = wt[0] - wb[0], wt[1] - wb[1]
L = math.hypot(ux, uy)
ux, uy = ux / L, uy / L
nx, ny = -uy, ux


def along(t, off=0.0):
    return (wb[0] + ux * t + nx * off, wb[1] + uy * t + ny * off)


def capsule(t0, t1, r, round0=True, round1=True):
    a0, a1 = along(t0, r), along(t1, r)
    b1, b0 = along(t1, -r), along(t0, -r)
    s = f"M{f(a0[0])},{f(a0[1])} L{f(a1[0])},{f(a1[1])} "
    s += (f"A{r},{r} 0 0 0 {f(b1[0])},{f(b1[1])} " if round1
          else f"L{f(b1[0])},{f(b1[1])} ")
    s += f"L{f(b0[0])},{f(b0[1])} "
    s += f"A{r},{r} 0 0 0 {f(a0[0])},{f(a0[1])}Z" if round0 else "Z"
    return s


tip_t = L - 66
wand_body = capsule(0, L, wr)
sep0, sep1 = along(tip_t, wr), along(tip_t, -wr)
wand_sep = f"M{f(sep0[0])},{f(sep0[1])} L{f(sep1[0])},{f(sep1[1])}"
glow_c = along(L - 18)

# ---------------------------------------------------------------- swoosh
# tapered ribbon: outer and inner edges of a curve from the wand to the star
swoosh = ("M676,548 "
          "C716,612 790,662 880,685 C940,700 990,700 1040,694 "
          "C990,705 940,708 878,697 C786,676 714,624 676,548Z")

swoosh_glow = ("M690,545 C730,600 790,640 860,665 C900,680 940,688 980,690 "
               "C930,700 880,698 840,688 C770,668 715,625 690,545Z")

# ---------------------------------------------------------------- sparkles
sparkles = [
    # (cx, cy, r, color, glow)
    (1320, 428, 30, "url(#gSpark)", True),   # dot of the i
    (1348, 398, 9, SKY, False),
    (830, 408, 20, "#8c9cf6", True),
    (898, 307, 11, "#7aa6f5", False),
    (1040, 292, 12, "#8a7cf0", False),
    (1120, 353, 10, "#7a68e6", False),
    (690, 423, 11, "#77dcf6", False),
    (1205, 368, 7, "#7fd4f6", False),
    (688, 566, 7, "#a8d8ff", False),
    (972, 736, 8, "#8fd0ff", False),
]
crosses = [(620, 425, 6, "#77dcf6"), (588, 508, 5, "#8cc4f2"),
           (690, 390, 4, "#7a7fd8"), (1263, 340, 4, "#7a8ae0")]
dots = [
    (650, 452, 3.5, "#6fd8f4"), (570, 461, 3, "#6fd8f4"), (525, 508, 3, "#6aa8ee"),
    (545, 556, 3, "#7a8ae0"), (810, 315, 3.5, "#6fd8f4"), (790, 448, 4.5, "#8ab4f8"),
    (882, 411, 3.5, "#7f6ae8"), (887, 352, 3, "#7f6ae8"), (960, 333, 4, "#6fb8f4"),
    (1020, 348, 3, "#6fb8f4"), (1068, 340, 6, "#7a6ce8"), (1122, 312, 2.5, "#7a6ce8"),
    (1172, 342, 3, "#7a6ce8"), (1200, 326, 2.5, "#7a8ae0"), (1245, 368, 4, "#7a6ce8"),
    (1295, 401, 3, "#7fd4f6"), (1350, 455, 3, "#7fd4f6"), (782, 605, 3.5, "#6b62d8"),
    (867, 668, 3.5, "#8fb4ff"), (628, 538, 2.5, "#6aa8ee"),
]
quads = [(848, 348, "#7fb8f2"), (997, 388, "#7fb8f2"), (1157, 290, "#7a7fd8")]
diamond = (930, 372, 10, "#7fb8f2")
grid = (768, 346, "#6c7fd8")

digits = [  # (char, x, baseline, cap, color)
    ("1", 668, 614, 12, "#5a4cb0"), ("0", 695, 654, 18, "#6a58c8"),
    ("1", 806, 644, 18, "#6a58c8"), ("1", 750, 692, 18, "#7666d8"),
    ("0", 790, 706, 13, "#7666d8"), ("0", 828, 732, 18, "#7f82e6"),
    ("1", 872, 744, 18, "#7f9cf0"), ("1", 918, 744, 12, "#7f9cf0"),
    ("0", 899, 683, 18, "#89b8f6"), ("1", 938, 692, 12, "#89b8f6"),
]

# ---------------------------------------------------------------- build
o = []
w = o.append
# crop: content bounds (465,280)-(1516,749), measured from a render, plus a
# 32px margin (room for the letter shadow); drawing coordinates stay in the original 2000x1116 space
VB = (433, 248, 1115, 533)
w(f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="{" ".join(map(str, VB))}" '
  f'width="{VB[2]}" height="{VB[3]}">')
w("<title>magic: a pure Go port of libmagic</title>")
w("<defs>")
w(f'<linearGradient id="gDoc" gradientUnits="userSpaceOnUse" x1="500" y1="370" '
  f'x2="700" y2="690"><stop offset="0" stop-color="{CYAN}"/>'
  f'<stop offset=".55" stop-color="#5d9ce8"/>'
  f'<stop offset="1" stop-color="{INDIGO}"/></linearGradient>')
w(f'<linearGradient id="gWand" gradientUnits="userSpaceOnUse" '
  f'x1="{wb[0]}" y1="{wb[1]}" x2="{wt[0]}" y2="{wt[1]}">'
  f'<stop offset="0" stop-color="#6a5fd6"/><stop offset=".55" stop-color="#5aa6ee"/>'
  f'<stop offset="1" stop-color="#7cf0ff"/></linearGradient>')
w('<linearGradient id="gSwoosh" gradientUnits="userSpaceOnUse" x1="680" y1="560" '
  'x2="1040" y2="694"><stop offset="0" stop-color="#5a3fc0" stop-opacity="0"/>'
  f'<stop offset=".2" stop-color="{PURPLE}"/><stop offset=".6" stop-color="#6f78e8"/>'
  f'<stop offset="1" stop-color="{SKY}"/></linearGradient>')
w('<radialGradient id="gSpark"><stop offset="0" stop-color="#e8fbff"/>'
  '<stop offset="1" stop-color="#6fd0f8"/></radialGradient>')
w('<radialGradient id="gHalo"><stop offset="0" stop-color="#7fe6ff" stop-opacity=".55"/>'
  '<stop offset=".4" stop-color="#5fb8ff" stop-opacity=".18"/>'
  '<stop offset="1" stop-color="#5fb8ff" stop-opacity="0"/></radialGradient>')
w('<filter id="glow" x="-50%" y="-50%" width="200%" height="200%">'
  '<feGaussianBlur stdDeviation="4" result="b"/>'
  '<feMerge><feMergeNode in="b"/><feMergeNode in="SourceGraphic"/></feMerge></filter>')
w('<filter id="glowBig" x="-50%" y="-50%" width="200%" height="200%">'
  '<feGaussianBlur stdDeviation="9" result="b"/>'
  '<feMerge><feMergeNode in="b"/><feMergeNode in="b"/>'
  '<feMergeNode in="SourceGraphic"/></feMerge></filter>')
w('<filter id="glowSoft" x="-20%" y="-20%" width="140%" height="140%">'
  '<feGaussianBlur stdDeviation="3" result="b"/><feComponentTransfer in="b" result="b2">'
  '<feFuncA type="linear" slope=".6"/></feComponentTransfer>'
  '<feMerge><feMergeNode in="b2"/><feMergeNode in="SourceGraphic"/></feMerge></filter>')
w('<filter id="softBig" x="-30%" y="-60%" width="160%" height="220%">'
  '<feGaussianBlur stdDeviation="14"/></filter>')
w('<filter id="soft" x="-20%" y="-50%" width="140%" height="200%">'
  '<feGaussianBlur stdDeviation="6"/></filter>')
# masks cut real holes (transparent background): the wand's clearance
# through everything beneath it, and the bear's eyes and nose
mask_rect = f'<rect x="0" y="0" width="{W}" height="{H}" fill="#fff"/>'
w(f'<mask id="mWand" maskUnits="userSpaceOnUse" x="0" y="0" width="{W}" height="{H}">'
  f'{mask_rect}<path d="{capsule(0, L, wr + 7)}" fill="#000"/></mask>')
w(f'<mask id="mFace" maskUnits="userSpaceOnUse" x="0" y="0" width="{W}" height="{H}">'
  f'{mask_rect}<g fill="#000">{g_face}<path d="{g_nose}"/></g></mask>')
w('<filter id="shadow" x="-10%" y="-30%" width="120%" height="160%">'
  '<feGaussianBlur in="SourceAlpha" stdDeviation="9"/><feOffset dy="5" result="b"/>'
  '<feFlood flood-color="#000" flood-opacity=".55"/><feComposite in2="b" operator="in"/>'
  '<feMerge><feMergeNode/><feMergeNode in="SourceGraphic"/></feMerge></filter>')
w("</defs>")
w('<g mask="url(#mWand)">')

# document outline
w(f'<g fill="none" stroke="url(#gDoc)" stroke-width="4.5" stroke-linejoin="round" '
  f'stroke-linecap="round" filter="url(#glowSoft)">'
  f'<path d="{doc}"/><path d="{doc_fold}"/></g>')

# swoosh (glow + ribbon), behind the letters
w(f'<path d="{swoosh_glow}" fill="#6a48d8" filter="url(#softBig)" opacity=".55"/>')
w(f'<path d="{swoosh}" fill="url(#gSwoosh)" filter="url(#soft)" opacity=".8"/>')
w(f'<path d="{swoosh}" fill="url(#gSwoosh)"/>')

# binary digits
for ch, x, b, cap, col in digits:
    w(f'<path fill="{col}" d="{text_path(ch, x, b, cap * upm / cap_h)[0]}"/>')

# sparkle field
for cx, cy, r, col in crosses:
    w(f'<path d="M{cx-r},{cy}h{2*r}M{cx},{cy-r}v{2*r}" stroke="{col}" '
      f'stroke-width="2" stroke-linecap="round"/>')
for cx, cy, r, col in dots:
    w(f'<circle cx="{cx}" cy="{cy}" r="{r}" fill="{col}"/>')
for cx, cy, col in quads:
    w(f'<g fill="{col}">' + "".join(
        f'<circle cx="{cx+dx}" cy="{cy+dy}" r="1.8"/>'
        for dx, dy in ((-5, -5), (5, -5), (-5, 5), (5, 5))) + "</g>")
dx_, dy_, dr, dcol = diamond
w(f'<path d="M{dx_},{dy_-dr}L{dx_+dr},{dy_}L{dx_},{dy_+dr}L{dx_-dr},{dy_}Z" '
  f'fill="none" stroke="{dcol}" stroke-width="2.5" stroke-linejoin="round"/>')
gx_, gy_, gcol = grid
w(f'<g fill="{gcol}">' + "".join(
    f'<rect x="{gx_-8+3*a_}" y="{gy_-8+3*b_}" width="1.8" height="1.8"/>'
    for a_ in range(6) for b_ in range(6) if (a_ + b_) % 2 == 0) + "</g>")

# bear g
w(f'<g mask="url(#mFace)"><g fill="{BEAR}"><path d="{g_head}"/>{g_ears}</g>'
  f'<path d="{g_stem}" fill="none" stroke="{BEAR}" stroke-width="34"/></g>')
w(f'<g fill="{BEAR}">{pupils}</g>')
w('</g>')  # end mWand

# wand: body, tip, glow
w(f'<path d="{wand_body}" fill="none" stroke="url(#gWand)" stroke-width="3.5" '
  f'filter="url(#glowSoft)"/>')
w(f'<circle cx="{f(glow_c[0])}" cy="{f(glow_c[1])}" r="60" fill="url(#gHalo)" opacity=".45"/>')
w(f'<path d="{capsule(tip_t, L, wr)}" fill="none" stroke="#b8f8ff" stroke-width="3.5" filter="url(#glow)"/>')
w(f'<path d="{wand_sep}" stroke="#7cf0ff" stroke-width="3"/>')

# letters: above the wand and the document, not cut by the wand mask
w(f'<g fill="{LETTER}" filter="url(#shadow)">'
  f'<g transform="{LETTERS["transform"]}">'
  f'<path d="{LETTERS["ma"]}"/><path d="{LETTERS["ic"]}"/></g></g>')

# star at the end of the swoosh
w(f'<circle cx="1040" cy="694" r="55" fill="url(#gHalo)"/>')
w(f'<path d="{star(1040, 694, 34, 0.12, 30)}" fill="url(#gSpark)" filter="url(#glowBig)"/>')
w('<g stroke="#8fd0ff" stroke-width="2.5" stroke-linecap="round">'
  '<path d="M1006,675 l6,5"/><path d="M1006,716 l6,-5"/></g>')

# sparkles
for cx, cy, r, col, glow in sparkles:
    filt = ' filter="url(#glowBig)"' if glow else ""
    if glow and r > 25:
        w(f'<circle cx="{cx}" cy="{cy}" r="{r*1.7}" fill="url(#gHalo)"/>')
    w(f'<path d="{star(cx, cy, r)}" fill="{col}"{filt}/>')


w("</svg>")
open("magic.svg", "w").write("\n".join(o) + "\n")
