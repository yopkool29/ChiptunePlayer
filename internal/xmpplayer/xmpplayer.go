// Package xmpplayer plays PC tracker modules (XM, IT, S3M, ...) through
// the system libxmp. UADE's emulated Paula tops out at 4 channels, so
// multi-channel XM cannot play through it — libxmp is the reference
// replayer for these formats.
package xmpplayer

/*
#cgo !windows LDFLAGS: -lxmp
#cgo windows CFLAGS: -DLIBXMP_STATIC
#cgo windows LDFLAGS: -l:libxmp.a
#include <stdlib.h>
#include <xmp.h>
*/
import "C"

import (
	"errors"
	"unsafe"
)

// Player wraps one libxmp context (single module at a time).
type Player struct {
	ctx     C.xmp_context
	info    C.struct_xmp_frame_info
	rate    int
	started bool
	end     bool
	loop    bool
	path    string
	name    string
	format  string
	voices  int
	songs   int
	dur     uint32 // ms
	pending []byte // decoded-but-unconsumed frame bytes
}

// LoadFor creates a player if libxmp accepts the file, nil error otherwise.
func LoadFor(path string, song int) (*Player, error) {
	p := &Player{rate: 44100}
	if err := p.Load(path, song); err != nil {
		return nil, err
	}
	return p, nil
}

func (p *Player) Load(path string, song int) error {
	p.Close()
	ctx := C.xmp_create_context()
	cpath := C.CString(path)
	defer C.free(unsafe.Pointer(cpath))
	if C.xmp_load_module(ctx, cpath) != 0 {
		C.xmp_free_context(ctx)
		return errors.New("libxmp rejected this file")
	}
	C.xmp_scan_module(ctx)
	var mi C.struct_xmp_module_info
	C.xmp_get_module_info(ctx, &mi)
	p.ctx = ctx
	p.path = path
	p.name = C.GoString(&mi.mod.name[0])
	p.format = C.GoString(&mi.mod._type[0])
	p.voices = int(mi.mod.chn)
	p.songs = int(mi.num_sequences)
	if p.songs < 1 {
		p.songs = 1
	}
	if mi.seq_data != nil {
		p.dur = uint32(mi.seq_data.duration)
	}
	p.started = false
	p.end = false
	return nil
}

func (p *Player) Reinit(song int) error {
	if p.ctx == nil {
		return errors.New("no module")
	}
	p.end = false
	p.pending = nil
	C.xmp_end_player(p.ctx)
	if C.xmp_start_player(p.ctx, C.int(p.rate), 0) != 0 {
		return errors.New("restart failed")
	}
	p.started = true
	return nil
}

func (p *Player) Close() {
	if p.ctx == nil {
		return
	}
	C.xmp_end_player(p.ctx)
	C.xmp_release_module(p.ctx)
	C.xmp_free_context(p.ctx)
	p.ctx = nil
	p.pending = nil
}

func (p *Player) ensureStarted() {
	if !p.started && p.ctx != nil {
		if C.xmp_start_player(p.ctx, C.int(p.rate), 0) == 0 {
			p.started = true
		}
	}
}

// FillBuffer renders PCM s16le stereo until buf is full. Frame granularity
// means a remainder is kept between calls.
func (p *Player) FillBuffer(buf []byte) {
	p.ensureStarted()
	off := 0
	for off < len(buf) {
		if len(p.pending) == 0 {
			if p.ctx == nil || p.end {
				break
			}
			if C.xmp_play_frame(p.ctx) != 0 { // XMP_END or error
				if p.loop {
					p.pending = nil
					C.xmp_restart_module(p.ctx)
					continue
				}
				p.end = true
				break
			}
			C.xmp_get_frame_info(p.ctx, &p.info)
			if p.info.buffer_size > 0 {
				p.pending = unsafe.Slice((*byte)(p.info.buffer), p.info.buffer_size)
			}
		}
		if len(p.pending) > 0 {
			n := copy(buf[off:], p.pending)
			off += n
			p.pending = p.pending[n:]
		}
	}
	for i := off; i < len(buf); i++ {
		buf[i] = 0
	}
}

func (p *Player) SongEnd() bool      { return p.end }
func (p *Player) Duration() uint32   { return p.dur }
func (p *Player) Songs() int         { return p.songs }
func (p *Player) FormatID() string   { return "xmp" }
func (p *Player) FormatName() string { return p.format }
func (p *Player) Name() string       { return p.name }
func (p *Player) Artist() string     { return "" }
func (p *Player) Title() string      { return p.name }
func (p *Player) Game() string       { return "" }
func (p *Player) Voices() int        { return p.voices }
func (p *Player) SetPath(s string)   { p.path = s }

func (p *Player) MixerInit(freq, precision, channels, zero, panning int) {
	if freq > 0 && !p.started {
		p.rate = freq
	}
}

func (p *Player) MuteVoice(voice int, mute bool) {
	if p.ctx == nil {
		return
	}
	m := C.int(0)
	if mute {
		m = 1
	}
	C.xmp_channel_mute(p.ctx, C.int(voice), m)
}

func (p *Player) VoiceVolume(voice int) int {
	if p.ctx == nil || !p.started || voice < 0 || voice >= p.voices {
		return 0
	}
	// channel_info.volume is 0..64 (tracker scale) -> 0..100
	return int(p.info.channel_info[voice].volume) * 100 / 64
}

func (p *Player) SetLoopMode(on bool) { p.loop = on }

func (p *Player) SetFiltering(on bool) {
	if p.ctx == nil {
		return
	}
	v := C.int(0)
	if on {
		v = C.XMP_DSP_LOWPASS
	}
	C.xmp_set_player(p.ctx, C.XMP_PLAYER_DSP, v)
}

func (p *Player) SetPanning(int) {}

func (p *Player) Seek(ms int) {
	if p.ctx == nil || !p.started {
		return
	}
	p.pending = nil
	p.end = false
	C.xmp_seek_time(p.ctx, C.int(ms))
}

func (p *Player) EndShorts(on bool, secs int) {}
