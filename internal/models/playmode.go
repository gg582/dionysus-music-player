package models

// PlaybackState represents the current transport state.
type PlaybackState int

const (
	PlaybackStateStopped PlaybackState = iota
	PlaybackStatePlaying
	PlaybackStatePaused
)

// PlayMode controls what happens when a track finishes.
type PlayMode int

const (
	PlayModeSequential PlayMode = iota // play next track, stop at end
	PlayModeRepeatAll                  // repeat the entire queue
	PlayModeRepeatOne                  // repeat the current track
	PlayModeSingle                     // play the current track, then stop
)

func (pm PlayMode) String() string {
	switch pm {
	case PlayModeSequential:
		return "Sequential"
	case PlayModeRepeatAll:
		return "Repeat All"
	case PlayModeRepeatOne:
		return "Repeat One"
	case PlayModeSingle:
		return "Single"
	}
	return "Unknown"
}

func (pm PlayMode) Next() PlayMode {
	switch pm {
	case PlayModeSequential:
		return PlayModeRepeatAll
	case PlayModeRepeatAll:
		return PlayModeRepeatOne
	case PlayModeRepeatOne:
		return PlayModeSingle
	case PlayModeSingle:
		return PlayModeSequential
	}
	return PlayModeSequential
}
