// Package bundled embeds the 68k player binaries and the Amiga DOS
// support files (ahx.waves & friends) into the executable, so an
// installed binary works without the source tree next to it.
package bundled

import (
	"embed"
	"io/fs"
	"os"
	"path/filepath"
)

//go:embed players players-uade amiga-dos genies uade
var data embed.FS

// Install extracts the bundled files into dir (typically a user
// cache dir) and returns it. Files already present with a matching
// size are skipped, so repeat calls stay cheap. The UADE runtime is
// made runnable in place: uadecore gets the exec bit and uade/players
// links to the extracted players-uade dir.
func Install(dir string) error {
	err := fs.WalkDir(data, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		dst := filepath.Join(dir, p)
		if st, e := os.Stat(dst); e == nil {
			if fi, e2 := d.Info(); e2 == nil && st.Size() == fi.Size() {
				return nil
			}
		}
		buf, e := data.ReadFile(p)
		if e != nil {
			return e
		}
		if e := os.MkdirAll(filepath.Dir(dst), 0o755); e != nil {
			return e
		}
		return os.WriteFile(dst, buf, 0o644)
	})
	if err != nil {
		return err
	}
	os.Chmod(filepath.Join(dir, "uade/uadecore"), 0o755)
	lp := filepath.Join(dir, "uade/players")
	if _, e := os.Stat(lp); e != nil {
		os.Symlink("../players-uade", lp)
	}
	return nil
}
