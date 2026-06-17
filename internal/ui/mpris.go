package ui

import (
	"fmt"

	playerv1 "github.com/gg582/gozik/api/player/v1"
	"github.com/gg582/gozik/internal/grpcserver"
	"github.com/gg582/gozik/internal/models"
	"github.com/gg582/gozik/internal/mpris"
	"github.com/godbus/dbus/v5"
	"github.com/gotk3/gotk3/glib"
)

type mprisController struct {
	mw *MainWindow
}

func (c *mprisController) Play()     { glib.IdleAdd(func() bool { c.mw.onPlay(); return false }) }
func (c *mprisController) Pause()    { glib.IdleAdd(func() bool { c.mw.onPause(); return false }) }
func (c *mprisController) Stop()     { glib.IdleAdd(func() bool { c.mw.onStop(); return false }) }
func (c *mprisController) Next()     { glib.IdleAdd(func() bool { c.mw.onNext(); return false }) }
func (c *mprisController) Previous() { glib.IdleAdd(func() bool { c.mw.onPrev(); return false }) }

func (c *mprisController) IsPlaying() bool {
	return c.mw.player != nil && c.mw.player.IsPlaying()
}

func (c *mprisController) IsPaused() bool {
	return c.mw.player != nil && c.mw.player.IsPaused()
}

func (c *mprisController) CurrentTitle() string {
	if c.mw.player == nil {
		return ""
	}
	idx := c.mw.selectedIdx
	if idx >= 0 && idx < len(c.mw.songs) {
		s := c.mw.songs[idx]
		if s.Title != "" {
			return s.Title
		}
		return s.Name
	}
	return ""
}

func (c *mprisController) CurrentArtist() string {
	if c.mw.player == nil {
		return ""
	}
	idx := c.mw.selectedIdx
	if idx >= 0 && idx < len(c.mw.songs) {
		return c.mw.songs[idx].Artist
	}
	return ""
}

func (c *mprisController) CurrentAlbum() string {
	if c.mw.player == nil {
		return ""
	}
	idx := c.mw.selectedIdx
	if idx >= 0 && idx < len(c.mw.songs) {
		return c.mw.songs[idx].Album
	}
	return ""
}

func (c *mprisController) CurrentLengthMicros() int64 {
	if c.mw.player == nil {
		return 0
	}
	return c.mw.player.Length().Microseconds()
}

func (mw *MainWindow) initMPRIS() {
	var err error
	mw.mprisServer, err = mpris.NewServer("Gozik", &mprisController{mw: mw})
	if err != nil {
		// Non-fatal: MPRIS is a nice-to-have on Linux desktops.
		return
	}
}

func (mw *MainWindow) updateMPRISStatus() {
	if mw.mprisServer == nil {
		return
	}
	var status string
	switch {
	case mw.player == nil:
		status = "Stopped"
	case mw.player.IsPlaying():
		status = "Playing"
	case mw.player.IsPaused():
		status = "Paused"
	default:
		status = "Stopped"
	}
	mw.mprisServer.SetPlaybackStatus(status)
	mw.publishPlaybackStateChanged()
}

func (mw *MainWindow) updateMPRISMetadata() {
	if mw.mprisServer == nil {
		return
	}
	meta := make(map[string]interface{})
	if mw.player == nil || mw.selectedIdx < 0 || mw.selectedIdx >= len(mw.songs) {
		mw.mprisServer.SetMetadata(meta)
		return
	}
	s := mw.songs[mw.selectedIdx]
	title := s.Title
	if title == "" {
		title = s.Name
	}
	trackID := fmt.Sprintf("/org/mpris/MediaPlayer2/Track/%d", mw.selectedIdx)
	meta["mpris:trackid"] = dbus.ObjectPath(trackID)
	meta["xesam:title"] = title
	if s.Artist != "" {
		meta["xesam:artist"] = []string{s.Artist}
	}
	if s.Album != "" {
		meta["xesam:album"] = s.Album
	}
	meta["mpris:length"] = mw.player.Length().Microseconds()
	mw.mprisServer.SetMetadata(meta)
	mw.publishTrackChanged()
}

func (mw *MainWindow) updateMPRISPosition() {
	if mw.mprisServer == nil || mw.player == nil {
		return
	}
	mw.mprisServer.SetPosition(mw.player.Position().Microseconds())
	mw.publishPositionChanged()
}

// ---------------------------------------------------------------------------
// gRPC event publishing
// ---------------------------------------------------------------------------

func (mw *MainWindow) publishEvent(eventType playerv1.EventType) {
	if mw.grpcEventPublisher == nil {
		return
	}
	status := grpcserver.Status{
		CurrentIndex: mw.playingIdx,
		Volume:       1.0,
		Muted:        mw.muted,
		RepeatMode:   mw.playMode,
		Shuffle:      mw.shuffle,
	}
	if mw.volumeScale != nil {
		status.Volume = mw.volumeScale.GetValue()
	}
	if mw.player != nil {
		status.Position = mw.player.Position()
		status.Length = mw.player.Length()
		switch {
		case mw.player.IsPlaying():
			status.PlaybackState = models.PlaybackStatePlaying
		case mw.player.IsPaused():
			status.PlaybackState = models.PlaybackStatePaused
		default:
			status.PlaybackState = models.PlaybackStateStopped
		}
	} else {
		status.PlaybackState = models.PlaybackStateStopped
	}

	event := &playerv1.PlayerEvent{
		Type:       eventType,
		Status:     grpcserver.StatusToProto(status),
		PositionMs: status.Position.Milliseconds(),
	}
	if status.CurrentIndex >= 0 && status.CurrentIndex < len(mw.songs) {
		event.Track = grpcserver.SongToProto(status.CurrentIndex, mw.songs[status.CurrentIndex])
	}
	mw.grpcEventPublisher.Publish(event)
}

func (mw *MainWindow) publishPlaybackStateChanged() {
	mw.publishEvent(playerv1.EventType_EVENT_TYPE_PLAYBACK_STATE_CHANGED)
}

func (mw *MainWindow) publishTrackChanged() {
	mw.publishEvent(playerv1.EventType_EVENT_TYPE_TRACK_CHANGED)
}

func (mw *MainWindow) publishPositionChanged() {
	mw.publishEvent(playerv1.EventType_EVENT_TYPE_POSITION_CHANGED)
}

func (mw *MainWindow) publishQueueChanged() {
	mw.publishEvent(playerv1.EventType_EVENT_TYPE_QUEUE_CHANGED)
}

func (mw *MainWindow) publishVolumeChanged() {
	mw.publishEvent(playerv1.EventType_EVENT_TYPE_VOLUME_CHANGED)
}

func (mw *MainWindow) publishRepeatModeChanged() {
	mw.publishEvent(playerv1.EventType_EVENT_TYPE_REPEAT_MODE_CHANGED)
}

func (mw *MainWindow) publishShuffleChanged() {
	mw.publishEvent(playerv1.EventType_EVENT_TYPE_SHUFFLE_CHANGED)
}
