package main

import (
	"fmt"
	"amiga-player/internal/deliplayer"
	"os"
)

func main() {
	pl, err := deliplayer.LoadFor("testdata/Protracker/0men/gr@@ve-snu$.mod", 0)
	if err != nil { fmt.Println("ERR", err); os.Exit(1) }
	defer pl.Close()
	// render 10 s, count pattern-change transitions via nonzero windows
	buf := make([]byte, 8820*4) // 100ms
	nonzero := 0
	for i := 0; i < 100; i++ {
		pl.FillBuffer(buf)
		nz := 0
		for j := 0; j < len(buf); j += 4 {
			if buf[j] != 0 || buf[j+1] != 0 { nz++ }
		}
		if nz > 0 { nonzero++ }
	}
	fmt.Println("nonzero 100ms blocks:", nonzero, "/100")
}
