package main

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/widget"
)

// vuBar is a vertical segmented VU-meter widget in classic Amiga
// tracker style: green segments at the bottom, yellow then red at
// the top, on a dark navy background, with a decaying peak marker.
type vuBar struct {
	widget.BaseWidget
	value float64 // 0..100
	peak  float64 // 0..100, decays
}

func newVUBar() *vuBar {
	v := &vuBar{}
	v.ExtendBaseWidget(v)
	return v
}

func (v *vuBar) SetValue(f float64) {
	if f > 100 {
		f = 100
	}
	if f < 0 {
		f = 0
	}
	v.value = f
	if f >= v.peak {
		v.peak = f
	} else {
		v.peak -= 4 // ~0.7s fall time at 10Hz refresh
		if v.peak < f {
			v.peak = f
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
	for i := 0; i < vuSegments; i++ {
		r.segs = append(r.segs, canvas.NewRectangle(vuSegColor(i, false)))
	}
	r.peak = canvas.NewRectangle(vuPeakColor)
	r.objs = append(r.objs, r.bg)
	for _, s := range r.segs {
		r.objs = append(r.objs, s)
	}
	r.objs = append(r.objs, r.peak)
	return r
}

const vuSegments = 14

var (
	vuBgColor   = color.NRGBA{0x08, 0x08, 0x30, 0xFF} // dark navy, tracker style
	vuPeakColor = color.NRGBA{0xFF, 0xFF, 0xFF, 0xFF}
	vuOffColor  = color.NRGBA{0x14, 0x14, 0x40, 0xFF} // unlit segment
)

// vuSegColor returns the color of segment i (0 = bottom).
func vuSegColor(i int, off bool) color.Color {
	if off {
		return vuOffColor
	}
	switch {
	case i >= vuSegments-2: // top 2: red
		return color.NRGBA{0xFF, 0x20, 0x20, 0xFF}
	case i >= vuSegments-5: // next 3: yellow
		return color.NRGBA{0xF0, 0xE0, 0x00, 0xFF}
	default: // rest: green
		return color.NRGBA{0x00, 0xD8, 0x20, 0xFF}
	}
}

type vuRenderer struct {
	bar   *vuBar
	bg    *canvas.Rectangle
	segs  []*canvas.Rectangle
	peak  *canvas.Rectangle
	objs  []fyne.CanvasObject
}

func (r *vuRenderer) Objects() []fyne.CanvasObject { return r.objs }
func (r *vuRenderer) Destroy()                     {}

func (r *vuRenderer) Layout(size fyne.Size) {
	r.bg.Resize(size)
	r.bg.Move(fyne.NewPos(0, 0))

	const gap = float32(2)
	const pad = float32(3)
	inner := size.Height - 2*pad
	segH := (inner - gap*(vuSegments-1)) / vuSegments

	lit := int(r.bar.value * vuSegments / 100.0)
	peakPos := int(r.bar.peak*vuSegments/100.0) - 1
	if peakPos < 0 && r.bar.peak > 0 {
		peakPos = 0
	}

	for i := 0; i < vuSegments; i++ {
		y := size.Height - pad - float32(i+1)*segH - float32(i)*gap // bottom-up
		r.segs[i].Move(fyne.NewPos(pad, y))
		r.segs[i].Resize(fyne.NewSize(size.Width-2*pad, segH))
		r.segs[i].FillColor = vuSegColor(i, i >= lit)
	}

	if peakPos >= 0 && peakPos < vuSegments {
		y := size.Height - pad - float32(peakPos+1)*segH - float32(peakPos)*gap
		r.peak.Move(fyne.NewPos(pad, y+segH/4))
		r.peak.Resize(fyne.NewSize(size.Width-2*pad, segH/2))
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
