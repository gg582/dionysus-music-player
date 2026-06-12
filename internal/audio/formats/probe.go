package formats

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/gg582/gozik/internal/audio/ffmpeg"
	"github.com/gg582/gozik/internal/audio/pcm"
	"github.com/gopxl/beep"
	"github.com/gopxl/beep/flac"
	"github.com/gopxl/beep/mp3"
	"github.com/gopxl/beep/vorbis"
	"github.com/gopxl/beep/wav"
)

const rawPCMSampleRate = 44100

// ProbeDuration decodes a file just far enough to read its total length, then
// closes it. Used to compute the queue's total play time without playback.
func ProbeDuration(filename string) (time.Duration, error) {
	if ffmpeg.IsStreamURL(filename) {
		return 0, nil
	}
	f, err := os.Open(filename)
	if err != nil {
		return 0, err
	}
	ext := strings.ToLower(filename[strings.LastIndex(filename, ".")+1:])
	var streamer beep.StreamSeekCloser
	var format beep.Format
	switch ext {
	case "mp3":
		streamer, format, err = mp3.Decode(f)
	case "flac":
		streamer, format, err = flac.Decode(f)
	case "wav":
		streamer, format, err = wav.Decode(f)
	case "ogg":
		streamer, format, err = vorbis.Decode(f)
	case "opus":
		streamer, format, err = DecodeOpus(f)
	case "aac":
		streamer, format, err = DecodeADTS(f)
	case "aiff", "aif":
		streamer, format, err = DecodeAIFF(f)
	case "pcm", "raw":
		pcmFormat, detectErr := pcm.DetectPcmFormat(filename)
		if detectErr != nil {
			f.Close()
			return 0, detectErr
		}
		info, statErr := f.Stat()
		if statErr != nil {
			f.Close()
			return 0, statErr
		}
		frameSize, frameErr := pcm.RawPCMFrameSize(pcmFormat)
		if frameErr != nil {
			f.Close()
			return 0, frameErr
		}
		f.Close()
		return time.Second * time.Duration(info.Size()/int64(frameSize)) / rawPCMSampleRate, nil
	default:
		if _, ok := GaudioFormatForExt(ext); ok {
			f.Close()
			streamer, format, err = DecodeGaudioExt(filename, ext)
			break
		}
		f.Close()
		return 0, fmt.Errorf("unsupported format: %s", ext)
	}
	if err != nil {
		f.Close()
		return 0, err
	}
	defer streamer.Close()

	n := streamer.Len()
	if n <= 0 || format.SampleRate == 0 {
		return 0, nil
	}
	return time.Second * time.Duration(n) / time.Duration(format.SampleRate), nil
}
