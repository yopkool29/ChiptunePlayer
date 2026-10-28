package deliplayer

import (
	"testing"
	"time"
)

func TestGateTimings(t *testing.T) {
	testChdirRoot()
	for _, f := range []string{
		"testdata/Protracker/Paniel Deter/goodday.mod",
		"testdata/TFMX ST/Rudolf Stember/mdat.patrician st 2",
		"testdata/AHX/Mortimer Twang/moonmaster.ahx",
	} {
		t.Run(f, func(t *testing.T) {
			t0 := time.Now()
			p, err := LoadFor(f, 0)
			d := time.Since(t0)
			if err != nil {
				t.Logf("%s -> reject in %v", f, d)
				return
			}
			p.Close()
			t.Logf("%s -> accept in %v", f, d)
		})
	}
}
