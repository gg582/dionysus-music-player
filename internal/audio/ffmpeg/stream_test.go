package ffmpeg

import (
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestFFmpegStreamSeek(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}

	// 1. Generate a 10-second audio file using FFmpeg
	tempDir := t.TempDir()
	audioPath := filepath.Join(tempDir, "tone.mp3")
	cmd := exec.Command(
		"ffmpeg",
		"-y",
		"-hide_banner",
		"-loglevel", "error",
		"-f", "lavfi",
		"-i", "sine=frequency=440:duration=10.0",
		"-ac", "2",
		"-ar", "48000",
		audioPath,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to generate audio fixture: %v: %s", err, out)
	}

	// 2. Start local HTTP server to serve the audio file
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f, err := os.Open(audioPath)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer f.Close()
		http.ServeContent(w, r, "tone.mp3", time.Now(), f)
	}))
	defer server.Close()

	// 3. Decode HTTP stream
	streamer, format, err := DecodeStream(server.URL, nil)
	if err != nil {
		t.Fatalf("decodeFFmpegStream failed: %v", err)
	}
	defer streamer.Close()

	if format.SampleRate != ffmpegFallbackSampleRate {
		t.Errorf("expected sample rate %v, got %v", ffmpegFallbackSampleRate, format.SampleRate)
	}

	// 4. Stream a small amount of audio
	buf := make([][2]float64, 1024)
	n, ok := streamer.Stream(buf)
	if !ok || n == 0 {
		t.Fatalf("streamer didn't output any audio initially")
	}

	// 5. Seek to 5 seconds (5 * SampleRate)
	seekTarget := int(5 * time.Second * time.Duration(format.SampleRate) / time.Second)
	t.Logf("Seeking to target samples: %d", seekTarget)
	if err := streamer.Seek(seekTarget); err != nil {
		t.Fatalf("Seek failed: %v", err)
	}

	// 6. Check if streaming resumes and outputs audio
	n, ok = streamer.Stream(buf)
	if !ok || n == 0 {
		t.Fatalf("streamer failed to output audio after seeking")
	}

	// Verify position is around the seek target
	pos := streamer.Position()
	t.Logf("Position after seek and one stream read: %d", pos)
	if math.Abs(float64(pos-seekTarget)) > 20000 {
		t.Errorf("expected position close to %d, got %d", seekTarget, pos)
	}
}
