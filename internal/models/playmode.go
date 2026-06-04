package models

// PlayMode controls what happens when a track finishes.
type PlayMode int

const (
	PlayModeSequential PlayMode = iota // play next track, stop at end
	PlayModeRepeatAll                  // repeat the entire queue
	PlayModeRepeatOne                  // repeat the current track
)

func (pm PlayMode) String() string {
	switch pm {
	case PlayModeSequential:
		return "Sequential"
	case PlayModeRepeatAll:
		return "Repeat All"
	case PlayModeRepeatOne:
		return "Repeat One"
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
		return PlayModeSequential
	}
	return PlayModeSequential
}
