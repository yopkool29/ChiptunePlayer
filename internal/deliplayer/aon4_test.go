package deliplayer

import "testing"

// AON4: ArtOfNoise-4V writes the channel volume with
// `move.b d0,AUDxVOL+1` — a byte store at an odd register address.
// Regression for the byte-lane Paula write path (paula_write8).
// Also pins the CIA-B timer-B rate: -4V hooks its tick on ICR bit 1
// of ciab.resource, programs the latch to 0x1279 itself (~150 Hz —
// its 3-phase interrupt cycle then yields a 50 Hz row clock), and
// skips that programming entirely if AddICRVector does not return 0.
func TestAON4(t *testing.T) {
	testChdirRoot()
	p, err := Open("internal/bundled/players-uade/ArtOfNoise-4V")
	if err != nil { t.Fatalf("open: %v", err) }
	defer p.Close()
	err = p.Load("testdata/Art Of Noise/DSN/action section.aon", 0)
	if err != nil { t.Fatalf("Load: %v", err) }
	buf := make([]byte, 8192)
	for i := 0; i < 16; i++ { p.FillBuffer(buf) }
	peak := byte(0)
	for _, s := range buf { if s > peak { peak = s } }
	if peak == 0 { t.Fatal("rendered silence") }
	if hz := p.tickHz(); hz < 140 || hz > 160 {
		t.Fatalf("tick rate %.1f Hz — want ~150 (CIA-B timer B latch 0x1279)", hz)
	}
}

// End-to-end: LoadFor must pick a player for the AON4 module —
// accept + audio probe, same contract as the AON8 test.
func TestAON4LoadFor(t *testing.T) {
	testChdirRoot()
	PlayerDirs = []string{"internal/bundled/players", "internal/bundled/players-uade"}
	p, err := LoadFor("testdata/Art Of Noise/DSN/action section.aon", 0)
	if err != nil { t.Fatalf("LoadFor: %v", err) }
	defer p.Close()
}
