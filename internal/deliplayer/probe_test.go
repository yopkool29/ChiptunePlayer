package deliplayer

import (
	"errors"
	"testing"
)

// mdat.patrician st 2 has no smpl.* pair: a weak Check2 accepts it but
// only renders silence — the silence gate must reject it.
func TestPatrician(t *testing.T) {
	testChdirRoot()
	bad := "testdata/TFMX ST/Rudolf Stember/mdat.patrician st 2"
	p, err := LoadFor(bad, 0)
	if err == nil {
		p.Close()
		t.Fatal("silence-only module was accepted")
	}
	if !errors.Is(err, ErrNoPlayer) {
		t.Fatalf("unexpected error: %v", err)
	}
}
