//go:build !windows

package uadeplayer

import (
	"os"
	"path/filepath"
	"testing"
)

// setupRuntime stages the bundled UADE runtime (uadecore, score,
// eagleplayer.conf) into a temp dir plus a players/ symlink to the
// bundled players-uade tree.
func setupRuntime(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(root, "internal/bundled/uade")
	dir := t.TempDir()
	for _, f := range []string{"uadecore", "score", "eagleplayer.conf", "uade.conf", "uaerc"} {
		buf, err := os.ReadFile(filepath.Join(src, f))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, f), buf, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(root, "internal/bundled/players-uade"),
		filepath.Join(dir, "players")); err != nil {
		t.Fatal(err)
	}
	return dir
}

// story.xm is a 6-channel FastTracker2 module that requires the
// NotePlayer path — the case the native backend cannot handle.
func TestStoryXMProducesAudio(t *testing.T) {
	SetBaseDir(setupRuntime(t))
	p, err := LoadFor("../../testdata/Fasttracker 2/4-Mat/story.xm", -1)
	if err != nil {
		t.Fatalf("LoadFor: %v", err)
	}
	defer p.Close()
	var nz, total int
	buf := make([]byte, 16384)
	for i := 0; i < 50; i++ {
		p.FillBuffer(buf)
		total += len(buf)
		for _, b := range buf {
			if b != 0 {
				nz++
			}
		}
	}
	t.Logf("read %d bytes, nonzero: %d", total, nz)
	if nz == 0 {
		t.Fatal("no audio produced")
	}
}

// kaneel.xm crashed the stock score (CopyMem unimplemented); the
// patched score must sustain audio for the full read — the discriminating
// check is nonzero in the LAST reads, not just the first.
func TestKaneelXMSustained(t *testing.T) {
	SetBaseDir(setupRuntime(t))
	p, err := LoadFor("../../testdata/Fasttracker 2/4-Mat/kaneel.xm", -1)
	if err != nil {
		t.Fatalf("LoadFor: %v", err)
	}
	defer p.Close()
	buf := make([]byte, 16384)
	var lastNz int
	for i := 0; i < 200; i++ { // ~9.3s of audio
		p.FillBuffer(buf)
		lastNz = 0
		for _, b := range buf {
			if b != 0 {
				lastNz++
			}
		}
	}
	t.Logf("last block nonzero: %d", lastNz)
	if lastNz == 0 {
		t.Fatal("audio died before end of reads")
	}
}

// Switching modules must not crash: two sequential players.
func TestLoadCloseLoad(t *testing.T) {
	SetBaseDir(setupRuntime(t))
	for _, f := range []string{
		"../../testdata/Fasttracker 2/4-Mat/kaneel.xm",
		"../../testdata/AHX/N00b/carni larvae.ahx",
	} {
		p, err := LoadFor(f, -1)
		if err != nil {
			t.Fatalf("LoadFor %s: %v", f, err)
		}
		buf := make([]byte, 16384)
		p.FillBuffer(buf)
		p.Close()
	}
}
