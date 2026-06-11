package models

import "time"

// Waveform holds 200 compressed RMS amplitude values (0-255).
type Waveform struct {
	Data [200]uint8
}

// Chapter represents a single media chapter.
type Chapter struct {
	ID    int64
	Title string
	Start time.Duration
	End   time.Duration
}

// Song represents a single music file entry, CD track, or provider stream.
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
	Duration    int // seconds
	Lyrics      string
	CoverArtURL string
	CoverData   []byte
	// CUE segment playback (seconds; EndOffset=0 means until file end)
	StartOffset int
	EndOffset   int
	// Prescan results populated asynchronously by the audio engine.
	Waveform *Waveform
	Chapters []Chapter
	// Provider fields (for tracks sourced from a remote provider)
	ProviderID      string
	ProviderAddress string
	ProviderTrackID string
	ProviderName    string
	StreamURL       string // resolved ephemeral URL
	StreamHeaders   map[string]string
}
