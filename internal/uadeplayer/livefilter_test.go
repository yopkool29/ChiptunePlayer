//go:build !windows

package uadeplayer

import "testing"

// Toggle the LED filter mid-stream: subsequent audio must differ.
func TestLiveFilterToggle(t *testing.T) {
	SetBaseDir(setupRuntime(t))
	p, err := LoadFor("../../testdata/Protracker/Paniel Deter/goodday.mod", -1)
	if err != nil { t.Fatal(err) }
	defer p.Close()
	buf := make([]byte, 65536)
	p.FillBuffer(buf)              // play a bit unforced
	p.SetFiltering(false)          // force filter OFF
	p.FillBuffer(buf)
	copyA := append([]byte(nil), buf...)
	p.SetFiltering(true)           // force filter ON
	p.FillBuffer(buf)
	diff := 0
	for i := range buf { if buf[i] != copyA[i] { diff++ } }
	t.Logf("diff after live toggle: %d / %d", diff, len(buf))
	if diff == 0 { t.Fatal("live toggle had no effect") }
}
