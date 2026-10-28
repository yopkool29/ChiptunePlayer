package main

import "os"

// chdirRoot lands the test cwd at the repo root (idempotent).
func chdirRoot() {
	if _, err := os.Stat("extracted/players"); err != nil {
		os.Chdir("../..")
	}
}
