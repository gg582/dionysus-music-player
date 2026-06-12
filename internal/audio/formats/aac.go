package formats

import (
	"fmt"
	"io"

	"github.com/gg582/gozik/internal/audio/utils"
	"github.com/gopxl/beep"
	"github.com/skrashevich/go-aac/pkg/adts"
	"github.com/skrashevich/go-aac/pkg/decoder"
)

func DecodeADTS(rsc io.ReadSeekCloser) (beep.StreamSeekCloser, beep.Format, error) {
	data, err := io.ReadAll(rsc)
	if err != nil {
		rsc.Close()
		return nil, beep.Format{}, err
	}
	if !adts.Probe(data) {
		rsc.Close()
		return nil, beep.Format{}, fmt.Errorf("aac: ADTS stream not detected")
	}

	dec := decoder.New()
	var decoded []float32
	offset := 0
	for offset < len(data) {
		if len(data)-offset < 7 {
			break
		}

		hdr, err := adts.ReadHeaderFromBytes(data[offset:])
		if err != nil {
			rsc.Close()
			return nil, beep.Format{}, fmt.Errorf("aac: %w", err)
		}
		if hdr.FrameLength < 7 || offset+hdr.FrameLength > len(data) {
			rsc.Close()
			return nil, beep.Format{}, fmt.Errorf("aac: invalid ADTS frame length %d", hdr.FrameLength)
		}

		samples, err := dec.DecodeFrame(data[offset : offset+hdr.FrameLength])
		if err != nil {
			rsc.Close()
			return nil, beep.Format{}, fmt.Errorf("aac: %w", err)
		}
		decoded = append(decoded, samples...)
		offset += hdr.FrameLength
	}

	return utils.NewFloat32Streamer(decoded, aacChannelCount(dec.Config.ChanConfig), dec.Config.SampleRate, rsc.Close)
}

func aacChannelCount(config int) int {
	switch config {
	case 1:
		return 1
	case 2:
		return 2
	default:
		return 2
	}
}
