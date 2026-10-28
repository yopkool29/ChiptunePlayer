package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// appConfig is persisted in config.json next to the executable (or in
// the working directory when the exe dir isn't writable).
// debug=true skips interactive confirmations (quit dialog).
type appConfig struct {
	Debug      bool   `json:"debug"`
	ExportPath string `json:"export_path"`
	VuSyncMs   int    `json:"vu_sync_ms"` // VU latency calibration past oto's ring
}

var cfg appConfig

func configPath() string {
	if exe, err := os.Executable(); err == nil {
		if st, err := os.Stat(filepath.Join(filepath.Dir(exe), "config.json")); err == nil && !st.IsDir() {
			return filepath.Join(filepath.Dir(exe), "config.json")
		}
	}
	return "config.json"
}

func loadConfig() {
	cfg = appConfig{}
	if d, err := os.ReadFile(configPath()); err == nil {
		json.Unmarshal(d, &cfg)
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
	d, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return
	}
	os.WriteFile(configPath(), d, 0o644)
}
