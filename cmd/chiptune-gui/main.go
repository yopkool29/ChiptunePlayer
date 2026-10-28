// chiptune-gui: small desktop player for Amiga TFMX / Future Composer
// modules — giu (Dear ImGui) UI, embedded file browser, draw-list VU
// meters.
package main

import (
	"bytes"
	_ "embed"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/AllenDang/cimgui-go/imgui"
	g "github.com/AllenDang/giu"
	"github.com/ebitengine/oto/v3"

	"amiga-player/internal/bundled"
	"amiga-player/internal/decoder"
	"amiga-player/internal/deliplayer"
	"amiga-player/internal/render"
)

const sampleRate = 44100

// appName is the single place the application name lives — window
// title, user-facing messages, quit dialog.
const appName = "ChiptunePlayer"

var moduleExts = []string{
	"fc", "fc3", "fc4", "fc13", "fc14", "smod",
	"hip", "hipc", "hip7", "mcmd",
	"tfx", "tfm", "tfmx", "mdat", "sog", "soc", "s7g",
	"mod", // TFMX-MOD files use this too; real ProTracker .mod will just fail to load
	"ahx",
}

var modulePrefixes = []string{"mdat.", "mcmd."}

// nonModuleExts are rendered-audio files that must never match a
// module prefix (an exported "mdat.wav" is a WAV, not a module).
var nonModuleExts = map[string]bool{
	".wav": true, ".mp3": true, ".ogg": true, ".flac": true,
	".aiff": true, ".aif": true, ".mid": true, ".mp4": true,
}

// moduleMeta snapshots the display info of the last loaded module so
// the Info popup still works after Stop (decoder already closed).
type moduleMeta struct {
	formatID, formatName      string
	name, artist, title, game string
	songs, voices, duration   int
}

// audioDecoder is the surface both backends implement:
// *decoder.Decoder (native TFMX/FC/Hippel) and *deliplayer.Player
// (original 68k DeliPlayer binaries under Musashi+Paula).
type audioDecoder interface {
	Load(path string, song int) error
	Reinit(song int) error
	Close()
	SetPath(string)
	MixerInit(freq, precision, channels, zero, panning int)
	FillBuffer([]byte)
	SongEnd() bool
	Duration() uint32
	Songs() int
	FormatID() string
	FormatName() string
	Name() string
	Artist() string
	Title() string
	Game() string
	Voices() int
	MuteVoice(voice int, mute bool)
	VoiceVolume(voice int) int
	SetLoopMode(on bool)
	SetFiltering(on bool)
	SetPanning(p int)
	Seek(ms int)
	EndShorts(on bool, secs int)
}

// engine owns the native decoder and the playback state.
type engine struct {
	mu       sync.Mutex // guards dec against reload while Read is in flight
	dec      audioDecoder
	next     audioDecoder // pending decoder swapped in when fade-out reaches 0
	fading   bool
	gain     float64 // current volume ramp 0..1
	fadeStep float64 // <0 fading out, >0 fading in
	player   *oto.Player
	filePath string
	sub      int
	meta     *moduleMeta

	playing  atomic.Bool
	paused   atomic.Bool
	posBytes atomic.Int64 // bytes pushed to audio since song start
}

// Fade lengths in stereo frames @44.1kHz.
const (
	fadeOutStep = -1.0 / 2000.0 // ~45 ms
	fadeInStep  = 1.0 / 1100.0  // ~25 ms
)

func (e *engine) position() time.Duration {
	return time.Duration(e.posBytes.Load()/4) * time.Second / time.Duration(sampleRate)
}

// transition fades the current decoder out and swaps in next
// (nil = stop) at the silence point, avoiding clicks.
func (e *engine) transition(next audioDecoder) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.dec == nil || !e.playing.Load() || e.paused.Load() {
		// nothing audible: swap immediately
		if e.dec != nil {
			e.dec.Close()
		}
		e.dec = next
		e.fading = false
		e.gain = 1.0
		e.posBytes.Store(0)
		if next == nil {
			e.playing.Store(false)
		}
		return
	}
	e.next = next
	e.fading = true
	e.fadeStep = fadeOutStep
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
	defer e.mu.Unlock()

	if e.dec == nil {
		for i := range p {
			p[i] = 0
		}
		return len(p), nil
	}

	off := 0
	for off < len(p) {
		if e.fading {
			if e.gain <= 0 {
				// silence point reached: swap decoders
				if e.dec != nil {
					e.dec.Close()
				}
				e.dec = e.next
				e.next = nil
				e.posBytes.Store(0)
				if e.dec == nil {
					e.fading = false
					e.playing.Store(false)
					for i := off; i < len(p); i++ {
						p[i] = 0
					}
					e.posBytes.Add(int64(off))
					if off == 0 {
						return 0, io.EOF
					}
					return len(p), nil
				}
				e.fadeStep = fadeInStep // fade the new module in
			}

			// frames we can render before the gain hits its boundary
			var lim int
			if e.fadeStep < 0 {
				lim = int(e.gain / -e.fadeStep)
			} else {
				lim = int((1.0 - e.gain) / e.fadeStep)
			}
			if lim < 1 {
				lim = 1
			}
			chunk := len(p) - off
			if chunk > lim*4 {
				chunk = lim * 4
			}
			e.dec.FillBuffer(p[off : off+chunk])
			for i := 0; i < chunk/4; i++ {
				idx := off + i*4
				l := int16(binary.LittleEndian.Uint16(p[idx:]))
				r := int16(binary.LittleEndian.Uint16(p[idx+2:]))
				binary.LittleEndian.PutUint16(p[idx:], uint16(float64(l)*e.gain))
				binary.LittleEndian.PutUint16(p[idx+2:], uint16(float64(r)*e.gain))
				e.gain += e.fadeStep
			}
			if e.gain < 0 {
				e.gain = 0
			}
			if e.fadeStep > 0 && e.gain >= 1.0 {
				e.gain = 1.0
				e.fading = false
			}
			off += chunk
		} else {
			e.dec.FillBuffer(p[off:])
			off = len(p)
		}
	}

	e.posBytes.Add(int64(len(p)))
	return len(p), nil
}

// ---------------------------------------------------------------------
// UI state
// ---------------------------------------------------------------------

var (
	eng        *engine
	otoCtx     *oto.Context
	wnd        *g.MasterWindow
	uiFontPath string
	fontFixed  bool
	logoFont   *imgui.Font
	logoTex    *g.Texture
	logoImg    image.Image

	configOpen bool
	quitAsk    bool

	panV     float32 = 50
	volV     float32 = 100
	volMuted bool
	infoOpen bool
	posV     float32
	seeking  bool
	loopOn   bool
	filterOn bool
	muted    [8]bool
	vols     [8]int
	nVoices  int
	durMs    int
	vu       [8]vuBar

	browseDir  string
	exportPath string
	exportMsg  string
	exportBusy atomic.Bool
	loadErr    string

	searchOn   bool
	searchPat  string
	searchHits []browseEntry
	lastPat    string
	lastDir    string
)

// isModuleFile reports whether name looks like a loadable module:
// known extension or AmigaDOS-style mdat.*/MCMD.* prefix.
func isModuleFile(name string) bool {
	l := strings.ToLower(name)
	if nonModuleExts[filepath.Ext(l)] {
		return false
	}
	for _, p := range modulePrefixes {
		if strings.HasPrefix(l, p) {
			return true
		}
	}
	ext := strings.ToLower(filepath.Ext(l))
	for _, e := range moduleExts {
		if ext == "."+e {
			return true
		}
	}
	return false
}

// browseEntry is one selectable row of the file browser.
type browseEntry struct {
	name  string // display name
	path  string // absolute path
	isDir bool
	isUp  bool
}

// runSearch walks browseDir recursively collecting dirs and module
// files whose name contains the pattern (case-insensitive), capped.
func runSearch() {
	pat := strings.ToLower(searchPat)
	dir := browseDir
	hits := []browseEntry{}
	if pat != "" {
		filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil || len(hits) >= 300 {
				return err
			}
			name := d.Name()
			if d.IsDir() {
				if p != dir && strings.HasPrefix(name, ".") {
					return fs.SkipDir
				}
				if p != dir && strings.Contains(strings.ToLower(name), pat) {
					hits = append(hits, browseEntry{name: name + "/", path: p, isDir: true})
				}
				return nil
			}
			if isModuleFile(name) && strings.Contains(strings.ToLower(name), pat) {
				rel, _ := filepath.Rel(dir, p)
				hits = append(hits, browseEntry{name: rel, path: p})
			}
			return nil
		})
	}
	searchHits = hits
	lastPat = searchPat
	lastDir = browseDir
}

// pickDir opens a native directory chooser (zenity) to set the
// library's start directory. Runs async so the frame loop stays free.
func pickDir() {
	go func() {
		out, err := exec.Command("zenity", "--file-selection",
			"--directory", "--title=Dossier de départ").Output()
		if err == nil {
			if p := strings.TrimSpace(string(out)); p != "" {
				browseDir = p
			}
		}
	}()
}

func browserEntries() []browseEntry {
	var dirs, files []browseEntry
	if rd, err := os.ReadDir(browseDir); err == nil {
		for _, e := range rd {
			p := filepath.Join(browseDir, e.Name())
			if e.IsDir() {
				if !strings.HasPrefix(e.Name(), ".") {
					dirs = append(dirs, browseEntry{name: e.Name() + "/", path: p, isDir: true})
				}
			} else if isModuleFile(e.Name()) {
				files = append(files, browseEntry{name: e.Name(), path: p})
			}
		}
	}
	sort.Slice(dirs, func(i, j int) bool { return dirs[i].name < dirs[j].name })
	sort.Slice(files, func(i, j int) bool { return files[i].name < files[j].name })

	out := make([]browseEntry, 0, len(dirs)+len(files)+1)
	if parent := filepath.Dir(browseDir); parent != browseDir {
		out = append(out, browseEntry{name: "../", path: parent, isDir: true, isUp: true})
	}
	return append(append(out, dirs...), files...)
}

// ---------------------------------------------------------------------
// Transport / playback helpers
// ---------------------------------------------------------------------

func withDec(fn func(d audioDecoder)) {
	eng.mu.Lock()
	defer eng.mu.Unlock()
	if eng.dec != nil {
		fn(eng.dec)
	}
}

// applyVolume pushes the effective volume (0 when muted) to oto.
func applyVolume() {
	if eng.player == nil {
		return
	}
	v := float64(volV) / 100
	if volMuted {
		v = 0
	}
	eng.player.SetVolume(v)
}

func startPlayback() {
	eng.posBytes.Store(0)
	eng.playing.Store(true)
	eng.paused.Store(false)
	if eng.player == nil || !eng.player.IsPlaying() {
		if eng.player != nil {
			eng.player.Close()
		}
		eng.player = otoCtx.NewPlayer(eng)
		applyVolume()
		eng.player.Play()
	}
}

// loadBackend picks the decoder for path: native tfmxdec first, then
// the original-68k DeliPlayer backend (each bundled player binary gets
// a Check2 probe — the first one to accept the module wins).
// backendMu serializes loadBackend callers: the async load worker and
// the WAV export goroutine must never probe the single-instance deli
// core at the same time.
var backendMu sync.Mutex

func loadBackend(path string, sub int) (audioDecoder, error) {
	backendMu.Lock()
	defer backendMu.Unlock()
	// the DeliPlayer core is single-instance: a live emulated player
	// must be closed before any probe runs (hard swap, no crossfade)
	eng.mu.Lock()
	if _, isDeli := eng.dec.(*deliplayer.Player); isDeli && eng.dec != nil {
		eng.dec.Close()
		eng.dec = nil
		eng.playing.Store(false)
	}
	eng.mu.Unlock()

	d := decoder.New()
	if err := d.Load(path, sub); err == nil {
		return d, nil
	}
	d.Close()
	return deliplayer.LoadFor(path, sub)
}

// Async module loading: probing DeliPlayer candidates costs seconds,
// so the UI thread only enqueues the request — a worker runs the probe
// and the result is applied from the main loop (applyLoadResult).
type loadReq struct {
	path string
	sub  int
}

type loadResult struct {
	d    audioDecoder
	path string
	sub  int
	err  error
}

var (
	loadReqCh = make(chan loadReq, 1)
	loadResCh = make(chan loadResult, 1)
	loadBusy  atomic.Bool
)

func loadWorker() {
	for req := range loadReqCh {
		loadBusy.Store(true)
		d, err := loadBackend(req.path, req.sub)
		loadResCh <- loadResult{d, req.path, req.sub, err}
	}
}

// loadModule queues a module load; only the newest pending request is
// kept so rapid double-clicks don't pile up stale probes.
func loadModule(path string, sub int) {
	select {
	case loadReqCh <- loadReq{path, sub}:
	default:
		<-loadReqCh
		loadReqCh <- loadReq{path, sub}
	}
}

// applyLoadResult finishes a load on the UI thread (all GUI state is
// touched here, never from the worker).
func applyLoadResult(r loadResult) {
	loadBusy.Store(false)
	if r.err != nil {
		if errors.Is(r.err, deliplayer.ErrNoPlayer) {
			loadErr = fmt.Sprintf("%s: %s rejected this file",
				filepath.Base(r.path), appName)
		} else {
			loadErr = fmt.Sprintf("%s: %v", filepath.Base(r.path), r.err)
		}
		return
	}
	d, path, sub := r.d, r.path, r.sub
	loadErr = ""

	// configure before swapping so the first rendered frames are correct
	d.MixerInit(sampleRate, 16, 2, 0, int(panV))
	d.SetFiltering(filterOn)
	d.SetLoopMode(loopOn)
	d.EndShorts(true, 5)
	for i := range muted {
		d.MuteVoice(i, muted[i])
	}

	eng.mu.Lock()
	eng.meta = &moduleMeta{
		formatID:   d.FormatID(),
		formatName: d.FormatName(),
		name:       d.Name(),
		artist:     d.Artist(),
		title:      d.Title(),
		game:       d.Game(),
		songs:      d.Songs(),
		voices:     d.Voices(),
		duration:   int(d.Duration()),
	}
	eng.mu.Unlock()

	eng.transition(d) // fade out old module, fade in the new one
	for i := range vu {
		vu[i].Reset()
	}
	eng.filePath = path
	eng.sub = sub
	browseDir = filepath.Dir(path)

	// suggested WAV name: for mdat.*/MCMD.* the real name sits after
	// the AmigaDOS prefix
	base := filepath.Base(path)
	for _, p := range modulePrefixes {
		if strings.HasPrefix(strings.ToLower(base), p) {
			base = base[len(p):]
			break
		}
	}
	exportPath = filepath.Join(filepath.Dir(path),
		strings.TrimSuffix(base, filepath.Ext(base))+".wav")

	if !eng.playing.Load() {
		startPlayback()
	}
}

func switchSub(delta int) {
	if eng.dec == nil {
		return
	}
	n := eng.dec.Songs()
	if n <= 1 {
		return
	}
	loadModule(eng.filePath, (eng.sub+delta+n)%n)
}

func subsongText() string {
	eng.mu.Lock()
	defer eng.mu.Unlock()
	if eng.dec == nil {
		return ""
	}
	n := eng.dec.Songs()
	if n <= 1 {
		return "single song"
	}
	return fmt.Sprintf("subsong %d/%d", eng.sub+1, n)
}

func stopClicked() {
	eng.transition(nil) // fade out, then EOF -> player stops
	eng.paused.Store(false)
}

// ---------------------------------------------------------------------
// Widgets
// ---------------------------------------------------------------------

var (
	accentCol   = imgui.Vec4{X: 0.98, Y: 0.60, Z: 0.16, W: 1}
	accentHiCol = imgui.Vec4{X: 1.00, Y: 0.74, Z: 0.34, W: 1}
	dimCol      = imgui.Vec4{X: 0.52, Y: 0.55, Z: 0.62, W: 1}
)

func fileLine() string {
	if eng.filePath == "" {
		return "(no module loaded)"
	}
	if n := eng.meta.name; n != "" {
		return n
	}
	return filepath.Base(eng.filePath)
}

func infoLine() string {
	if eng.meta == nil {
		return "pick a module in the library →"
	}
	return fmt.Sprintf("%s · %s · %d voice(s) · %s",
		eng.meta.formatName, eng.meta.formatID, eng.meta.voices,
		mmss(int64(eng.meta.duration)))
}

// dimText renders a line in the muted caption color.
func dimText(s string) g.Widget {
	return g.Custom(func() {
		imgui.PushStyleColorVec4(imgui.ColText, dimCol)
		imgui.TextUnformatted(s)
		imgui.PopStyleColor()
	})
}

// accentText renders a line in the Amiga accent color.
func accentText(s string) g.Widget {
	return g.Custom(func() {
		imgui.PushStyleColorVec4(imgui.ColText, accentCol)
		imgui.TextUnformatted(s)
		imgui.PopStyleColor()
	})
}

// section renders a SeparatorText header.
func section(name string) g.Widget {
	return g.Custom(func() { imgui.SeparatorText(name) })
}

// minimizeWindow iconifies our frameless window — the GLFW backend
// doesn't expose iconify, so we go through wmctrl on X11.
func minimizeWindow() {
	out, err := exec.Command("wmctrl", "-l").Output()
	if err != nil {
		fmt.Fprintln(os.Stderr, "minimize: wmctrl -l:", err)
		return
	}
	for _, l := range strings.Split(string(out), "\n") {
		if strings.Contains(l, appName) {
			id := strings.Fields(l)[0]
			err := exec.Command("wmctrl", "-i", "-r", id,
				"-b", "add,hidden").Run()
			fmt.Fprintln(os.Stderr, "minimize: hide", id, "->", err)
			return
		}
	}
	fmt.Fprintln(os.Stderr, "minimize: window not found")
}

//go:embed assets/logo.png
var logoPNG []byte

//go:embed assets/icon_32.png
var icon32PNG []byte

//go:embed assets/icon_64.png
var icon64PNG []byte

//go:embed assets/icon_128.png
var icon128PNG []byte

// loadAppIcons decodes the embedded window icons (taskbar/alt-tab),
// smallest to largest so the WM can pick.
func loadAppIcons() []image.Image {
	var out []image.Image
	for _, b := range [][]byte{icon32PNG, icon64PNG, icon128PNG} {
		if im, err := png.Decode(bytes.NewReader(b)); err == nil {
			out = append(out, im)
		}
	}
	return out
}

// drawLogoTex lazily uploads the embedded PNG logo as a GL texture and
// draws it at (pos + dy) scaled to height h.  Returns the drawn width.
func drawLogoTex(pos imgui.Vec2, h float32) float32 {
	if logoTex == nil && logoImg != nil {
		g.NewTextureFromRgba(logoImg, func(t *g.Texture) { logoTex = t })
	}
	if logoTex == nil {
		return 0
	}
	b := logoImg.Bounds()
	w := h * float32(b.Dx()) / float32(b.Dy())
	imgui.WindowDrawList().AddImage(logoTex.ID(),
		imgui.Vec2{X: pos.X, Y: pos.Y},
		imgui.Vec2{X: pos.X + w, Y: pos.Y + h})
	return w
}

// titleBar replaces the native title bar (frameless window): the
// empty zone drags the window, ✕ closes it.
const titleH = 44 // titlebar row height; the logo fills it

func titleBar() g.Widget {
	return g.Custom(func() {
		// PNG logo in place of the plain title (drawn into the drag zone)
		pos := imgui.CursorScreenPos()
		lw := drawLogoTex(imgui.Vec2{X: pos.X + 4, Y: pos.Y + 2}, titleH-4)
		imgui.Dummy(imgui.Vec2{X: lw + 10, Y: titleH})
		imgui.SameLine()

		avail := imgui.ContentRegionAvail().X
		const volW = 138        // speaker + slider + gear reserved zone
		const rightMargin = 12  // breathing room after the ✕ button
		imgui.InvisibleButton("##wdrag",
			imgui.Vec2{X: avail - 3*(24+6) - volW - rightMargin, Y: titleH})
		if imgui.IsItemActive() && imgui.IsMouseDragging(0) {
			md := imgui.CurrentIO().MouseDelta()
			x, y := wnd.GetPos()
			wnd.SetPos(x+int(md.X), y+int(md.Y))
		}
		imgui.SameLine()

		// discreet volume: speaker icon = mute toggle (arcs ⇄ X),
		// then the compact slider; sliding un-mutes
		const lift = 3 // nudge the whole control row up a bit
		imgui.SetCursorPosY(imgui.CursorPosY() + (titleH-22)/2 - lift)
		imgui.InvisibleButton("##volmute", imgui.Vec2{X: 20, Y: 22})
		if imgui.IsItemClickedV(0) {
			volMuted = !volMuted
			applyVolume()
		}
		vp := imgui.ItemRectMin()
		drawVolIcon(imgui.WindowDrawList(),
			(vp.X+imgui.ItemRectMax().X)/2+1, vp.Y+11, volMuted)
		imgui.SameLine()
		imgui.SetCursorPosY(imgui.CursorPosY() + (titleH-20)/2 - lift)
		imgui.SetNextItemWidth(82)
		if imgui.SliderFloatV("##vol", &volV, 0, 100, "", 0) {
			if volMuted {
				volMuted = false // sliding un-mutes
			}
			applyVolume()
		}
		imgui.SameLine()

		// config gear next to the volume
		imgui.SetCursorPosY(imgui.CursorPosY() + (titleH-24)/2 - lift)
		iconButtonS("cfg", iconGear, 24, false, func() {
			configOpen = true
			g.OpenPopup("Config")
		})
		imgui.SameLine()

		// window controls, compact (centered on the row)
		imgui.SetCursorPosY(imgui.CursorPosY() + (titleH-24)/2 - lift)
		iconButtonS("min", iconMinus, 24, false, minimizeWindow)
		imgui.SameLine()
		imgui.SetCursorPosY(imgui.CursorPosY() + (titleH-24)/2 - lift)
		iconButtonS("quit", iconClose, 24, false, requestQuit)
	})
}

func header() g.Widget {
	return g.Column(
		titleBar(),
		g.Dummy(0, 2),
		accentText(strings.ToUpper(fileLine())),
		dimText(infoLine()),
	)
}

func transport() g.Widget {
	return g.Custom(func() {
		hasDec := eng.dec != nil
		playing := eng.playing.Load()
		paused := eng.paused.Load()
		songs := 0
		if eng.meta != nil {
			songs = eng.meta.songs
		}

		iconButton("rw", iconRestart, eng.filePath == "", func() {
			loadModule(eng.filePath, eng.sub)
		})
		imgui.SameLine()

		kind := iconPlay
		if playing && !paused {
			kind = iconPause
		}
		iconButton("pp", kind, !hasDec && !playing, func() {
			switch {
			case eng.paused.Load():
				eng.paused.Store(false)
			case eng.playing.Load():
				eng.paused.Store(true)
			default:
				startPlayback()
			}
		})
		imgui.SameLine()

		iconButton("st", iconStop, !playing, stopClicked)
		imgui.SameLine()

		iconButton("subp", iconPrev, songs <= 1, func() { switchSub(-1) })
		imgui.SameLine()

		// fixed-width centered subsong label so the buttons never shift
		const field = 96
		txt := subsongText()
		tw := imgui.CalcTextSize(txt).X
		pad := (field - tw) / 2
		if pad < 0 {
			pad = 0
		}
		imgui.Dummy(imgui.Vec2{X: pad, Y: 1})
		imgui.SameLine()
		imgui.AlignTextToFramePadding()
		imgui.TextUnformatted(txt)
		imgui.SameLine()
		imgui.Dummy(imgui.Vec2{X: field - tw - pad, Y: 1})
		imgui.SameLine()

		iconButton("subn", iconNext, songs <= 1, func() { switchSub(+1) })
		imgui.SameLine()

		iconButton("inf", iconInfo, eng.meta == nil, func() {
			infoOpen = true
			g.OpenPopup("Module info")
		})
		imgui.SameLine()

		iconButton("exp", iconExport, eng.dec == nil || exportBusy.Load(), func() {
			out := exportPath
			if out == "" {
				out = filepath.Join(browseDir, "export.wav")
			}
			go exportWav(out)
		})
	})
}

func positionRow() g.Widget {
	return g.Custom(func() {
		max := float32(durMs)
		if max < 1 {
			max = 1
		}
		imgui.SetNextItemWidth(430)
		imgui.SliderFloatV("##pos", &posV, 0, max, "", 0)
		if imgui.IsItemActive() {
			seeking = true
		}
		if imgui.IsItemDeactivatedAfterEdit() {
			seeking = false
			v := posV
			withDec(func(d audioDecoder) {
				d.Seek(int(v))
				eng.posBytes.Store(int64(v) * sampleRate * 4 / 1000)
			})
		}
		imgui.SameLine()
		imgui.TextUnformatted(fmt.Sprintf("%s / %s",
			mmss(eng.position().Milliseconds()),
			mmss(int64(durMs))))
	})
}

// mmss formats a duration in ms as m:ss — fixed-width so the time
// label never resizes as the track advances.
func mmss(ms int64) string {
	if ms < 0 {
		ms = 0
	}
	return fmt.Sprintf("%d:%02d", ms/60000, ms/1000%60)
}

func vuDraw(i int) g.Widget {
	return g.Custom(func() {
		pos := g.GetCursorScreenPos()
		const w, h = 22, 84
		imgui.Dummy(imgui.Vec2{X: w, Y: h})
		drawVU(imgui.WindowDrawList(),
			float32(pos.X), float32(pos.Y), w, h, float32(vu[i].disp))
	})
}

func voicesPanel() g.Widget {
	cols := make([]g.Widget, 0, 8)
	for i := 0; i < 8; i++ {
		v := i
		cols = append(cols, g.Column(
			vuDraw(v),
			g.Custom(func() {
				imgui.BeginDisabledV(v >= nVoices)
				g.Checkbox(fmt.Sprintf("V%d", v), &muted[v]).OnChange(func() {
					withDec(func(d audioDecoder) { d.MuteVoice(v, muted[v]) })
				}).Build()
				imgui.EndDisabled()
			}),
		))
	}
	return g.Row(cols...)
}

// exportRow shows just the export status; the button lives in the
// transport row and the output path in the Config popup.
func exportRow() g.Widget {
	return g.Custom(func() {
		imgui.TextUnformatted(exportMsg)
	})
}

// exportBrowseCh delivers the directory picked in zenity back to the UI.
var (
	exportBrowseCh = make(chan string, 1)
	exportBrowsing bool
)

// configPopupLayout: export path field + hint about config.json.
func configPopupLayout() []g.Widget {
	return []g.Widget{
		g.Custom(func() {
			select {
			case p := <-exportBrowseCh:
				if p != "" {
					exportPath = p
				}
				exportBrowsing = false
			default:
			}
		}),
		g.Row(
			dimText("export path"),
			g.InputText(&exportPath).Size(268).Label("##wav"),
			g.Custom(func() {
				bevelButton("cfgbrowse", "…", exportBrowsing, func() {
					if exportBrowsing {
						return
					}
					exportBrowsing = true
					go func() {
						out, err := exec.Command("zenity", "--file-selection",
							"--directory", "--title=Choose export directory",
							"--filename="+exportPath+string(filepath.Separator)).Output()
						p := ""
						if err == nil {
							p = strings.TrimSpace(string(out))
						}
						exportBrowseCh <- p
					}()
				})
			}),
		),
		g.Row(
			dimText("vu sync"),
			g.Custom(func() {
				imgui.SetNextItemWidth(200)
				v := float32(cfg.VuSyncMs)
				if imgui.SliderFloatV("##vusync", &v, 5, 800, "%.0f ms", 0) {
					cfg.VuSyncMs = int(v)
				}
			}),
		),
		g.Custom(func() {
			imgui.PushStyleColorVec4(imgui.ColText,
				imgui.Vec4{X: 0.55, Y: 0.57, Z: 0.66, W: 1})
			imgui.TextUnformatted("saved to config.json · set debug:true")
			imgui.TextUnformatted("there to quit without confirmation")
			imgui.PopStyleColor()
		}),
		g.Custom(func() {
			bevelButton("cfgsave", "Save", false, func() {
				saveConfig()
				configOpen = false
			})
			imgui.SameLine()
			bevelButton("cfgclose", "Close", false, func() {
				configOpen = false
			})
		}),
	}
}

// quitPopupLayout: confirm-quit dialog (skipped when cfg.Debug).
func quitPopupLayout() []g.Widget {
	return []g.Widget{
		g.Custom(func() { imgui.Dummy(imgui.Vec2{X: 1, Y: 12}) }),
		g.Custom(func() {
			msg := "Quit " + appName + "?"
			tw := imgui.CalcTextSize(msg).X
			imgui.SetCursorPosX((imgui.WindowWidth() - tw) / 2)
			imgui.TextUnformatted(msg)
		}),
		g.Custom(func() { imgui.Dummy(imgui.Vec2{X: 1, Y: 20}) }),
		g.Custom(func() {
			const bw, gap = 96, 14
			imgui.SetCursorPosX((imgui.WindowWidth() - (2*bw + gap)) / 2)
			bevelButtonW("quityes", "Quit", bw, false, func() {
				quitAsk = false
				imgui.CloseCurrentPopup()
				doQuit()
			})
			imgui.SameLine()
			imgui.Dummy(imgui.Vec2{X: gap - 8, Y: 1})
			imgui.SameLine()
			bevelButtonW("quitno", "Cancel", bw, false, func() {
				quitAsk = false
				imgui.CloseCurrentPopup()
			})
		}),
	}
}

// requestQuit honors config.json: debug mode exits immediately,
// otherwise a confirm dialog pops up.
func requestQuit() {
	if cfg.Debug {
		doQuit()
		return
	}
	quitAsk = true
	g.OpenPopup("Quit?")
}

// doQuit stops audio cleanly before closing the window so nothing
// keeps playing in the background.
func doQuit() {
	eng.mu.Lock()
	if eng.dec != nil {
		eng.dec.Close()
		eng.dec = nil
	}
	eng.mu.Unlock()
	if eng.player != nil {
		eng.player.Close()
		eng.player = nil
	}
	wnd.SetShouldClose(true)
}

func exportWav(out string) {
	exportBusy.Store(true)
	exportMsg = "exporting…"
	defer func() { exportBusy.Store(false) }()

	d, err := loadBackend(eng.filePath, eng.sub)
	if err != nil {
		exportMsg = "load: " + err.Error()
		return
	}
	defer d.Close()
	d.MixerInit(sampleRate, 16, 2, 0, int(panV))
	d.EndShorts(true, 5)
	if _, err := render.ToWAV(d, out, sampleRate, 0, nil); err != nil {
		exportMsg = "export: " + err.Error()
		return
	}
	exportMsg = "→ " + filepath.Base(out)
}

func browserPanel() g.Widget {
	searching := searchOn && searchPat != ""
	if searching && (searchPat != lastPat || browseDir != lastDir) {
		runSearch()
	}

	var entries []browseEntry
	if searching {
		entries = searchHits
	} else {
		entries = browserEntries()
	}
	kids := make([]g.Widget, 0, len(entries))
	for _, e := range entries {
		e := e
		sel := g.Selectable(e.name).
			Selected(e.path == eng.filePath).
			OnClick(func() {
				if e.isDir {
					browseDir = e.path
					searchPat = ""
					return
				}
				loadModule(e.path, 0)
			})
		kids = append(kids, sel)
	}

	var searchRow g.Widget = g.Dummy(0, 0)
	if searchOn {
		// frame-centered texts so label/input/count sit on one visual line
		centered := func(s string, dim bool) g.Widget {
			return g.Custom(func() {
				imgui.AlignTextToFramePadding()
				if dim {
					imgui.TextDisabled(s)
				} else {
					imgui.TextUnformatted(s)
				}
			})
		}
		searchRow = g.Row(
			centered("Search", false),
			g.InputText(&searchPat).Size(190).Hint("pattern…"),
			g.Custom(func() {
				// compact ✕, sized to the input frame
				imgui.SetCursorPosY(imgui.CursorPosY() - 1)
				iconButtonS("clr", iconClose, 24, searchPat == "", func() { searchPat = "" })
			}),
			centered(func() string {
				if searching {
					return fmt.Sprintf("%d", len(searchHits))
				}
				return ""
			}(), true),
		)
	}

	return g.Column(
		g.Row(
			g.Custom(func() {
				bevelButton("up", "↑", false, func() {
					if p := filepath.Dir(browseDir); p != browseDir {
						browseDir = p
						searchPat = ""
					}
				})
			}),
			dimText(shortPath(browseDir, 30)),
			g.Custom(func() {
				// right-aligned: folder (set start dir) + search toggle
				imgui.Dummy(imgui.Vec2{
					X: imgui.ContentRegionAvail().X - 2*iconBtnSize - 14, Y: 1})
				imgui.SameLine()
				iconButton("dir", iconFolder, false, pickDir)
				imgui.SameLine()
				iconButton("srch", iconSearch, false, func() {
					searchOn = !searchOn
					if !searchOn {
						searchPat = ""
						searchHits = nil
					}
				})
			}),
		),
		searchRow,
		g.Child().ID("liblist").Size(-1, -1).Flags(g.WindowFlagsHorizontalScrollbar).Layout(kids...),
	)
}

func shortPath(p string, max int) string {
	if len(p) <= max {
		return p
	}
	return "…" + p[len(p)-max:]
}

// ---------------------------------------------------------------------
// Fonts — Inter Regular + clean atlas (same trick as giu-demo:
// bypass giu's hardcoded RasterizerMultiply=1.5).
// ---------------------------------------------------------------------

func loadDesktopFont() {
	candidates := []string{
		"/usr/share/fonts/opentype/inter/Inter-Regular.otf",
		"/usr/share/fonts/opentype/inter/Inter-Medium.otf",
		"/usr/share/fonts/truetype/liberation/LiberationSans-Regular.ttf",
		"/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf",
		"/usr/share/fonts/truetype/noto/NotoSans-Regular.ttf",
		`C:\Windows\Fonts\segoeui.ttf`,
		`C:\Windows\Fonts\arial.ttf`,
		"/System/Library/Fonts/SFNSText.ttf",
		"/System/Library/Fonts/Helvetica.ttc",
	}
	for _, path := range candidates {
		data, err := os.ReadFile(path)
		if err == nil {
			g.Context.FontAtlas.SetDefaultFontFromBytes(data)
			uiFontPath = path
			break
		}
	}
	g.Context.FontAtlas.SetDefaultFontSize(15)
}

func fixFontAtlas() {
	if fontFixed || uiFontPath == "" {
		return
	}
	fontFixed = true

	io := imgui.CurrentIO()
	fc := imgui.NewFontConfig()
	fc.SetPixelSnapH(true)
	fc.SetOversampleH(3)
	fc.SetOversampleV(1)
	f := io.Fonts().AddFontFromFileTTFV(uiFontPath, 15, fc, nil)
	if f != nil {
		io.SetFontDefault(f)
	}
	// logo font: extrabold italic = demoscene letterforms
	for _, p := range []string{
		"/usr/share/fonts/opentype/inter/InterDisplay-ExtraBoldItalic.otf",
		"/usr/share/fonts/opentype/inter/Inter-BoldItalic.otf",
		uiFontPath,
	} {
		logoFont = io.Fonts().AddFontFromFileTTFV(p, 22, fc, nil)
		if logoFont != nil {
			break
		}
	}
}

// ---------------------------------------------------------------------
// Frame loop
// ---------------------------------------------------------------------

func loop() {
	fixFontAtlas()

	// finished module loads land here (worker → main thread)
	select {
	case r := <-loadResCh:
		applyLoadResult(r)
	default:
	}

	// Poll decoder state once per frame under the lock — same job the
	// fyne version did on a 33 ms ticker; here the frame loop IS the ticker.
	eng.mu.Lock()
	hasDec := eng.dec != nil
	var end bool
	nv := 0
	if hasDec {
		end = eng.dec.SongEnd()
		durMs = int(eng.dec.Duration())
		if nv = eng.dec.Voices(); nv > 8 {
			nv = 8
		}
		active := eng.playing.Load() && !eng.paused.Load()
		// align VU levels with the audible position: samples rendered
		// so far minus samples still queued in oto (+~driver latency)
		if dp, ok := eng.dec.(*deliplayer.Player); ok && eng.player != nil {
			bufMs := int(int64(eng.player.BufferedSize()) * 1000 / (int64(sampleRate) * 4))
			dp.SetLatencyMs(bufMs + cfg.VuSyncMs)
		}
		for i := 0; i < nv; i++ {
			if active {
				vols[i] = eng.dec.VoiceVolume(i)
			} else {
				vols[i] = 0
			}
		}
	}
	for i := nv; i < 8; i++ {
		vols[i] = 0
	}
	nVoices = nv
	eng.mu.Unlock()

	if end && eng.playing.Load() {
		stopClicked()
	}
	if !seeking {
		posV = float32(eng.position().Milliseconds())
	}
	for i := range vu {
		vu[i].SetValue(float64(vols[i]))
	}

	// each pane lives in its own bordered Child so SeparatorText lines
	// clip to the pane instead of crossing the whole window
	left := []g.Widget{
		section("PLAYBACK"),
		transport(),
		g.Dummy(0, 6),
		positionRow(),
		g.Dummy(0, 6),
		g.Row(
			dimText("pan"),
			g.Custom(func() {
				imgui.SetNextItemWidth(300)
				if imgui.SliderFloatV("##pan", &panV, 0, 100, "%.0f%%", 0) {
					withDec(func(d audioDecoder) { d.SetPanning(int(panV)) })
				}
			}),
			g.Checkbox("loop", &loopOn).OnChange(func() {
				withDec(func(d audioDecoder) { d.SetLoopMode(loopOn) })
			}),
			g.Checkbox("LED filter", &filterOn).OnChange(func() {
				withDec(func(d audioDecoder) { d.SetFiltering(filterOn) })
			}),
		),
		g.Custom(func() {
			// always submit an item so the pending SameLine
			// doesn't leak onto the next widget
			if loadBusy.Load() {
				imgui.PushStyleColorVec4(imgui.ColText,
					imgui.Vec4{X: 1, Y: 0.8, Z: 0.3, W: 1})
				imgui.TextUnformatted("loading…")
				imgui.PopStyleColor()
			} else {
				if loadErr != "" {
					imgui.PushStyleColorVec4(imgui.ColText,
						imgui.Vec4{X: 1, Y: 0.4, Z: 0.4, W: 1})
				}
				imgui.TextUnformatted(loadErr)
				if loadErr != "" {
					imgui.PopStyleColor()
				}
			}
		}),
		section("VOICES"),
		voicesPanel(),
		g.Dummy(0, 6),
		exportRow(),
	}
	right := []g.Widget{
		section("LIBRARY"),
		browserPanel(),
	}

	g.SingleWindow().Layout(
		header(),
		g.Row(
			g.Child().Size(536, -1).Border(true).Layout(left...),
			g.Child().Size(360, -1).Border(true).Layout(right...),
		),
		g.PopupModal("Module info").IsOpen(&infoOpen).Layout(moduleInfoLayout(eng)...),
		g.PopupModal("Config").IsOpen(&configOpen).Layout(configPopupLayout()...),
		g.Custom(func() {
			imgui.SetNextWindowSizeV(imgui.Vec2{X: 240, Y: 140}, imgui.CondAlways)
		}),
		g.PopupModal("Quit?").IsOpen(&quitAsk).Layout(quitPopupLayout()...),
	)
}

// installRuntime extracts the embedded 68k players + Amiga DOS files
// to a cache dir so the binary works installed, without the source
// tree. Existing disk dirs (source tree, DELI_DOSROOT) keep priority.
func installRuntime() {
	cache, err := os.UserCacheDir()
	if err != nil {
		return
	}
	rt := filepath.Join(cache, "chiptuneplayer")
	if err := bundled.Install(rt); err != nil {
		return
	}
	deliplayer.PlayerDirs = append([]string{
		filepath.Join(rt, "players"),
		filepath.Join(rt, "players-uade"),
	}, deliplayer.PlayerDirs...)
	if os.Getenv("DELI_DOSROOT") == "" {
		os.Setenv("DELI_DOSROOT", filepath.Join(rt, "amiga-dos"))
	}
}

func main() {
	ctx, ready, err := oto.NewContext(&oto.NewContextOptions{
		SampleRate:   sampleRate,
		ChannelCount: 2,
		Format:       oto.FormatSignedInt16LE,
		BufferSize:   120 * time.Millisecond,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "audio device:", err)
		os.Exit(1)
	}
	<-ready
	otoCtx = ctx

	loadConfig()

	installRuntime()

	if d, err := png.Decode(bytes.NewReader(logoPNG)); err == nil {
		logoImg = d
	}

	eng = &engine{}
	go loadWorker()

	cwd, _ := os.Getwd()
	browseDir = cwd
	if fi, err := os.Stat(filepath.Join(cwd, "testdata")); err == nil && fi.IsDir() {
		browseDir = filepath.Join(cwd, "testdata")
	}
	if len(os.Args) > 1 {
		if abs, err := filepath.Abs(os.Args[1]); err == nil {
			browseDir = filepath.Dir(abs)
			loadModule(abs, 0)
		}
	}

	wnd = g.NewMasterWindow(appName, 940, 560,
		g.MasterWindowFlagsNotResizable|g.MasterWindowFlagsFrameless)
	if icons := loadAppIcons(); len(icons) > 0 {
		wnd.SetIcon(icons...)
	}
	// cimgui's GLFW backend sleeps to a 30 fps default cap; vsync paces
	// the loop once the target is above the refresh rate.
	wnd.SetTargetFPS(240)
	loadDesktopFont()
	// Amiga-ish accents: orange grabs/marks, softened buttons, dim
	// separators — the rest stays ImGui dark.
	wnd.SetStyle(g.Style().
		SetColorVec4(g.StyleColorSliderGrab, accentCol).
		SetColorVec4(g.StyleColorSliderGrabActive, accentHiCol).
		SetColorVec4(g.StyleColorCheckMark, accentCol).
		SetColorVec4(g.StyleColorSeparator, imgui.Vec4{X: 0.28, Y: 0.30, Z: 0.38, W: 1}).
		SetColorVec4(g.StyleColorScrollbarGrab, imgui.Vec4{X: 0.30, Y: 0.32, Z: 0.42, W: 1}).
		SetColorVec4(g.StyleColorScrollbarGrabHovered, imgui.Vec4{X: 0.38, Y: 0.40, Z: 0.52, W: 1}).
		SetColorVec4(g.StyleColorHeaderHovered, imgui.Vec4{X: 0.25, Y: 0.32, Z: 0.50, W: 0.6}).
		SetColorVec4(g.StyleColorFrameBgHovered, imgui.Vec4{X: 0.22, Y: 0.24, Z: 0.32, W: 1}).
		SetStyleFloat(g.StyleVarFrameRounding, 4).
		SetStyleFloat(g.StyleVarGrabRounding, 4).
		SetStyleFloat(g.StyleVarScrollbarRounding, 4))
	wnd.Run(loop)

	// cleanup on exit
	eng.playing.Store(false)
	eng.mu.Lock()
	if eng.dec != nil {
		eng.dec.Close()
		eng.dec = nil
	}
	eng.mu.Unlock()
	if eng.player != nil {
		eng.player.Close()
	}
}
