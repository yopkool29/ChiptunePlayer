// Package openmptplayer plays PC tracker modules (XM, IT, S3M, ...) via
// the system libopenmpt — the OpenMPT engine, reference-grade replay for
// these formats. UADE cannot render them beyond 4 Paula channels.
package openmptplayer

/*
#cgo CFLAGS: -I${SRCDIR}/../../third_party/libopenmpt/include
#cgo LDFLAGS: -l:libopenmpt.so.0
#include <stdlib.h>
#include <string.h>
#include <libopenmpt/libopenmpt.h>
#include <libopenmpt/libopenmpt_ext.h>

static openmpt_module_ext* ompt_load(const void* data, size_t size) {
	return openmpt_module_ext_create_from_memory(data, size, NULL, NULL,
		openmpt_error_func_ignore, NULL, NULL, NULL, NULL);
}

static int ompt_mute(openmpt_module_ext *ext, int channel, int mute) {
	openmpt_module_ext_interface_interactive iface;
	memset(&iface, 0, sizeof(iface));
	if (!openmpt_module_ext_get_interface(ext, "interactive", &iface, sizeof(iface)))
		return 0;
	if (!iface.set_channel_mute_status)
		return 0;
	return iface.set_channel_mute_status(ext, channel, mute);
}

static double ompt_chanvol(openmpt_module_ext *ext, int channel) {
	openmpt_module_ext_interface_interactive iface;
	memset(&iface, 0, sizeof(iface));
	if (!openmpt_module_ext_get_interface(ext, "interactive", &iface, sizeof(iface)))
		return 0;
	if (!iface.get_channel_volume)
		return 0;
	return iface.get_channel_volume(ext, channel);
}
*/
import "C"

import (
	"errors"
	"os"
	"unsafe"
)

// Player wraps one openmpt_module_ext (ext gives channel muting).
type Player struct {
	mod    *C.openmpt_module
	ext    *C.openmpt_module_ext
	rate   int
	end    bool
	loop   bool
	filter bool // software LED-filter approximation
	bqL    [4]float64
	bqR    [4]float64
	path   string
	name   string
	format string
	voices int
	songs  int
	dur    uint32 // ms
}

// LoadFor creates a player if libopenmpt accepts the file.
func LoadFor(path string, song int) (*Player, error) {
	p := &Player{rate: 44100}
	if err := p.Load(path, song); err != nil {
		return nil, err
	}
	return p, nil
}

func metadata(mod *C.openmpt_module, key string) string {
	k := C.CString(key)
	defer C.free(unsafe.Pointer(k))
	return C.GoString(C.openmpt_module_get_metadata(mod, k))
}

func (p *Player) Load(path string, song int) error {
	p.Close()
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	cdata := C.CBytes(data)
	defer C.free(cdata)
	ext := C.ompt_load(cdata, C.size_t(len(data)))
	if ext == nil {
		return errors.New("libopenmpt rejected this file")
	}
	p.ext = ext
	p.mod = C.openmpt_module_ext_get_module(ext)
	if p.mod == nil {
		C.openmpt_module_ext_destroy(ext)
		p.ext = nil
		return errors.New("libopenmpt: no module")
	}
	p.path = path
	p.name = metadata(p.mod, "title")
	p.format = metadata(p.mod, "type_long")
	if p.format == "" {
		p.format = metadata(p.mod, "type")
	}
	p.voices = int(C.openmpt_module_get_num_channels(p.mod))
	p.songs = int(C.openmpt_module_get_num_subsongs(p.mod))
	if p.songs < 1 {
		p.songs = 1
	}
	if song >= 0 && song < p.songs {
		C.openmpt_module_select_subsong(p.mod, C.int32_t(song))
	}
	p.dur = uint32(C.openmpt_module_get_duration_seconds(p.mod) * 1000)
	return nil
}

func (p *Player) Reinit(song int) error {
	if p.mod == nil {
		return errors.New("no module")
	}
	if song >= 0 && song < p.songs {
		C.openmpt_module_select_subsong(p.mod, C.int32_t(song))
	}
	C.openmpt_module_set_position_seconds(p.mod, 0)
	p.end = false
	return nil
}

func (p *Player) Close() {
	if p.ext != nil {
		C.openmpt_module_ext_destroy(p.ext)
		p.ext = nil
		p.mod = nil
	}
}

// FillBuffer renders s16le stereo; openmpt returns 0 frames at song end.
func (p *Player) FillBuffer(buf []byte) {
	if p.mod == nil {
		for i := range buf {
			buf[i] = 0
		}
		return
	}
	off := 0
	for off < len(buf) {
		n := C.openmpt_module_read_interleaved_stereo(p.mod,
			C.int32_t(p.rate), C.size_t((len(buf)-off)/4),
			(*C.int16_t)(unsafe.Pointer(&buf[off])))
		if n == 0 {
			if p.loop {
				p.end = false
				C.openmpt_module_set_position_seconds(p.mod, 0)
				continue
			}
			p.end = true
			break
		}
		off += int(n) * 4
	}
	for i := off; i < len(buf); i++ {
		buf[i] = 0
	}
	if p.filter {
		p.applyLowpass(buf)
	}
}

// applyLowpass approximates the Amiga LED filter: a 3.3 kHz 12 dB/oct
// Butterworth (RBJ biquad, fs=44.1k, Q=1/√2), so the toggle stays clearly
// audible on PC formats too.
func (p *Player) applyLowpass(buf []byte) {
	const (
		b0 = 0.0410
		b1 = 0.0820
		b2 = 0.0410
		a1 = -1.3512
		a2 = 0.5151
	)
	for i := 0; i+3 < len(buf); i += 4 {
		for ch := 0; ch < 2; ch++ {
			o := i + ch*2
			x := float64(int16(buf[o]) | int16(buf[o+1])<<8)
			s := &p.bqL
			if ch == 1 {
				s = &p.bqR
			}
			y := b0*x + b1*s[0] + b2*s[1] - a1*s[2] - a2*s[3]
			s[1], s[0] = s[0], x
			s[3], s[2] = s[2], y
			v := int16(y)
			buf[o] = byte(v)
			buf[o+1] = byte(v >> 8)
		}
	}
}

func (p *Player) SongEnd() bool      { return p.end }
func (p *Player) Duration() uint32   { return p.dur }
func (p *Player) Songs() int         { return p.songs }
func (p *Player) FormatID() string   { return "openmpt" }
func (p *Player) FormatName() string { return p.format }
func (p *Player) Name() string       { return p.name }
func (p *Player) Artist() string     { return "" }
func (p *Player) Title() string      { return p.name }
func (p *Player) Game() string       { return "" }
func (p *Player) Voices() int        { return p.voices }
func (p *Player) SetPath(s string)   { p.path = s }

func (p *Player) MixerInit(freq, precision, channels, zero, panning int) {
	if freq > 0 {
		p.rate = freq
	}
}

func (p *Player) MuteVoice(voice int, mute bool) {
	if p.ext == nil {
		return
	}
	m := C.int(0)
	if mute {
		m = 1
	}
	C.ompt_mute(p.ext, C.int(voice), m)
}

func (p *Player) VoiceVolume(voice int) int {
	if p.ext == nil || voice < 0 || voice >= p.voices {
		return 0
	}
	return int(C.ompt_chanvol(p.ext, C.int(voice)) * 100)
}

func (p *Player) SetLoopMode(on bool) { p.loop = on }

func (p *Player) SetFiltering(on bool) {
	p.filter = on
	if p.mod == nil {
		return
	}
	// interpolation length 1 (~raw Amiga) vs 8 (default quality)
	v := C.int32_t(8)
	if on {
		v = 1
	}
	C.openmpt_module_set_render_param(p.mod,
		C.OPENMPT_MODULE_RENDER_INTERPOLATIONFILTER_LENGTH, v)
}

func (p *Player) SetPanning(pan int) {
	if p.mod == nil {
		return
	}
	C.openmpt_module_set_render_param(p.mod,
		C.OPENMPT_MODULE_RENDER_STEREOSEPARATION_PERCENT, C.int32_t(pan))
}

func (p *Player) Seek(ms int) {
	if p.mod == nil {
		return
	}
	p.end = false
	C.openmpt_module_set_position_seconds(p.mod, C.double(ms)/1000)
}

func (p *Player) EndShorts(on bool, secs int) {}
