package audio

import (
	"fmt"
	"io"

	"github.com/gopxl/beep"
	"github.com/kazzmir/opus"
	"github.com/pkg/errors"
)

// decodeOpus takes a ReadSeekCloser containing Opus audio data and returns a
// beep StreamSeekCloser.  The caller must not close the supplied reader; use
// the returned StreamSeekCloser's Close method instead.
func decodeOpus(rsc io.ReadSeekCloser) (beep.StreamSeekCloser, beep.Format, error) {
	d, err := opus.NewDecoder(rsc)
	if err != nil {
		return nil, beep.Format{}, fmt.Errorf("opus: %w", err)
	}
	if d.ChannelCount() > 2 {
		return nil, beep.Format{}, fmt.Errorf("opus: unsupported number of channels, %d", d.ChannelCount())
	}

	return &opusDecoder{
		rsc:     rsc,
		decoder: d,
	}, beep.Format{
		SampleRate:  beep.SampleRate(opus.SampleRate),
		NumChannels: d.ChannelCount(),
		Precision:   2,
	}, nil
}

type opusDecoder struct {
	rsc     io.ReadSeekCloser
	decoder *opus.Decoder
	err     error
}

func (d *opusDecoder) Stream(samples [][2]float64) (int, bool) {
	if d.err != nil {
		return 0, false
	}

	tmp := make([]float32, len(samples)*d.decoder.ChannelCount())
	n, err := d.decoder.ReadFloat(tmp)
	if err != nil {
		if err == io.EOF {
			return n, n > 0
		}
		d.err = errors.Wrap(err, "opus")
		return n, false
	}

	if d.decoder.ChannelCount() == 1 {
		for i := 0; i < n; i++ {
			samples[i][0] = float64(tmp[i])
			samples[i][1] = float64(tmp[i])
		}
	} else {
		for i := 0; i < n; i++ {
			samples[i][0] = float64(tmp[i*2])
			samples[i][1] = float64(tmp[i*2+1])
		}
	}

	return n, true
}

func (d *opusDecoder) Err() error {
	return d.err
}

func (d *opusDecoder) Len() int {
	return int(d.decoder.Len())
}

func (d *opusDecoder) Position() int {
	pos, err := d.decoder.Position()
	if err != nil {
		d.err = errors.Wrap(err, "opus")
	}
	return int(pos)
}

func (d *opusDecoder) Seek(p int) error {
	if p < 0 || d.Len() < p {
		return fmt.Errorf("opus: seek position %v out of range [%v, %v]", p, 0, d.Len())
	}
	if err := d.decoder.Seek(int64(p)); err != nil {
		return errors.Wrap(err, "opus")
	}
	return nil
}

func (d *opusDecoder) Close() error {
	d.decoder.Destroy()
	return d.rsc.Close()
}
