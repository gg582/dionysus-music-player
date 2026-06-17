package ui

import (
	"context"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	musicv1 "github.com/gg582/gozik/api/music/v1"
	audioutils "github.com/gg582/gozik/internal/audio/utils"
	"github.com/gg582/gozik/internal/cdrom"
	"github.com/gg582/gozik/internal/grpcserver"
	"github.com/gg582/gozik/internal/models"
	"github.com/gg582/gozik/internal/playlist"
	"github.com/gg582/gozik/internal/provider"
	"github.com/gotk3/gotk3/glib"
)

// grpcController adapts MainWindow to the grpcserver.Controller interface.
// All state-mutating methods schedule work on the GTK main thread.
type grpcController struct {
	mw *MainWindow
}

// ---------------------------------------------------------------------------
// Playback control
// ---------------------------------------------------------------------------

func (c *grpcController) Play()     { glib.IdleAdd(func() bool { c.mw.onPlay(); return false }) }
func (c *grpcController) Pause()    { glib.IdleAdd(func() bool { c.mw.onPause(); return false }) }
func (c *grpcController) Stop()     { glib.IdleAdd(func() bool { c.mw.onStop(); return false }) }
func (c *grpcController) Next()     { glib.IdleAdd(func() bool { c.mw.onNext(); return false }) }
func (c *grpcController) Previous() { glib.IdleAdd(func() bool { c.mw.onPrev(); return false }) }

func (c *grpcController) PlayIndex(idx int) {
	glib.IdleAdd(func() bool {
		if c.mw.listBox == nil || idx < 0 || idx >= len(c.mw.songs) {
			return false
		}
		c.mw.listBox.SelectRow(c.mw.listBox.GetRowAtIndex(idx))
		c.mw.onPlay()
		return false
	})
}

// ---------------------------------------------------------------------------
// Transport
// ---------------------------------------------------------------------------

func (c *grpcController) Seek(pos time.Duration) {
	glib.IdleAdd(func() bool {
		if c.mw.player != nil {
			_ = c.mw.player.Seek(pos)
		}
		return false
	})
}

func (c *grpcController) SetVolume(v float64) {
	glib.IdleAdd(func() bool {
		if c.mw.volumeScale != nil {
			c.mw.volumeScale.SetValue(v)
		}
		if c.mw.player != nil {
			c.mw.player.SetVolume(v)
		}
		if v > 0 && c.mw.muted {
			c.mw.muted = false
			c.mw.updateVolumeVisual()
		}
		c.mw.publishVolumeChanged()
		return false
	})
}

func (c *grpcController) GetVolume() float64 {
	if c.mw.volumeScale != nil {
		return c.mw.volumeScale.GetValue()
	}
	return 1.0
}

func (c *grpcController) SetMute(muted bool) {
	if muted == c.mw.muted {
		return
	}
	glib.IdleAdd(func() bool {
		c.mw.toggleMute()
		return false
	})
}

func (c *grpcController) GetMute() bool {
	return c.mw.muted
}

// ---------------------------------------------------------------------------
// Queue / playlist
// ---------------------------------------------------------------------------

func (c *grpcController) GetQueue() []models.Song {
	songs := make([]models.Song, len(c.mw.songs))
	copy(songs, c.mw.songs)
	return songs
}

func (c *grpcController) AddToQueue(paths []string) {
	glib.IdleAdd(func() bool {
		c.mw.LoadFiles(paths)
		c.mw.publishQueueChanged()
		return false
	})
}

func (c *grpcController) RemoveFromQueue(indices []int) {
	glib.IdleAdd(func() bool {
		if c.mw.listBox == nil {
			return false
		}
		// Remove from highest index to lowest so indices stay valid.
		sorted := make([]int, len(indices))
		copy(sorted, indices)
		sort.Sort(sort.Reverse(sort.IntSlice(sorted)))

		for _, idx := range sorted {
			if idx < 0 || idx >= len(c.mw.songs) {
				continue
			}
			if idx < len(c.mw.rows) && c.mw.rows[idx].row != nil {
				c.mw.listBox.Remove(c.mw.rows[idx].row)
			}
			c.mw.songs = append(c.mw.songs[:idx], c.mw.songs[idx+1:]...)
			c.mw.rows = append(c.mw.rows[:idx], c.mw.rows[idx+1:]...)
			if c.mw.selectedIdx == idx {
				c.mw.selectedIdx = -1
			} else if c.mw.selectedIdx > idx {
				c.mw.selectedIdx--
			}
			if c.mw.playingIdx == idx {
				c.mw.playingIdx = -1
			} else if c.mw.playingIdx > idx {
				c.mw.playingIdx--
			}
		}
		c.mw.refreshPlayingHighlight()
		c.mw.updateQueueHeader()
		c.mw.publishQueueChanged()
		return false
	})
}

func (c *grpcController) ClearQueue() {
	glib.IdleAdd(func() bool {
		c.mw.songs = c.mw.songs[:0]
		if c.mw.listBox != nil {
			for _, row := range c.mw.rows {
				if row.row != nil {
					c.mw.listBox.Remove(row.row)
				}
			}
		}
		c.mw.rows = c.mw.rows[:0]
		c.mw.selectedIdx = -1
		c.mw.playingIdx = -1
		c.mw.refreshPlayingHighlight()
		c.mw.updateQueueHeader()
		c.mw.publishQueueChanged()
		return false
	})
}

func (c *grpcController) LoadPlaylist(path string) {
	c.AddToQueue([]string{path})
}

func (c *grpcController) SavePlaylist(path string) {
	glib.IdleAdd(func() bool {
		if len(c.mw.songs) == 0 {
			return false
		}
		if err := playlist.Save(path, "", c.mw.songs); err != nil {
			log.Printf("gRPC SavePlaylist failed: %v", err)
		}
		return false
	})
}

// ---------------------------------------------------------------------------
// Modes
// ---------------------------------------------------------------------------

func (c *grpcController) SetRepeatMode(mode models.PlayMode) {
	glib.IdleAdd(func() bool {
		c.mw.playMode = mode
		c.mw.updateRepeatButton()
		c.mw.publishRepeatModeChanged()
		return false
	})
}

func (c *grpcController) GetRepeatMode() models.PlayMode {
	return c.mw.playMode
}

func (c *grpcController) SetShuffle(enabled bool) {
	glib.IdleAdd(func() bool {
		c.mw.shuffle = enabled
		c.mw.updateShuffleButton()
		c.mw.publishShuffleChanged()
		return false
	})
}

func (c *grpcController) GetShuffle() bool {
	return c.mw.shuffle
}

// ---------------------------------------------------------------------------
// Status
// ---------------------------------------------------------------------------

func (c *grpcController) GetStatus() grpcserver.Status {
	st := grpcserver.Status{
		CurrentIndex: c.mw.playingIdx,
		Volume:       c.GetVolume(),
		Muted:        c.mw.muted,
		RepeatMode:   c.mw.playMode,
		Shuffle:      c.mw.shuffle,
	}
	if c.mw.player != nil {
		st.Position = c.mw.player.Position()
		st.Length = c.mw.player.Length()
		switch {
		case c.mw.player.IsPlaying():
			st.PlaybackState = models.PlaybackStatePlaying
		case c.mw.player.IsPaused():
			st.PlaybackState = models.PlaybackStatePaused
		default:
			st.PlaybackState = models.PlaybackStateStopped
		}
	} else {
		st.PlaybackState = models.PlaybackStateStopped
	}
	return st
}

func (c *grpcController) GetCurrentTrack() grpcserver.CurrentTrack {
	idx := c.mw.playingIdx
	if idx < 0 || idx >= len(c.mw.songs) {
		return grpcserver.CurrentTrack{Index: -1}
	}
	return grpcserver.CurrentTrack{Index: idx, Song: c.mw.songs[idx]}
}

// ---------------------------------------------------------------------------
// CD
// ---------------------------------------------------------------------------

func (c *grpcController) GetCDDevices() []grpcserver.CDDevice {
	return []grpcserver.CDDevice{
		{Device: cdrom.DefaultDevice(), Supported: cdrom.IsSupported()},
	}
}

func (c *grpcController) ReadCD(device string) ([]cdrom.Track, error) {
	dev, err := cdrom.Open(device)
	if err != nil {
		return nil, err
	}
	defer dev.Close()
	return dev.ReadTOC()
}

func (c *grpcController) LoadCD(device string) {
	glib.IdleAdd(func() bool {
		if !cdrom.IsSupported() || device == "" {
			return false
		}
		go func() {
			dev, err := cdrom.Open(device)
			if err != nil {
				return
			}
			tracks, err := dev.ReadTOC()
			dev.Close()
			if err != nil {
				return
			}
			glib.IdleAdd(func() bool {
				for _, t := range tracks {
					if !t.IsAudio {
						continue
					}
					song := models.Song{
						Name:     fmt.Sprintf("CD Track %02d", t.Number),
						Device:   device,
						TrackNum: t.Number,
						IsCD:     true,
					}
					c.mw.songs = append(c.mw.songs, song)
					c.mw.appendSongToList(song)
				}
				c.mw.updateQueueHeader()
				c.mw.publishQueueChanged()
				return false
			})

			toc := cdrom.MusicBrainzTOC(tracks)
			if toc != "" {
				go func() {
					release, err := audioutils.SearchMusicBrainzDisc(toc)
					if err != nil {
						log.Printf("MusicBrainz disc lookup failed: %v", err)
						return
					}
					glib.IdleAdd(func() bool {
						c.mw.applyCDMetadata(device, release)
						return false
					})
				}()
			}
		}()
		return false
	})
}

// ---------------------------------------------------------------------------
// Music Provider
// ---------------------------------------------------------------------------

func (c *grpcController) ListProviders() []grpcserver.DiscoveredProvider {
	if c.mw.providerMgr == nil {
		return nil
	}
	providers := c.mw.providerMgr.Providers()
	out := make([]grpcserver.DiscoveredProvider, 0, len(providers))
	for _, p := range providers {
		out = append(out, grpcserver.DiscoveredProvider{
			ID:      p.ProviderID,
			Name:    p.DisplayName,
			Address: p.Address,
		})
	}
	return out
}

func (c *grpcController) SearchTracks(providerID, query string, limit int32) ([]*musicv1.Track, error) {
	if c.mw.providerMgr == nil {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return c.mw.providerMgr.SearchTracks(ctx, providerID, query, limit)
}

func (c *grpcController) SearchPlaylists(providerID, query string, limit int32) ([]*musicv1.Playlist, error) {
	if c.mw.providerMgr == nil {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return c.mw.providerMgr.SearchPlaylists(ctx, providerID, query, limit)
}

func (c *grpcController) GetProviderTrackDetails(providerID, trackID string) (*musicv1.Track, error) {
	if c.mw.providerMgr == nil {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return c.mw.providerMgr.GetTrackMetadata(ctx, providerID, trackID)
}

func (c *grpcController) ResolveProviderStream(providerID, trackID string) (string, map[string]string, error) {
	if c.mw.providerMgr == nil {
		return "", nil, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return c.mw.providerMgr.ResolveStream(ctx, providerID, trackID)
}

func (c *grpcController) GetProviderPlaylistDetails(providerID, playlistID string, limit int32) (*musicv1.Playlist, []*musicv1.Track, error) {
	if c.mw.providerMgr == nil {
		return nil, nil, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return c.mw.providerMgr.GetPlaylistDetails(ctx, providerID, playlistID, limit)
}

func (c *grpcController) AddProviderTrack(providerID, trackID string) {
	if c.mw.providerMgr == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		track, err := c.mw.providerMgr.GetTrackMetadata(ctx, providerID, trackID)
		cancel()
		if err != nil {
			log.Printf("gRPC AddProviderTrack failed: %v", err)
			return
		}
		glib.IdleAdd(func() bool {
			song := models.Song{
				Name:            track.Title,
				Artist:          providerArtists(track.Artists),
				Title:           track.Title,
				Album:           albumTitle(track.Album),
				Duration:        int(track.DurationMs / 1000),
				ProviderID:      providerID,
				ProviderName:    providerName(c.mw.providerMgr, providerID),
				ProviderTrackID: track.Id,
			}
			if len(track.Images) > 0 {
				song.CoverArtURL = track.Images[0].Url
			}
			c.mw.songs = append(c.mw.songs, song)
			c.mw.appendSongToList(song)
			c.mw.updateQueueHeader()
			c.mw.publishQueueChanged()
			return false
		})
	}()
}

func providerArtists(artists []*musicv1.Artist) string {
	names := make([]string, 0, len(artists))
	for _, a := range artists {
		if a.Name != "" {
			names = append(names, a.Name)
		}
	}
	return strings.Join(names, ", ")
}

func albumTitle(album *musicv1.Album) string {
	if album == nil {
		return ""
	}
	return album.Title
}

func providerName(mgr *provider.Manager, id string) string {
	for _, p := range mgr.Providers() {
		if p.ProviderID == id {
			return p.DisplayName
		}
	}
	return id
}

// ---------------------------------------------------------------------------
// Lifecycle
// ---------------------------------------------------------------------------

// grpcAddr returns the gRPC listen address from the environment, defaulting to
// localhost:50051 so only local clients can connect by default.
func grpcAddr() string {
	if addr := os.Getenv("GOZIK_GRPC_ADDR"); addr != "" {
		return addr
	}
	return "127.0.0.1:50051"
}

func (mw *MainWindow) initGRPC() {
	ctrl := &grpcController{mw: mw}
	s, err := grpcserver.NewServer(grpcAddr(), ctrl)
	if err != nil {
		log.Printf("Failed to start gRPC server: %v", err)
		return
	}
	mw.grpcServer = s
	mw.grpcEventPublisher = s
	s.Start()
}

func (mw *MainWindow) closeGRPC() {
	if mw.grpcServer != nil {
		mw.grpcServer.Close()
		mw.grpcServer = nil
		mw.grpcEventPublisher = nil
	}
}
