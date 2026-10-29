package xmpplayer

import "testing"

// story.xm is a 6-channel XM — the case UADE's 4-channel Paula cannot
// render. libxmp must sustain audio across the whole 4-pattern song.
func TestStoryXMSustained(t *testing.T) {
	p, err := LoadFor("../../testdata/Fasttracker 2/4-Mat/story.xm", -1)
	if err != nil {
		t.Fatalf("LoadFor: %v", err)
	}
	defer p.Close()
	t.Logf("format=%s voices=%d dur=%dms", p.FormatName(), p.Voices(), p.Duration())
	buf := make([]byte, 17640) // 0.1s
	var lastNz int
	for i := 0; i < 200; i++ { // 20s of audio
		p.FillBuffer(buf)
		lastNz = 0
		for _, b := range buf {
			if b != 0 {
				lastNz++
			}
		}
	}
	t.Logf("last block nonzero: %d", lastNz)
	if lastNz == 0 {
		t.Fatal("audio died before end of reads")
	}
}

// An Amiga-native file must be rejected — those belong to UADE.
func TestRejectsAmigaFormat(t *testing.T) {
	_, err := LoadFor("../../testdata/AHX/N00b/carni larvae.ahx", -1)
	if err == nil {
		t.Fatal("expected rejection of AHX")
	}
}
