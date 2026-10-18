package main

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/widget"
)

// vuBar is a continuous vertical VU-meter widget in classic Amiga
// tracker style: a smooth green -> yellow -> orange -> red gradient
// on a dark navy background, with VU ballistics (instant attack,
// smoothed release) and a decaying white peak marker.
type vuBar struct {
	widget.BaseWidget
	target float64 // latest measured value 0..100
	disp   float64 // displayed value (smoothed)
	peak   float64 // peak marker, decays
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
		v.disp += (f - v.disp) * 0.18 // smooth release (~6 frames time constant)
	}
	if v.disp >= v.peak {
		v.peak = v.disp
	} else {
		v.peak -= 0.9 // ~1s fall time at 30 Hz
		if v.peak < v.disp {
			v.peak = v.disp
		}
	}
	v.Refresh()
}

func (v *vuBar) MinSize() fyne.Size {
	return fyne.NewSize(22, 150)
}

func (v *vuBar) CreateRenderer() fyne.WidgetRenderer {
	r := &vuRenderer{bar: v}
	r.bg = canvas.NewRectangle(vuBgColor)
	r.bg.CornerRadius = 2
	// Vertical gradient: start color at top, end color at bottom.
	r.grad = canvas.NewVerticalGradient(
		color.NRGBA{0xFF, 0x28, 0x10, 0xFF}, // top: red
		color.NRGBA{0x00, 0x50, 0x08, 0xFF}, // bottom: dark green
	)
	r.cover = canvas.NewRectangle(vuBgColor)
	r.peak = canvas.NewRectangle(vuPeakColor)
	r.objs = []fyne.CanvasObject{r.bg, r.grad, r.cover, r.peak}
	return r
}

var (
	vuBgColor   = color.NRGBA{0x08, 0x08, 0x30, 0xFF} // dark navy, tracker style
	vuPeakColor = color.NRGBA{0xFF, 0xFF, 0xFF, 0xFF}
)

type vuRenderer struct {
	bar   *vuBar
	bg    *canvas.Rectangle
	grad  *canvas.LinearGradient
	cover *canvas.Rectangle
	peak  *canvas.Rectangle
	objs  []fyne.CanvasObject
}

func (r *vuRenderer) Objects() []fyne.CanvasObject { return r.objs }
func (r *vuRenderer) Destroy()                     {}

func (r *vuRenderer) Layout(size fyne.Size) {
	r.bg.Resize(size)
	r.bg.Move(fyne.NewPos(0, 0))

	const pad = float32(2)
	innerH := size.Height - 2*pad
	litH := float32(r.bar.disp/100) * innerH
	coverH := innerH - litH

	r.grad.Resize(size)
	r.grad.Move(fyne.NewPos(0, 0))

	// cover hides the unlit top part of the gradient
	r.cover.Move(fyne.NewPos(pad, pad))
	r.cover.Resize(fyne.NewSize(size.Width-2*pad, coverH))
	if coverH <= 0 {
		r.cover.Hide()
	} else {
		r.cover.Show()
	}

	// peak line just above the lit part
	if r.bar.peak > 0.5 {
		py := pad + float32(1-r.bar.peak/100)*innerH
		r.peak.Move(fyne.NewPos(pad, py))
		r.peak.Resize(fyne.NewSize(size.Width-2*pad, 3))
		r.peak.Show()
	} else {
		r.peak.Hide()
	}
}

func (r *vuRenderer) MinSize() fyne.Size {
	return r.bar.MinSize()
}

func (r *vuRenderer) Refresh() {
	r.Layout(r.bar.Size())
	canvas.Refresh(r.bar)
}
