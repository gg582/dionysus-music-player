package audio

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"github.com/gopxl/beep"
	"github.com/pion/opus"
	"github.com/pion/opus/pkg/oggreader"
)

const opusSampleRate = 48000
const opusChannels = 2
const opusMaxFrameSamples = 5760

func decodeOpus(rsc io.ReadSeekCloser) (beep.StreamSeekCloser, beep.Format, error) {
	ogg, _, err := oggreader.NewWith(rsc)
	if err != nil {
		rsc.Close()
		return nil, beep.Format{}, fmt.Errorf("opus: %w", err)
	}

	decoder, err := opus.NewDecoderWithOutput(opusSampleRate, opusChannels)
	if err != nil {
		rsc.Close()
		return nil, beep.Format{}, fmt.Errorf("opus: %w", err)
	}

	var decoded []float32
	pcm := make([]float32, opusMaxFrameSamples*opusChannels)
	for {
		packet, _, err := ogg.ParseNextPacket()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			rsc.Close()
			return nil, beep.Format{}, fmt.Errorf("opus: %w", err)
		}
		if bytes.HasPrefix(packet, []byte("OpusTags")) {
			continue
		}

		n, err := decoder.DecodeToFloat32(packet, pcm)
		if err != nil {
			rsc.Close()
			return nil, beep.Format{}, fmt.Errorf("opus: %w", err)
		}
		decoded = append(decoded, pcm[:n*opusChannels]...)
	}

	return newFloat32Streamer(decoded, opusChannels, opusSampleRate, rsc.Close)
}
