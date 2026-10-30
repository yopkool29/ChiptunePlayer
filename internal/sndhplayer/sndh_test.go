package sndhplayer

import (
	"path/filepath"
	"testing"
)

var sndhFiles = []string{
	"cybernoid.sndh",
	"ikari_union.sndh",
	"childrens_song.sndh",
}

func testdata(name string) string {
	return filepath.Join("..", "..", "testdata", name)
}

// TestLoadAll: every SNDH fixture must open, report metadata and at
// least one subsong.
func TestLoadAll(t *testing.T) {
	for _, f := range sndhFiles {
		p, err := LoadFor(testdata(f), 0)
		if err != nil {
			t.Fatalf("%s: LoadFor: %v", f, err)
		}
		t.Logf("%s: format=%q title=%q author=%q year=%q subs=%d dur=%dms",
			f, p.FormatName(), p.Name(), p.Artist(), p.Game(),
			p.Songs(), p.Duration())
		if p.Songs() < 1 {
			t.Errorf("%s: no subsongs", f)
		}
		if p.Name() == "" {
			t.Errorf("%s: empty title", f)
		}
		p.Close()
	}
}

// TestRendersAudio: two seconds of output must contain non-silent
// samples — guards against "loads but plays nothing".
func TestRendersAudio(t *testing.T) {
	for _, f := range sndhFiles {
		p, err := LoadFor(testdata(f), 0)
		if err != nil {
			t.Fatalf("%s: LoadFor: %v", f, err)
		}
		buf := make([]byte, rate*2*4) // 2s stereo s16
		p.FillBuffer(buf)
		peak := 0
		for i := 0; i+1 < len(buf); i += 2 {
			s := int(buf[i]) | int(int8(buf[i+1]))<<8
			if s < 0 {
				s = -s
			}
			if s > peak {
				peak = s
			}
		}
		if peak == 0 {
			t.Errorf("%s: rendered 2s of pure silence", f)
		} else {
			t.Logf("%s: peak=%d vu=%v", f, peak, p.vu)
		}
		p.Close()
	}
}

// TestSubsongSwitch: multi-subsong files must cycle through every
// declared subsong without error.
func TestSubsongSwitch(t *testing.T) {
	p, err := LoadFor(testdata("ikari_union.sndh"), 0)
	if err != nil {
		t.Skipf("LoadFor: %v", err)
	}
	defer p.Close()
	n := p.Songs()
	for s := 0; s < n; s++ {
		if err := p.Reinit(s); err != nil {
			t.Fatalf("Reinit(%d): %v", s, err)
		}
		buf := make([]byte, rate/10*4)
		p.FillBuffer(buf)
	}
}

// TestRejectForeign: an Amiga module must be refused so routing falls
// through to the right backend.
func TestRejectForeign(t *testing.T) {
	if _, err := LoadFor(testdata("test.mod"), 0); err == nil {
		t.Fatal("accepted a ProTracker module — sniffing too permissive")
	}
}

// TestMute: muting all voices must render silence.
func TestMute(t *testing.T) {
	p, err := LoadFor(testdata("cybernoid.sndh"), 0)
	if err != nil {
		t.Skipf("LoadFor: %v", err)
	}
	defer p.Close()
	buf := make([]byte, rate/10*4)
	p.FillBuffer(buf) // warm up
	for v := 0; v < p.Voices(); v++ {
		p.MuteVoice(v, true)
	}
	// dcAdjust is a ~46ms DC-block filter: let its history drain, then
	// the output must be exactly silent.
	for i := 0; i < 3; i++ {
		p.FillBuffer(buf)
	}
	for i := 0; i+1 < len(buf); i += 2 {
		if buf[i] != 0 || buf[i+1] != 0 {
			t.Fatalf("byte %d nonzero after muting all voices", i)
		}
	}
}
