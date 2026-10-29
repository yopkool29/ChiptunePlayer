package deliplayer

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestScanTag38(t *testing.T) {
	testChdirRoot()
	var paths []string
	filepath.WalkDir("extracted/players", func(p string, e os.DirEntry, err error) error {
		if err == nil && !e.IsDir() {
			paths = append(paths, p)
		}
		return nil
	})
	t.Logf("paths: %d", len(paths))
	nok := 0
	for _, p := range paths {
		pl, err := Open(p)
		if err != nil {
			nok++
			continue
		}
		if pl.tag(38) != 0 {
			fmt.Println("NOTEPLAYER:", filepath.Base(p), "tag38=", pl.tag(38))
		}
		pl.Close()
	}
	t.Logf("open failures: %d", nok)
}
