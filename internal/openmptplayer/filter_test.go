package openmptplayer

import (
	"encoding/binary"
	"math"
	"math/rand"
	"testing"
)

// The software LED-filter biquad must actually filter: white noise loses
// almost all energy above the 3.3 kHz cutoff.
func TestBiquadOnNoise(t *testing.T) {
	p := &Player{filter: true}
	buf := make([]byte, 1<<16)
	r := rand.New(rand.NewSource(1))
	for i := 0; i < len(buf); i += 2 {
		binary.LittleEndian.PutUint16(buf[i:], uint16(int16(r.Intn(65536)-32768)))
	}
	var inHF float64
	for i := 4; i < len(buf); i += 4 {
		d := int(int16(binary.LittleEndian.Uint16(buf[i:]))) - int(int16(binary.LittleEndian.Uint16(buf[i-4:])))
		inHF += float64(d * d)
	}
	p.applyLowpass(buf)
	var outHF float64
	for i := 4; i < len(buf); i += 4 {
		d := int(int16(binary.LittleEndian.Uint16(buf[i:]))) - int(int16(binary.LittleEndian.Uint16(buf[i-4:])))
		outHF += float64(d * d)
	}
	ratio := math.Sqrt(outHF / inHF)
	if ratio > 0.5 {
		t.Fatalf("filter ineffective: HF ratio %.3f", ratio)
	}
}
