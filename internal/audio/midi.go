package audio

import (
	"bytes"
	"embed"
	"fmt"
	"io"
	"os"

	"github.com/gopxl/beep/v2"
	"github.com/gopxl/beep/v2/midi"
)

var _ embed.FS

//go:embed assets/soundfont.sf2
var defaultSoundFont []byte

// midiCloser wraps beep.StreamSeeker into beep.StreamSeekCloser.
type midiCloser struct {
	beep.StreamSeeker
}

func (midiCloser) Close() error { return nil }

// decodeMIDI synthesizes a MIDI file into a seekable PCM stream using the
// embedded General MIDI SoundFont. The returned format is always 48 kHz stereo float32.
func decodeMIDI(path string) (beep.StreamSeekCloser, beep.Format, error) {
	if len(defaultSoundFont) == 0 {
		return nil, beep.Format{}, fmt.Errorf("no embedded SoundFont; place a .sf2 file at internal/audio/assets/soundfont.sf2")
	}

	sf, err := midi.NewSoundFont(io.NopCloser(bytes.NewReader(defaultSoundFont)))
	if err != nil {
		return nil, beep.Format{}, fmt.Errorf("load soundfont: %w", err)
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, beep.Format{}, err
	}

	s, format, err := midi.Decode(f, sf, beep.SampleRate(engineSampleRate))
	if err != nil {
		return nil, beep.Format{}, fmt.Errorf("midi decode: %w", err)
	}

	return midiCloser{s}, format, nil
}
