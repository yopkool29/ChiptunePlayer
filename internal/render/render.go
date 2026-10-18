// Package render decodes a module to 16-bit stereo PCM / WAV.
package render

import (
	"encoding/binary"
	"io"
	"os"

	"amiga-player/internal/decoder"
)

// ToWAV decodes the whole song into a 16-bit stereo WAV file.
// maxSec <= 0 uses the module duration (10 min safety cap).
// progress (may be nil) is called with the rendered duration in ms.
// Returns the number of PCM bytes written.
func ToWAV(dec *decoder.Decoder, out string, rate, maxSec int, progress func(ms int)) (int64, error) {
	f, err := os.Create(out)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	if _, err := f.Write(make([]byte, 44)); err != nil {
		return 0, err
	}

	max := int64(maxSec) * int64(rate) * 4
	if max <= 0 {
		d := dec.Duration()
		if d > 0 && d < 10*60*1000 {
			max = (int64(d) + 1000) * int64(rate) * 4 / 1000
		} else {
			max = 10 * 60 * int64(rate) * 4
		}
	}

	buf := make([]byte, 65536)
	var written int64
	for written < max && !dec.SongEnd() {
		chunk := int64(len(buf))
		if rem := max - written; rem < chunk {
			chunk = rem
		}
		dec.FillBuffer(buf[:chunk])
		if _, err := f.Write(buf[:chunk]); err != nil {
			return written, err
		}
		written += chunk
		if progress != nil {
			progress(int(written / 4 * 1000 / int64(rate)))
		}
	}

	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return written, err
	}
	dataSize := uint32(written)
	hdr := make([]byte, 44)
	copy(hdr[0:4], "RIFF")
	binary.LittleEndian.PutUint32(hdr[4:8], 36+dataSize)
	copy(hdr[8:12], "WAVE")
	copy(hdr[12:16], "fmt ")
	binary.LittleEndian.PutUint32(hdr[16:20], 16)
	binary.LittleEndian.PutUint16(hdr[20:22], 1)
	binary.LittleEndian.PutUint16(hdr[22:24], 2)
	binary.LittleEndian.PutUint32(hdr[24:28], uint32(rate))
	binary.LittleEndian.PutUint32(hdr[28:32], uint32(rate*4))
	binary.LittleEndian.PutUint16(hdr[32:34], 4)
	binary.LittleEndian.PutUint16(hdr[34:36], 16)
	copy(hdr[36:40], "data")
	binary.LittleEndian.PutUint32(hdr[40:44], dataSize)
	_, err = f.Write(hdr)
	return written, err
}
