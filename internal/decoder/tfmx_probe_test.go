package decoder

import (
	"fmt"
	"testing"
)

func TestTfmxAON(t *testing.T) {
	for _, f := range []string{
		"../../testdata/Art Of Noise/DSN/action section.aon",
		"../../testdata/Art Of Noise/Slaxx/dream01.aon",
	} {
		d := New()
		err := d.Load(f, 0)
		fmt.Printf("%s -> %v\n", f, err)
		if err == nil {
			buf := make([]byte, 8192)
			d.FillBuffer(buf)
			nz := 0
			for _, b := range buf { if b != 0 { nz++ } }
			fmt.Printf("   nonzero bytes: %d/8192\n", nz)
			d.Close()
		}
	}
}
