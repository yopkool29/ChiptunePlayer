package main

import "testing"

// Hammer: repeated loads of a file whose player opens DOS files —
// leaked handles used to exhaust the 32-slot table, then AHX died.
func TestDOSHandleLeak(t *testing.T) {
	eng = &engine{}
	chdirRoot() // repo root, like the GUI
	ahx := "testdata/AHX/Mortimer Twang/moonmaster.ahx"
	tfmx := "testdata/TFMX ST/Chris Huelsbeck/mdat.jim power level 1"

	for i := 0; i < 40; i++ {
		d, err := loadBackend(tfmx, 0)
		if err != nil {
			t.Fatalf("iter %d: %v", i, err)
		}
		d.Close()
	}
	d, err := loadBackend(ahx, 0)
	if err != nil {
		t.Fatalf("ahx after 40 loads: %v", err)
	}
	d.Close()
}
