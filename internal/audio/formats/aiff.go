package formats

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"

	"github.com/gopxl/beep"
)

type aiffStreamer struct {
	r           io.ReadSeeker
	rc          io.Closer
	err         error
	dataStart   int64
	pos         int
	numFrames   int
	numChannels int
	bitDepth    int
	sampleBytes int
	frameBytes  int
	little      bool
	encoding    aiffSampleEncoding
	frameBuf    []byte
}

type aiffSampleEncoding int

const (
	aiffEncodingSignedPCM aiffSampleEncoding = iota
	aiffEncodingUnsignedPCM
	aiffEncodingFloatPCM
	aiffEncodingULaw
	aiffEncodingALaw
)

type aiffInfo struct {
	formType    [4]byte
	numChannels int
	numFrames   int
	bitDepth    int
	sampleRate  int
	compression [4]byte
	dataStart   int64
	dataBytes   int64
}

func DecodeAIFF(r io.ReadSeeker) (beep.StreamSeekCloser, beep.Format, error) {
	info, err := parseAIFF(r)
	if err != nil {
		return nil, beep.Format{}, err
	}

	sampleBytes := (info.bitDepth + 7) / 8
	frameBytes := sampleBytes * info.numChannels
	availableFrames := int(info.dataBytes / int64(frameBytes))
	if info.numFrames <= 0 || info.numFrames > availableFrames {
		info.numFrames = availableFrames
	}
	if info.numFrames <= 0 {
		return nil, beep.Format{}, fmt.Errorf("aiff contains no audio frames")
	}

	encoding, little, err := classifyAIFFEncoding(info)
	if err != nil {
		return nil, beep.Format{}, err
	}
	if _, err := r.Seek(info.dataStart, io.SeekStart); err != nil {
		return nil, beep.Format{}, err
	}

	format := beep.Format{
		SampleRate:  beep.SampleRate(info.sampleRate),
		NumChannels: info.numChannels,
		Precision:   sampleBytes,
	}

	streamer := &aiffStreamer{
		r:           r,
		dataStart:   info.dataStart,
		numFrames:   info.numFrames,
		numChannels: info.numChannels,
		bitDepth:    info.bitDepth,
		sampleBytes: sampleBytes,
		frameBytes:  frameBytes,
		little:      little,
		encoding:    encoding,
		frameBuf:    make([]byte, frameBytes),
	}
	if c, ok := r.(io.Closer); ok {
		streamer.rc = c
	}

	return streamer, format, nil
}

func parseAIFF(r io.ReadSeeker) (*aiffInfo, error) {
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}

	var header [12]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, err
	}
	if string(header[0:4]) != "FORM" {
		return nil, fmt.Errorf("invalid aiff file: missing FORM header")
	}

	info := &aiffInfo{}
	copy(info.formType[:], header[8:12])
	if info.formType != [4]byte{'A', 'I', 'F', 'F'} && info.formType != [4]byte{'A', 'I', 'F', 'C'} {
		return nil, fmt.Errorf("unsupported aiff form type: %q", string(info.formType[:]))
	}
	if info.formType == [4]byte{'A', 'I', 'F', 'F'} {
		info.compression = [4]byte{'N', 'O', 'N', 'E'}
	}

	formSize := int64(binary.BigEndian.Uint32(header[4:8]))
	formEnd := int64(8) + formSize
	if formSize < 4 {
		return nil, fmt.Errorf("invalid aiff FORM size")
	}

	for {
		pos, err := r.Seek(0, io.SeekCurrent)
		if err != nil {
			return nil, err
		}
		if pos+8 > formEnd {
			break
		}

		var chunkHeader [8]byte
		if _, err := io.ReadFull(r, chunkHeader[:]); err != nil {
			return nil, err
		}
		chunkID := string(chunkHeader[0:4])
		chunkSize := int64(binary.BigEndian.Uint32(chunkHeader[4:8]))
		dataStart := pos + 8
		next := dataStart + chunkSize
		if chunkSize%2 != 0 {
			next++
		}
		if next > formEnd+1 {
			return nil, fmt.Errorf("invalid aiff chunk %q size", chunkID)
		}

		switch chunkID {
		case "COMM":
			if err := parseAIFFComm(r, info, chunkSize); err != nil {
				return nil, err
			}
		case "SSND":
			if err := parseAIFFSoundData(r, info, chunkSize, dataStart); err != nil {
				return nil, err
			}
		}

		if _, err := r.Seek(next, io.SeekStart); err != nil {
			return nil, err
		}
	}

	if info.numChannels == 0 || info.bitDepth == 0 || info.sampleRate == 0 {
		return nil, fmt.Errorf("invalid aiff file: missing COMM chunk")
	}
	if info.dataStart == 0 || info.dataBytes == 0 {
		return nil, fmt.Errorf("invalid aiff file: missing SSND chunk")
	}
	if info.numChannels < 1 {
		return nil, fmt.Errorf("invalid aiff channel count: %d", info.numChannels)
	}
	if _, _, err := classifyAIFFEncoding(info); err != nil {
		return nil, err
	}

	return info, nil
}

func classifyAIFFEncoding(info *aiffInfo) (aiffSampleEncoding, bool, error) {
	if info.formType == [4]byte{'A', 'I', 'F', 'F'} {
		if err := validateAIFFPCMBitDepth(info.bitDepth); err != nil {
			return 0, false, err
		}
		return aiffEncodingSignedPCM, false, nil
	}

	switch info.compression {
	case [4]byte{'N', 'O', 'N', 'E'}, [4]byte{'t', 'w', 'o', 's'}, [4]byte{'i', 'n', '2', '4'}, [4]byte{'i', 'n', '3', '2'}:
		if err := validateAIFFPCMBitDepth(info.bitDepth); err != nil {
			return 0, false, err
		}
		return aiffEncodingSignedPCM, false, nil
	case [4]byte{'s', 'o', 'w', 't'}, [4]byte{'4', '2', 'n', 'i'}, [4]byte{'4', '2', 'n', '1'}, [4]byte{'2', '3', 'n', 'i'}:
		if err := validateAIFFPCMBitDepth(info.bitDepth); err != nil {
			return 0, false, err
		}
		return aiffEncodingSignedPCM, true, nil
	case [4]byte{'r', 'a', 'w', ' '}:
		if info.bitDepth != 8 {
			return 0, false, fmt.Errorf("unsupported aifc raw bit depth: %d", info.bitDepth)
		}
		return aiffEncodingUnsignedPCM, false, nil
	case [4]byte{'f', 'l', '3', '2'}, [4]byte{'F', 'L', '3', '2'}:
		if info.bitDepth != 32 {
			return 0, false, fmt.Errorf("unsupported aifc fl32 bit depth: %d", info.bitDepth)
		}
		return aiffEncodingFloatPCM, false, nil
	case [4]byte{'f', 'l', '6', '4'}, [4]byte{'F', 'L', '6', '4'}:
		if info.bitDepth != 64 {
			return 0, false, fmt.Errorf("unsupported aifc fl64 bit depth: %d", info.bitDepth)
		}
		return aiffEncodingFloatPCM, false, nil
	case [4]byte{'u', 'l', 'a', 'w'}, [4]byte{'U', 'L', 'A', 'W'}:
		if info.bitDepth != 8 {
			return 0, false, fmt.Errorf("unsupported aifc ulaw bit depth: %d", info.bitDepth)
		}
		return aiffEncodingULaw, false, nil
	case [4]byte{'a', 'l', 'a', 'w'}, [4]byte{'A', 'L', 'A', 'W'}:
		if info.bitDepth != 8 {
			return 0, false, fmt.Errorf("unsupported aifc alaw bit depth: %d", info.bitDepth)
		}
		return aiffEncodingALaw, false, nil
	default:
		return 0, false, fmt.Errorf("unsupported aifc compression: %q", string(info.compression[:]))
	}
}

func validateAIFFPCMBitDepth(bitDepth int) error {
	switch bitDepth {
	case 8, 16, 24, 32:
		return nil
	default:
		return fmt.Errorf("unsupported aiff pcm bit depth: %d", bitDepth)
	}
}

func parseAIFFComm(r io.Reader, info *aiffInfo, chunkSize int64) error {
	minSize := int64(18)
	if info.formType == [4]byte{'A', 'I', 'F', 'C'} {
		minSize = 22
	}
	if chunkSize < minSize {
		return fmt.Errorf("invalid AIFF COMM chunk size")
	}

	var comm [22]byte
	if _, err := io.ReadFull(r, comm[:minSize]); err != nil {
		return err
	}

	info.numChannels = int(binary.BigEndian.Uint16(comm[0:2]))
	info.numFrames = int(binary.BigEndian.Uint32(comm[2:6]))
	info.bitDepth = int(binary.BigEndian.Uint16(comm[6:8]))
	info.sampleRate = parseAIFFExtendedSampleRate(comm[8:18])
	if info.formType == [4]byte{'A', 'I', 'F', 'C'} {
		copy(info.compression[:], comm[18:22])
	}

	return nil
}

func parseAIFFSoundData(r io.Reader, info *aiffInfo, chunkSize int64, chunkDataStart int64) error {
	if chunkSize < 8 {
		return fmt.Errorf("invalid AIFF SSND chunk size")
	}

	var ssnd [8]byte
	if _, err := io.ReadFull(r, ssnd[:]); err != nil {
		return err
	}

	offset := int64(binary.BigEndian.Uint32(ssnd[0:4]))
	if offset > chunkSize-8 {
		return fmt.Errorf("invalid AIFF SSND offset")
	}
	info.dataStart = chunkDataStart + 8 + offset
	info.dataBytes = chunkSize - 8 - offset
	return nil
}

func parseAIFFExtendedSampleRate(b []byte) int {
	if len(b) != 10 {
		return 0
	}

	signExp := binary.BigEndian.Uint16(b[0:2])
	if signExp&0x7fff == 0 {
		return 0
	}

	sign := 1.0
	if signExp&0x8000 != 0 {
		sign = -1.0
	}
	exponent := int(signExp&0x7fff) - 16383
	mantissa := binary.BigEndian.Uint64(b[2:10])
	value := sign * math.Ldexp(float64(mantissa), exponent-63)
	if value <= 0 || value > float64(math.MaxInt32) {
		return 0
	}
	return int(math.Round(value))
}

func (s *aiffStreamer) Stream(samples [][2]float64) (n int, ok bool) {
	if s.pos >= s.numFrames {
		return 0, false
	}

	for i := range samples {
		if s.pos >= s.numFrames {
			return i, i > 0
		}
		if _, err := io.ReadFull(s.r, s.frameBuf); err != nil {
			if err != io.EOF && err != io.ErrUnexpectedEOF {
				s.err = err
				return 0, false
			}
			return i, i > 0
		}

		left := s.decodeSample(s.frameBuf[0:s.sampleBytes])
		right := left
		if s.numChannels >= 2 {
			right = s.decodeSample(s.frameBuf[s.sampleBytes : s.sampleBytes*2])
		}
		samples[i][0] = left
		samples[i][1] = right
		s.pos++
		n++
	}

	return n, true
}

func (s *aiffStreamer) decodeSample(b []byte) float64 {
	switch s.encoding {
	case aiffEncodingUnsignedPCM:
		return float64(b[0])/127.5 - 1
	case aiffEncodingFloatPCM:
		return s.decodeFloatSample(b)
	case aiffEncodingULaw:
		return float64(decodeULaw(b[0])) / 32768
	case aiffEncodingALaw:
		return float64(decodeALaw(b[0])) / 32768
	}

	var raw int64
	if s.little {
		for i := len(b) - 1; i >= 0; i-- {
			raw = (raw << 8) | int64(b[i])
		}
	} else {
		for _, v := range b {
			raw = (raw << 8) | int64(v)
		}
	}

	unusedBits := s.sampleBytes*8 - s.bitDepth
	if unusedBits > 0 {
		raw >>= unusedBits
	}
	signBit := int64(1) << (s.bitDepth - 1)
	if raw&signBit != 0 {
		raw -= signBit << 1
	}

	scale := math.Ldexp(1, s.bitDepth-1)
	return float64(raw) / scale
}

func (s *aiffStreamer) decodeFloatSample(b []byte) float64 {
	switch len(b) {
	case 4:
		var bits uint32
		if s.little {
			bits = binary.LittleEndian.Uint32(b)
		} else {
			bits = binary.BigEndian.Uint32(b)
		}
		return float64(math.Float32frombits(bits))
	case 8:
		var bits uint64
		if s.little {
			bits = binary.LittleEndian.Uint64(b)
		} else {
			bits = binary.BigEndian.Uint64(b)
		}
		return math.Float64frombits(bits)
	default:
		return 0
	}
}

func decodeULaw(v byte) int16 {
	v = ^v
	sign := int16(v & 0x80)
	exponent := (v >> 4) & 0x07
	mantissa := v & 0x0f
	sample := int16(((int(mantissa) << 3) + 0x84) << exponent)
	sample -= 0x84
	if sign != 0 {
		return -sample
	}
	return sample
}

func decodeALaw(v byte) int16 {
	v ^= 0x55
	sign := v & 0x80
	exponent := (v >> 4) & 0x07
	mantissa := v & 0x0f

	var sample int16
	if exponent == 0 {
		sample = int16((int(mantissa) << 4) + 8)
	} else {
		sample = int16(((int(mantissa) << 4) + 0x108) << (exponent - 1))
	}
	if sign == 0 {
		return -sample
	}
	return sample
}

func (s *aiffStreamer) Err() error {
	return s.err
}

func (s *aiffStreamer) Len() int {
	return s.numFrames
}

func (s *aiffStreamer) Position() int {
	return s.pos
}

func (s *aiffStreamer) Seek(p int) error {
	if p < 0 || p > s.numFrames {
		return fmt.Errorf("seek out of bounds")
	}
	if _, err := s.r.Seek(s.dataStart+int64(p*s.frameBytes), io.SeekStart); err != nil {
		return err
	}
	s.pos = p
	return nil
}

func (s *aiffStreamer) Close() error {
	if s.rc != nil {
		return s.rc.Close()
	}
	return nil
}
