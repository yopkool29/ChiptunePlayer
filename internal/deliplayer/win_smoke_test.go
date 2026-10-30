package deliplayer

import (
	"os"
	"path/filepath"
	"testing"
)

// writeTinyMod builds a minimal valid ProTracker .mod: one pattern,
// instrument 1 = 1000 bytes of square wave, C-2 note on row 0.
func writeTinyMod(t *testing.T) string {
	t.Helper()
	var m []byte
	m = append(m, []byte("win-smoke")...)
	m = append(m, make([]byte, 20-9)...)
	// sample header 1: 500 words = 1000 bytes, vol 64, no loop
	hdr := make([]byte, 30)
	hdr[22], hdr[23] = 0x01, 0xF4 // length 500 words
	hdr[25] = 64                  // volume
	hdr[29] = 1                   // loop length 1 word = disabled
	m = append(m, hdr...)
	m = append(m, make([]byte, 30*30)...) // headers 2..31
	m = append(m, 1, 0x7F)                // songlen=1, restart
	m = append(m, make([]byte, 128)...)
	m = append(m, []byte("M.K.")...)
	// pattern 0: 64 rows x 4ch x 4B; row 0 ch 0 = C-2 (period 428) ins 1
	pat := make([]byte, 1024)
	pat[0], pat[1], pat[2], pat[3] = 0x01, 0xAC, 0x10, 0x00
	m = append(m, pat...)
	for i := 0; i < 1000; i++ { // square wave sample data
		if i&4 != 0 {
			m = append(m, 0x40)
		} else {
			m = append(m, 0xC0)
		}
	}
	p := filepath.Join(t.TempDir(), "smoke.mod")
	if err := os.WriteFile(p, m, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestWindowsSmoke renders a few buffers of a generated .mod and
// expects audible output — proves the Musashi/Paula core, hunk loader
// and dos.library host shims all work on this platform.
func TestWindowsSmoke(t *testing.T) {
	if _, err := os.Stat("internal/bundled/players"); err != nil {
		os.Chdir("../..")
	}
	if abs, err := filepath.Abs("amiga-dos"); err == nil {
		os.Setenv("DELI_DOSROOT", abs)
	}
	PlayerDirs = []string{"internal/bundled/players", "internal/bundled/players-uade"}
	GenieDirs = []string{"internal/bundled/genies"}

	pl, err := LoadFor(writeTinyMod(t), 0)
	if err != nil {
		t.Fatalf("LoadFor: %v", err)
	}
	defer pl.Close()
	buf := make([]byte, 4410*4)
	nonzero := false
	for i := 0; i < 20 && !nonzero; i++ {
		pl.FillBuffer(buf)
		for _, b := range buf {
			if b != 0 {
				nonzero = true
				break
			}
		}
	}
	if !nonzero {
		t.Fatal("2s of audio rendered as pure silence")
	}
}
