package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
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

// showModuleInfo opens a dialog describing the loaded module and the
// player technology behind it. Uses the metadata snapshot so it also
// works while stopped.
func showModuleInfo(eng *engine, w fyne.Window, pan float64) {
	eng.mu.Lock()
	m := eng.meta
	sub := eng.sub
	path := eng.filePath
	eng.mu.Unlock()
	if m == nil {
		return
	}

	items := []*widget.FormItem{
		widget.NewFormItem("File", widget.NewLabel(path)),
	}
	if fi, err := os.Stat(path); err == nil {
		items = append(items, widget.NewFormItem("Size", widget.NewLabel(humanSize(fi.Size()))))
	}
	if s := m.name; s != "" {
		items = append(items, widget.NewFormItem("Name", widget.NewLabel(s)))
	}
	if s := m.artist; s != "" {
		items = append(items, widget.NewFormItem("Artist", widget.NewLabel(s)))
	}
	if s := m.title; s != "" {
		items = append(items, widget.NewFormItem("Title", widget.NewLabel(s)))
	}
	if s := m.game; s != "" {
		items = append(items, widget.NewFormItem("Game", widget.NewLabel(s)))
	}
	items = append(items,
		widget.NewFormItem("Format", widget.NewLabel(m.formatName)),
		widget.NewFormItem("Format ID", widget.NewLabel(m.formatID)),
		widget.NewFormItem("DeliPlayer", widget.NewLabel(deliPlayerFor(m.formatID, m.formatName))),
		widget.NewFormItem("Decoder", widget.NewLabel(decoderEngine(m.formatName)+" (C++)")),
		widget.NewFormItem("Voices", widget.NewLabel(fmt.Sprintf("%d (Paula)", m.voices))),
		widget.NewFormItem("Subsongs", widget.NewLabel(fmt.Sprintf("%d — playing #%d", m.songs, sub+1))),
		widget.NewFormItem("Duration", widget.NewLabel(
			(time.Duration(m.duration)*time.Millisecond).Round(time.Second).String())),
		widget.NewFormItem("Output", widget.NewLabel(fmt.Sprintf(
			"%d Hz, 16-bit stereo, panning %.0f%%", sampleRate, pan))),
	)

	dlg := dialog.NewCustom("Module info", "Close",
		container.NewVBox(widget.NewForm(items...)), w)
	dlg.Resize(fyne.NewSize(560, 420))
	dlg.Show()
}
