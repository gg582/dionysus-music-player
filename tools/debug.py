#!/usr/bin/env python3
"""
Convert an input audio file into every format supported by Gozik.
Outputs are placed under testfiles/ by default.

Usage:
    python3 tools/debug.py input.flac
    python3 tools/debug.py input.mp3 -o my_test_files
    python3 tools/debug.py              # generates a dummy input and converts it
"""

import argparse
import os
import subprocess
import sys
import tempfile


FORMATS = {
    "mp3": ["-c:a", "libmp3lame", "-q:a", "2"],
    "flac": ["-c:a", "flac"],
    "wav": ["-c:a", "pcm_s16le"],
    "ogg": ["-c:a", "libvorbis", "-q:a", "4"],
    "opus": ["-c:a", "libopus", "-b:a", "128k"],
    "m4a": ["-c:a", "aac", "-b:a", "192k"],
    "aac": ["-c:a", "aac", "-b:a", "192k"],
    "wma": ["-c:a", "wmav2", "-b:a", "192k"],
    "ape": ["-c:a", "ape"],
    "wv": ["-c:a", "wavpack"],
    "tta": ["-c:a", "tta"],
    "aiff": ["-c:a", "pcm_s16be"],
    "pcm": ["-f", "s16le", "-ac", "2", "-ar", "44100"],
}


def run_ffmpeg(input_path: str, output_path: str, extra_args: list[str]) -> None:
    cmd = [
        "ffmpeg",
        "-y",
        "-hide_banner",
        "-loglevel", "error",
        "-i", input_path,
    ] + extra_args + [output_path]
    subprocess.run(cmd, check=True)


def generate_dummy_audio(output_path: str, duration: int = 5) -> None:
    cmd = [
        "ffmpeg",
        "-y",
        "-hide_banner",
        "-loglevel", "error",
        "-f", "lavfi",
        "-i", f"sine=frequency=1000:duration={duration}",
        "-ac", "2",
        "-ar", "44100",
        output_path,
    ]
    subprocess.run(cmd, check=True)


def convert(input_path: str | None, out_dir: str) -> None:
    os.makedirs(out_dir, exist_ok=True)

    if not input_path or not os.path.isfile(input_path):
        dummy_path = os.path.join(tempfile.gettempdir(), "gozik_test_input.wav")
        print(f"Input file not provided or not found, generating dummy audio: {dummy_path}")
        generate_dummy_audio(dummy_path)
        input_path = dummy_path
        base = "test_input"
    else:
        base = os.path.splitext(os.path.basename(input_path))[0]

    created = []

    for ext, args in FORMATS.items():
        out_path = os.path.join(out_dir, f"{base}.{ext}")
        print(f"Generating {out_path} ...")
        run_ffmpeg(input_path, out_path, args)
        created.append(out_path)

    # Also create an M3U playlist pointing at the generated files
    m3u_path = os.path.join(out_dir, f"{base}.m3u")
    print(f"Generating {m3u_path} ...")
    with open(m3u_path, "w", encoding="utf-8") as f:
        f.write("#EXTM3U\n")
        for fp in created:
            f.write(f"{os.path.basename(fp)}\n")

    print("Done.")


def main() -> None:
    parser = argparse.ArgumentParser(
        description="Convert an audio file to all Gozik-supported formats for debugging."
    )
    parser.add_argument(
        "input",
        nargs="?",
        default=None,
        help="Input audio file (any ffmpeg-readable format). If omitted, a dummy tone is generated.",
    )
    parser.add_argument(
        "-o", "--output-dir", default="testfiles", help="Output directory (default: testfiles)"
    )
    args = parser.parse_args()
    convert(args.input, args.output_dir)


if __name__ == "__main__":
    main()
