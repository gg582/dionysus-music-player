package cdrom

import (
	"encoding/binary"
	"errors"
	"math"

	"github.com/gg582/gozik/internal/models"
)

// WaveformForTrack reads a sparse set of audio sectors across the track and
// builds a 200-point RMS waveform. It opens its own device handle so it can
// run independently of the active playback streamer.
func WaveformForTrack(device string, track Track) (*models.Waveform, error) {
	if !track.IsAudio {
		return nil, errors.New("cannot generate waveform for data track")
	}
	if track.StartLBA >= track.EndLBA {
		return nil, errors.New("invalid track boundaries")
	}

	dev, err := Open(device)
	if err != nil {
		return nil, err
	}
	defer dev.Close()

	// Verify the track is still present in the current TOC.
	tracks, err := dev.ReadTOC()
	if err != nil {
		return nil, err
	}
	var current *Track
	for i := range tracks {
		if tracks[i].Number == track.Number {
			current = &tracks[i]
			break
		}
	}
	if current == nil || !current.IsAudio {
		return nil, errors.New("track no longer available")
	}

	trackLen := current.EndLBA - current.StartLBA
	if trackLen <= 0 {
		return nil, errors.New("invalid track length")
	}

	var wf models.Waveform
	points := len(wf.Data)

	for i := 0; i < points; i++ {
		// Pick one sector per bar, evenly distributed across the track.
		lba := current.StartLBA + (i * trackLen / points)
		if lba >= current.EndLBA {
			lba = current.EndLBA - 1
		}

		buf, err := dev.ReadAudioFrames(lba, 1)
		if err != nil {
			return nil, err
		}

		var sumSquares float64
		var count int
		for pos := 0; pos+3 < len(buf); pos += 4 {
			left := float64(int16(binary.LittleEndian.Uint16(buf[pos:]))) / 32768.0
			right := float64(int16(binary.LittleEndian.Uint16(buf[pos+2:]))) / 32768.0
			sumSquares += left*left + right*right
			count += 2
		}

		if count > 0 {
			rms := math.Sqrt(sumSquares / float64(count))
			amp := rms * 1024.0 // match ffmpeg prescan heuristic gain
			if amp > 255.0 {
				amp = 255.0
			}
			wf.Data[i] = uint8(amp)
		}
	}

	return &wf, nil
}
