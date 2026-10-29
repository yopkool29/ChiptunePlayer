package openmptplayer

import "testing"

func TestStoryXMSustained(t *testing.T) {
	p, err := LoadFor("../../testdata/Fasttracker 2/4-Mat/story.xm", -1)
	if err != nil {
		t.Fatalf("LoadFor: %v", err)
	}
	defer p.Close()
	t.Logf("format=%s voices=%d dur=%dms", p.FormatName(), p.Voices(), p.Duration())
	buf := make([]byte, 17640) // 0.1s
	var midNz, endAt int
	for i := 0; i < 200; i++ { // 20s; song is ~15.4s
		p.FillBuffer(buf)
		nz := 0
		for _, b := range buf {
			if b != 0 {
				nz++
			}
		}
		if i >= 100 && i <= 140 { // 10-14s window: song must be audible
			midNz += nz
		}
		if nz > 0 {
			endAt = i
		}
	}
	t.Logf("mid-song nonzero total: %d, last audible block at %.1fs", midNz, float64(endAt)/10)
	if midNz == 0 {
		t.Fatal("no audio during the song body")
	}
	if endAt < 140 {
		t.Fatalf("audio stopped early at %.1fs (song is ~15.4s)", float64(endAt)/10)
	}
}

func TestRejectsNonModule(t *testing.T) {
	if _, err := LoadFor("/dev/null", -1); err == nil {
		t.Fatal("expected rejection")
	}
}
