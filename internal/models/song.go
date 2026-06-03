package models

import "time"

// Song represents a single music file entry or CD track.
type Song struct {
	Name        string
	Location    string
	Device      string
	TrackNum    int
	IsCD        bool
	Artist      string
	Title       string
	Album       string
	AlbumYear   string
	Lyrics      string
	CoverArtURL string
	CoverData   []byte
	Duration    time.Duration // total length; 0 until probed (or unknown for CD)
}
