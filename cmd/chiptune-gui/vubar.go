package main

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/widget"
)

// vuBar is a continuous vertical VU-meter widget in classic Amiga
// tracker style: a smooth green -> yellow -> red gradient on a dark
// navy background, with VU ballistics (instant attack, smooth release).
type vuBar struct {
	widget.BaseWidget
	target float64 // latest measured value 0..100
	disp   float64 // displayed value (smoothed)
}

func newVUBar() *vuBar {
	v := &vuBar{}
	v.ExtendBaseWidget(v)
	return v
}

// SetValue feeds the meter; call at ~30 Hz for smooth animation.
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
	v.Refresh()
}

// Reset clears the meter instantly (module change, stop…).
func (v *vuBar) Reset() {
	v.target = 0
	v.disp = 0
	v.Refresh()
}

func (v *vuBar) MinSize() fyne.Size {
	return fyne.NewSize(22, 150)
}

// vuStrips thin stacked rectangles form the continuous gradient.
const vuStrips = 48

// Gradient stops, bottom to top: pure green on the lower half, then
// yellow, orange, and red-orange only near the top.
type vuStop struct {
	t float64
	c color.NRGBA
}

var vuStops = []vuStop{
	{0.00, color.NRGBA{0x00, 0xC8, 0x10, 0xFF}},
	{0.45, color.NRGBA{0x10, 0xD8, 0x08, 0xFF}},
	{0.65, color.NRGBA{0xD8, 0xE0, 0x00, 0xFF}},
	{0.82, color.NRGBA{0xFF, 0x90, 0x00, 0xFF}},
	{1.00, color.NRGBA{0xFF, 0x30, 0x08, 0xFF}},
}

func lerp(a, b uint8, f float64) uint8 {
	return a + uint8(float64(b-a)*f)
}

func stripColor(t float64) color.NRGBA {
	lo := vuStops[0]
	for _, s := range vuStops[1:] {
		if t <= s.t {
			f := (t - lo.t) / (s.t - lo.t)
			return color.NRGBA{
				lerp(lo.c.R, s.c.R, f),
				lerp(lo.c.G, s.c.G, f),
				lerp(lo.c.B, s.c.B, f),
				0xFF,
			}
		}
		lo = s
	}
	return vuStops[len(vuStops)-1].c
}

var vuBgColor = color.NRGBA{0x08, 0x08, 0x30, 0xFF} // dark navy

func (v *vuBar) CreateRenderer() fyne.WidgetRenderer {
	r := &vuRenderer{bar: v}
	r.bg = canvas.NewRectangle(vuBgColor)
	for i := 0; i < vuStrips; i++ {
		t := float64(i) / float64(vuStrips-1)
		r.strips = append(r.strips, canvas.NewRectangle(stripColor(t)))
	}
	r.cover = canvas.NewRectangle(vuBgColor)
	r.objs = append(r.objs, r.bg)
	for _, s := range r.strips {
		r.objs = append(r.objs, s)
	}
	r.objs = append(r.objs, r.cover)
	return r
}

type vuRenderer struct {
	bar    *vuBar
	bg     *canvas.Rectangle
	strips []*canvas.Rectangle
	cover  *canvas.Rectangle
	objs   []fyne.CanvasObject
}

func (r *vuRenderer) Objects() []fyne.CanvasObject { return r.objs }
func (r *vuRenderer) Destroy()                     {}

func (r *vuRenderer) Layout(size fyne.Size) {
	r.bg.Resize(size)
	r.bg.Move(fyne.NewPos(0, 0))

	innerH := size.Height
	for i := 0; i < vuStrips; i++ {
		// strip i, bottom-up; overlaps its neighbour by 1px to avoid seams
		y0 := innerH - float32(i)*(innerH/vuStrips)
		y1 := innerH - float32(i+1)*(innerH/vuStrips)
		r.strips[i].Move(fyne.NewPos(0, y1))
		r.strips[i].Resize(fyne.NewSize(size.Width, y0-y1+1))
	}

	// pixel-precise cover hides the unlit top part
	coverH := float32(1-r.bar.disp/100) * innerH
	r.cover.Move(fyne.NewPos(0, 0))
	r.cover.Resize(fyne.NewSize(size.Width, coverH))
}

func (r *vuRenderer) MinSize() fyne.Size {
	return r.bar.MinSize()
}

func (r *vuRenderer) Refresh() {
	r.Layout(r.bar.Size())
	canvas.Refresh(r.bar)
}
