package cdrom

import (
	"fmt"
	"strings"
)

const (
	CD_FRAMES     = 75
	CD_SECS       = 60
	CD_FRAME_SIZE = 2352
	CD_SAMPLES    = 588 // 2352 / 4
)

// Track represents a single CD track.
type Track struct {
	Number   int
	StartLBA int
	EndLBA   int
	Length   int // in frames
	IsAudio  bool
}

// MusicBrainzTOC builds a MusicBrainz TOC string from raw TOC tracks.
// Format: firstTrack+lastTrack+leadoutOffset+track1Offset+track2Offset+...
func MusicBrainzTOC(tracks []Track) string {
	if len(tracks) == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d+%d+%d", tracks[0].Number, tracks[len(tracks)-1].Number, tracks[len(tracks)-1].EndLBA)
	for _, t := range tracks {
		fmt.Fprintf(&b, "+%d", t.StartLBA)
	}
	return b.String()
}
