package main

// Simulates the installed-app scenario: only the extracted cache
// provides players + DOS files (no source-tree dirs resolve).

import (
	"os"
	"path/filepath"
	"testing"

	"amiga-player/internal/deliplayer"
)

func TestBundledRuntimeLoadsAHX(t *testing.T) {
	// wipe the cache to force a fresh extraction
	cache, _ := os.UserCacheDir()
	rt := filepath.Join(cache, "chiptuneplayer")
	os.RemoveAll(rt)

	installRuntime()

	// restrict PlayerDirs to the cache only — anything repo-relative
	// must not be needed
	deliplayer.PlayerDirs = []string{
		filepath.Join(rt, "players"),
		filepath.Join(rt, "players-uade"),
	}

	ahx, _ := filepath.Abs("../../testdata/AHX/Mortimer Twang/amanda.ahx")
	if _, err := os.Stat(ahx); err != nil {
		t.Skip("testdata ahx missing:", err)
	}
	pl, err := deliplayer.LoadFor(ahx, 0)
	if err != nil {
		t.Fatalf("LoadFor(ahx) via bundled cache: %v", err)
	}
	defer pl.Close()
	buf := make([]byte, 8192)
	pl.FillBuffer(buf)
	peak := 0
	for _, b := range buf {
		if int(b) > peak {
			peak = int(b)
		}
	}
	if peak == 0 {
		t.Fatal("ahx loaded but rendered silence")
	}
	t.Logf("ahx renders via bundled cache, peak=%d", peak)
}
