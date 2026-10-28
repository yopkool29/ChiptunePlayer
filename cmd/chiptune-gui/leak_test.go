package main

import "testing"

// Hammer: repeated loads of a file whose player opens DOS files —
// leaked handles used to exhaust the 32-slot table, then AHX died.
// AHX opens ENV:EaglePlayer/ahx + ahx.waves on every load.
func TestDOSHandleLeak(t *testing.T) {
	eng = &engine{}
	chdirRoot() // repo root, like the GUI
	ahx := "testdata/AHX/Mortimer Twang/moonmaster.ahx"

	for i := 0; i < 40; i++ {
		d, err := loadBackend(ahx, 0)
		if err != nil {
			t.Fatalf("iter %d: %v", i, err)
		}
		d.Close()
	}
}
