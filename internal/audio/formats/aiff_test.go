package formats

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"
)

func TestDecodeAIFFParsesChunkPaddingAndSoundOffset(t *testing.T) {
	audioData := []byte{
		0x40, 0x00, // left: 0.5
		0xc0, 0x00, // right: -0.5
		0x20, 0x00, // left: 0.25
		0xe0, 0x00, // right: -0.25
	}
	data := buildAIFF(t, "AIFF", 2, 2, 16, 44100, "NONE", audioData, 3, true)

	streamer, format, err := DecodeAIFF(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("DecodeAIFF() error = %v", err)
	}
	defer streamer.Close()

	if format.SampleRate != 44100 || format.NumChannels != 2 || format.Precision != 2 {
		t.Fatalf("format = %+v", format)
	}
	if streamer.Len() != 2 {
		t.Fatalf("Len() = %d, want 2", streamer.Len())
	}

	samples := make([][2]float64, 2)
	n, ok := streamer.Stream(samples)
	if n != 2 || !ok {
		t.Fatalf("Stream() = %d, %v", n, ok)
	}
	assertSample(t, samples[0][0], 0.5)
	assertSample(t, samples[0][1], -0.5)
	assertSample(t, samples[1][0], 0.25)
	assertSample(t, samples[1][1], -0.25)
}

func TestDecodeAIFCSowtParsesLittleEndianPCM(t *testing.T) {
	audioData := []byte{
		0x00, 0x40, // 0.5 little-endian
		0x00, 0xc0, // -0.5 little-endian
	}
	data := buildAIFF(t, "AIFC", 1, 2, 16, 48000, "sowt", audioData, 0, false)

	streamer, format, err := DecodeAIFF(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("DecodeAIFF() error = %v", err)
	}
	defer streamer.Close()

	if format.SampleRate != 48000 || format.NumChannels != 1 || format.Precision != 2 {
		t.Fatalf("format = %+v", format)
	}

	samples := make([][2]float64, 2)
	n, ok := streamer.Stream(samples)
	if n != 2 || !ok {
		t.Fatalf("Stream() = %d, %v", n, ok)
	}
	assertSample(t, samples[0][0], 0.5)
	assertSample(t, samples[0][1], 0.5)
	assertSample(t, samples[1][0], -0.5)
	assertSample(t, samples[1][1], -0.5)
}

func TestDecodeAIFFParses24BitBigEndianPCM(t *testing.T) {
	audioData := []byte{
		0x40, 0x00, 0x00, // 0.5
		0xc0, 0x00, 0x00, // -0.5
	}
	data := buildAIFF(t, "AIFF", 1, 2, 24, 96000, "NONE", audioData, 0, false)

	streamer, format, err := DecodeAIFF(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("DecodeAIFF() error = %v", err)
	}
	defer streamer.Close()

	if format.SampleRate != 96000 || format.NumChannels != 1 || format.Precision != 3 {
		t.Fatalf("format = %+v", format)
	}

	samples := make([][2]float64, 2)
	n, ok := streamer.Stream(samples)
	if n != 2 || !ok {
		t.Fatalf("Stream() = %d, %v", n, ok)
	}
	assertSample(t, samples[0][0], 0.5)
	assertSample(t, samples[1][0], -0.5)
}

func TestDecodeAIFCParsesFloat32PCM(t *testing.T) {
	audioData := make([]byte, 8)
	binary.BigEndian.PutUint32(audioData[0:4], math.Float32bits(0.5))
	binary.BigEndian.PutUint32(audioData[4:8], math.Float32bits(-0.5))
	data := buildAIFF(t, "AIFC", 1, 2, 32, 44100, "fl32", audioData, 0, false)

	streamer, format, err := DecodeAIFF(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("DecodeAIFF() error = %v", err)
	}
	defer streamer.Close()

	if format.SampleRate != 44100 || format.NumChannels != 1 || format.Precision != 4 {
		t.Fatalf("format = %+v", format)
	}

	samples := make([][2]float64, 2)
	n, ok := streamer.Stream(samples)
	if n != 2 || !ok {
		t.Fatalf("Stream() = %d, %v", n, ok)
	}
	assertSample(t, samples[0][0], 0.5)
	assertSample(t, samples[1][0], -0.5)
}

func TestDecodeAIFCParsesRawUnsigned8BitPCM(t *testing.T) {
	data := buildAIFF(t, "AIFC", 1, 2, 8, 44100, "raw ", []byte{0x00, 0xff}, 0, false)

	streamer, _, err := DecodeAIFF(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("DecodeAIFF() error = %v", err)
	}
	defer streamer.Close()

	samples := make([][2]float64, 2)
	n, ok := streamer.Stream(samples)
	if n != 2 || !ok {
		t.Fatalf("Stream() = %d, %v", n, ok)
	}
	assertSample(t, samples[0][0], -1)
	assertSample(t, samples[1][0], 1)
}

func TestDecodeAIFCParsesCompandedPCM(t *testing.T) {
	tests := []struct {
		name        string
		compression string
		data        []byte
		want        []float64
	}{
		{name: "ulaw", compression: "ulaw", data: []byte{0xff, 0x80}, want: []float64{0, 32124.0 / 32768.0}},
		{name: "alaw", compression: "alaw", data: []byte{0xd5, 0x2a}, want: []float64{8.0 / 32768.0, -32256.0 / 32768.0}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := buildAIFF(t, "AIFC", 1, 2, 8, 44100, tt.compression, tt.data, 0, false)

			streamer, _, err := DecodeAIFF(bytes.NewReader(data))
			if err != nil {
				t.Fatalf("DecodeAIFF() error = %v", err)
			}
			defer streamer.Close()

			samples := make([][2]float64, 2)
			n, ok := streamer.Stream(samples)
			if n != 2 || !ok {
				t.Fatalf("Stream() = %d, %v", n, ok)
			}
			assertSample(t, samples[0][0], tt.want[0])
			assertSample(t, samples[1][0], tt.want[1])
		})
	}
}

func TestAIFFStreamerSeek(t *testing.T) {
	audioData := []byte{
		0x00, 0x00,
		0x20, 0x00,
		0x40, 0x00,
	}
	data := buildAIFF(t, "AIFF", 1, 3, 16, 44100, "NONE", audioData, 0, false)

	streamer, _, err := DecodeAIFF(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("DecodeAIFF() error = %v", err)
	}
	defer streamer.Close()

	if err := streamer.Seek(2); err != nil {
		t.Fatalf("Seek() error = %v", err)
	}
	samples := make([][2]float64, 1)
	n, ok := streamer.Stream(samples)
	if n != 1 || !ok {
		t.Fatalf("Stream() = %d, %v", n, ok)
	}
	assertSample(t, samples[0][0], 0.5)
	if streamer.Position() != 3 {
		t.Fatalf("Position() = %d, want 3", streamer.Position())
	}
}

func buildAIFF(t *testing.T, formType string, channels, frames, bitDepth, sampleRate int, compression string, audioData []byte, ssndOffset int, addOddChunk bool) []byte {
	t.Helper()

	var body bytes.Buffer
	body.WriteString(formType)

	if addOddChunk {
		writeChunk(t, &body, "JUNK", []byte{0x7f})
	}

	var comm bytes.Buffer
	binary.Write(&comm, binary.BigEndian, uint16(channels))
	binary.Write(&comm, binary.BigEndian, uint32(frames))
	binary.Write(&comm, binary.BigEndian, uint16(bitDepth))
	comm.Write(encodeAIFFExtendedSampleRate(sampleRate))
	if formType == "AIFC" {
		comm.WriteString(compression)
		comm.WriteByte(0)
	}
	writeChunk(t, &body, "COMM", comm.Bytes())

	var ssnd bytes.Buffer
	binary.Write(&ssnd, binary.BigEndian, uint32(ssndOffset))
	binary.Write(&ssnd, binary.BigEndian, uint32(0))
	ssnd.Write(bytes.Repeat([]byte{0xaa}, ssndOffset))
	ssnd.Write(audioData)
	writeChunk(t, &body, "SSND", ssnd.Bytes())

	var out bytes.Buffer
	out.WriteString("FORM")
	binary.Write(&out, binary.BigEndian, uint32(body.Len()))
	out.Write(body.Bytes())
	return out.Bytes()
}

func writeChunk(t *testing.T, dst *bytes.Buffer, id string, data []byte) {
	t.Helper()

	dst.WriteString(id)
	binary.Write(dst, binary.BigEndian, uint32(len(data)))
	dst.Write(data)
	if len(data)%2 != 0 {
		dst.WriteByte(0)
	}
}

func encodeAIFFExtendedSampleRate(sampleRate int) []byte {
	rate := uint64(sampleRate)
	shift := 0
	for rate >= 1<<16 {
		rate >>= 1
		shift++
	}

	exponent := uint16(16383 + 15 + shift)
	mantissa := uint64(sampleRate) << uint(48-shift)
	out := make([]byte, 10)
	binary.BigEndian.PutUint16(out[0:2], exponent)
	binary.BigEndian.PutUint64(out[2:10], mantissa)
	return out
}

func assertSample(t *testing.T, got, want float64) {
	t.Helper()

	if math.Abs(got-want) > 0.00001 {
		t.Fatalf("sample = %f, want %f", got, want)
	}
}
