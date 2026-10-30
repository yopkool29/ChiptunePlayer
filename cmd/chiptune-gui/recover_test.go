package main

import "testing"

// GUI repro: ahx → tfmx-st → ahx through loadBackend.
func TestLoadBackendRecover(t *testing.T) {
	eng = &engine{}
	chdirRoot() // repo root, like the GUI
	root := "testdata"
	ahx := root + "/AHX/Mortimer Twang/moonmaster.ahx"
	bad := root + "/TFMX ST/Chris Huelsbeck/mdat.jim power level 1"

	d1, err := loadBackend(ahx, 0, 0)
	if err != nil {
		t.Fatalf("ahx #1: %v", err)
	}
	eng.dec = d1
	t.Logf("ahx #1: %T", d1)

	d2, err := loadBackend(bad, 0, 0)
	if err != nil {
		t.Logf("tfmx-st rejected: %v", err)
	} else {
		eng.dec = d2
		t.Logf("tfmx-st accepted: %T", d2)
	}

	d3, err := loadBackend(ahx, 0, 0)
	if err != nil {
		t.Fatalf("ahx #2: %v", err)
	}
	eng.dec = d3
	t.Logf("ahx #2: %T", d3)
}
