# ChiptunePlayer

A small command-line player for classic Commodore Amiga music formats:

- **TFMX** ("The Final Musicsystem eXtended", Chris Hülsbeck) — `mdat.*`/`smpl.*`
  file pairs, packed `TFHD` files, TFMX 1.x / Pro / 7V
- **Future Composer 1.x** (Jochen Hippel) — `.fc`, `.fc13`, `.fc14`, `.smod`
- plus the Hippel TFMX variants (`.hip`, `.hip7`, `.mcmd`…) as a bonus

The decoding core is Michael Schwendt's
[libtfmxaudiodecoder](https://github.com/mschwendt/libtfmxaudiodecoder)
(vendored in `clib/`), wrapped in Go via cgo — the same decoder lineage used by
Audacious, XMMS and GStreamer plugins. Audio output uses
[oto](https://github.com/ebitengine/oto) (PulseAudio on Linux, no C deps).

The player format research started from the original DeliTracker 2.32
player modules (`TFMX`, `FC1.3_note`, `FC1.4_note`).

## Download

Pre-built Linux x86_64 binaries are on the
[Releases](https://github.com/yopkool29/ChiptunePlayer/releases) page:

- `ChiptunePlayer-*-linux-amd64.tar.gz` — `amiga-player` (CLI) + `chiptune-gui` (GUI)
- `chiptuneplayer_*_amd64.deb` — installs both to `/usr/bin` plus a `.desktop` entry

## Build

Requirements: Go ≥ 1.21, a C++ compiler, GNU make
(the GUI additionally needs X11/OpenGL dev headers — `libgl1-mesa-dev xorg-dev`
on Debian/Ubuntu).

```sh
make          # builds clib/ → lib/libtfmxdec.a, then bin/amiga-player + bin/chiptune-gui
make player   # CLI only
make gui      # GUI only
```

## GUI

```sh
./bin/chiptune-gui
```

Fyne-based desktop player: file picker, transport controls, seek slider,
subsong navigation, loop / LED-filter / panning options, per-voice mute
checkboxes with live volume meters, and WAV export.

## CLI usage

```sh
amiga-player module.fc                    # play
amiga-player -sub 3 mdat.turrican2        # subsong #3 (TFMX: pass the mdat.* file)
amiga-player -wav out.wav module.smod     # render to 16-bit stereo WAV
amiga-player -loop -pan 75 module.fc      # loop / stereo panning
amiga-player -filter module.fc            # Amiga LED low-pass filter emulation
amiga-player -mute 1,2 module.fc          # mute voices 1 and 2
amiga-player -list module.tfx             # list subsongs, don't play
```

Run `amiga-player -h` for all options.

## Layout

```
clib/                    vendored C++ decoder (GPL v2, see LICENSE)
internal/decoder/        Go cgo wrapper around the C API
cmd/amiga-player/        CLI frontend
```

## License

GPL v2 — see [LICENSE](LICENSE). The vendored decoder in `clib/` is
Copyright (C) Michael Schwendt, GNU GPL v2+.
