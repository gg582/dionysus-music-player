package audio

import (
	"fmt"

	"github.com/gopxl/beep"
	"github.com/zeozeozeo/gaudio"
)

func decodeGaudio(path string, format gaudio.AudioFormat) (beep.StreamSeekCloser, beep.Format, error) {
	segment, err := gaudio.LoadAudioFromPath(path, format)
	if err != nil {
		return nil, beep.Format{}, err
	}
	return newFloat32Streamer(segment.Data, segment.Channels, int(segment.SampleRate), nil)
}

func gaudioFormatForExt(ext string) (gaudio.AudioFormat, bool) {
	switch ext {
	case "mod":
		return gaudio.FormatMOD, true
	case "s3m":
		return gaudio.FormatS3M, true
	case "xm":
		return gaudio.FormatXM, true
	case "it":
		return gaudio.FormatIT, true
	default:
		return 0, false
	}
}

func decodeGaudioExt(path, ext string) (beep.StreamSeekCloser, beep.Format, error) {
	format, ok := gaudioFormatForExt(ext)
	if !ok {
		return nil, beep.Format{}, fmt.Errorf("unsupported gaudio format: %s", ext)
	}
	return decodeGaudio(path, format)
}
