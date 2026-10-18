// chiptune-gui: small desktop player for Amiga TFMX / Future Composer modules.
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/widget"
	"github.com/ebitengine/oto/v3"

	"amiga-player/internal/decoder"
	"amiga-player/internal/render"
)

const sampleRate = 44100

var moduleExts = []string{
	"fc", "fc3", "fc4", "fc13", "fc14", "smod",
	"hip", "hipc", "hip7", "mcmd",
	"tfx", "tfm", "tfmx", "mdat", "sog", "soc", "s7g",
}

// engine owns the native decoder and the playback state.
type engine struct {
	mu       sync.Mutex // guards dec against reload while Read is in flight
	dec      *decoder.Decoder
	player   *oto.Player
	filePath string
	sub      int

	playing  atomic.Bool
	paused   atomic.Bool
	posBytes atomic.Int64 // bytes pushed to audio since song start
}

func (e *engine) position() time.Duration {
	return time.Duration(e.posBytes.Load()/4) * time.Second / time.Duration(sampleRate)
}

// Read implements io.Reader for oto.
func (e *engine) Read(p []byte) (int, error) {
	if !e.playing.Load() {
		return 0, io.EOF
	}
	if e.paused.Load() {
		for i := range p {
			p[i] = 0
		}
		return len(p), nil
	}
	e.mu.Lock()
	e.dec.FillBuffer(p)
	e.mu.Unlock()
	e.posBytes.Add(int64(len(p)))
	return len(p), nil
}

func main() {
	a := app.NewWithID("chiptuneplayer")
	w := a.NewWindow("ChiptunePlayer")
	w.Resize(fyne.NewSize(620, 420))

	otoCtx, ready, err := oto.NewContext(&oto.NewContextOptions{
		SampleRate:   sampleRate,
		ChannelCount: 2,
		Format:       oto.FormatSignedInt16LE,
		BufferSize:   120 * time.Millisecond,
	})
	if err != nil {
		dialog.ShowError(fmt.Errorf("audio device: %w", err), w)
		w.Close()
		return
	}
	<-ready

	eng := &engine{}

	// ---------- widgets ----------

	fileLabel := widget.NewLabelWithStyle("(no module loaded)", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	infoLabel := widget.NewLabel("")
	subLabel := widget.NewLabel("")

	posSlider := widget.NewSlider(0, 1)
	posLabel := widget.NewLabel("0:00 / 0:00")

	playBtn := widget.NewButton("Play", nil)
	pauseBtn := widget.NewButton("Pause", nil)
	stopBtn := widget.NewButton("Stop", nil)
	prevSub := widget.NewButton("<", nil)
	nextSub := widget.NewButton(">", nil)
	loopChk := widget.NewCheck("loop", nil)
	filterChk := widget.NewCheck("LED filter", nil)
	panSlider := widget.NewSlider(0, 100)
	panSlider.SetValue(75)

	voiceChecks := make([]*widget.Check, 8)
	voiceBars := make([]*widget.ProgressBar, 8)
	voiceBox := container.NewVBox()

	var updPosGuard bool

	setTransport := func(loaded bool) {
		playBtn.Enable()
		if !loaded {
			playBtn.Disable()
		}
		pauseBtn.Disable()
		stopBtn.Disable()
	}
	setTransport(false)

	startPlayback := func() {
		if eng.player != nil {
			eng.player.Close()
			eng.player = nil
		}
		eng.posBytes.Store(0)
		eng.playing.Store(true)
		eng.paused.Store(false)
		eng.player = otoCtx.NewPlayer(eng)
		eng.player.Play()
		playBtn.SetText("Restart")
		pauseBtn.SetText("Pause")
		pauseBtn.Enable()
		stopBtn.Enable()
	}

	cwd, _ := os.Getwd()
	startDir, _ := storage.ListerForURI(storage.NewFileURI(cwd))

	loadModule := func(path string, sub int) {
		d := decoder.New()
		if err := d.Load(path, sub); err != nil {
			d.Close()
			dialog.ShowError(err, w)
			return
		}
		eng.mu.Lock()
		if eng.dec != nil {
			eng.dec.Close()
		}
		eng.dec = d
		eng.mu.Unlock()
		eng.filePath = path
		eng.sub = sub
		if lu, err := storage.ListerForURI(storage.NewFileURI(filepath.Dir(path))); err == nil {
			startDir = lu
		}

		d.MixerInit(sampleRate, 16, 2, 0, int(panSlider.Value))
		d.SetFiltering(filterChk.Checked)
		d.SetLoopMode(loopChk.Checked)
		d.EndShorts(true, 5)
		for i, c := range voiceChecks {
			if c != nil {
				d.MuteVoice(i, c.Checked)
			}
		}

		fileLabel.SetText(path)
		infoLabel.SetText(fmt.Sprintf("%s (%s) — %d voice(s)",
			d.FormatName(), d.FormatID(), d.Voices()))
		subLabel.SetText(subsongText(d, sub))
		dur := float64(d.Duration())
		if dur <= 0 {
			dur = 1
		}
		posSlider.Max = dur
		startPlayback()
	}

	openFile := func() {
		fd := dialog.NewFileOpen(func(rc fyne.URIReadCloser, err error) {
			if err != nil || rc == nil {
				return
			}
			rc.Close()
			loadModule(rc.URI().Path(), 0)
		}, w)
		fd.SetFilter(&extFilter{exts: moduleExts})
		if startDir != nil {
			fd.SetLocation(startDir)
		}
		fd.Resize(fyne.NewSize(1000, 700))
		fd.Show()
	}

	exportWav := func() {
		if eng.dec == nil {
			return
		}
		sd := dialog.NewFileSave(func(wc fyne.URIWriteCloser, err error) {
			if err != nil || wc == nil {
				return
			}
			out := wc.URI().Path()
			wc.Close()
			go func() {
				// render on a fresh decoder to not disturb live playback
				d := decoder.New()
				defer d.Close()
				if err := d.Load(eng.filePath, eng.sub); err != nil {
					fyne.Do(func() { dialog.ShowError(err, w) })
					return
				}
				d.MixerInit(sampleRate, 16, 2, 0, 75)
				d.EndShorts(true, 5)
				prog := dialog.NewCustomWithoutButtons("Exporting WAV…",
					widget.NewLabel(out), w)
				prog.Show()
				_, rerr := render.ToWAV(d, out, sampleRate, 0, nil)
				fyne.Do(func() {
					prog.Hide()
					if rerr != nil {
						dialog.ShowError(rerr, w)
					} else {
						dialog.ShowInformation("Export done", out, w)
					}
				})
			}()
		}, w)
		sd.SetFileName("export.wav")
		if startDir != nil {
			sd.SetLocation(startDir)
		}
		sd.Resize(fyne.NewSize(1000, 700))
		sd.Show()
	}

	playBtn.OnTapped = func() {
		if eng.dec == nil {
			return
		}
		if eng.playing.Load() {
			eng.dec.Reinit(eng.sub) // restart current subsong
		}
		startPlayback()
	}
	pauseBtn.OnTapped = func() {
		if !eng.playing.Load() {
			return
		}
		if eng.paused.Load() {
			eng.paused.Store(false)
			pauseBtn.SetText("Pause")
		} else {
			eng.paused.Store(true)
			pauseBtn.SetText("Resume")
		}
	}
	stopBtn.OnTapped = func() {
		eng.playing.Store(false)
		eng.paused.Store(false)
		playBtn.SetText("Play")
		pauseBtn.Disable()
		stopBtn.Disable()
		eng.posBytes.Store(0)
	}
	prevSub.OnTapped = func() { switchSub(eng, loadModule, -1) }
	nextSub.OnTapped = func() { switchSub(eng, loadModule, +1) }

	loopChk.OnChanged = func(b bool) {
		if eng.dec != nil {
			eng.dec.SetLoopMode(b)
		}
	}
	filterChk.OnChanged = func(b bool) {
		if eng.dec != nil {
			eng.dec.SetFiltering(b)
		}
	}
	panSlider.OnChanged = func(v float64) {
		if eng.dec != nil {
			eng.dec.SetPanning(int(v))
		}
	}
	posSlider.OnChangeEnded = func(v float64) {
		if eng.dec != nil {
			eng.dec.Seek(int(v))
			eng.posBytes.Store(int64(v) * int64(sampleRate) * 4 / 1000)
		}
		updPosGuard = false
	}
	posSlider.OnChanged = func(float64) { updPosGuard = true }

	// ---------- layout ----------

	openBtn := widget.NewButton("Open…", openFile)
	wavBtn := widget.NewButton("Export WAV", exportWav)

	transport := container.NewHBox(playBtn, pauseBtn, stopBtn,
		layout.NewSpacer(), prevSub, subLabel, nextSub, loopChk, filterChk)

	posRow := container.NewBorder(nil, nil, nil, posLabel, posSlider)

	for i := 0; i < 8; i++ {
		v := i
		chk := widget.NewCheck(fmt.Sprintf("V%d", v), func(b bool) {
			if eng.dec != nil {
				eng.dec.MuteVoice(v, b)
			}
		})
		bar := widget.NewProgressBar()
		bar.Max = 100
		chk.Hide()
		bar.Hide()
		voiceChecks[i] = chk
		voiceBars[i] = bar
		voiceBox.Add(container.NewBorder(nil, nil, chk, nil, bar))
	}
	voicesGroup := widget.NewCard("Voices", "", voiceBox)

	panRow := container.NewBorder(nil, nil, widget.NewLabel("panning"), nil, panSlider)

	w.SetContent(container.NewBorder(
		container.NewVBox(
			container.NewHBox(openBtn, wavBtn, fileLabel),
			infoLabel,
			widget.NewSeparator(),
		),
		container.NewVBox(
			panRow,
			transport,
			posRow,
		),
		nil, nil,
		voicesGroup,
	))

	// ---------- ui refresh loop ----------

	go func() {
		t := time.NewTicker(100 * time.Millisecond)
		for range t.C {
			if eng.dec == nil {
				continue
			}
			fyne.Do(func() {
				if eng.playing.Load() && eng.dec.SongEnd() {
					stopBtn.OnTapped()
				}
				if !updPosGuard {
					posSlider.Value = float64(eng.position().Milliseconds())
					posSlider.Refresh()
				}
				total := time.Duration(eng.dec.Duration()) * time.Millisecond
				posLabel.SetText(fmt.Sprintf("%s / %s",
					eng.position().Round(time.Second), total.Round(time.Second)))
				if n := eng.dec.Voices(); n > 0 {
					for i := 0; i < n && i < 8; i++ {
						voiceBars[i].SetValue(float64(eng.dec.VoiceVolume(i)))
						voiceBars[i].Show()
						voiceChecks[i].Show()
					}
					for i := n; i < 8; i++ {
						voiceBars[i].Hide()
						voiceChecks[i].Hide()
					}
				}
			})
		}
	}()

	w.SetOnClosed(func() {
		eng.playing.Store(false)
		if eng.player != nil {
			eng.player.Close()
		}
		if eng.dec != nil {
			eng.dec.Close()
		}
	})

	w.ShowAndRun()
}

func subsongText(d *decoder.Decoder, sub int) string {
	n := d.Songs()
	if n <= 1 {
		return "single song"
	}
	return fmt.Sprintf("subsong %d/%d", sub+1, n)
}

func switchSub(eng *engine, load func(string, int), delta int) {
	if eng.dec == nil {
		return
	}
	n := eng.dec.Songs()
	if n <= 1 {
		return
	}
	sub := (eng.sub + delta + n) % n
	load(eng.filePath, sub)
}

// extFilter accepts files by extension (case-insensitive).
type extFilter struct{ exts []string }

func (f *extFilter) Matches(uri fyne.URI) bool {
	ext := uri.Extension()
	for _, e := range f.exts {
		if ext == "."+e {
			return true
		}
	}
	return false
}
