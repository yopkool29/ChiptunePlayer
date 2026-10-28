package deliplayer

import (
	"os"
	"path/filepath"
)

// testChdirRoot lands the test cwd at the repo root (idempotent).
// DELI_DOSROOT must be absolute: a relative path breaks once another
// test chdirs elsewhere (order-dependent ENOENT on ENV: opens).
func testChdirRoot() {
	if _, err := os.Stat("extracted/players"); err != nil {
		os.Chdir("../..")
	}
	if abs, err := filepath.Abs("amiga-dos"); err == nil {
		os.Setenv("DELI_DOSROOT", abs)
	}
	PlayerDirs = []string{"extracted/players", "extracted/players/DeliPlayers"}
}
