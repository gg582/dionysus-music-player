package cdrom

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
