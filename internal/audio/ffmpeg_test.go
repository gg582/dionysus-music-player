package audio

import (
	"os/exec"
	"path/filepath"
	"testing"
)

func TestDecodeFFmpegWMA(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}

	path := filepath.Join(t.TempDir(), "tone.wma")
	cmd := exec.Command(
		"ffmpeg",
		"-y",
		"-hide_banner",
		"-loglevel", "error",
		"-f", "lavfi",
		"-i", "sine=frequency=440:duration=0.25",
		"-ac", "2",
		"-ar", "44100",
		"-c:a", "wmav2",
		path,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("could not generate WMA fixture: %v: %s", err, out)
	}

	streamer, format, err := decodeFFmpeg(path)
	if err != nil {
		t.Fatalf("decodeFFmpeg() error = %v", err)
	}
	defer streamer.Close()

	if format.SampleRate != ffmpegFallbackSampleRate {
		t.Fatalf("SampleRate = %v, want %v", format.SampleRate, ffmpegFallbackSampleRate)
	}
	if streamer.Len() <= 0 {
		t.Fatal("decoded stream has no samples")
	}
	if err := streamer.Seek(streamer.Len() / 2); err != nil {
		t.Fatalf("Seek() error = %v", err)
	}

	samples := make([][2]float64, 128)
	n, ok := streamer.Stream(samples)
	if !ok || n == 0 {
		t.Fatalf("Stream() = %d, %v; want audio", n, ok)
	}
}
