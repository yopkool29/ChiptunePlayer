# ChiptunePlayer

A small command-line player for classic Commodore Amiga music formats:

- **Broad Amiga replay via [UADE](https://gitlab.com/uade-music-player/uade)**
  (primary backend): the original 68k EaglePlayer/DeliTracker replay
  routines run inside a real Amiga emulation — MOD/ProTracker, XM/
  FastTracker 2 (NotePlayer path included), AHX, AON, MED, SoundMon,
  and ~180 other formats from `eagleplayer.conf`
- **TFMX** ("The Final Musicsystem eXtended", Chris Hülsbeck) — `mdat.*`/`smpl.*`
  file pairs, packed `TFHD` files, TFMX 1.x / Pro / 7V
- **Future Composer 1.x** (Jochen Hippel) — `.fc`, `.fc13`, `.fc14`, `.smod`
- plus the Hippel TFMX variants (`.hip`, `.hip7`, `.mcmd`…) as a bonus

Backend order at load time: **UADE** (`internal/uadeplayer`, libuade +
uadecore running the original 68k replay binaries) → native Go decoders
(TFMX/FC, Michael Schwendt's
[libtfmxaudiodecoder](https://github.com/mschwendt/libtfmxaudiodecoder)
vendored in `clib/`) → **deliplayer** (`internal/deliplayer`, our
experimental native backend: Musashi 68k + Paula/CIA emulation running
DeliPlayer binaries in-process). Audio output uses
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

On Windows, build inside an [MSYS2](https://www.msys2.org) shell
(`pacman -S mingw-w64-ucrt-x86_64-gcc mingw-w64-ucrt-x86_64-libxmp
mingw-w64-ucrt-x86_64-libopenmpt make`, plus a Windows Go toolchain);
`make` produces `bin/amiga-player.exe` + `bin/chiptune-gui.exe`. Copy
the runtime DLLs next to the binaries:
`cp /ucrt64/bin/{libstdc++-6,libgcc_s_seh-1,libwinpthread-1,libopenmpt-0,libvorbisfile-3,zlib1,libmpg123-0,libogg-0,libvorbis-0}.dll bin/`.
The UADE backend is not available on Windows (libuade/uadecore are
POSIX-only); deliplayer + the native decoders + openmpt/xmp take over.

## GUI

```sh
./bin/chiptune-gui
```

Dear ImGui desktop player ([giu](https://github.com/AllenDang/giu) +
OpenGL): embedded module browser, icon transport controls, seek slider,
subsong navigation, loop / LED-filter / panning options, per-voice mute
checkboxes with live VU meters, module-info popup, and WAV export.

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
clib/                    vendored C++ decoder + native 68k emu (Musashi, Paula, CIA)
internal/decoder/        Go cgo wrapper around the C API (TFMX/FC)
internal/deliplayer/     experimental native backend (68k DeliPlayer binaries in-process)
internal/uadeplayer/     primary backend — libuade/uadecore (full Amiga emulation)
internal/bundled/        embedded players, genies, amiga-dos env + UADE runtime assets
third_party/uade/        vendored libuade/bencodetools/zakalwe static libs + headers
cmd/amiga-player/        CLI frontend
cmd/chiptune-gui/        Dear ImGui GUI frontend
```

## License

GPL v2 — see [LICENSE](LICENSE). The vendored decoder in `clib/` is
Copyright (C) Michael Schwendt, GNU GPL v2+.
