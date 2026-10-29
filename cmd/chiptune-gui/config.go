package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// appConfig is persisted in ~/.config/chiptuneplayer/config.json —
// the only location read or written.
// debug=true skips interactive confirmations (quit dialog).
type appConfig struct {
	Debug      bool   `json:"debug"`
	ExportPath string `json:"export_path"`
	VuSyncMs   int    `json:"vu_sync_ms"` // VU latency calibration past oto's ring
	MusicDir   string `json:"music_dir"`
	LEDFilter  bool   `json:"led_filter"` // A500 output lowpass (default: on, like real HW)
	PreferDeli bool   `json:"prefer_deli"` // replay via the native deliplayer before uade
}

var cfg appConfig

func configPath() string {
	if d, err := os.UserConfigDir(); err == nil {
		return filepath.Join(d, "chiptuneplayer", "config.json")
	}
	return "config.json"
}

func loadConfig() {
	// defaults chosen so an absent field gets the authentic value
	cfg = appConfig{LEDFilter: true}
	data, err := os.ReadFile(configPath())
	if err == nil {
		json.Unmarshal(data, &cfg)
	}
	if cfg.ExportPath != "" {
		exportPath = cfg.ExportPath
		// migrate: older configs stored a full .wav path — the
		// setting is a directory now
		if strings.EqualFold(filepath.Ext(exportPath), ".wav") {
			exportPath = filepath.Dir(exportPath)
		}
	}
	if cfg.VuSyncMs <= 0 {
		cfg.VuSyncMs = 380 // typical driver/PipeWire latency past oto's ring
	}
	filterOn = cfg.LEDFilter
}

func saveConfig() {
	cfg.ExportPath = exportPath
	cfg.MusicDir = browseDir
	cfg.LEDFilter = filterOn
	d, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return
	}
	p := configPath()
	os.MkdirAll(filepath.Dir(p), 0o755)
	os.WriteFile(p, d, 0o644)
}
