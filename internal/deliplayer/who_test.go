package deliplayer

import (
	"path/filepath"
	"testing"
)

// Which player accepts the TFMX ST file?
func TestWhoAccepts(t *testing.T) {
	testChdirRoot()
	bad := "testdata/TFMX ST/Chris Huelsbeck/mdat.jim power level 1"
	for _, c := range allPlayers() {
		if !sniffPlayer(c) {
			continue
		}
		pl, err := Open(c)
		if err != nil {
			t.Logf("%s: open err %v", filepath.Base(c), err)
			continue
		}
		if err := pl.Load(bad, 0); err == nil {
			t.Logf(">>> ACCEPTED by %s", c)
		} else {
			t.Logf("    rejected by %s (%v)", filepath.Base(c), err)
		}
		pl.Close()
	}
}
