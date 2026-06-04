package models

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
	Duration    int // seconds
	Lyrics      string
	CoverArtURL string
	CoverData   []byte
	// CUE segment playback (seconds; EndOffset=0 means until file end)
	StartOffset int
	EndOffset   int
}
