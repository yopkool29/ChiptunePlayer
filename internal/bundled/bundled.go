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

//go:embed players players-uade amiga-dos
var data embed.FS

// Install extracts the bundled files into dir (typically a user
// cache dir) and returns it. Files already present with a matching
// size are skipped, so repeat calls stay cheap.
func Install(dir string) error {
	return fs.WalkDir(data, ".", func(p string, d fs.DirEntry, err error) error {
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
}
