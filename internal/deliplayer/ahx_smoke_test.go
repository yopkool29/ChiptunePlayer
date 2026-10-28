package deliplayer

import (
	"testing"
)

func TestAHXSmoke(t *testing.T) {
	testChdirRoot()
	p, err := LoadFor("testdata/AHX/N00b/carni larvae.ahx", 0)
	if err != nil {
		t.Skipf("player/module not available: %v", err)
	}
	defer p.Close()
	buf := make([]byte, 44100*4)
	peak := 0
	for i := 0; i < 5; i++ {
		p.FillBuffer(buf)
		for j := 0; j < len(buf); j += 2 {
			v := int(int16(buf[j]) | int16(buf[j+1])<<8)
			if v < 0 {
				v = -v
			}
			if v > peak {
				peak = v
			}
		}
	}
	t.Logf("peak=%d", peak)
	if peak < 100 {
		t.Fatalf("silent AHX output (peak=%d)", peak)
	}
}
