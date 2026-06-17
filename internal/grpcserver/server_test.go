package grpcserver

import (
	"context"
	"testing"
	"time"

	musicv1 "github.com/gg582/gozik/api/music/v1"
	playerv1 "github.com/gg582/gozik/api/player/v1"
	"github.com/gg582/gozik/internal/cdrom"
	"github.com/gg582/gozik/internal/models"
	"google.golang.org/grpc"
)

type fakeController struct {
	playing    bool
	paused     bool
	volume     float64
	muted      bool
	repeatMode models.PlayMode
	shuffle    bool
	queue      []models.Song
	played     int
	pausedN    int
	stopped    int
	nexted     int
	previoused int
	seeked     time.Duration
	volSet     float64
	muteSet    bool
	removed    []int
	loadedPath string
	savedPath  string
	repeatSet  models.PlayMode
	shuffleSet bool
	indexSet   int
}

func (f *fakeController) Play()                  { f.played++ }
func (f *fakeController) Pause()                 { f.pausedN++; f.playing = false; f.paused = true }
func (f *fakeController) Stop()                  { f.stopped++; f.playing = false; f.paused = false }
func (f *fakeController) Next()                  { f.nexted++ }
func (f *fakeController) Previous()              { f.previoused++ }
func (f *fakeController) PlayIndex(idx int)      { f.indexSet = idx }
func (f *fakeController) Seek(pos time.Duration) { f.seeked = pos }
func (f *fakeController) SetVolume(v float64)    { f.volSet = v; f.volume = v }
func (f *fakeController) GetVolume() float64     { return f.volume }
func (f *fakeController) SetMute(muted bool)     { f.muteSet = muted; f.muted = muted }
func (f *fakeController) GetMute() bool          { return f.muted }
func (f *fakeController) GetQueue() []models.Song {
	out := make([]models.Song, len(f.queue))
	copy(out, f.queue)
	return out
}
func (f *fakeController) AddToQueue(paths []string) {
	for _, p := range paths {
		f.queue = append(f.queue, models.Song{Name: p, Location: p})
	}
}
func (f *fakeController) RemoveFromQueue(indices []int) { f.removed = indices }
func (f *fakeController) ClearQueue()                   { f.queue = nil }
func (f *fakeController) LoadPlaylist(path string)      { f.loadedPath = path }
func (f *fakeController) SavePlaylist(path string)      { f.savedPath = path }
func (f *fakeController) SetRepeatMode(mode models.PlayMode) {
	f.repeatSet = mode
	f.repeatMode = mode
}
func (f *fakeController) GetRepeatMode() models.PlayMode { return f.repeatMode }
func (f *fakeController) SetShuffle(enabled bool)        { f.shuffleSet = enabled; f.shuffle = enabled }
func (f *fakeController) GetShuffle() bool               { return f.shuffle }
func (f *fakeController) GetStatus() Status {
	return Status{
		PlaybackState: models.PlaybackStateStopped,
		CurrentIndex:  -1,
		Volume:        f.volume,
		Muted:         f.muted,
		RepeatMode:    f.repeatMode,
		Shuffle:       f.shuffle,
	}
}
func (f *fakeController) GetCurrentTrack() CurrentTrack {
	return CurrentTrack{Index: -1}
}
func (f *fakeController) GetCDDevices() []CDDevice {
	return []CDDevice{{Device: "/dev/sr0", Supported: true}}
}
func (f *fakeController) ReadCD(device string) ([]cdrom.Track, error) {
	return []cdrom.Track{{Number: 1, IsAudio: true}}, nil
}
func (f *fakeController) LoadCD(device string) {}
func (f *fakeController) ListProviders() []DiscoveredProvider {
	return []DiscoveredProvider{{ID: "test", Name: "Test Provider", Address: "127.0.0.1:50000"}}
}
func (f *fakeController) SearchTracks(providerID, query string, limit int32) ([]*musicv1.Track, error) {
	return []*musicv1.Track{{Id: "t1", Title: query}}, nil
}
func (f *fakeController) SearchPlaylists(providerID, query string, limit int32) ([]*musicv1.Playlist, error) {
	return []*musicv1.Playlist{{Id: "p1", Title: query}}, nil
}
func (f *fakeController) GetProviderTrackDetails(providerID, trackID string) (*musicv1.Track, error) {
	return &musicv1.Track{Id: trackID, Title: "track"}, nil
}
func (f *fakeController) ResolveProviderStream(providerID, trackID string) (string, map[string]string, error) {
	return "http://stream", map[string]string{"x": "y"}, nil
}
func (f *fakeController) GetProviderPlaylistDetails(providerID, playlistID string, limit int32) (*musicv1.Playlist, []*musicv1.Track, error) {
	return &musicv1.Playlist{Id: playlistID}, []*musicv1.Track{{Id: "t1"}}, nil
}
func (f *fakeController) AddProviderTrack(providerID, trackID string) {}

func TestServerPlaybackControls(t *testing.T) {
	ctrl := &fakeController{volume: 0.8}
	srv, err := NewServer("127.0.0.1:0", ctrl)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	srv.Start()
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	conn, err := grpc.DialContext(ctx, srv.Addr(), grpc.WithInsecure(), grpc.WithBlock())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	client := playerv1.NewPlayerServiceClient(conn)

	if _, err := client.Play(ctx, &playerv1.Empty{}); err != nil {
		t.Fatalf("Play: %v", err)
	}
	if ctrl.played != 1 {
		t.Errorf("expected Play called once, got %d", ctrl.played)
	}

	if _, err := client.Pause(ctx, &playerv1.Empty{}); err != nil {
		t.Fatalf("Pause: %v", err)
	}
	if ctrl.pausedN != 1 {
		t.Errorf("expected Pause called once, got %d", ctrl.pausedN)
	}

	status, err := client.GetStatus(ctx, &playerv1.Empty{})
	if err != nil {
		t.Fatalf("GetStatus: %v", err)
	}
	if status.Volume != 0.8 {
		t.Errorf("expected volume 0.8, got %f", status.Volume)
	}
	if status.PlaybackState != playerv1.PlaybackState_PLAYBACK_STATE_STOPPED {
		t.Errorf("expected stopped state, got %v", status.PlaybackState)
	}
}

func TestServerQueueAndModes(t *testing.T) {
	ctrl := &fakeController{queue: []models.Song{{Name: "a", Location: "/a.flac"}}}
	srv, err := NewServer("127.0.0.1:0", ctrl)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	srv.Start()
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	conn, err := grpc.DialContext(ctx, srv.Addr(), grpc.WithInsecure(), grpc.WithBlock())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	client := playerv1.NewPlayerServiceClient(conn)

	if _, err := client.AddToQueue(ctx, &playerv1.QueueRequest{Paths: []string{"/b.flac"}}); err != nil {
		t.Fatalf("AddToQueue: %v", err)
	}
	if len(ctrl.queue) != 2 {
		t.Errorf("expected 2 tracks in queue, got %d", len(ctrl.queue))
	}

	queue, err := client.GetQueue(ctx, &playerv1.Empty{})
	if err != nil {
		t.Fatalf("GetQueue: %v", err)
	}
	if len(queue.Tracks) != 2 {
		t.Errorf("expected 2 tracks from GetQueue, got %d", len(queue.Tracks))
	}

	if _, err := client.SetRepeatMode(ctx, &playerv1.RepeatModeRequest{Mode: playerv1.RepeatMode_REPEAT_MODE_REPEAT_ALL}); err != nil {
		t.Fatalf("SetRepeatMode: %v", err)
	}
	if ctrl.repeatMode != models.PlayModeRepeatAll {
		t.Errorf("expected RepeatAll, got %v", ctrl.repeatMode)
	}

	mode, err := client.GetRepeatMode(ctx, &playerv1.Empty{})
	if err != nil {
		t.Fatalf("GetRepeatMode: %v", err)
	}
	if mode.Mode != playerv1.RepeatMode_REPEAT_MODE_REPEAT_ALL {
		t.Errorf("expected RepeatAll from GetRepeatMode, got %v", mode.Mode)
	}
}

func TestServerSubscribeEvents(t *testing.T) {
	ctrl := &fakeController{}
	srv, err := NewServer("127.0.0.1:0", ctrl)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	srv.Start()
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	conn, err := grpc.DialContext(ctx, srv.Addr(), grpc.WithInsecure(), grpc.WithBlock())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	client := playerv1.NewPlayerServiceClient(conn)
	stream, err := client.SubscribeEvents(ctx, &playerv1.SubscribeRequest{})
	if err != nil {
		t.Fatalf("SubscribeEvents: %v", err)
	}

	done := make(chan struct{})
	var received playerv1.EventType
	go func() {
		ev, err := stream.Recv()
		if err != nil {
			close(done)
			return
		}
		received = ev.Type
		close(done)
	}()

	// Give the stream a moment to establish before publishing.
	time.Sleep(100 * time.Millisecond)
	srv.Publish(&playerv1.PlayerEvent{Type: playerv1.EventType_EVENT_TYPE_PLAYBACK_STATE_CHANGED})

	select {
	case <-done:
		if received != playerv1.EventType_EVENT_TYPE_PLAYBACK_STATE_CHANGED {
			t.Errorf("unexpected event type: %v", received)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}
