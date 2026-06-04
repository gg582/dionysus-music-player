package audio

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestDecodePCMValidLength(t *testing.T) {
	data := []byte{0x00, 0x40, 0x00, 0xc0} // 4 bytes = 1 frame
	s, format, err := decodePCM(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decodePCM() error = %v", err)
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
	_, _, err := decodePCM(bytes.NewReader(data))
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

	if err := validateRawPCMSize(validPath); err != nil {
		t.Fatalf("valid raw pcm: %v", err)
	}
	if err := validateRawPCMSize(invalidPath); err == nil {
		t.Fatal("invalid raw pcm: expected error")
	}
	if err := validateRawPCMSize(otherPath); err != nil {
		t.Fatalf("non-raw file: %v", err)
	}
}
