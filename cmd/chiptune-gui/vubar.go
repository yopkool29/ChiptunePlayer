package main

import (
	"image"
	"image/color"
	"math"

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

// vuGradientImg is a 1x256 smooth gradient, built once and stretched
// onto each bar: GPU-filtered, zero banding.
var vuGradientImg = func() image.Image {
	const h = 256
	img := image.NewNRGBA(image.Rect(0, 0, 1, h))
	for y := 0; y < h; y++ {
		t := 1 - float64(y)/float64(h-1) // y=0 is the top of the bar
		img.SetNRGBA(0, y, hsv(hueAt(t), 0.95, 0.92))
	}
	return img
}()

func (v *vuBar) CreateRenderer() fyne.WidgetRenderer {
	r := &vuRenderer{bar: v}
	r.bg = canvas.NewRectangle(vuBgColor)
	r.grad = canvas.NewImageFromImage(vuGradientImg)
	r.grad.FillMode = canvas.ImageFillStretch
	r.cover = canvas.NewRectangle(vuBgColor)
	r.objs = []fyne.CanvasObject{r.bg, r.grad, r.cover}
	return r
}

type vuRenderer struct {
	bar   *vuBar
	bg    *canvas.Rectangle
	grad  *canvas.Image
	cover *canvas.Rectangle
	objs  []fyne.CanvasObject
}

func (r *vuRenderer) Objects() []fyne.CanvasObject { return r.objs }
func (r *vuRenderer) Destroy()                     {}

func (r *vuRenderer) Layout(size fyne.Size) {
	r.bg.Resize(size)
	r.bg.Move(fyne.NewPos(0, 0))
	r.grad.Resize(size)
	r.grad.Move(fyne.NewPos(0, 0))

	// pixel-precise cover hides the unlit top part
	coverH := float32(1-r.bar.disp/100) * size.Height
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
