package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/AllenDang/cimgui-go/imgui"
	g "github.com/AllenDang/giu"
)

// deliPlayerFor maps the detected format to the DeliTracker II player
// module (from DeliTracker 2.32's players.lha) that would handle it
// on a real Amiga.
func deliPlayerFor(formatID, formatName string) string {
	switch formatID {
	case "FC14":
		return "FC1.4_note V2.5 — Future Composer 1.4"
	case "SMOD":
		return "FC1.3_note V2.6 — Future Composer 1.0-1.3"
	case "MCMD":
		return "MarkII_note V1.7 — Mark II (MCMD)"
	case "COSO":
		return "Hippel-COSO_note V2.1 — compressed Hippel TFMX"
	case "DNS":
		return "— (Dynamic Synthesizer)"
	case "TFMX", "TFMX7V":
		if strings.Contains(formatName, "Hippel") {
			return "Hippel V1.5 — Jochen Hippel TFMX"
		}
		return "TFMX V2.29 — TFMX 1.x / Pro / 7V (Hülsbeck)"
	default:
		return "?"
	}
}

// decoderEngine names the C++ decoder backend handling the format.
func decoderEngine(formatName string) string {
	switch {
	case strings.Contains(formatName, "Huelsbeck"):
		return "Chris/TFMXDecoder"
	case strings.Contains(formatName, "Synthesizer"):
		return "Chris/DNSDecoder"
	default:
		return "Jochen/HippelDecoder"
	}
}

func humanSize(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/float64(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

// moduleInfoLayout builds the popup body describing the loaded module
// and the player technology behind it. Uses the metadata snapshot so
// it also works while stopped.
func moduleInfoLayout(eng *engine) []g.Widget {
	eng.mu.Lock()
	m := eng.meta
	sub := eng.sub
	path := eng.filePath
	eng.mu.Unlock()
	if m == nil {
		return nil
	}

	rows := []g.Widget{
		rowKV("File", path),
	}
	if fi, err := os.Stat(path); err == nil {
		rows = append(rows, rowKV("Size", humanSize(fi.Size())))
	}
	if s := m.name; s != "" {
		rows = append(rows, rowKV("Name", s))
	}
	if s := m.artist; s != "" {
		rows = append(rows, rowKV("Artist", s))
	}
	if s := m.title; s != "" {
		rows = append(rows, rowKV("Title", s))
	}
	if s := m.game; s != "" {
		rows = append(rows, rowKV("Game", s))
	}
	rows = append(rows,
		rowKV("Format", m.formatName),
		rowKV("Format ID", m.formatID),
		rowKV("DeliPlayer", deliPlayerFor(m.formatID, m.formatName)),
		rowKV("Decoder", decoderEngine(m.formatName)+" (C++)"),
		rowKV("Voices", fmt.Sprintf("%d (Paula)", m.voices)),
		rowKV("Subsongs", fmt.Sprintf("%d — playing #%d", max(m.songs, 1), sub+1)),
		rowKV("Duration", (time.Duration(m.duration)*time.Millisecond).Round(time.Second).String()),
		rowKV("Output", fmt.Sprintf("%d Hz, 16-bit stereo, panning %.0f%%", sampleRate, panV)),
	)
	rows = append(rows,
		g.Separator(),
		g.Custom(func() {
			bevelButton("ferm", "Fermer", false, func() {
				imgui.CloseCurrentPopup()
			})
		}),
	)
	return rows
}

// rowKV renders a "label : value" pair with a fixed label column.
func rowKV(k, v string) g.Widget {
	return g.Custom(func() {
		imgui.Text(k)
		imgui.SameLineV(120, 0)
		imgui.TextUnformatted(v)
	})
}
