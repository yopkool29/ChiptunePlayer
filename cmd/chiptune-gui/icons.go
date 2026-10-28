package main

import (
	"math"

	"github.com/AllenDang/cimgui-go/imgui"
)

func cosf(a float32) float32 { return float32(math.Cos(float64(a))) }
func sinf(a float32) float32 { return float32(math.Sin(float64(a))) }

// Vector transport icons + Winamp-style beveled buttons drawn on the
// imgui draw list — no icon font needed, crisp at any DPI.
type iconKind int

const (
	iconPlay iconKind = iota
	iconPause
	iconStop
	iconRestart // back to song start (previous-track glyph)
	iconPrev    // previous subsong
	iconNext    // next subsong
	iconInfo
	iconClose
	iconMinus  // minimize
	iconFolder // set start directory
	iconSearch // search files/dirs
	iconGear   // config menu
	iconExport // export wav
)

const iconBtnSize = 30

// Beveled-button palette (dark face, light top-left edge, dark
// bottom-right edge — classic 90s player chrome).
var (
	bevFaceTop = [3]byte{72, 76, 92} // face gradient, top
	bevFaceBot = [3]byte{44, 47, 60} // face gradient, bottom
	bevEdgeHi  = uint32(0xFFB0B8CC)  // top/left bevel
	bevEdgeLo  = uint32(0xFF0A0B10)  // bottom/right bevel
	bevInk     = uint32(0xFFFFFFFF)  // icon ink (white)
	bevInkDis  = uint32(0xFF666666)  // icon ink disabled
)

func rgbU32(c [3]byte) uint32 {
	return imgui.ColorConvertFloat4ToU32(imgui.Vec4{
		X: float32(c[0]) / 255, Y: float32(c[1]) / 255,
		Z: float32(c[2]) / 255, W: 1,
	})
}

// drawBevel renders the Winamp-style 3D button frame at the rect.
// pressed inverts the bevel (sunken look), hover brightens the face.
func drawBevel(dl *imgui.DrawList, x0, y0, x1, y1 float32, hov, pressed, disabled bool) {
	top, bot := bevFaceTop, bevFaceBot
	switch {
	case disabled:
		top = [3]byte{36, 38, 48}
		bot = [3]byte{30, 32, 40}
	case pressed:
		top = [3]byte{30, 32, 42}
		bot = [3]byte{40, 43, 56}
	case hov:
		top = [3]byte{86, 91, 110}
		bot = [3]byte{54, 58, 74}
	}
	p0 := imgui.Vec2{X: x0, Y: y0}
	p1 := imgui.Vec2{X: x1, Y: y1}
	dl.AddRectFilledMultiColor(p0, p1, rgbU32(top), rgbU32(top), rgbU32(bot), rgbU32(bot))

	hi, lo := bevEdgeHi, bevEdgeLo
	if pressed {
		hi, lo = lo, hi
	}
	// 2 px bevel: light top+left, dark bottom+right
	dl.AddLineV(imgui.Vec2{X: x0, Y: y0 + .5}, imgui.Vec2{X: x1, Y: y0 + .5}, hi, 1.5)
	dl.AddLineV(imgui.Vec2{X: x0 + .5, Y: y0}, imgui.Vec2{X: x0 + .5, Y: y1}, hi, 1.5)
	dl.AddLineV(imgui.Vec2{X: x0, Y: y1 - .5}, imgui.Vec2{X: x1, Y: y1 - .5}, lo, 1.5)
	dl.AddLineV(imgui.Vec2{X: x1 - .5, Y: y0}, imgui.Vec2{X: x1 - .5, Y: y1}, lo, 1.5)
	// crisp outer border
	dl.AddRect(p0, p1, uint32(0xFF000000))
}

// iconButton draws a beveled button containing a vector icon.
func iconButton(id string, kind iconKind, disabled bool, onClick func()) {
	iconButtonS(id, kind, iconBtnSize, disabled, onClick)
}

// iconButtonS is iconButton with an explicit button size.
func iconButtonS(id string, kind iconKind, sz float32, disabled bool, onClick func()) {
	pos := imgui.CursorScreenPos()
	pressed := imgui.InvisibleButton("##"+id, imgui.Vec2{X: sz, Y: sz})
	hov := imgui.IsItemHovered() && !disabled
	act := imgui.IsItemActive() && !disabled
	if pressed && !disabled {
		onClick()
	}
	if !imgui.IsItemVisible() {
		return
	}
	dl := imgui.WindowDrawList()
	drawBevel(dl, pos.X, pos.Y, pos.X+sz, pos.Y+sz, hov, act, disabled)
	off := float32(0)
	if act {
		off = 1.5 // pressed: icon sinks down-right with the face
	}
	col := bevInk
	if disabled {
		col = bevInkDis
	}
	drawIcon(dl, kind, pos.X+sz/2+off, pos.Y+sz/2+off, sz*0.22, col)
}

// bevelButton draws a beveled text button (Export WAV, ↑, …).
func bevelButton(id, label string, disabled bool, onClick func()) {
	ts := imgui.CalcTextSize(label)
	bevelButtonW(id, label, ts.X+22, disabled, onClick)
}

// bevelButtonW is bevelButton with an explicit width.
func bevelButtonW(id, label string, w float32, disabled bool, onClick func()) {
	ts := imgui.CalcTextSize(label)
	h := float32(26)
	pos := imgui.CursorScreenPos()
	pressed := imgui.InvisibleButton("##tb"+id, imgui.Vec2{X: w, Y: h})
	hov := imgui.IsItemHovered() && !disabled
	act := imgui.IsItemActive() && !disabled
	if pressed && !disabled {
		onClick()
	}
	if !imgui.IsItemVisible() {
		return
	}
	dl := imgui.WindowDrawList()
	drawBevel(dl, pos.X, pos.Y, pos.X+w, pos.Y+h, hov, act, disabled)
	off := float32(0)
	if act {
		off = 1.5
	}
	col := bevInk
	if disabled {
		col = bevInkDis
	}
	dl.AddTextVec2V(
		imgui.Vec2{X: pos.X + (w-ts.X)/2 + off, Y: pos.Y + (h-ts.Y)/2 + off},
		col, label)
}

// drawLogo renders the app wordmark, 90s demoscene style: a 3D
// diamond glyph + extrabold-italic text with a real outline and a
// vertical orange-metal gradient (multi-band clip rects).
// Returns the drawn width.
func drawLogo(dl *imgui.DrawList, font *imgui.Font, x, y float32) float32 {
	const txt = "CHIPTUNE PLAYER"
	const fs = 22

	// diamond glyph (metal rhombus, orange like the app accent)
	cx, cy, r := x+9, y+fs*0.55, float32(8)
	dl.AddTriangleFilled(
		imgui.Vec2{X: cx, Y: cy - r},
		imgui.Vec2{X: cx - r, Y: cy},
		imgui.Vec2{X: cx + r, Y: cy}, uint32(0xFF4AB0FF))
	dl.AddTriangleFilled(
		imgui.Vec2{X: cx, Y: cy + r},
		imgui.Vec2{X: cx - r, Y: cy},
		imgui.Vec2{X: cx + r, Y: cy}, uint32(0xFF104A8A))
	dl.AddLineV(imgui.Vec2{X: cx - r, Y: cy}, imgui.Vec2{X: cx + r, Y: cy},
		uint32(0xFF08305A), 1)

	tx := x + 22
	if font == nil {
		dl.AddTextVec2V(imgui.Vec2{X: tx + 2, Y: y + 2}, uint32(0xFF000000), txt)
		dl.AddTextVec2V(imgui.Vec2{X: tx, Y: y}, uint32(0xFFDDE3F0), txt)
		return 178
	}
	tw := font.CalcTextSizeA(fs, 4096, -1, txt).X // max_width=-1 would clamp to ~0
	th := float32(fs) * 1.15

	// demoscene lean: the wordmark is drawn as thin horizontal strips,
	// each shifted right toward the top = shear transform (~16deg over
	// the italic font's own slant). Outline passes ride the same shear.
	const lean = 7.0
	const strips = 12
	outCol := uint32(0xFF16090A)

	// orange-metal ramp, sampled per strip
	ramp := func(f float32) uint32 {
		switch {
		case f < 0.30:
			return 0xFFD8F3FF // near-white hot top
		case f < 0.55:
			return 0xFF3DB6FF // light orange
		case f < 0.80:
			return 0xFF147EF0 // orange
		default:
			return 0xFF0A4296 // burnt orange bottom
		}
	}
	for i := 0; i < strips; i++ {
		f0 := float32(i) / strips
		f1 := float32(i+1) / strips
		xo := lean * (1 - (f0+f1)/2) // top strips shifted right
		dl.PushClipRectV(
			imgui.Vec2{X: tx - 2, Y: y + th*f0},
			imgui.Vec2{X: tx + tw + lean + 2, Y: y + th*f1}, false)
		for _, o := range [][2]float32{{-1.4, 0}, {1.4, 0}, {0, -1.4}, {0, 1.4}} {
			dl.AddTextFontPtr(font, fs,
				imgui.Vec2{X: tx + xo + o[0], Y: y + o[1]}, outCol, txt)
		}
		dl.AddTextFontPtr(font, fs,
			imgui.Vec2{X: tx + xo, Y: y}, ramp((f0+f1)/2), txt)
		dl.PopClipRect()
	}
	return tx + tw + lean - x
}

// drawVolIcon renders a tiny speaker glyph at (x,y) — titlebar volume.
// muted draws it dimmed with a slash.
func drawVolIcon(dl *imgui.DrawList, x, y float32, muted bool) {
	col := uint32(0xFF9AA0B4)
	if muted {
		col = uint32(0xFF555A6A)
	}
	// speaker silhouette: one convex polygon (rect body + flared cone)
	for _, p := range []imgui.Vec2{
		{X: x - 7, Y: y - 3}, {X: x - 3.5, Y: y - 3},
		{X: x + 1.5, Y: y - 6.5}, {X: x + 1.5, Y: y + 6.5},
		{X: x - 3.5, Y: y + 3}, {X: x - 7, Y: y + 3},
	} {
		dl.PathLineTo(p)
	}
	dl.PathFillConvex(col)
	if muted {
		// X mark in orange
		xc := uint32(0xFF2E9CFF)
		dl.AddLineV(imgui.Vec2{X: x + 4, Y: y - 4},
			imgui.Vec2{X: x + 9, Y: y + 4}, xc, 1.8)
		dl.AddLineV(imgui.Vec2{X: x + 4, Y: y + 4},
			imgui.Vec2{X: x + 9, Y: y - 4}, xc, 1.8)
	} else {
		// two sound arcs
		dl.PathArcToV(imgui.Vec2{X: x + 1, Y: y}, 4.5, -1.0, 1.0, 8)
		dl.PathStrokeV(col, 0, 1.3)
		dl.PathArcToV(imgui.Vec2{X: x + 1, Y: y}, 7, -0.9, 0.9, 8)
		dl.PathStrokeV(col, 0, 1.1)
	}
}

func drawIcon(dl *imgui.DrawList, kind iconKind, cx, cy, r float32, col uint32) {
	switch kind {
	case iconPlay:
		dl.AddTriangleFilled(
			imgui.Vec2{X: cx - r*0.7, Y: cy - r},
			imgui.Vec2{X: cx - r*0.7, Y: cy + r},
			imgui.Vec2{X: cx + r, Y: cy}, col)

	case iconPause:
		w := r * 0.55
		dl.AddRectFilled(
			imgui.Vec2{X: cx - r, Y: cy - r},
			imgui.Vec2{X: cx - r + w, Y: cy + r}, col)
		dl.AddRectFilled(
			imgui.Vec2{X: cx + r - w, Y: cy - r},
			imgui.Vec2{X: cx + r, Y: cy + r}, col)

	case iconStop:
		dl.AddRectFilled(
			imgui.Vec2{X: cx - r, Y: cy - r},
			imgui.Vec2{X: cx + r, Y: cy + r}, col)

	case iconRestart, iconPrev: // bar on the left + left-pointing triangle
		dl.AddRectFilled(
			imgui.Vec2{X: cx - r, Y: cy - r},
			imgui.Vec2{X: cx - r*0.55, Y: cy + r}, col)
		dl.AddTriangleFilled(
			imgui.Vec2{X: cx + r, Y: cy - r},
			imgui.Vec2{X: cx + r, Y: cy + r},
			imgui.Vec2{X: cx - r*0.3, Y: cy}, col)

	case iconNext: // right-pointing triangle + bar on the right
		dl.AddTriangleFilled(
			imgui.Vec2{X: cx - r, Y: cy - r},
			imgui.Vec2{X: cx - r, Y: cy + r},
			imgui.Vec2{X: cx + r*0.3, Y: cy}, col)
		dl.AddRectFilled(
			imgui.Vec2{X: cx + r*0.55, Y: cy - r},
			imgui.Vec2{X: cx + r, Y: cy + r}, col)

	case iconInfo:
		dl.AddCircleV(imgui.Vec2{X: cx, Y: cy}, r, col, 0, 1.6)
		// "i" : dot + stem
		dl.AddRectFilled(
			imgui.Vec2{X: cx - 1, Y: cy - r*0.55},
			imgui.Vec2{X: cx + 1, Y: cy - r*0.22}, col)
		dl.AddRectFilled(
			imgui.Vec2{X: cx - 1, Y: cy - r*0.02},
			imgui.Vec2{X: cx + 1, Y: cy + r*0.62}, col)

	case iconClose:
		dl.AddLineV(imgui.Vec2{X: cx - r*0.7, Y: cy - r*0.7},
			imgui.Vec2{X: cx + r*0.7, Y: cy + r*0.7}, col, 2)
		dl.AddLineV(imgui.Vec2{X: cx - r*0.7, Y: cy + r*0.7},
			imgui.Vec2{X: cx + r*0.7, Y: cy - r*0.7}, col, 2)

	case iconMinus:
		dl.AddRectFilled(
			imgui.Vec2{X: cx - r*0.8, Y: cy - 1.4},
			imgui.Vec2{X: cx + r*0.8, Y: cy + 1.4}, col)

	case iconFolder: // tab + body
		dl.AddRectFilled(
			imgui.Vec2{X: cx - r*0.9, Y: cy - r*0.75},
			imgui.Vec2{X: cx - r*0.1, Y: cy - r*0.35}, col)
		dl.AddRectFilled(
			imgui.Vec2{X: cx - r*0.9, Y: cy - r*0.35},
			imgui.Vec2{X: cx + r*0.9, Y: cy + r*0.75}, col)

	case iconSearch: // lens circle + handle
		dl.AddCircleV(imgui.Vec2{X: cx - r*0.25, Y: cy - r*0.25},
			r*0.6, col, 0, 2)
		dl.AddLineV(imgui.Vec2{X: cx + r*0.2, Y: cy + r*0.2},
			imgui.Vec2{X: cx + r*0.75, Y: cy + r*0.75}, col, 2)

	case iconGear: // cog: hub circle + 8 teeth
		for i := 0; i < 8; i++ {
			a := float32(i) * 0.78539816
			dx, dy := cosf(a), sinf(a)
			dl.AddLineV(
				imgui.Vec2{X: cx + dx*r*0.75, Y: cy + dy*r*0.75},
				imgui.Vec2{X: cx + dx*r*1.05, Y: cy + dy*r*1.05}, col, 2.4)
		}
		dl.AddCircleV(imgui.Vec2{X: cx, Y: cy}, r*0.62, col, 0, 2)
		dl.AddCircleFilledV(imgui.Vec2{X: cx, Y: cy}, r*0.22, col, 0)

	case iconExport: // arrow down into a tray
		dl.AddRectFilled(
			imgui.Vec2{X: cx - r*0.16, Y: cy - r},
			imgui.Vec2{X: cx + r*0.16, Y: cy + r*0.15}, col)
		dl.AddTriangleFilled(
			imgui.Vec2{X: cx - r*0.62, Y: cy + r*0.1},
			imgui.Vec2{X: cx + r*0.62, Y: cy + r*0.1},
			imgui.Vec2{X: cx, Y: cy + r*0.7}, col)
		dl.AddRectFilled(
			imgui.Vec2{X: cx - r*0.9, Y: cy + r*0.78},
			imgui.Vec2{X: cx + r*0.9, Y: cy + r*0.98}, col)
	}
}
