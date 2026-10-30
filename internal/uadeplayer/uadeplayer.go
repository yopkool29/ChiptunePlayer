//go:build !windows

// Package uadeplayer plays Amiga modules through libuade (Unix Amiga
// Delitracker Emulator): the original 68k replay routines run inside a
// real Amiga emulation (UAE CPU + Paula + the UADE "score" OS layer).
// It is the primary backend; internal/deliplayer stays as the
// experimental native fallback.
package uadeplayer

/*
#cgo CFLAGS: -I${SRCDIR}/../../third_party/uade/include
#cgo LDFLAGS: ${SRCDIR}/../../third_party/uade/lib/libuade.a ${SRCDIR}/../../third_party/uade/lib/libbencodetools.a ${SRCDIR}/../../third_party/uade/lib/libzakalwe.a -lpthread -lm
#include <uade/uade.h>
#include <stdlib.h>
*/
import "C"

import (
	"errors"
	"unsafe"
)

var baseDir string

// SetBaseDir points the package at the extracted UADE runtime dir
// (must contain uadecore, score, eagleplayer.conf and players/).
func SetBaseDir(dir string) { baseDir = dir }

// Player wraps a libuade state. One uadecore subprocess per instance.
type Player struct {
	st      *C.struct_uade_state
	songs   int
	end     bool
	format  string
	modname string
	player  string
	dur     uint32
	vuL     int32 // latest left-channel peak, for the VU meters
	vuR     int32
}

// LoadFor implements the same entry point as deliplayer.LoadFor so the
// app can pick this backend first.
func LoadFor(path string, song int) (*Player, error) {
	if baseDir == "" {
		return nil, errors.New("uadeplayer: base dir not set")
	}
	var uc C.struct_uade_config
	C.uade_config_set_defaults(&uc)
	base := C.CString(baseDir)
	defer C.free(unsafe.Pointer(base))
	C.uade_config_set_option(&uc, C.UC_BASE_DIR, base)
	score := C.CString(baseDir + "/score")
	defer C.free(unsafe.Pointer(score))
	C.uade_config_set_option(&uc, C.UC_SCORE_FILE, score)
	core := C.CString(baseDir + "/uadecore")
	defer C.free(unsafe.Pointer(core))
	C.uade_config_set_option(&uc, C.UC_UADECORE_FILE, core)

	st := C.uade_new_state(&uc)
	if st == nil {
		return nil, errors.New("uadeplayer: uade_new_state failed")
	}
	p := &Player{st: st}
	if err := p.Load(path, song); err != nil {
		C.uade_cleanup_state(st)
		return nil, err
	}
	return p, nil
}

func (p *Player) Load(path string, song int) error {
	cs := C.CString(path)
	defer C.free(unsafe.Pointer(cs))
	sub := C.int(-1)
	if song > 0 {
		sub = C.int(song)
	}
	if C.uade_play(cs, sub, p.st) <= 0 {
		return errors.New("uadeplayer: cannot play " + path)
	}
	info := C.uade_get_song_info(p.st)
	p.songs = int(info.subsongs.max) - int(info.subsongs.min) + 1
	if p.songs < 1 {
		p.songs = 1
	}
	p.dur = uint32(info.duration)
	p.format = C.GoString(&info.formatname[0])
	p.modname = C.GoString(&info.modulename[0])
	p.player = C.GoString(&info.playername[0])
	p.end = false
	return nil
}

func (p *Player) Reinit(song int) error {
	path := ""
	if info := C.uade_get_song_info(p.st); info != nil {
		path = C.GoString(&info.modulefname[0])
	}
	C.uade_stop(p.st)
	if path == "" {
		return errors.New("uadeplayer: no module path to reload")
	}
	return p.Load(path, song)
}

func (p *Player) Close() {
	if p.st != nil {
		C.uade_cleanup_state(p.st)
		p.st = nil
	}
}

func (p *Player) FillBuffer(buf []byte) {
	if p.end || len(buf) == 0 {
		for i := range buf {
			buf[i] = 0
		}
		return
	}
	n := C.uade_read(unsafe.Pointer(&buf[0]), C.size_t(len(buf)), p.st)
	if n <= 0 {
		p.end = true
		for i := range buf {
			buf[i] = 0
		}
		return
	}
	for i := int(n); i < len(buf); i++ {
		buf[i] = 0
	}
	// Paula mixes voices 0+3 into left, 1+2 into right — per-side peaks
	// are the best channel VU we can get without a uadecore API.
	var l, r int32
	for i := 0; i+3 < int(n); i += 4 {
		v := int32(int16(buf[i]) | int16(buf[i+1])<<8)
		if v < 0 {
			v = -v
		}
		if v > l {
			l = v
		}
		v = int32(int16(buf[i+2]) | int16(buf[i+3])<<8)
		if v < 0 {
			v = -v
		}
		if v > r {
			r = v
		}
	}
	p.vuL, p.vuR = l, r
}

func (p *Player) SongEnd() bool          { return p.end }
func (p *Player) Duration() uint32       { return p.dur }
func (p *Player) Songs() int             { return p.songs }
func (p *Player) FormatID() string       { return "uade" }
func (p *Player) FormatName() string     { return p.format }
func (p *Player) Name() string           { return p.modname }
func (p *Player) Artist() string         { return "" }
func (p *Player) Title() string          { return "" }
func (p *Player) Game() string           { return "" }
func (p *Player) Voices() int            { return 4 }
// VoiceVolume approximates channel VU from the rendered mix: Paula
// hardware-pans voices 0+3 left and 1+2 right.
func (p *Player) VoiceVolume(v int) int {
	var pk int32
	if v == 0 || v == 3 {
		pk = p.vuL
	} else {
		pk = p.vuR
	}
	return int(pk * 100 / 32767)
}
func (p *Player) SetPath(string)         {}
func (p *Player) MixerInit(freq, precision, channels, zero, panning int) {
}
func (p *Player) MuteVoice(int, bool)  {}
func (p *Player) SetLoopMode(bool)     {}

// SetFiltering toggles the emulated "LED filter" (Paula's 3.3 kHz
// Butterworth lowpass): on = filter engaged (authentic A500 sound),
// off = filter bypassed (brighter highs). Forcing the LED keeps the
// module from re-toggling it at runtime.
func (p *Player) SetFiltering(on bool) {
	if p.st == nil {
		return
	}
	uc := C.uade_get_effective_config(p.st)
	if uc == nil {
		return
	}
	// UC_FORCE_LED_ON marks the LED state forced; the actual on/off
	// is then driven by uade_set_filter_state.
	C.uade_config_set_option(uc, C.UC_FORCE_LED_ON, nil)
	state := C.int(0)
	if on {
		state = 1
	}
	C.uade_set_filter_state(p.st, state)
}
func (p *Player) SetPanning(int)       {}
func (p *Player) Seek(int)             {}
func (p *Player) EndShorts(bool, int)  {}
