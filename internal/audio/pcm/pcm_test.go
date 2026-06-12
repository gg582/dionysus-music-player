package pcm

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestDecodePCMValidLength(t *testing.T) {
	data := []byte{0x00, 0x40, 0x00, 0xc0} // 4 bytes = 1 frame
	s, format, err := DecodePCM(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("DecodePCM() error = %v", err)
	}
	defer s.Close()

	if format.SampleRate != 44100 || format.NumChannels != 2 || format.Precision != 2 {
		t.Fatalf("format = %+v", format)
	}
	if s.Len() != 1 {
		t.Fatalf("Len() = %d, want 1", s.Len())
	}
}

func TestDecodePCMInvalidLength(t *testing.T) {
	data := []byte{0x00, 0x40, 0x00} // 3 bytes, not multiple of 4
	_, _, err := DecodePCM(bytes.NewReader(data))
	if err == nil {
		t.Fatal("expected error for raw pcm data length not multiple of 4")
	}
}

func TestValidateRawPCMSize(t *testing.T) {
	validPath := filepath.Join(t.TempDir(), "valid.raw")
	invalidPath := filepath.Join(t.TempDir(), "invalid.raw")
	otherPath := filepath.Join(t.TempDir(), "other.mp3")

	if err := os.WriteFile(validPath, make([]byte, 8), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(invalidPath, make([]byte, 3), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(otherPath, []byte{0}, 0644); err != nil {
		t.Fatal(err)
	}

	if err := ValidateRawPCMSize(validPath); err != nil {
		t.Fatalf("valid raw pcm: %v", err)
	}
	if err := ValidateRawPCMSize(invalidPath); err == nil {
		t.Fatal("invalid raw pcm: expected error")
	}
	if err := ValidateRawPCMSize(otherPath); err != nil {
		t.Fatalf("non-raw file: %v", err)
	}
}

func TestParseDurationFromFilename(t *testing.T) {
	tests := []struct {
		name string
		want float64
	}{
		{"track_[03m45s].pcm", 225},
		{"audio_[225s].raw", 225},
	}

	for _, tt := range tests {
		got, ok := ParseDuration(tt.name)
		if !ok {
			t.Fatalf("ParseDuration(%q) did not find duration", tt.name)
		}
		if got != tt.want {
			t.Fatalf("ParseDuration(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestParseDurationFromCueFallback(t *testing.T) {
	dir := t.TempDir()
	rawPath := filepath.Join(dir, "album.raw")
	cuePath := filepath.Join(dir, "album.cue")
	cueData := `FILE "album.raw" BINARY
  TRACK 01 AUDIO
    INDEX 01 00:00:00
  TRACK 02 AUDIO
    INDEX 01 03:45:00
`

	if err := os.WriteFile(rawPath, nil, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cuePath, []byte(cueData), 0644); err != nil {
		t.Fatal(err)
	}

	got, ok := ParseDuration(rawPath)
	if !ok {
		t.Fatal("ParseDuration() did not find cue duration")
	}
	if got != 225 {
		t.Fatalf("ParseDuration() = %v, want 225", got)
	}
}

func TestDetectPcmFormat(t *testing.T) {
	tests := []struct {
		name string
		size int
		want string
	}{
		{"tone_[1s].pcm", 44100 * 2 * 2, "s16le"},
		{"tone_[1s].raw", 44100 * 2 * 3, "s24le"},
		{"tone_[1s].pcm", 44100 * 2 * 4, "s32le"},
	}

	for _, tt := range tests {
		path := filepath.Join(t.TempDir(), tt.name)
		if err := os.WriteFile(path, make([]byte, tt.size), 0644); err != nil {
			t.Fatal(err)
		}
		got, err := DetectPcmFormat(path)
		if err != nil {
			t.Fatalf("DetectPcmFormat(%q) error = %v", tt.name, err)
		}
		if got != tt.want {
			t.Fatalf("DetectPcmFormat(%q) = %q, want %q", tt.name, got, tt.want)
		}
	}
}
