package audio

import (
	"os"

	"github.com/dhowden/tag"
)

// Metadata holds extracted tag information from an audio file.
type Metadata struct {
	Title  string
	Artist string
	Album  string
	Lyrics string
}

// ExtractMetadata reads metadata (including lyrics) from the given audio file.
func ExtractMetadata(path string) (*Metadata, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	m, err := tag.ReadFrom(f)
	if err != nil {
		return nil, err
	}

	return &Metadata{
		Title:  m.Title(),
		Artist: m.Artist(),
		Album:  m.Album(),
		Lyrics: m.Lyrics(),
	}, nil
}
