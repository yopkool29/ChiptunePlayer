package main

import (
	"image/color"
	"math"

	"github.com/AllenDang/cimgui-go/imgui"
)

// vuBar is a continuous vertical VU-meter drawn on the imgui draw list,
// in classic Amiga tracker style: a smooth green -> yellow -> red
// gradient on a dark navy background, with VU ballistics (instant
// attack, smooth release). Feed it once per frame.
type vuBar struct {
	target float64 // latest measured value 0..100
	disp   float64 // displayed value (smoothed)
}

// SetValue feeds the meter; call once per frame for smooth animation.
func (v *vuBar) SetValue(f float64) {
	if f > 100 {
		f = 100
	}
	if f < 0 {
		f = 0
	}
	v.target = f
	if f > v.disp {
		v.disp = f // instant attack
	} else {
		v.disp += (f - v.disp) * 0.18 // smooth release
	}
}

// Reset clears the meter instantly (module change, stop…).
func (v *vuBar) Reset() {
	v.target = 0
	v.disp = 0
}

// drawVU renders the bar on the window draw list inside the rect at
// (x,y,w,h). Full gradient is drawn, then the unlit top is covered
// with the background color — pixel-precise, same trick as the fyne
// version.
func drawVU(dl *imgui.DrawList, x, y, w, h, disp float32) {
	bg := colU32(vuBgColor)

	// 32 strips for a band-free gradient; top of bar = t 1, bottom = t 0
	const strips = 32
	for s := 0; s < strips; s++ {
		t := float64(s+1) / strips
		y0 := y + h*float32(1-t)
		y1 := y + h*float32(1-float64(s)/strips)
		c := hsv(hueAt(t), 0.95, 0.92)
		dl.AddRectFilled(
			imgui.Vec2{X: x, Y: y0},
			imgui.Vec2{X: x + w, Y: y1},
			colU32(c))
	}

	// cover hides the unlit top
	coverH := h * (1 - disp/100)
	if coverH > 0.5 {
		dl.AddRectFilled(
			imgui.Vec2{X: x, Y: y},
			imgui.Vec2{X: x + w, Y: y + coverH},
			bg)
	}

	// hairline frame
	dl.AddRectV(
		imgui.Vec2{X: x, Y: y},
		imgui.Vec2{X: x + w, Y: y + h},
		colU32(color.NRGBA{0x3a, 0x3a, 0x5a, 0xFF}), 0, 0, 1)
}

var vuBgColor = color.NRGBA{0x08, 0x08, 0x30, 0xFF} // dark navy

// Hue ramp across the bar (t = 0 bottom → 1 top): stays green on the
// lower half, then sweeps through yellow and orange to red at the top.
var vuHueStops = [][2]float64{
	{0.00, 122},
	{0.42, 118},
	{0.62, 60},
	{0.82, 28},
	{1.00, 8},
}

func hueAt(t float64) float64 {
	lo := vuHueStops[0]
	for _, s := range vuHueStops[1:] {
		if t <= s[0] {
			return lo[1] + (s[1]-lo[1])*(t-lo[0])/(s[0]-lo[0])
		}
		lo = s
	}
	return vuHueStops[len(vuHueStops)-1][1]
}

func hsv(h, s, v float64) color.NRGBA {
	h = math.Mod(h+360, 360) / 60
	i := math.Floor(h)
	f := h - i
	p := v * (1 - s)
	q := v * (1 - s*f)
	u := v * (1 - s*(1-f))
	var r, g, b float64
	switch int(i) {
	case 0:
		r, g, b = v, u, p
	case 1:
		r, g, b = q, v, p
	case 2:
		r, g, b = p, v, u
	case 3:
		r, g, b = p, q, v
	case 4:
		r, g, b = u, p, v
	default:
		r, g, b = v, p, q
	}
	return color.NRGBA{uint8(r*255 + .5), uint8(g*255 + .5), uint8(b*255 + .5), 0xFF}
}

func colU32(c color.NRGBA) uint32 {
	return imgui.ColorConvertFloat4ToU32(imgui.Vec4{
		X: float32(c.R) / 255,
		Y: float32(c.G) / 255,
		Z: float32(c.B) / 255,
		W: float32(c.A) / 255,
	})
}
