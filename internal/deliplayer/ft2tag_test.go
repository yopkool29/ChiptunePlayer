package deliplayer

import (
	"fmt"
	"testing"
)

func TestFT2Tags(t *testing.T) {
	testChdirRoot()
	pl, err := Open("internal/bundled/players-uade/FastTracker2")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer pl.Close()
	for i := 34; i <= 50; i++ {
		if v := pl.tag(i); v != 0 {
			fmt.Printf("tag %d = %x\n", i, v)
		}
	}
}
