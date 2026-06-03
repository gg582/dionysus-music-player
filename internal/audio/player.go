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
	currentFile   string
	currentDevice string
	currentTrack  int
	isCD          bool
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

func (p *Player) Load(filename string) error {
	p.reset()

	f, err := os.Open(filename)
	if err != nil {
		return err
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
	case "aiff", "aif":
		streamer, format, err = decodeAIFF(f)
	case "pcm", "raw":
		streamer, format, err = decodePCM(f)
	default:
		f.Close()
		streamer, format, err = decodeFFmpeg(filename)
	}

	if err != nil {
		f.Close()
		streamer, format, err = decodeFFmpeg(filename)
		if err != nil {
			return err
		}
	}

	if err := p.initSpeaker(); err != nil {
		streamer.Close()
		return err
	}

	p.mu.Lock()
	p.streamer = streamer
	p.format = format
	p.ctrl = &beep.Ctrl{Streamer: p.streamer, Paused: false}
	p.resampled = beep.Resample(4, p.format.SampleRate, targetSampleRate, p.ctrl)
	p.volume = &effects.Volume{Streamer: p.resampled, Base: 2, Volume: 0}
	p.state = StateIdle
	p.currentFile = filename
	p.currentDevice = ""
	p.currentTrack = 0
	p.isCD = false
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
	p.streamer = streamer
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
	p.mu.Unlock()
	if volume == nil {
		return
	}
	if v <= 0 {
		v = 0.0001
	}
	speaker.Lock()
	volume.Volume = math.Log2(v)
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

func (p *Player) Length() time.Duration {
	p.mu.Lock()
	streamer := p.streamer
	format := p.format
	p.mu.Unlock()
	if streamer == nil || format.SampleRate == 0 {
		return 0
	}
	samples := streamer.Len()
	return time.Second * time.Duration(samples) / time.Duration(format.SampleRate)
}

func (p *Player) Seek(pos time.Duration) error {
	p.mu.Lock()
	streamer := p.streamer
	format := p.format
	ctrl := p.ctrl
	volume := p.volume
	p.mu.Unlock()
	if streamer == nil || format.SampleRate == 0 {
		return fmt.Errorf("no file loaded")
	}
	samplePos := int(pos * time.Duration(format.SampleRate) / time.Second)
	if samplePos < 0 {
		samplePos = 0
	}
	if samplePos > streamer.Len() {
		samplePos = streamer.Len()
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
