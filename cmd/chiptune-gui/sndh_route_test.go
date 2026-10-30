package main

import (
	"testing"
	"time"

	"amiga-player/internal/sndhplayer"
)

// SNDH must route straight to the AtariAudio backend — never through
// the deliplayer sweep (~60s guaranteed reject) or UADE (cannot play).
func TestSndhRoutesToAtariAudio(t *testing.T) {
	eng = &engine{}
	chdirRoot() // repo root, like the GUI
	start := time.Now()
	d, err := loadBackend("testdata/cybernoid.sndh", 0, 0)
	if err != nil {
		t.Fatalf("loadBackend: %v", err)
	}
	defer d.Close()
	if _, ok := d.(*sndhplayer.Player); !ok {
		t.Fatalf("routed to %T, want *sndhplayer.Player", d)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("routing took %v — probably swept deliplayer", elapsed)
	}
	if d.FormatID() != "sndh" {
		t.Fatalf("FormatID = %q", d.FormatID())
	}
	buf := make([]byte, 44100/5*4)
	d.FillBuffer(buf)
	peak := 0
	for i := 0; i+1 < len(buf); i += 2 {
		s := int(int16(uint16(buf[i]) | uint16(buf[i+1])<<8))
		if s < 0 {
			s = -s
		}
		if s > peak {
			peak = s
		}
	}
	if peak == 0 {
		t.Fatal("routed decoder renders silence")
	}
}

// Switching module formats mid-session: SNDH -> .dm2 -> .mod must all
// resolve; each backend replaces the previous one cleanly.
func TestSwitchFormats(t *testing.T) {
	eng = &engine{}
	chdirRoot()
	for _, f := range []string{
		"testdata/cybernoid.sndh",
		"testdata/Delta Music 2/Zetrix/arcane-enjoystick.dm2",
		"testdata/Protracker/Ryan/devastat.mod",
	} {
		d, err := loadBackend(f, 0, 0)
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		buf := make([]byte, 4410*4)
		d.FillBuffer(buf)
		d.Close()
	}
}
