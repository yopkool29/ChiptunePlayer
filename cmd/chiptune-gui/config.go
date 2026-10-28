package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// appConfig is persisted in ~/.config/chiptuneplayer/config.json.
// Older locations (config.json next to the exe or in the working
// directory) are still read when the XDG file does not exist yet.
// debug=true skips interactive confirmations (quit dialog).
type appConfig struct {
	Debug      bool   `json:"debug"`
	ExportPath string `json:"export_path"`
	VuSyncMs   int    `json:"vu_sync_ms"` // VU latency calibration past oto's ring
	MusicDir   string `json:"music_dir"`
}

var cfg appConfig

func configPath() string {
	if d, err := os.UserConfigDir(); err == nil {
		return filepath.Join(d, "chiptuneplayer", "config.json")
	}
	return "config.json"
}

func loadConfig() {
	cfg = appConfig{}
	data, err := os.ReadFile(configPath())
	if err != nil {
		// legacy locations: exe dir, then cwd
		if exe, e := os.Executable(); e == nil {
			data, err = os.ReadFile(filepath.Join(filepath.Dir(exe), "config.json"))
		}
		if err != nil {
			data, err = os.ReadFile("config.json")
		}
	}
	if err == nil {
		json.Unmarshal(data, &cfg)
	}
	if cfg.ExportPath != "" {
		exportPath = cfg.ExportPath
	}
	if cfg.VuSyncMs <= 0 {
		cfg.VuSyncMs = 380 // typical driver/PipeWire latency past oto's ring
	}
}

func saveConfig() {
	cfg.ExportPath = exportPath
	cfg.MusicDir = browseDir
	d, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return
	}
	p := configPath()
	os.MkdirAll(filepath.Dir(p), 0o755)
	os.WriteFile(p, d, 0o644)
}
