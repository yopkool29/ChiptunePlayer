package deliplayer

import (
	"path/filepath"
	"testing"
)

// Iterate LoadFor on the tfmx-st file — log who accepts each round.
func TestIterAccepts(t *testing.T) {
	testChdirRoot()
	bad := "testdata/TFMX ST/Chris Huelsbeck/mdat.jim power level 1"
	for i := 0; i < 6; i++ {
		var who string
		var openErr, loadErr int
		var lastErr error
		for _, c := range allPlayers() {
			if !sniffPlayer(c) {
				continue
			}
			pl, err := Open(c)
			if err != nil {
				openErr++
				lastErr = err
				continue
			}
			if err := pl.Load(bad, 0); err == nil {
				who = filepath.Base(c)
				pl.Close()
				break
			} else {
				loadErr++
				lastErr = err
			}
			pl.Close()
		}
		t.Logf("iter %d: accepted=%q openErr=%d loadErr=%d last=%v",
			i, who, openErr, loadErr, lastErr)
	}
}
