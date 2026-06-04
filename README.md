# Gozik

![Gozik Screenshot](./gozik.png)

A simple desktop music player for Linux, macOS, and Windows. It plays local audio files and audio CDs with a minimal GTK3 interface.

## Supported Formats

**Audio**

- MP3
- FLAC
- WAV
- Ogg Vorbis / Opus
- AIFF / AIF
- WMA
- M4A / AAC / ALAC
- APE, WavPack, TTA, Musepack, AC3, DTS, AMR, RealAudio, CAF, AU, VOC, DSF/DFF, and other FFmpeg-readable audio files
- Raw PCM (`.raw`, `.pcm`) with heuristic dynamic bit-depth detection (16/24/32-bit signed little-endian, stereo, 44100 Hz) via filename or CUE-sheet duration hints

**Playlists**

- M3U / M3U8
- PLS
- XSPF
- CUE sheets

**Other Sources**

- Audio CD tracks (via CD-ROM drive)
- HTTP/HTTPS streams

## Platform Support

| OS      | Architecture | Distribution Format | Notes                          |
|---------|--------------|---------------------|--------------------------------|
| Linux   | amd64        | AppImage            | Native & cross-build supported |
| Linux   | arm64        | AppImage            | Cross-build via QEMU / toolchain |
| Linux   | riscv64      | AppImage            | Cross-build via Debian ports   |
| macOS   | arm64        | DMG                 | Apple Silicon only             |
| Windows | amd64        | ZIP                 | MSYS2 / UCRT64 build           |

## Build Requirements

- **Go** 1.25.6 or later
- **GTK3** development headers (`libgtk-3-dev`)
- **libopusfile** development headers (`libopusfile-dev`)
- **libasound2** development headers (`libasound2-dev`)
- **FFmpeg** libraries (`libavcodec-dev`, `libavformat-dev`, `libavutil-dev`, `libswresample-dev`)
- For cross-compilation:
  - `gcc-aarch64-linux-gnu` (for `linux/arm64`)
  - `gcc-riscv64-linux-gnu` (for `linux/riscv64`)

## Quick Build

```bash
make
```

Or directly with Go:

```bash
go build ./cmd/gozik
```

## AppImage Packaging (Linux)

To produce standalone AppImage bundles for all supported Linux architectures:

```bash
# Ensure cross toolchains and mksquashfs are installed
sudo apt-get install -y gcc-aarch64-linux-gnu gcc-riscv64-linux-gnu mksquashfs

# Build per architecture
./build_appimage.sh amd64
./build_appimage.sh arm64
./build_appimage.sh riscv64
```

Artifacts are written to `dist/`:

- `dist/gozik-amd64.AppImage`
- `dist/gozik-arm64.AppImage`
- `dist/gozik-riscv64.AppImage`

> **Note:** `riscv64` requires Debian ports multiarch setup (`dpkg --add-architecture riscv64`). The script attempts to configure this automatically; if it fails, add the ports repository manually.

## Run

```bash
./gozik
```

You can also pass files or playlists as arguments:

```bash
./gozik song.flac playlist.m3u
```

## Controls

- **Open**: Add local audio files to the playlist
- **Open CD**: Load audio tracks from a CD-ROM drive (`/dev/sr0`)
- **Play / Pause / Stop**: Standard playback controls
- **Progress bar**: Click or drag to seek within the current track
- **Volume knob**: Adjust playback volume
- **Right-click on a track**: Remove it from the playlist
- **Theme toggle**: Cycle light / dark / system theme

## Online Metadata & Lyrics

Gozik enriches tracks automatically using:

- **MusicBrainz** for artist, album, year, and cover-art lookup
- **LRCLIB** (https://lrclib.net) for synced and plain-text lyrics

Metadata is fetched in the background when a track starts playing or is added to the queue.

## Project Structure

```
assets/         Icons, CSS, Glade UI files, and desktop entry
cmd/gozik/      Application entrypoint
internal/
  audio/        Audio playback engine (beep + FFmpeg), metadata, lyrics
  cdrom/        CD-ROM reading (Linux)
  config/       Asset paths and theme persistence
  models/       Data types (Song, Waveform, Chapter)
  ui/           GTK3 window, controls, and MPRIS integration
  utils/        Small helpers
```

## License

This project is open source. See the repository for license details.
