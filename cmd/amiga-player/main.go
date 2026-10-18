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
	"amiga-player/internal/render"
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
		written, err := render.ToWAV(dec, *flagWav, *flagRate, *flagDur, nil)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("wrote %s (%s, %d bytes)\n",
			filepath.Base(*flagWav),
			(time.Duration(written/4)*time.Second/time.Duration(*flagRate)).Round(time.Second),
			written+44)
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


