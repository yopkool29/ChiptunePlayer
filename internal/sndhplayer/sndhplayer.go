// Package sndhplayer decodes Atari ST music formats (.sndh, .ym) through
// the vendored AtariAudio library (Musashi 68k + YM2149 + MFP emulation,
// MIT licensed). AtariAudio renders mono s16; the engine consumes
// interleaved stereo s16le, so FillBuffer duplicates each sample.
package sndhplayer

/*
#cgo LDFLAGS: -L../../lib -latariaudio -lstdc++
#cgo CPPFLAGS: -I../../third_party/atariaudio/src
#cgo CXXFLAGS: -fno-exceptions -fno-rtti
#include "shim.h"
#include <stdlib.h>
*/
import "C"
import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unsafe"
)

const rate = 44100

// Player is a single SNDH/YM decoder instance. Metadata fields are set
// once at Load and immutable afterwards.
type Player struct {
	mu     sync.Mutex
	p      *C.AAPlayer
	title  string
	author string
	year   string
	format string
	subs   int
	defsub int
	sub    int // zero-based subsong index
	vu     [4]int
	muted  uint32
	loop   bool
	end    bool
	endM   bool   // EndShorts: honor duration even while looping
	pos    uint32 // mono samples rendered since subsong start
	dur    uint32 // subsong length in mono samples, 0 = unknown
}

// Supported reports whether path has an Atari extension; the real sniff
// happens in aa_open on the first bytes.
func Supported(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".sndh", ".ym", ".ymst":
		return true
	}
	return false
}

// LoadFor probes path and returns a ready-to-play Player.
func LoadFor(path string, sub int) (*Player, error) {
	p := new(Player)
	if err := p.Load(path, sub); err != nil {
		p.Close()
		return nil, err
	}
	return p, nil
}

// Load (re)opens a file — audioDecoder contract.
func (p *Player) Load(path string, song int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.p != nil {
		C.aa_close(p.p)
		p.p = nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if len(data) == 0 {
		return errors.New("sndhplayer: empty file")
	}
	p.p = C.aa_open(unsafe.Pointer(&data[0]), C.uint32_t(len(data)), rate)
	if p.p == nil {
		return errors.New("sndhplayer: unrecognized file")
	}
	var i C.AAInfo
	C.aa_info(p.p, &i)
	p.title, p.author = gostr(i.title), gostr(i.author)
	p.year, p.format = gostr(i.year), gostr(i.format)
	p.subs, p.defsub = int(i.subsongs), int(i.defsub)
	return p.initSub(song)
}

func gostr(s *C.char) string {
	if s == nil {
		return ""
	}
	return C.GoString(s)
}

// initSub selects a subsong: GUI index is zero-based, AtariAudio is
// 1-based, out-of-range falls back to the file's default.
func (p *Player) initSub(sub int) error {
	if p.subs < 1 {
		return errors.New("sndhplayer: no subsongs")
	}
	s := sub + 1
	if s < 1 || s > p.subs {
		s = p.defsub
	}
	if C.aa_init_subsong(p.p, C.int(s)) == 0 {
		return fmt.Errorf("sndhplayer: InitSubSong %d rejected", s)
	}
	p.sub = s - 1
	p.dur = uint32(C.aa_duration_samples(p.p, C.int(s)))
	p.pos, p.end = 0, false
	C.aa_mute(p.p, C.uint32_t(p.muted))
	return nil
}

func (p *Player) Reinit(song int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.initSub(song)
}

func (p *Player) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.p != nil {
		C.aa_close(p.p)
		p.p = nil
	}
}

func (p *Player) SetPath(string)                     {}
func (p *Player) MixerInit(int, int, int, int, int)  {}
func (p *Player) SetFiltering(bool)                  {}
func (p *Player) SetPanning(int)                     {}
func (p *Player) Title() string                      { return "" }
func (p *Player) FormatID() string                   { return "sndh" }
func (p *Player) Voices() int                        { return 4 } // YM A/B/C + STE DAC
func (p *Player) Name() string                       { return p.title }
func (p *Player) Artist() string                     { return p.author }
func (p *Player) Game() string                       { return p.year }
func (p *Player) FormatName() string                 { return p.format }
func (p *Player) Songs() int                         { return p.subs }
func (p *Player) VoiceVolume(v int) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	if v < 0 || v > 3 {
		return 0
	}
	return p.vu[v]
}

// FillBuffer renders mono then duplicates each sample to stereo,
// tracking per-voice peak levels from the packed 8888 VU words.
func (p *Player) FillBuffer(buf []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.p == nil || p.end {
		clear(buf)
		return
	}
	n := len(buf) / 4
	if n == 0 {
		return
	}
	mono := make([]int16, n)
	vis := make([]uint32, n)
	C.aa_render_vu(p.p, (*C.int16_t)(unsafe.Pointer(&mono[0])), C.uint32_t(n), (*C.uint32_t)(unsafe.Pointer(&vis[0])))
	var peak [4]uint32
	for i, s := range mono {
		j := i * 4
		buf[j], buf[j+1], buf[j+2], buf[j+3] = byte(s), byte(s>>8), byte(s), byte(s>>8)
		v := vis[i]
		for ch, l := range [4]uint32{v & 255, v >> 8 & 255, v >> 16 & 255, v >> 24} {
			if l > peak[ch] {
				peak[ch] = l
			}
		}
	}
	for ch := range peak {
		p.vu[ch] = int(peak[ch]) * 100 / 255
	}
	p.pos += uint32(n)
	if p.dur > 0 && p.pos >= p.dur && (!p.loop || p.endM) {
		p.end = true
	}
}

// SongEnd reports the "auto-advance" condition: a finite song played to
// its SNDH-declared duration with looping off (or EndShorts forced).
func (p *Player) SongEnd() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.end
}

// Duration returns the subsong length in ms, 0 when the SNDH tag has none.
func (p *Player) Duration() uint32 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.dur * 1000 / rate
}

// MuteVoice maps voice index to MuteVoices mask bits (A=0..DAC=3).
func (p *Player) MuteVoice(voice int, mute bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if voice < 0 || voice > 3 {
		return
	}
	if mute {
		p.muted |= 1 << voice
	} else {
		p.muted &^= 1 << voice
	}
	if p.p != nil {
		C.aa_mute(p.p, C.uint32_t(p.muted))
	}
}

func (p *Player) SetLoopMode(on bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.loop = on
}

// Seek is forward-capable only: rewind re-inits the subsong then
// fast-forwards, which is cheap on these small files.
func (p *Player) Seek(ms int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.p == nil || ms < 0 {
		return
	}
	target := uint32(ms) * rate / 1000
	if target < p.pos {
		if err := p.initSub(p.sub); err != nil {
			return
		}
	}
	if target > p.pos {
		C.aa_ffwd(p.p, C.uint32_t(target-p.pos))
		p.pos, p.end = target, false
	}
}

// EndShorts makes loop mode still honor the SNDH duration — used by the
// GUI's "end after N seconds" path.
func (p *Player) EndShorts(on bool, _ int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.endM = on
}
