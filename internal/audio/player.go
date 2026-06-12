package audio

import (
	"fmt"
	"math"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gg582/gozik/internal/cdrom"
	"github.com/gopxl/beep"
	"github.com/gopxl/beep/effects"
	"github.com/gopxl/beep/flac"
	"github.com/gopxl/beep/mp3"
	"github.com/gopxl/beep/speaker"
	"github.com/gopxl/beep/vorbis"
	"github.com/gopxl/beep/wav"
)

const targetSampleRate = beep.SampleRate(48000)

var speakerInitOnce sync.Once
var speakerInitErr error

type PlayerState int

const (
	StateIdle PlayerState = iota
	StatePlaying
	StatePaused
	StateStopping
	StateClosed
)

type Player struct {
	mu        sync.Mutex
	streamer  beep.StreamSeekCloser
	format    beep.Format
	ctrl      *beep.Ctrl
	volume    *effects.Volume
	resampled beep.Streamer
	state     PlayerState
	cdDev     *cdrom.Device

	// current loaded track info
	currentFile       string
	currentDevice     string
	currentTrack      int
	isCD              bool
	currentStart      int           // seconds
	currentEnd        int           // seconds
	replayGain        float64       // log2-scaled gain correction
	expectedDuration  time.Duration // fallback when streamer.Len() == 0 (e.g. network streams)
}

func NewPlayer() (*Player, error) {
	return &Player{state: StateIdle}, nil
}

func (p *Player) closeCD() {
	if p.cdDev != nil {
		p.cdDev.Close()
		p.cdDev = nil
	}
}

func (p *Player) reset() {
	p.mu.Lock()
	streamer := p.streamer
	cdDev := p.cdDev
	hasSpeakerStreamer := p.ctrl != nil || p.volume != nil || p.resampled != nil
	p.state = StateStopping
	p.streamer = nil
	p.ctrl = nil
	p.volume = nil
	p.resampled = nil
	p.cdDev = nil
	p.currentFile = ""
	p.currentDevice = ""
	p.currentTrack = 0
	p.isCD = false
	p.currentStart = 0
	p.currentEnd = 0
	p.replayGain = 0
	p.expectedDuration = 0
	p.format = beep.Format{}
	p.mu.Unlock()

	if streamer != nil {
		_ = streamer.Close()
	}
	if cdDev != nil {
		_ = cdDev.Close()
	}
	if hasSpeakerStreamer {
		speaker.Clear()
	}

	p.mu.Lock()
	if p.state != StateClosed {
		p.state = StateIdle
	}
	p.mu.Unlock()
}

func (p *Player) Close() {
	p.mu.Lock()
	if p.state == StateClosed {
		p.mu.Unlock()
		return
	}
	p.mu.Unlock()
	p.reset()
	p.mu.Lock()
	p.state = StateClosed
	p.mu.Unlock()
}

func (p *Player) initSpeaker() error {
	speakerInitOnce.Do(func() {
		speakerInitErr = speaker.Init(targetSampleRate, targetSampleRate.N(time.Second/10))
	})
	return speakerInitErr
}

// ProbeDuration decodes a file just far enough to read its total length, then
// closes it. Used to compute the queue's total play time without playback.
func ProbeDuration(filename string) (time.Duration, error) {
	if IsStreamURL(filename) {
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
		streamer, format, err = decodeOpus(f)
	case "aac":
		streamer, format, err = decodeADTS(f)
	case "aiff", "aif":
		streamer, format, err = decodeAIFF(f)
	case "pcm", "raw":
		pcmFormat, detectErr := DetectPcmFormat(filename)
		if detectErr != nil {
			f.Close()
			return 0, detectErr
		}
		info, statErr := f.Stat()
		if statErr != nil {
			f.Close()
			return 0, statErr
		}
		frameSize, frameErr := rawPCMFrameSize(pcmFormat)
		if frameErr != nil {
			f.Close()
			return 0, frameErr
		}
		f.Close()
		return time.Second * time.Duration(info.Size()/int64(frameSize)) / rawPCMSampleRate, nil
	default:
		if _, ok := gaudioFormatForExt(ext); ok {
			f.Close()
			streamer, format, err = decodeGaudioExt(filename, ext)
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

func (p *Player) Load(filename string) error {
	return p.LoadSegment(filename, 0, 0)
}

// LoadSegment loads a file and restricts playback to the [startSec,endSec)
// range.  endSec=0 means "until the end of the file".
func (p *Player) LoadSegment(filename string, startSec, endSec int) error {
	p.reset()

	f, err := os.Open(filename)
	if err != nil {
		return err
	}

	ext := strings.ToLower(filename[strings.LastIndex(filename, ".")+1:])
	var streamer beep.StreamSeekCloser
	var format beep.Format
	useSegment := false

	if IsStreamURL(filename) {
		f.Close()
		streamer, format, err = decodeFFmpegStream(filename, nil)
	} else {
		switch ext {
		case "mp3":
			streamer, format, err = mp3.Decode(f)
			useSegment = true
		case "flac":
			streamer, format, err = flac.Decode(f)
			useSegment = true
		case "wav":
			streamer, format, err = wav.Decode(f)
			useSegment = true
		case "ogg":
			streamer, format, err = vorbis.Decode(f)
			useSegment = true
		case "opus":
			streamer, format, err = decodeOpus(f)
			useSegment = true
		case "aac":
			streamer, format, err = decodeADTS(f)
			useSegment = true
		case "aiff", "aif":
			streamer, format, err = decodeAIFF(f)
			useSegment = true
		case "pcm", "raw":
			f.Close()
			streamer, format, err = decodeFFmpegSegment(filename, startSec, endSec)
		default:
			if _, ok := gaudioFormatForExt(ext); ok {
				f.Close()
				streamer, format, err = decodeGaudioExt(filename, ext)
				useSegment = true
				break
			}
			f.Close()
			streamer, format, err = decodeFFmpegSegment(filename, startSec, endSec)
		}

		if err != nil {
			f.Close()
			streamer, format, err = decodeFFmpegSegment(filename, startSec, endSec)
			if err != nil {
				return err
			}
		}
	}

	if useSegment && (startSec > 0 || endSec > 0) {
		seg, segErr := NewSegmentStreamer(streamer, format, startSec, endSec)
		if segErr == nil {
			streamer = seg
		}
	}

	if err := p.initSpeaker(); err != nil {
		streamer.Close()
		return err
	}

	p.mu.Lock()
	p.streamer = newEOFDetector(streamer)
	p.format = format
	p.ctrl = &beep.Ctrl{Streamer: p.streamer, Paused: false}
	p.resampled = beep.Resample(4, p.format.SampleRate, targetSampleRate, p.ctrl)
	p.volume = &effects.Volume{Streamer: p.resampled, Base: 2, Volume: 0}
	p.state = StateIdle
	p.currentFile = filename
	p.currentDevice = ""
	p.currentTrack = 0
	p.isCD = false
	p.currentStart = startSec
	p.currentEnd = endSec
	p.replayGain = 0
	p.mu.Unlock()

	if gain, err := ExtractReplayGain(filename); err == nil && gain != 0 {
		// Convert dB to log2 scale: log2(10^(db/20)) = db * log2(10) / 20
		p.mu.Lock()
		p.replayGain = gain * (math.Log2(10) / 20)
		p.mu.Unlock()
	}

	return nil
}

// LoadStream loads an HTTP(S) stream URL with optional HTTP headers.
func (p *Player) LoadStream(url string, headers map[string]string) error {
	p.reset()

	streamer, format, err := decodeFFmpegStream(url, headers)
	if err != nil {
		return err
	}

	if err := p.initSpeaker(); err != nil {
		streamer.Close()
		return err
	}

	p.mu.Lock()
	p.streamer = newEOFDetector(streamer)
	p.format = format
	p.ctrl = &beep.Ctrl{Streamer: p.streamer, Paused: false}
	p.resampled = beep.Resample(4, p.format.SampleRate, targetSampleRate, p.ctrl)
	p.volume = &effects.Volume{Streamer: p.resampled, Base: 2, Volume: 0}
	p.state = StateIdle
	p.currentFile = url
	p.currentDevice = ""
	p.currentTrack = 0
	p.isCD = false
	p.currentStart = 0
	p.currentEnd = 0
	p.replayGain = 0
	p.mu.Unlock()

	return nil
}

func (p *Player) LoadCD(device string, trackNum int) error {
	p.reset()

	dev, err := cdrom.Open(device)
	if err != nil {
		return err
	}

	tracks, err := dev.ReadTOC()
	if err != nil {
		dev.Close()
		return err
	}

	var target *cdrom.Track
	for i := range tracks {
		if tracks[i].Number == trackNum {
			target = &tracks[i]
			break
		}
	}
	if target == nil {
		dev.Close()
		return fmt.Errorf("track %d not found", trackNum)
	}
	if !target.IsAudio {
		dev.Close()
		return fmt.Errorf("track %d is not an audio track", trackNum)
	}

	if err := p.initSpeaker(); err != nil {
		dev.Close()
		return err
	}

	p.mu.Lock()
	p.format = beep.Format{SampleRate: 44100, NumChannels: 2, Precision: 2}
	p.cdDev = dev
	streamer := cdrom.NewTrackStreamer(dev, *target)
	p.streamer = newEOFDetector(streamer)
	p.ctrl = &beep.Ctrl{Streamer: p.streamer, Paused: false}
	p.resampled = beep.Resample(4, p.format.SampleRate, targetSampleRate, p.ctrl)
	p.volume = &effects.Volume{Streamer: p.resampled, Base: 2, Volume: 0}
	p.state = StateIdle
	p.currentFile = ""
	p.currentDevice = device
	p.currentTrack = trackNum
	p.isCD = true
	p.mu.Unlock()

	return nil
}

func (p *Player) CurrentFile() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.currentFile
}

func (p *Player) CurrentSegment() (startSec, endSec int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.currentStart, p.currentEnd
}

func (p *Player) CurrentDevice() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.currentDevice
}

func (p *Player) CurrentTrack() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.currentTrack
}

func (p *Player) IsCDLoaded() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.isCD
}

func (p *Player) IsPaused() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.streamer != nil && p.state == StatePaused
}

func (p *Player) Play() error {
	p.mu.Lock()
	streamer := p.streamer
	ctrl := p.ctrl
	volume := p.volume
	started := p.state == StatePlaying || p.state == StatePaused
	if streamer == nil || ctrl == nil || volume == nil {
		p.mu.Unlock()
		return fmt.Errorf("no file loaded")
	}
	p.state = StatePlaying
	p.mu.Unlock()

	speaker.Lock()
	ctrl.Paused = false
	speaker.Unlock()

	if !started {
		speaker.Play(volume)
	}
	return nil
}

func (p *Player) Pause() {
	p.mu.Lock()
	ctrl := p.ctrl
	if ctrl == nil || p.state == StateIdle || p.state == StateStopping || p.state == StateClosed {
		p.mu.Unlock()
		return
	}
	paused := p.state != StatePaused
	if paused {
		p.state = StatePaused
	} else {
		p.state = StatePlaying
	}
	p.mu.Unlock()

	speaker.Lock()
	ctrl.Paused = paused
	speaker.Unlock()
}

func (p *Player) Stop() {
	p.mu.Lock()
	streamer := p.streamer
	ctrl := p.ctrl
	format := p.format
	volume := p.volume
	if streamer == nil || ctrl == nil {
		p.mu.Unlock()
		return
	}
	p.state = StateStopping
	p.mu.Unlock()

	if err := p.initSpeaker(); err != nil {
		p.mu.Lock()
		if p.state == StateStopping {
			p.state = StateIdle
		}
		p.mu.Unlock()
		return
	}
	speaker.Clear()
	speaker.Lock()
	ctrl.Paused = false
	_ = streamer.Seek(0)
	resampled := beep.Resample(4, format.SampleRate, targetSampleRate, ctrl)
	if volume != nil {
		volume.Streamer = resampled
	}
	speaker.Unlock()

	p.mu.Lock()
	if p.state != StateClosed {
		p.resampled = resampled
		p.state = StateIdle
	}
	p.mu.Unlock()
}

func (p *Player) SetVolume(v float64) {
	p.mu.Lock()
	volume := p.volume
	replayGain := p.replayGain
	p.mu.Unlock()
	if volume == nil {
		return
	}
	if v <= 0 {
		v = 0.0001
	}
	speaker.Lock()
	volume.Volume = math.Log2(v) + replayGain
	speaker.Unlock()
}

func (p *Player) Position() time.Duration {
	p.mu.Lock()
	streamer := p.streamer
	format := p.format
	p.mu.Unlock()
	if streamer == nil || format.SampleRate == 0 {
		return 0
	}
	samples := streamer.Position()
	return time.Second * time.Duration(samples) / time.Duration(format.SampleRate)
}

func (p *Player) SetDuration(d time.Duration) {
	p.mu.Lock()
	p.expectedDuration = d
	p.mu.Unlock()
}

func (p *Player) Length() time.Duration {
	p.mu.Lock()
	streamer := p.streamer
	format := p.format
	expected := p.expectedDuration
	p.mu.Unlock()
	if streamer == nil || format.SampleRate == 0 {
		return 0
	}
	samples := streamer.Len()
	if samples > 0 {
		return time.Second * time.Duration(samples) / time.Duration(format.SampleRate)
	}
	return expected
}

func (p *Player) Seek(pos time.Duration) error {
	p.mu.Lock()
	streamer := p.streamer
	format := p.format
	ctrl := p.ctrl
	volume := p.volume
	expected := p.expectedDuration
	p.mu.Unlock()
	if streamer == nil || format.SampleRate == 0 {
		return fmt.Errorf("no file loaded")
	}
	samplePos := int(pos * time.Duration(format.SampleRate) / time.Second)
	if samplePos < 0 {
		samplePos = 0
	}
	maxSamples := streamer.Len()
	if maxSamples <= 0 && expected > 0 {
		maxSamples = int(expected * time.Duration(format.SampleRate) / time.Second)
	}
	if samplePos > maxSamples {
		samplePos = maxSamples
	}
	speaker.Lock()
	err := streamer.Seek(samplePos)
	var resampled beep.Streamer
	if err == nil && ctrl != nil {
		resampled = beep.Resample(4, format.SampleRate, targetSampleRate, ctrl)
		if volume != nil {
			volume.Streamer = resampled
		}
	}
	speaker.Unlock()
	if resampled != nil {
		p.mu.Lock()
		p.resampled = resampled
		p.mu.Unlock()
	}
	return err
}

func (p *Player) EjectCD() error {
	p.mu.Lock()
	cdDev := p.cdDev
	p.mu.Unlock()
	if cdDev != nil {
		return cdDev.Eject()
	}
	return fmt.Errorf("no CD device open")
}

func (p *Player) IsPlaying() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.streamer != nil && p.state == StatePlaying
}

type eofDetector struct {
	beep.StreamSeekCloser
	mu  sync.Mutex
	eof bool
}

func newEOFDetector(s beep.StreamSeekCloser) *eofDetector {
	if s == nil {
		return nil
	}
	return &eofDetector{StreamSeekCloser: s}
}

func (d *eofDetector) Stream(samples [][2]float64) (n int, ok bool) {
	n, ok = d.StreamSeekCloser.Stream(samples)
	if !ok {
		d.mu.Lock()
		d.eof = true
		d.mu.Unlock()
	}
	return n, ok
}

func (d *eofDetector) IsEOF() bool {
	if d == nil {
		return false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.eof
}

func (d *eofDetector) Seek(p int) error {
	d.mu.Lock()
	d.eof = false
	d.mu.Unlock()
	return d.StreamSeekCloser.Seek(p)
}

func (p *Player) IsEOF() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if detector, ok := p.streamer.(*eofDetector); ok {
		return detector.IsEOF()
	}
	return false
}
