package audio

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gg582/gozik/internal/models"
	"github.com/Lotlab/cue-go"
	"github.com/gopxl/beep"
)

// ParseCUE reads a cue sheet and returns a slice of Song entries, one per
// audio track.  The audio-file paths are resolved relative to the cue file.
func ParseCUE(cuePath string) ([]models.Song, error) {
	f, err := os.Open(cuePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	sheet, err := cue.Parse(f)
	if err != nil {
		return nil, fmt.Errorf("cue parse failed: %w", err)
	}

	baseDir := filepath.Dir(cuePath)
	var songs []models.Song

	for _, file := range sheet.Files {
		if len(file.Tracks) == 0 {
			continue
		}

		audioPath := resolveAudioPath(baseDir, file.Name)
		// Total file length is needed for the last track's end boundary.
		var fileLen int
		if d, err := ProbeDuration(audioPath); err == nil {
			fileLen = int(d.Seconds())
		}

		for ti, track := range file.Tracks {
			if track.DataType != cue.DataTypeAudio {
				continue
			}

			start := int(track.StartPosition)
			end := int(track.EndPosition)
			if end == 0 {
				if ti+1 < len(file.Tracks) {
					end = int(file.Tracks[ti+1].StartPosition)
				} else {
					end = fileLen
				}
			}
			dur := end - start
			if dur < 0 {
				dur = 0
			}

			title := strings.TrimSpace(track.Title)
			if title == "" {
				title = fmt.Sprintf("Track %02d", track.Number)
			}

			songs = append(songs, models.Song{
				Name:        title,
				Artist:      strings.TrimSpace(track.Performer),
				Location:    audioPath,
				Duration:    dur,
				StartOffset: start,
				EndOffset:   end,
			})
		}
	}

	return songs, nil
}

func resolveAudioPath(baseDir, name string) string {
	name = strings.Trim(name, `"`)
	if filepath.IsAbs(name) {
		return name
	}
	return filepath.Join(baseDir, name)
}

// SegmentStreamer wraps a beep.StreamSeekCloser and stops streaming when the
// playback position reaches a configured end boundary.
type SegmentStreamer struct {
	beep.StreamSeekCloser
	format beep.Format
	start  int
	end    int // frames; 0 = unlimited
}

// NewSegmentStreamer creates a streamer that plays only the [start,end) slice
// of the underlying streamer.  start/end are in seconds.
func NewSegmentStreamer(s beep.StreamSeekCloser, format beep.Format, startSec, endSec int) (*SegmentStreamer, error) {
	startFrame := 0
	if startSec > 0 && format.SampleRate > 0 {
		startFrame = int(time.Duration(startSec) * time.Duration(format.SampleRate) / time.Second)
	}
	endFrame := 0
	if endSec > 0 && format.SampleRate > 0 {
		endFrame = int(time.Duration(endSec) * time.Duration(format.SampleRate) / time.Second)
	}
	if startFrame > 0 {
		if err := s.Seek(startFrame); err != nil {
			return nil, err
		}
	}
	return &SegmentStreamer{
		StreamSeekCloser: s,
		format:           format,
		start:            startFrame,
		end:              endFrame,
	}, nil
}

func (ss *SegmentStreamer) Stream(samples [][2]float64) (int, bool) {
	end := ss.end
	if end > 0 {
		pos := ss.Position()
		remaining := end - pos
		if remaining <= 0 {
			return 0, false
		}
		if len(samples) > remaining {
			samples = samples[:remaining]
		}
	}
	return ss.StreamSeekCloser.Stream(samples)
}

func (ss *SegmentStreamer) Len() int {
	l := ss.StreamSeekCloser.Len()
	if ss.end > 0 && ss.end < l {
		return ss.end
	}
	return l
}

func (ss *SegmentStreamer) Position() int {
	return ss.StreamSeekCloser.Position()
}

func (ss *SegmentStreamer) Seek(p int) error {
	if ss.end > 0 && p > ss.end {
		p = ss.end
	}
	return ss.StreamSeekCloser.Seek(p)
}
