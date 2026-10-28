package deliplayer

import "testing"

// AON8: ArtOfNoise-8V is a software-mixer player — its int4 handler
// (poked at $70, ends with rte) refills the Paula DMA double-buffers
// each audio interrupt. Regression: load must succeed and rendered
// audio must be non-silent.
func TestAONLoadRenders(t *testing.T) {
	testChdirRoot()
	p, err := Open("internal/bundled/players-uade/ArtOfNoise-8V")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer p.Close()
	if err := p.Load("testdata/Art Of Noise/Slaxx/dream01.aon", 0); err != nil {
		t.Fatalf("AON8 Load rejected: %v", err)
	}
	buf := make([]byte, 8192)
	for i := 0; i < 8; i++ {
		p.FillBuffer(buf)
	}
	peak := byte(0)
	for _, s := range buf {
		if s > peak {
			peak = s
		}
	}
	if peak == 0 {
		t.Fatal("AON8 rendered silence — int4 rte-frame path regressed")
	}
}

// End-to-end: LoadFor must pick ArtOfNoise-8V for a .aon module,
// i.e. the player must both accept the file and pass the audio probe.
func TestAONLoadFor(t *testing.T) {
	testChdirRoot()
	PlayerDirs = []string{"internal/bundled/players", "internal/bundled/players-uade"}
	p, err := LoadFor("testdata/Art Of Noise/Slaxx/dream01.aon", 0)
	if err != nil {
		t.Fatalf("LoadFor: %v", err)
	}
	defer p.Close()
}
