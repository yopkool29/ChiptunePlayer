// amiga-player: play Amiga TFMX (C. Huelsbeck) and Future Composer 1.x
// (J. Hippel) modules, decoded by libtfmxaudiodecoder.
//
// Usage:
//   amiga-player [options] <module>
//
// TFMX modules usually come as mdat.<name> + smpl.<name> file pairs:
// pass the mdat.* file. FC modules are single files (.fc, .fc13, .fc14, .smod).
package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ebitengine/oto/v3"

	"amiga-player/internal/decoder"
)

var (
	flagSub    = flag.Int("sub", 0, "subsong number (0 = first)")
	flagWav    = flag.String("wav", "", "render to WAV file instead of playing")
	flagLoop   = flag.Bool("loop", false, "loop song instead of ending")
	flagDur    = flag.Int("dur", 0, "max seconds to render/play (0 = module duration or 10min cap)")
	flagRate   = flag.Int("rate", 44100, "output sample rate")
	flagPan    = flag.Int("pan", 75, "stereo panning 0-100")
	flagFilter = flag.Bool("filter", false, "enable Paula LED low-pass filter emulation")
	flagMute   = flag.String("mute", "", "comma-separated voice numbers to mute")
	flagList   = flag.Bool("list", false, "list subsongs and exit")
)

type renderer struct {
	dec *decoder.Decoder
}

func (r *renderer) Read(p []byte) (int, error) {
	if r.dec.SongEnd() {
		return 0, io.EOF
	}
	r.dec.FillBuffer(p)
	return len(p), nil
}

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "amiga-player - TFMX / Future Composer 1.x module player\n\n")
		fmt.Fprintf(os.Stderr, "usage: amiga-player [options] <module>\n\noptions:\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(2)
	}
	path := flag.Arg(0)

	dec := decoder.New()
	defer dec.Close()

	if err := dec.Load(path, *flagSub); err != nil {
		fmt.Fprintf(os.Stderr, "error: cannot load %q: %v\n", path, err)
		os.Exit(1)
	}

	dec.MixerInit(*flagRate, 16, 2, 0, *flagPan)
	dec.SetFiltering(*flagFilter)
	dec.SetLoopMode(*flagLoop)
	dec.EndShorts(true, 5)

	for _, s := range strings.Split(*flagMute, ",") {
		if s = strings.TrimSpace(s); s != "" {
			if v, err := strconv.Atoi(s); err == nil {
				dec.MuteVoice(v, true)
			}
		}
	}

	dur := time.Duration(dec.Duration()) * time.Millisecond
	fmt.Printf("format   : %s (%s)\n", dec.FormatName(), dec.FormatID())
	fmt.Printf("name     : %s\n", dec.Name())
	fmt.Printf("subsongs : %d (playing #%d)\n", dec.Songs(), *flagSub)
	fmt.Printf("voices   : %d\n", dec.Voices())
	fmt.Printf("duration : %s\n", dur.Round(time.Second))

	if *flagList {
		os.Exit(0)
	}

	if *flagWav != "" {
		if err := renderWav(dec, *flagWav, *flagRate); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if err := play(dec, *flagRate); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

// play streams the module to the default audio device.
func play(dec *decoder.Decoder, rate int) error {
	ctx, ready, err := oto.NewContext(&oto.NewContextOptions{
		SampleRate:   rate,
		ChannelCount: 2,
		Format:       oto.FormatSignedInt16LE,
		BufferSize:   120 * time.Millisecond,
	})
	if err != nil {
		return err
	}
	<-ready

	p := ctx.NewPlayer(&renderer{dec: dec})
	defer p.Close()

	fmt.Println("playing... (Ctrl-C to quit)")
	p.Play()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)

	tick := time.NewTicker(200 * time.Millisecond)
	defer tick.Stop()

	for {
		select {
		case <-sig:
			fmt.Println("\ninterrupted")
			return nil
		case <-tick.C:
			if dec.SongEnd() || !p.IsPlaying() {
				return nil
			}
		}
	}
}

// renderWav decodes the whole song into a 16-bit stereo WAV file.
func renderWav(dec *decoder.Decoder, out string, rate int) error {
	f, err := os.Create(out)
	if err != nil {
		return err
	}
	defer f.Close()

	// Reserve space for the RIFF header, patched at the end.
	if _, err := f.Write(make([]byte, 44)); err != nil {
		return err
	}

	max := int64(*flagDur) * int64(rate) * 4 // bytes
	if max <= 0 {
		d := dec.Duration()
		if d > 0 && d < 10*60*1000 {
			max = (int64(d) + 1000) * int64(rate) * 4 / 1000
		} else {
			max = 10 * 60 * int64(rate) * 4 // safety cap: 10 min
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
			return err
		}
		written += chunk
	}

	// Patch RIFF/WAVE header.
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	dataSize := uint32(written)
	hdr := make([]byte, 44)
	copy(hdr[0:4], "RIFF")
	binary.LittleEndian.PutUint32(hdr[4:8], 36+dataSize)
	copy(hdr[8:12], "WAVE")
	copy(hdr[12:16], "fmt ")
	binary.LittleEndian.PutUint32(hdr[16:20], 16)
	binary.LittleEndian.PutUint16(hdr[20:22], 1) // PCM
	binary.LittleEndian.PutUint16(hdr[22:24], 2) // stereo
	binary.LittleEndian.PutUint32(hdr[24:28], uint32(rate))
	binary.LittleEndian.PutUint32(hdr[28:32], uint32(rate*4)) // byte rate
	binary.LittleEndian.PutUint16(hdr[32:34], 4)              // block align
	binary.LittleEndian.PutUint16(hdr[34:36], 16)             // bits
	copy(hdr[36:40], "data")
	binary.LittleEndian.PutUint32(hdr[40:44], dataSize)
	if _, err := f.Write(hdr); err != nil {
		return err
	}

	fmt.Printf("wrote %s (%s, %d bytes)\n",
		filepath.Base(out), (time.Duration(written/4) * time.Second / time.Duration(rate)).Round(time.Second), written+44)
	return nil
}
