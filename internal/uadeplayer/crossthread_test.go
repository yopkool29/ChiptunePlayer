//go:build !windows

package uadeplayer

import (
	"testing"
	"time"
)

func TestCrossThreadRead(t *testing.T) {
	SetBaseDir(setupRuntime(t))
	p, err := LoadFor("../../testdata/Fasttracker 2/4-Mat/story.xm", -1)
	if err != nil {
		t.Fatalf("LoadFor: %v", err)
	}
	defer p.Close()

	done := make(chan int, 1)
	go func() {
		var nz int
		buf := make([]byte, 16384)
		for i := 0; i < 50; i++ {
			p.FillBuffer(buf)
			for _, b := range buf {
				if b != 0 {
					nz++
				}
			}
		}
		done <- nz
	}()
	select {
	case nz := <-done:
		t.Logf("nonzero: %d", nz)
		if nz == 0 {
			t.Fatal("silent")
		}
	case <-time.After(30 * time.Second):
		t.Fatal("FillBuffer blocked >30s on foreign thread")
	}
}
