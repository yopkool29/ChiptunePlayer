// Package decoder wraps the C++ TFMX/FC audio decoder library
// (Michael Schwendt's libtfmxaudiodecoder) via its C API.
package decoder

/*
#cgo CFLAGS: -I${SRCDIR}/../../clib
#cgo CXXFLAGS: -I${SRCDIR}/../../clib
#cgo LDFLAGS: ${SRCDIR}/../../lib/libtfmxdec.a -lstdc++ -lm
#include <stdbool.h>
#include "../../clib/tfmxaudiodecoder.h"
#include <stdlib.h>
*/
import "C"

import (
	"errors"
	"unsafe"
)

// Decoder wraps a native tfmxdec handle.
type Decoder struct {
	p unsafe.Pointer
}

// New creates a decoder instance.
func New() *Decoder {
	return &Decoder{p: C.tfmxdec_new()}
}

// Close releases the native decoder.
func (d *Decoder) Close() {
	if d.p != nil {
		C.tfmxdec_delete(d.p)
		d.p = nil
	}
}

// SetPath sets the path used to resolve multi-file formats (e.g. smpl.*).
func (d *Decoder) SetPath(path string) {
	c := C.CString(path)
	defer C.free(unsafe.Pointer(c))
	C.tfmxdec_set_path(d.p, c)
}

// Load initializes the decoder with a local file (multi-file formats
// such as mdat./smpl. pairs are resolved from the file's directory).
func (d *Decoder) Load(path string, song int) error {
	c := C.CString(path)
	defer C.free(unsafe.Pointer(c))
	if C.tfmxdec_load(d.p, c, C.int(song)) == 0 {
		return errors.New("failed to load/init module")
	}
	return nil
}

// Init initializes the decoder with data from memory.
func (d *Decoder) Init(buf []byte, song int) error {
	if len(buf) == 0 {
		return errors.New("empty buffer")
	}
	if C.tfmxdec_init(d.p, unsafe.Pointer(&buf[0]), C.uint32_t(len(buf)), C.int(song)) == 0 {
		return errors.New("failed to init module")
	}
	return nil
}

// Reinit reinitializes with a different song number (current if < 0).
func (d *Decoder) Reinit(song int) error {
	if C.tfmxdec_reinit(d.p, C.int(song)) == 0 {
		return errors.New("reinit failed")
	}
	return nil
}

// MixerInit configures the output mixer (frequency, bits, channels, zero, panning 0-100).
func (d *Decoder) MixerInit(freq, precision, channels, zero, panning int) {
	C.tfmxdec_mixer_init(d.p, C.int(freq), C.int(precision), C.int(channels), C.int(zero), C.int(panning))
}

// FillBuffer renders audio into buf (bytes, format per MixerInit).
func (d *Decoder) FillBuffer(buf []byte) {
	if len(buf) == 0 {
		return
	}
	C.tfmxdec_buffer_fill(d.p, unsafe.Pointer(&buf[0]), C.uint32_t(len(buf)))
}

// SongEnd reports whether the song end has been reached.
func (d *Decoder) SongEnd() bool {
	return C.tfmxdec_song_end(d.p) != 0
}

// Duration returns the song length in milliseconds.
func (d *Decoder) Duration() uint32 {
	return uint32(C.tfmxdec_duration(d.p))
}

// Songs returns the number of (sub)songs.
func (d *Decoder) Songs() int {
	return int(C.tfmxdec_songs(d.p))
}

// FormatID is a short identifier of the detected format.
func (d *Decoder) FormatID() string {
	return C.GoString(C.tfmxdec_format_id(d.p))
}

// FormatName describes the detected format.
func (d *Decoder) FormatName() string {
	return C.GoString(C.tfmxdec_format_name(d.p))
}

// Name returns a name constructed from the filename.
func (d *Decoder) Name() string {
	return C.GoString(C.tfmxdec_get_name(d.p))
}

// Artist/Title/Game return metadata stored in TFMX-MOD files
// (usually empty for other formats).
func (d *Decoder) Artist() string {
	return C.GoString(C.tfmxdec_get_artist(d.p))
}

func (d *Decoder) Title() string {
	return C.GoString(C.tfmxdec_get_title(d.p))
}

func (d *Decoder) Game() string {
	return C.GoString(C.tfmxdec_get_game(d.p))
}

// Voices returns the number of Paula voices in use.
func (d *Decoder) Voices() int {
	return int(C.tfmxdec_voices(d.p))
}

// MuteVoice mutes/unmutes a single voice.
func (d *Decoder) MuteVoice(voice int, mute bool) {
	C.tfmxdec_mute_voice(d.p, C.bool(mute), C.uint(voice))
}

// VoiceVolume returns 0..100 for the given voice.
func (d *Decoder) VoiceVolume(voice int) int {
	return int(C.tfmxdec_get_voice_volume(d.p, C.uint(voice)))
}

// SetLoopMode loops back to the beginning instead of ending.
func (d *Decoder) SetLoopMode(on bool) {
	var flag C.int
	if on {
		flag = 1
	}
	C.tfmxdec_set_loop_mode(d.p, flag)
}

// SetFiltering toggles the Paula LED low-pass filter emulation.
func (d *Decoder) SetFiltering(on bool) {
	var flag C.int
	if on {
		flag = 1
	}
	C.tfmxdec_set_filtering(d.p, flag)
}

// SetPanning sets stereo panning 0..100 (75 default).
func (d *Decoder) SetPanning(p int) {
	C.tfmxdec_set_panning(d.p, C.int(p))
}

// Seek sets the play position in milliseconds.
func (d *Decoder) Seek(ms int) {
	C.tfmxdec_seek(d.p, C.int32_t(ms))
}

// EndShorts ends subsongs shorter than secs immediately.
func (d *Decoder) EndShorts(on bool, secs int) {
	var flag C.int
	if on {
		flag = 1
	}
	C.tfmxdec_end_shorts(d.p, flag, C.int(secs))
}
