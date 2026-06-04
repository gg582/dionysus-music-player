package audio

import (
	"fmt"
	"math"
	"sync"
	"time"
	"unsafe"

	"github.com/gg582/gozik/internal/models"
	"github.com/moonfdd/ffmpeg-go/ffcommon"
	"github.com/moonfdd/ffmpeg-go/libavcodec"
	"github.com/moonfdd/ffmpeg-go/libavformat"
	"github.com/moonfdd/ffmpeg-go/libavutil"
	"github.com/moonfdd/ffmpeg-go/libswresample"
)

// Prescan spawns a background analysis of path using FFmpeg CGO.
// It returns chapter metadata and a 200-point RMS waveform.
func Prescan(path string) (*models.Waveform, []models.Chapter, error) {
	if err := validateRawPCMSize(path); err != nil {
		return nil, nil, err
	}

	ctx := libavformat.AvformatAllocContext()
	if ctx == nil {
		return nil, nil, fmt.Errorf("avformat_alloc_context failed")
	}

	var fmt_ *libavformat.AVInputFormat
	var dict *libavutil.AVDictionary
	if isRawPCM(path) {
		fmt_ = libavformat.AvFindInputFormat("s16le")
		libavutil.AvDictSet(&dict, "sample_rate", "44100", 0)
		libavutil.AvDictSet(&dict, "channels", "2", 0)
	}

	ret := libavformat.AvformatOpenInput(&ctx, path, fmt_, &dict)
	if dict != nil {
		libavutil.AvDictFree(&dict)
	}
	if ret < 0 {
		return nil, nil, fmt.Errorf("avformat_open_input failed: %d", ret)
	}
	defer libavformat.AvformatCloseInput(&ctx)

	ret = ctx.AvformatFindStreamInfo(nil)
	if ret < 0 {
		return nil, nil, fmt.Errorf("avformat_find_stream_info failed: %d", ret)
	}

	chapters := extractChapters(ctx)
	waveform, err := extractWaveform(ctx)
	if err != nil {
		// Return chapters even if waveform extraction fails.
		return nil, chapters, err
	}
	return waveform, chapters, nil
}

func extractChapters(ctx *libavformat.AVFormatContext) []models.Chapter {
	nb := int(ctx.NbChapters)
	if nb == 0 {
		return nil
	}

	var out []models.Chapter
	for i := 0; i < nb; i++ {
		ch := *(**libavformat.AVChapter)(unsafe.Pointer(
			uintptr(unsafe.Pointer(ctx.Chapters)) + uintptr(i)*unsafe.Sizeof(uintptr(0)),
		))
		if ch == nil {
			continue
		}

		tb := float64(ch.TimeBase.Num) / float64(ch.TimeBase.Den)
		start := time.Duration(float64(ch.Start) * tb * float64(time.Second))
		end := time.Duration(float64(ch.End) * tb * float64(time.Second))

		var title string
		if ch.Metadata != nil {
			ent := ch.Metadata.AvDictGet("title", nil, libavutil.AV_DICT_IGNORE_SUFFIX)
			if ent != nil {
				title = ffcommon.StringFromPtr(ent.Value)
			}
		}

		out = append(out, models.Chapter{
			ID:    int64(ch.Id),
			Title: title,
			Start: start,
			End:   end,
		})
	}
	return out
}

// ScanResult is delivered back to the GUI/main thread via a channel.
type ScanResult struct {
	Path     string
	Waveform *models.Waveform
	Chapters []models.Chapter
	Err      error
}

// ScanPool limits concurrent FFmpeg scans via a semaphore and deduplicates
// in-flight paths with a mutex-protected ownership map.
type ScanPool struct {
	limit  chan struct{}       // capacity = max concurrent workers
	mu     sync.Mutex          // guards active + done
	active map[string]struct{} // owned paths
	out    chan ScanResult
	done   chan struct{}
}

// NewScanPool creates a pool allowing at most maxWorkers concurrent scans.
func NewScanPool(maxWorkers, outBuf int) *ScanPool {
	return &ScanPool{
		limit:  make(chan struct{}, maxWorkers),
		active: make(map[string]struct{}),
		out:    make(chan ScanResult, outBuf),
		done:   make(chan struct{}),
	}
}

// Submit attempts to take ownership of path and dispatch a worker.
// It returns false if the path is already owned or the pool is closed.
func (p *ScanPool) Submit(path string) bool {
	p.mu.Lock()
	select {
	case <-p.done:
		p.mu.Unlock()
		return false
	default:
	}
	if _, owned := p.active[path]; owned {
		p.mu.Unlock()
		return false
	}
	p.active[path] = struct{}{}
	p.mu.Unlock()

	select {
	case p.limit <- struct{}{}: // acquire slot
	case <-p.done:
		p.mu.Lock()
		delete(p.active, path)
		p.mu.Unlock()
		return false
	}

	go p.work(path)
	return true
}

// work owns exactly one goroutine lifecycle: scan, send, release.
func (p *ScanPool) work(path string) {
	defer func() {
		<-p.limit // release slot
		p.mu.Lock()
		delete(p.active, path)
		p.mu.Unlock()
	}()

	wf, ch, err := Prescan(path)

	select {
	case <-p.done:
		return
	case p.out <- ScanResult{Path: path, Waveform: wf, Chapters: ch, Err: err}:
	}
}

// Results returns the output channel.
func (p *ScanPool) Results() <-chan ScanResult { return p.out }

// Close shuts down the pool. After Close, Submit returns false.
func (p *ScanPool) Close() {
	p.mu.Lock()
	select {
	case <-p.done:
		p.mu.Unlock()
		return
	default:
	}
	close(p.done)
	p.mu.Unlock()
}

func extractWaveform(ctx *libavformat.AVFormatContext) (*models.Waveform, error) {
	audioIdx := -1
	for i := 0; i < int(ctx.NbStreams); i++ {
		stream := ctx.GetStream(uint32(i))
		if stream == nil {
			continue
		}
		if stream.Codecpar.CodecType == libavutil.AVMEDIA_TYPE_AUDIO {
			audioIdx = i
			break
		}
	}
	if audioIdx < 0 {
		return nil, fmt.Errorf("no audio stream")
	}

	stream := ctx.GetStream(uint32(audioIdx))
	codec := libavcodec.AvcodecFindDecoder(stream.Codecpar.CodecId)
	if codec == nil {
		return nil, fmt.Errorf("decoder not found")
	}
	codecCtx := codec.AvcodecAllocContext3()
	if codecCtx == nil {
		return nil, fmt.Errorf("avcodec_alloc_context3 failed")
	}
	defer libavcodec.AvcodecFreeContext(&codecCtx)

	ret := codecCtx.AvcodecParametersToContext(stream.Codecpar)
	if ret < 0 {
		return nil, fmt.Errorf("avcodec_parameters_to_context failed: %d", ret)
	}
	ret = codecCtx.AvcodecOpen2(codec, nil)
	if ret < 0 {
		return nil, fmt.Errorf("avcodec_open2 failed: %d", ret)
	}

	// Resample to 8 kHz mono float for fast analysis.
	swr := libswresample.SwrAlloc()
	defer libswresample.SwrFree(&swr)

	inLayout := stream.Codecpar.ChannelLayout
	if inLayout == 0 {
		inLayout = ffcommon.FUint64T(libavutil.AvGetDefaultChannelLayout(stream.Codecpar.Channels))
	}

	_ = swr.SwrAllocSetOpts(
		libavutil.AV_CH_LAYOUT_MONO,
		libavutil.AV_SAMPLE_FMT_FLT,
		8000,
		int64(inLayout),
		libavutil.AVSampleFormat(stream.Codecpar.Format),
		ffcommon.FInt(stream.Codecpar.SampleRate),
		0, ffcommon.FVoidP(0),
	)
	ret = swr.SwrInit()
	if ret < 0 {
		return nil, fmt.Errorf("swr_init failed: %d", ret)
	}

	// Determine analysis window size from duration.
	durationSec := 0.0
	if ctx.Duration != libavutil.AV_NOPTS_VALUE && ctx.Duration > 0 {
		durationSec = float64(ctx.Duration) / float64(libavutil.AV_TIME_BASE)
	} else if stream.Duration != libavutil.AV_NOPTS_VALUE && stream.Duration > 0 {
		durationSec = float64(stream.Duration) * float64(stream.TimeBase.Num) / float64(stream.TimeBase.Den)
	}
	if durationSec <= 0 {
		return nil, fmt.Errorf("unknown duration")
	}

	totalSamples := int(durationSec * 8000)
	windowSize := totalSamples / 200
	if windowSize < 1 {
		windowSize = 1
	}

	var w models.Waveform
	var sumSquares float64
	var sampleCount int
	windowIdx := 0

	pkt := libavcodec.AvPacketAlloc()
	defer libavcodec.AvPacketFree(&pkt)
	frame := libavutil.AvFrameAlloc()
	defer libavutil.AvFrameFree(&frame)

	for {
		ret = ctx.AvReadFrame(pkt)
		if ret < 0 {
			break
		}
		if pkt.StreamIndex != ffcommon.FUint(audioIdx) {
			pkt.AvPacketUnref()
			continue
		}

		ret = codecCtx.AvcodecSendPacket(pkt)
		pkt.AvPacketUnref()
		if ret < 0 {
			continue
		}

		for {
			ret = codecCtx.AvcodecReceiveFrame(frame)
			if ret == -libavutil.EAGAIN || ret == libavutil.AVERROR_EOF {
				break
			}
			if ret < 0 {
				return nil, fmt.Errorf("avcodec_receive_frame failed: %d", ret)
			}

			outSamples := int(swr.SwrGetOutSamples(frame.NbSamples))
			if outSamples <= 0 {
				continue
			}

			outBuf := make([]float32, outSamples)
			outPtr := (*ffcommon.FUint8T)(unsafe.Pointer(&outBuf[0]))
			outArr := [1]*ffcommon.FUint8T{outPtr}

			converted := swr.SwrConvert(
				(**ffcommon.FUint8T)(unsafe.Pointer(&outArr[0])),
				int32(outSamples),
				(**ffcommon.FUint8T)(unsafe.Pointer(&frame.Data[0])),
				frame.NbSamples,
			)
			if converted < 0 {
				continue
			}

			for i := 0; i < int(converted); i++ {
				v := float64(outBuf[i])
				sumSquares += v * v
				sampleCount++

				if sampleCount >= windowSize && windowIdx < 200 {
					rms := math.Sqrt(sumSquares / float64(sampleCount))
					amp := rms * 1024 // heuristic gain
					if amp > 255 {
						amp = 255
					}
					w.Data[windowIdx] = uint8(amp)
					windowIdx++
					sumSquares = 0
					sampleCount = 0
				}
			}
		}
	}

	// Flush decoder.
	codecCtx.AvcodecSendPacket(nil)
	for {
		ret = codecCtx.AvcodecReceiveFrame(frame)
		if ret == -libavutil.EAGAIN || ret == libavutil.AVERROR_EOF {
			break
		}
		if ret < 0 {
			break
		}
		outSamples := int(swr.SwrGetOutSamples(frame.NbSamples))
		if outSamples <= 0 {
			continue
		}
		outBuf := make([]float32, outSamples)
		outPtr := (*ffcommon.FUint8T)(unsafe.Pointer(&outBuf[0]))
		outArr := [1]*ffcommon.FUint8T{outPtr}
		converted := swr.SwrConvert(
			(**ffcommon.FUint8T)(unsafe.Pointer(&outArr[0])),
			int32(outSamples),
			(**ffcommon.FUint8T)(unsafe.Pointer(&frame.Data[0])),
			frame.NbSamples,
		)
		if converted < 0 {
			continue
		}
		for i := 0; i < int(converted); i++ {
			v := float64(outBuf[i])
			sumSquares += v * v
			sampleCount++
			if sampleCount >= windowSize && windowIdx < 200 {
				rms := math.Sqrt(sumSquares / float64(sampleCount))
				amp := rms * 1024
				if amp > 255 {
					amp = 255
				}
				w.Data[windowIdx] = uint8(amp)
				windowIdx++
				sumSquares = 0
				sampleCount = 0
			}
		}
	}

	// Drain resampler tail.
	for {
		outSamples := int(swr.SwrGetOutSamples(0))
		if outSamples <= 0 {
			break
		}
		outBuf := make([]float32, outSamples)
		outPtr := (*ffcommon.FUint8T)(unsafe.Pointer(&outBuf[0]))
		outArr := [1]*ffcommon.FUint8T{outPtr}
		converted := swr.SwrConvert(
			(**ffcommon.FUint8T)(unsafe.Pointer(&outArr[0])),
			int32(outSamples),
			nil,
			0,
		)
		if converted <= 0 {
			break
		}
		for i := 0; i < int(converted); i++ {
			v := float64(outBuf[i])
			sumSquares += v * v
			sampleCount++
			if sampleCount >= windowSize && windowIdx < 200 {
				rms := math.Sqrt(sumSquares / float64(sampleCount))
				amp := rms * 1024
				if amp > 255 {
					amp = 255
				}
				w.Data[windowIdx] = uint8(amp)
				windowIdx++
				sumSquares = 0
				sampleCount = 0
			}
		}
	}

	// Fill any remaining windows.
	lastVal := uint8(0)
	if windowIdx > 0 {
		lastVal = w.Data[windowIdx-1]
	}
	for ; windowIdx < 200; windowIdx++ {
		w.Data[windowIdx] = lastVal
	}

	return &w, nil
}
