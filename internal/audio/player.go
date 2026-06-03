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

type Player struct {
	streamer  beep.StreamSeekCloser
	format    beep.Format
	ctrl      *beep.Ctrl
	volume    *effects.Volume
	resampled beep.Streamer
	paused    bool
	started   bool
	cdDev     *cdrom.Device

	// current loaded track info
	currentFile   string
	currentDevice string
	currentTrack  int
	isCD          bool
}

func NewPlayer() (*Player, error) {
	return &Player{}, nil
}

func (p *Player) closeCD() {
	if p.cdDev != nil {
		p.cdDev.Close()
		p.cdDev = nil
	}
}

func (p *Player) reset() {
	if p.streamer != nil {
		speaker.Clear()
		p.streamer.Close()
		p.streamer = nil
	}
	p.ctrl = nil
	p.volume = nil
	p.resampled = nil
	p.paused = false
	p.started = false
	p.closeCD()
	p.currentFile = ""
	p.currentDevice = ""
	p.currentTrack = 0
	p.isCD = false
	p.format = beep.Format{}
}

func (p *Player) Close() {
	p.reset()
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

	p.streamer = streamer
	p.format = format
	p.ctrl = &beep.Ctrl{Streamer: p.streamer, Paused: false}
	p.resampled = beep.Resample(4, p.format.SampleRate, targetSampleRate, p.ctrl)
	p.volume = &effects.Volume{Streamer: p.resampled, Base: 2, Volume: 0}
	p.paused = false
	p.started = false
	p.currentFile = filename
	p.currentDevice = ""
	p.currentTrack = 0
	p.isCD = false

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

	p.format = beep.Format{SampleRate: 44100, NumChannels: 2, Precision: 2}
	p.cdDev = dev
	streamer := cdrom.NewTrackStreamer(dev, *target)
	p.streamer = streamer
	p.ctrl = &beep.Ctrl{Streamer: p.streamer, Paused: false}
	p.resampled = beep.Resample(4, p.format.SampleRate, targetSampleRate, p.ctrl)
	p.volume = &effects.Volume{Streamer: p.resampled, Base: 2, Volume: 0}
	p.paused = false
	p.started = false
	p.currentFile = ""
	p.currentDevice = device
	p.currentTrack = trackNum
	p.isCD = true

	return nil
}

func (p *Player) CurrentFile() string {
	return p.currentFile
}

func (p *Player) CurrentDevice() string {
	return p.currentDevice
}

func (p *Player) CurrentTrack() int {
	return p.currentTrack
}

func (p *Player) IsCDLoaded() bool {
	return p.isCD
}

func (p *Player) IsPaused() bool {
	return p.streamer != nil && p.paused
}

func (p *Player) Play() error {
	if p.streamer == nil || p.ctrl == nil || p.volume == nil {
		return fmt.Errorf("no file loaded")
	}

	speaker.Lock()
	p.paused = false
	p.ctrl.Paused = false
	speaker.Unlock()

	if !p.started {
		speaker.Play(p.volume)
		p.started = true
	}
	return nil
}

func (p *Player) Pause() {
	if p.ctrl == nil {
		return
	}
	speaker.Lock()
	p.paused = !p.paused
	p.ctrl.Paused = p.paused
	speaker.Unlock()
}

func (p *Player) Stop() {
	if p.streamer == nil || p.ctrl == nil {
		return
	}
	if err := p.initSpeaker(); err != nil {
		return
	}
	speaker.Clear()
	speaker.Lock()
	p.ctrl.Paused = false
	p.paused = false
	p.started = false
	_ = p.streamer.Seek(0)
	p.resampled = beep.Resample(4, p.format.SampleRate, targetSampleRate, p.ctrl)
	if p.volume != nil {
		p.volume.Streamer = p.resampled
	}
	speaker.Unlock()
}

func (p *Player) SetVolume(v float64) {
	if p.volume == nil {
		return
	}
	if v <= 0 {
		v = 0.0001
	}
	speaker.Lock()
	p.volume.Volume = math.Log2(v)
	speaker.Unlock()
}

func (p *Player) Position() time.Duration {
	if p.streamer == nil || p.format.SampleRate == 0 {
		return 0
	}
	samples := p.streamer.Position()
	return time.Second * time.Duration(samples) / time.Duration(p.format.SampleRate)
}

func (p *Player) Length() time.Duration {
	if p.streamer == nil || p.format.SampleRate == 0 {
		return 0
	}
	samples := p.streamer.Len()
	return time.Second * time.Duration(samples) / time.Duration(p.format.SampleRate)
}

func (p *Player) Seek(pos time.Duration) error {
	if p.streamer == nil || p.format.SampleRate == 0 {
		return fmt.Errorf("no file loaded")
	}
	samplePos := int(pos * time.Duration(p.format.SampleRate) / time.Second)
	if samplePos < 0 {
		samplePos = 0
	}
	if samplePos > p.streamer.Len() {
		samplePos = p.streamer.Len()
	}
	speaker.Lock()
	err := p.streamer.Seek(samplePos)
	if err == nil && p.ctrl != nil {
		p.resampled = beep.Resample(4, p.format.SampleRate, targetSampleRate, p.ctrl)
		if p.volume != nil {
			p.volume.Streamer = p.resampled
		}
	}
	speaker.Unlock()
	return err
}

func (p *Player) EjectCD() error {
	if p.cdDev != nil {
		return p.cdDev.Eject()
	}
	return fmt.Errorf("no CD device open")
}

func (p *Player) IsPlaying() bool {
	return p.streamer != nil && !p.paused
}
