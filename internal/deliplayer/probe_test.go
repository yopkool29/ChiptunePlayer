package deliplayer

import (
	"testing"
)

func TestPatrician(t *testing.T) {
	testChdirRoot()
	bad := "testdata/TFMX ST/Rudolf Stember/mdat.patrician st 2"
	p, err := LoadFor(bad, 0)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer p.Close()
	buf := make([]byte, 4*44100*10)
	p.FillBuffer(buf)
	var peak int16
	for i := 0; i+1 < len(buf); i += 2 {
		v := int16(buf[i]) | int16(buf[i+1])<<8
		if v < 0 {
			v = -v
		}
		if v > peak {
			peak = v
		}
	}
	t.Logf("peak=%d", peak)
}
