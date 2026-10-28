package deliplayer

import "os"

// testChdirRoot lands the test cwd at the repo root (idempotent).
func testChdirRoot() {
	if _, err := os.Stat("extracted/players"); err != nil {
		os.Chdir("../..")
	}
	PlayerDirs = []string{"extracted/players", "extracted/players/DeliPlayers"}
}
