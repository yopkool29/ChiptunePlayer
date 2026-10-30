// Package deliplayer wraps the mini-DeliTracker host: original 68k
// DeliPlayer/EaglePlayer binaries running under Musashi (m68k) + Paula.
// Used as a fallback backend for formats libtfmxaudiodecoder can't read
// (ProTracker .mod, and any format a bundled player accepts).
//
// The C core keeps global state (one emulated Amiga), so only ONE
// instance may exist at a time — Open returns an error while another
// player is alive.
package deliplayer

/*
#cgo CFLAGS: -I${SRCDIR}/../../clib -I${SRCDIR}/../../clib/deli -I${SRCDIR}/../../clib/musashi
#cgo LDFLAGS: ${SRCDIR}/../../lib/libdeli.a -lm
#include <stdint.h>
#include <stdlib.h>

typedef struct DeliPlayer DeliPlayer;
DeliPlayer *deliplayer_open(const char *);
int  deliplayer_genie(DeliPlayer *, const char *);
int  deliplayer_load(DeliPlayer *, const char *);
void deliplayer_render(DeliPlayer *, int16_t *, int, int);
int  deliplayer_songend(DeliPlayer *);
void deliplayer_close(DeliPlayer *);
int  deliplayer_subsongs(DeliPlayer *, int *, int *);
void deliplayer_setsong(DeliPlayer *, int);
void deliplayer_setvol(DeliPlayer *, int);
void deliplayer_setled(DeliPlayer *, int);
void deliplayer_mute(DeliPlayer *, int, int);
int  deliplayer_voice_vol(DeliPlayer *, int);
void deliplayer_set_vu_lead_ms(int);
uint32_t deliplayer_tag(DeliPlayer *, int);
const uint8_t *deliplayer_ram(DeliPlayer *);
double deliplayer_tickhz(DeliPlayer *);
uint32_t deliplayer_code(DeliPlayer *);
*/
import "C"

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"
)

var (
	mu     sync.Mutex // serializes the single emulated instance
	active bool       // an emulated Amiga is alive right now
)

// probeGen is bumped by CancelProbe: an in-flight LoadFor notices at
// the next candidate boundary and gives up, so a newer module request
// never waits behind a full brute-force scan.
var probeGen atomic.Uint64

// CancelProbe asks any running LoadFor scan to bail out early.
func CancelProbe() { probeGen.Add(1) }

// scanBudget caps the exhaustive candidate sweep in wall-clock time:
// past it we surrender and let the caller's next backend (uade/xmp)
// try the module.  Legitimate deep scans take ~35 s on the bundled
// corpus, so the budget sits above that — it only guards against
// pathological files that make every candidate burn its full probe.
// Interactive loads escape earlier via CancelProbe.
const scanBudget = 60 * time.Second

// PlayerDirs is searched for 68k player binaries, in order.
var PlayerDirs = []string{
	"internal/bundled/players",
	"internal/bundled/players-uade",
	"uade/players",
	"extracted/players",
	"extracted/players/DeliPlayers/PlayerStore",
	"extracted/players/DeliPlayers",
}

// GenieDirs is searched for NotePlayer genie binaries (the 68k
// virtual-audio-device plugins noteplayer-style players call into).
var GenieDirs = []string{
	"internal/bundled/genies",
	"x_genies/DeliGenies/GenieStore",
	"x_genies/DeliGenies",
}

// noteGenies lists the NotePlayer genies tried for noteplayer-style
// players (T_NOTESTRUCT present), best first.
var noteGenies = []string{
	"14Bit-NotePlayer",
	"8Voc-NotePlayer",
	"32Voc-NotePlayerPlus",
	"Headphone-NotePlayer",
	"4Voc-NotePlayer",
}

// extPlayer maps a file extension to a preferred player binary name.
var extPlayer = map[string]string{
	".mod": "PTK-Prowiz",
	".ahx": "AbyssHighestExperience",
	".nt":  "PTK-Prowiz",
	".stk": "PTK-Prowiz",
	".pt":  "PTK-Prowiz",
	".dm2": "DeltaMusic2.0",
}

type Player struct {
	p    *C.DeliPlayer
	path string
	name string
	vols [4]int
	mute [4]bool
}

func findPlayer(name string) string {
	if filepath.IsAbs(name) || strings.ContainsRune(name, '/') {
		if _, err := os.Stat(name); err == nil {
			return name
		}
		return ""
	}
	for _, d := range PlayerDirs {
		p := filepath.Join(d, name)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	// recursive basename match over the player dirs
	for _, d := range PlayerDirs {
		var hit string
		filepath.WalkDir(d, func(p string, de os.DirEntry, err error) error {
			if err == nil && !de.IsDir() && de.Name() == name {
				hit = p
			}
			if hit != "" {
				return filepath.SkipAll
			}
			return nil
		})
		if hit != "" {
			return hit
		}
	}
	return ""
}

// sniffPlayer is a cheap pre-filter: a DeliPlayer binary carries the
// ASCII marker "DELIRIUM" near its code hunk — files without it are
// skipped without spinning up a whole emulated Amiga.
func sniffPlayer(path string) bool {
	full := findPlayer(path)
	if full == "" {
		return false
	}
	f, err := os.Open(full)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, 65536)
	n, _ := f.Read(buf)
	return n > 0 && strings.Contains(string(buf[:n]), "DELIRIUM")
}

// allPlayers returns every regular file under the player dirs.
// Dedup is by basename: the same player may be present both in the
// bundled cache and in a source-tree dir.
func allPlayers() []string {
	var out []string
	seen := map[string]bool{}
	for _, d := range PlayerDirs {
		filepath.WalkDir(d, func(p string, de os.DirEntry, err error) error {
			if err != nil || de.IsDir() || seen[de.Name()] {
				return nil
			}
			seen[de.Name()] = true
			out = append(out, p)
			return nil
		})
	}
	return out
}

// Open creates the emulated instance around a player binary.
// Returns nil,error if a player is already live or binary missing.
func Open(playerBin string) (*Player, error) {
	mu.Lock()
	defer mu.Unlock()
	if active {
		return nil, errors.New("a DeliPlayer instance is already active")
	}
	full := findPlayer(playerBin)
	if full == "" {
		return nil, fmt.Errorf("player %q not found in %v", playerBin, PlayerDirs)
	}
	cp := C.CString(full)
	defer C.free(unsafe.Pointer(cp))
	h := C.deliplayer_open(cp)
	if h == nil {
		return nil, errors.New("deliplayer_open failed (bad hunk?)")
	}
	active = true
	return &Player{p: h}, nil
}

// ErrNoPlayer is returned when no player binary accepts the module.
var ErrNoPlayer = errors.New("no player accepted this file")

// findGenie resolves a NotePlayer genie binary by basename.
func findGenie(name string) string {
	if filepath.IsAbs(name) || strings.ContainsRune(name, '/') {
		if _, err := os.Stat(name); err == nil {
			return name
		}
		return ""
	}
	for _, d := range GenieDirs {
		var hit string
		filepath.WalkDir(d, func(p string, de os.DirEntry, err error) error {
			if err == nil && !de.IsDir() && de.Name() == name {
				hit = p
			}
			if hit != "" {
				return filepath.SkipAll
			}
			return nil
		})
		if hit != "" {
			return hit
		}
	}
	return ""
}

// attachGenie loads a NotePlayer genie into the player's emulated
// machine. Called before Load so the genie's InitSound/StartInt run in
// the load sequence.
func (pl *Player) attachGenie() error {
	for _, name := range noteGenies {
		full := findGenie(name)
		if full == "" {
			continue
		}
		cg := C.CString(full)
		r := C.deliplayer_genie(pl.p, cg)
		C.free(unsafe.Pointer(cg))
		if r == 0 {
			return nil
		}
	}
	return errors.New("no NotePlayer genie could be attached")
}

// learnedPlayer remembers the player that accepted a given extension —
// the brute-force scan only runs once per ext; later files of the same
// format go straight to the winning binary. Persisted in the cache dir
// so a restart keeps the mapping.
var (
	learnedPlayer = map[string]string{}
	learnedMu     sync.Mutex
	learnedOnce   sync.Once
)

func learnedFile() string {
	d, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(d, "chiptuneplayer", "learned.json")
}

func loadLearned() {
	learnedOnce.Do(func() {
		if f := learnedFile(); f != "" {
			if b, err := os.ReadFile(f); err == nil {
				learnedMu.Lock()
				json.Unmarshal(b, &learnedPlayer)
				learnedMu.Unlock()
			}
		}
	})
}

func saveLearned() {
	learnedMu.Lock()
	defer learnedMu.Unlock()
	if f := learnedFile(); f != "" {
		if b, err := json.Marshal(learnedPlayer); err == nil {
			os.WriteFile(f, b, 0o644)
		}
	}
}

// LoadFor probes candidates for path and returns a ready player or nil.
// Tries the extension-preferred binary first, then every file in the
// player dirs (each player's Check2 acts as the format detector).
func LoadFor(path string, song int) (*Player, error) {
	ext := strings.ToLower(filepath.Ext(path))
	seen := map[string]bool{}
	loadLearned()
	learnedMu.Lock()
	lp := learnedPlayer[ext]
	learnedMu.Unlock()
	var cands []string
	for _, n := range []string{extPlayer[ext], lp} {
		if n != "" && !seen[n] {
			seen[n] = true
			cands = append(cands, n)
		}
	}
	cands = append(cands, allPlayers()...)
	trace := os.Getenv("DELI_PROBE_TRACE") != ""
	gen := probeGen.Load()
	deadline := time.Now().Add(scanBudget)
	stale := func() bool {
		return probeGen.Load() != gen || time.Now().After(deadline)
	}
	for _, c := range cands {
		if stale() {
			if trace {
				fmt.Fprintf(os.Stderr, "[probe] aborted\n")
			}
			return nil, ErrNoPlayer
		}
		if !sniffPlayer(c) {
			continue
		}
		if trace {
			fmt.Fprintf(os.Stderr, "[probe] %s\n", c)
		}
		pl, err := Open(c)
		if err != nil {
			continue
		}
		// noteplayer-style players (FastTracker2/XM, ImpulseTracker…)
		// only decode notes: they call dtg_NotePlayer for a NotePlayer
		// genie to emit audio. Attach a genie before Load.
		if C.deliplayer_tag(pl.p, 38 /*T_NOTESTRUCT*/) != 0 {
			if err := pl.attachGenie(); err != nil {
				if trace {
					fmt.Fprintf(os.Stderr, "[probe] %s genie: %v\n", c, err)
				}
			}
		}
		if err := pl.Load(path, song); err != nil {
			pl.Close()
			continue
		}
		if !pl.probeAudioUntil(1.5, stale) {
			// weak Check2: the player accepted a foreign module but
			// only renders silence — keep probing other candidates
			pl.Close()
			continue
		}
		// verified: reopen a fresh instance so playback starts at t=0
		// instead of past the audio probe
		pl.Close()
		pl, err = Open(c)
		if err != nil {
			continue
		}
		if err := pl.Load(path, song); err != nil {
			pl.Close()
			continue
		}
		learnedMu.Lock()
		learnedPlayer[ext] = c
		learnedMu.Unlock()
		saveLearned()
		return pl, nil
	}
	return nil, ErrNoPlayer
}

// probeAudio renders up to secs of audio and reports whether any
// non-zero sample was produced. Returns as soon as audio appears —
// a real module is detected in a few ms, only a silent player burns
// the whole budget.
func (pl *Player) probeAudio(secs float64) bool {
	return pl.probeAudioUntil(secs, nil)
}

// probeAudioUntil is probeAudio with an abort check between render
// rounds: stale()==true (newer request pending or scan deadline hit)
// reports false so the caller moves on to the next candidate.
func (pl *Player) probeAudioUntil(secs float64, stale func() bool) bool {
	chunk := make([]byte, 44100/20*4) // ~50 ms per round
	for t := 0.0; t < secs; t += 0.05 {
		if stale != nil && stale() {
			return false
		}
		for i := range chunk {
			chunk[i] = 0
		}
		pl.FillBuffer(chunk)
		for _, b := range chunk {
			if b != 0 {
				return true
			}
		}
	}
	return false
}

// Load initializes the module via the player lifecycle.
func (pl *Player) Load(path string, song int) error {
	mu.Lock()
	defer mu.Unlock()
	if pl.p == nil {
		return errors.New("closed player")
	}
	cp := C.CString(path)
	defer C.free(unsafe.Pointer(cp))
	if r := C.deliplayer_load(pl.p, cp); r != 0 {
		return fmt.Errorf("player rejected module (%d)", int(r))
	}
	pl.path = path
	pl.name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	for i := range pl.vols {
		pl.vols[i] = 100
	}
	if song > 0 {
		C.deliplayer_setsong(pl.p, C.int(song))
	}
	return nil
}

// FillBuffer renders s16le stereo 44.1kHz into buf (like tfmxdec).
// mu guards pl.p against Close racing the audio callback.
func (pl *Player) FillBuffer(buf []byte) {
	mu.Lock()
	defer mu.Unlock()
	if pl.p == nil || len(buf) < 4 {
		return
	}
	frames := len(buf) / 4
	C.deliplayer_render(pl.p, (*C.int16_t)(unsafe.Pointer(&buf[0])), C.int(frames), 44100)
}

func (pl *Player) SongEnd() bool {
	mu.Lock()
	defer mu.Unlock()
	return pl.p == nil || C.deliplayer_songend(pl.p) != 0
}

func (pl *Player) Close() {
	mu.Lock()
	defer mu.Unlock()
	if pl.p != nil {
		C.deliplayer_close(pl.p)
		pl.p = nil
		active = false
	}
}

func (pl *Player) Songs() int {
	mu.Lock()
	defer mu.Unlock()
	if pl.p == nil {
		return 1
	}
	var lo, hi C.int
	if C.deliplayer_subsongs(pl.p, &lo, &hi) == 0 || hi < lo {
		return 1
	}
	return int(hi - lo + 1)
}

// --- metadata / control (subset of the decoder API used by the GUI) ---
func (pl *Player) Reinit(song int) error {
	mu.Lock()
	defer mu.Unlock()
	if pl.p == nil {
		return errors.New("closed")
	}
	C.deliplayer_setsong(pl.p, C.int(song))
	return nil
}
// codebase returns the player's hunk-0 load address.
func (pl *Player) codebase() uint32 { return uint32(C.deliplayer_code(pl.p)) }

// tag reads a parsed DeliPlayer tag (see DTP_* / T_* tags).
func (pl *Player) tag(n int) uint32 { return uint32(C.deliplayer_tag(pl.p, C.int(n))) }

// emuRead exposes emulated RAM for tests/debugging (big-endian space).
func (pl *Player) emuRead(addr, n uint32) []byte {
	b := make([]byte, n)
	base := unsafe.Pointer(C.deliplayer_ram(pl.p))
	copy(b, unsafe.Slice((*byte)(unsafe.Add(base, addr)), n))
	return b
}

// tickHz reports the current music-tick rate (test/debug helper).
func (pl *Player) tickHz() float64 { return float64(C.deliplayer_tickhz(pl.p)) }

func (pl *Player) SetPath(string)                 {}
func (pl *Player) MixerInit(freq, precision, channels, zero, panning int) {
}
func (pl *Player) Duration() uint32      { return 0 }
func (pl *Player) FormatID() string      { return "deli" }
func (pl *Player) FormatName() string    { return "DeliPlayer 68k" }
func (pl *Player) Name() string          { return pl.name }
func (pl *Player) Artist() string        { return "" }
func (pl *Player) Title() string         { return "" }
func (pl *Player) Game() string          { return "" }
func (pl *Player) Voices() int { return 4 }
func (pl *Player) VoiceVolume(v int) int {
	mu.Lock()
	defer mu.Unlock()
	if pl.p == nil {
		return 0
	}
	return int(C.deliplayer_voice_vol(pl.p, C.int(v&3))) * 100 / 64
}
func (pl *Player) MuteVoice(v int, m bool) {
	if v < 0 || v > 3 {
		return // 4 Paula channels — no wrap: callers may pass a wider voice index
	}
	mu.Lock()
	defer mu.Unlock()
	pl.mute[v] = m
	if pl.p != nil {
		f := C.int(0)
		if m {
			f = 1
		}
		C.deliplayer_mute(pl.p, C.int(v&3), f)
	}
}
func (pl *Player) SetLoopMode(on bool)   {}
func (pl *Player) SetFiltering(on bool) {
	mu.Lock()
	defer mu.Unlock()
	f := 0
	if on {
		f = 1
	}
	C.deliplayer_setled(pl.p, C.int(f))
}
func (pl *Player) SetPanning(p int)       {}

// SetLatencyMs tells the VU meter the output pipeline depth so the
// meters report what is audible now, not what is being rendered.
func (pl *Player) SetLatencyMs(ms int) {
	mu.Lock()
	defer mu.Unlock()
	if pl.p != nil {
		C.deliplayer_set_vu_lead_ms(C.int(ms))
	}
}
func (pl *Player) Seek(ms int)            {}
func (pl *Player) EndShorts(on bool, s int) {}
