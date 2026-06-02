package audio

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/bogem/id3v2/v2"
	"github.com/dhowden/tag"
)

// Metadata holds extracted tag information from an audio file.
type Metadata struct {
	Title   string
	Artist  string
	Album   string
	Lyrics  string
	Picture []byte
}

// ExtractMetadata reads metadata (including lyrics and cover art) from the given audio file.
func ExtractMetadata(path string) (*Metadata, error) {
	ext := strings.ToLower(filepath.Ext(path))

	// Use id3v2 for MP3 as it handles USLT and APIC more reliably
	if ext == ".mp3" {
		return extractID3v2(path)
	}

	return extractWithTagLib(path)
}

func extractWithTagLib(path string) (*Metadata, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	m, err := tag.ReadFrom(f)
	if err != nil {
		return nil, err
	}

	meta := &Metadata{
		Title:  m.Title(),
		Artist: m.Artist(),
		Album:  m.Album(),
		Lyrics: m.Lyrics(),
	}
	if pic := m.Picture(); pic != nil {
		meta.Picture = pic.Data
	}
	return meta, nil
}

func extractID3v2(path string) (*Metadata, error) {
	tag, err := id3v2.Open(path, id3v2.Options{Parse: true})
	if err != nil {
		return nil, err
	}
	defer tag.Close()

	meta := &Metadata{
		Title:  tag.Title(),
		Artist: tag.Artist(),
		Album:  tag.Album(),
	}

	// Read USLT frames (try standard first, then TXXX fallback for ffmpeg)
	usltFrames := tag.GetFrames(tag.CommonID("Unsynchronised lyrics/text transcription"))
	for _, f := range usltFrames {
		if uslt, ok := f.(id3v2.UnsynchronisedLyricsFrame); ok {
			if meta.Lyrics == "" {
				meta.Lyrics = uslt.Lyrics
			}
		}
	}
	if meta.Lyrics == "" {
		txxxFrames := tag.GetFrames(tag.CommonID("User defined text information frame"))
		for _, f := range txxxFrames {
			if txxx, ok := f.(id3v2.UserDefinedTextFrame); ok {
				if txxx.Description == "USLT" || txxx.Description == "Lyrics" {
					meta.Lyrics = txxx.Value
					break
				}
			}
		}
	}

	// Read APIC (cover art)
	apicFrames := tag.GetFrames(tag.CommonID("Attached picture"))
	for _, f := range apicFrames {
		if apic, ok := f.(id3v2.PictureFrame); ok {
			meta.Picture = apic.Picture
			break
		}
	}

	return meta, nil
}
