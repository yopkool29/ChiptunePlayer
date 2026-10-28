package deliplayer

import (
	"os"
	"path/filepath"
	"testing"
)

// Repro GUI: after a rejected module (TFMX ST), AHX must still load.
func TestRecoverAfterReject(t *testing.T) {
	testChdirRoot()
	root := "testdata"

	ahx := filepath.Join(root, "AHX", "Mortimer Twang", "moonmaster.ahx")
	bad := filepath.Join(root, "TFMX ST", "Chris Huelsbeck", "mdat.jim power level 1")
	if _, e := os.Stat(ahx); e != nil {
		t.Skip("ahx missing")
	}
	if _, e := os.Stat(bad); e != nil {
		t.Skip("tfmx-st missing")
	}

	p, err := LoadFor(ahx, 0)
	if err != nil {
		t.Fatalf("first ahx load: %v", err)
	}
	p.Close()
	t.Log("ahx #1 ok")

	if bp, berr := LoadFor(bad, 0); berr == nil {
		t.Log("tfmx-st unexpectedly accepted")
		bp.Close()
	} else {
		t.Logf("tfmx-st rejected: %v", berr)
	}

	p2, err := LoadFor(ahx, 0)
	if err != nil {
		t.Fatalf("ahx after reject: %v", err)
	}
	p2.Close()
	t.Log("ahx #2 ok")
}
