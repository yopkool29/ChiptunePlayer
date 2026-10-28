package deliplayer

import "testing"

// Runs the patrician probes then checks AHX directly, with stage detail.
func TestOrderBreaksAHX(t *testing.T) {
	testChdirRoot()
	bad := "testdata/TFMX ST/Rudolf Stember/mdat.patrician st 2"
	ahx := "testdata/AHX/Mortimer Twang/moonmaster.ahx"

	for i := 0; i < 6; i++ {
		p, err := LoadFor(bad, 0)
		if err == nil {
			p.Close()
		}
	}
	pl, err := Open("AbyssHighestExperience")
	if err != nil {
		t.Fatalf("open ahx player: %v", err)
	}
	defer pl.Close()
	if err := pl.Load(ahx, 0); err != nil {
		t.Fatalf("ahx Load: %v", err)
	}
	ok := pl.probeAudio(1.5)
	t.Logf("probeAudio=%v", ok)
	pl.Close()
}
