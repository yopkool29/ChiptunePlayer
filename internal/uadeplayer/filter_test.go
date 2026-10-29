package uadeplayer

import (
	"encoding/binary"
	"testing"
)

// The LED filter (3.3 kHz Butterworth) must actually change the rendered
// audio — and in the right direction: ON = less treble than OFF.
func TestSetFilteringChangesAudio(t *testing.T) {
	SetBaseDir(setupRuntime(t))
	render := func(on bool) []byte {
		p, err := LoadFor("../../testdata/Protracker/Paniel Deter/goodday.mod", -1)
		if err != nil {
			t.Fatal(err)
		}
		defer p.Close()
		p.SetFiltering(on)
		buf := make([]byte, 262144)
		p.FillBuffer(buf)
		return buf
	}
	a, b := render(true), render(false)
	diff := 0
	for i := range a {
		if a[i] != b[i] {
			diff++
		}
	}
	t.Logf("bytes differing (filter on vs off): %d / %d", diff, len(a))
	if diff == 0 {
		t.Fatal("filter toggle produced identical output — not wired")
	}
	// treble proxy: mean |s[n]-s[n-1]|
	hf := func(b []byte) float64 {
		var s float64
		for i := 4; i < len(b); i += 4 {
			d := int(int16(binary.LittleEndian.Uint16(b[i:]))) -
				int(int16(binary.LittleEndian.Uint16(b[i-4:])))
			if d < 0 {
				d = -d
			}
			s += float64(d)
		}
		return s / float64(len(b)/4)
	}
	on, off := hf(a), hf(b)
	t.Logf("treble proxy: filter ON=%.0f OFF=%.0f", on, off)
	if on >= off {
		t.Fatalf("filter ON should reduce treble (got ON=%.0f >= OFF=%.0f)", on, off)
	}
}
