package uadeplayer

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Regression record: UADE rejects SNDH (Atari ST format) — the format
// needs YM2149+MFP emulation, which UADE's Amiga-only machine lacks.
// These files route to internal/sndhplayer instead.
func TestSndhUade(t *testing.T) {
	base := filepath.Join(os.Getenv("HOME"), ".cache", "chiptuneplayer", "uade")
	if _, err := os.Stat(filepath.Join(base, "uadecore")); err != nil {
		t.Skip("uadecore runtime not installed")
	}
	SetBaseDir(base)
	for _, f := range []string{
		"cybernoid.sndh",
		"ikari_union.sndh",
		"childrens_song.sndh",
	} {
		done := make(chan error, 1)
		start := time.Now()
		go func(f string) {
			p, err := LoadFor(filepath.Join("..", "..", "testdata", f), 0)
			if p != nil {
				p.Close()
			}
			done <- err
		}(f)
		select {
		case err := <-done:
			if err == nil {
				t.Errorf("%s: UADE unexpectedly accepted SNDH", f)
			}
			t.Logf("%s -> %v err: %v", f, time.Since(start), err)
		case <-time.After(20 * time.Second):
			t.Fatalf("%s -> TIMEOUT 20s", f)
		}
	}
}
