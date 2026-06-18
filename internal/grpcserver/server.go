// Package grpcserver exposes a gRPC control surface for Gozik.
// It is intentionally decoupled from the UI: the Controller and EventPublisher
// interfaces are implemented by the ui package adapter.
package grpcserver

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	musicv1 "github.com/gg582/gozik/api/music/v1"
	playerv1 "github.com/gg582/gozik/api/player/v1"
	"github.com/gg582/gozik/internal/cdrom"
	"github.com/gg582/gozik/internal/models"
	"google.golang.org/grpc"
)

// Controller is the minimal surface the gRPC server needs from the application.
// Implementations are responsible for marshalling state-mutating calls to the
// GTK main thread (e.g. via glib.IdleAdd).
type Controller interface {
	// Playback control
	Play()
	Pause()
	Stop()
	Next()
	Previous()
	PlayIndex(idx int)

	// Transport
	Seek(pos time.Duration)
	SetVolume(v float64)
	GetVolume() float64
	SetMute(muted bool)
	GetMute() bool

	// Queue / playlist
	GetQueue() []models.Song
	AddToQueue(paths []string)
	RemoveFromQueue(indices []int)
	ClearQueue()
	LoadPlaylist(path string)
	SavePlaylist(path string)

	// Modes
	SetRepeatMode(mode models.PlayMode)
	GetRepeatMode() models.PlayMode
	SetShuffle(enabled bool)
	GetShuffle() bool

	// Status
	GetStatus() Status
	GetCurrentTrack() CurrentTrack

	// CD
	GetCDDevices() []CDDevice
	ReadCD(device string) ([]cdrom.Track, error)
	LoadCD(device string)

	// Music Provider
	ListProviders() []DiscoveredProvider
	SearchTracks(providerID, query string, limit int32) ([]*musicv1.Track, error)
	SearchPlaylists(providerID, query string, limit int32) ([]*musicv1.Playlist, error)
	GetProviderTrackDetails(providerID, trackID string) (*musicv1.Track, error)
	ResolveProviderStream(providerID, trackID string) (string, map[string]string, error)
	GetProviderPlaylistDetails(providerID, playlistID string, limit int32) (*musicv1.Playlist, []*musicv1.Track, error)
	AddProviderTrack(providerID, trackID string) error
}

// CDDevice describes a single CD-ROM device.
type CDDevice struct {
	Device    string
	Supported bool
}

// DiscoveredProvider describes a discovered music provider service.
type DiscoveredProvider struct {
	ID      string
	Name    string
	Address string
}

// Status is the current playback/transport state returned by Controller.GetStatus.
type Status struct {
	PlaybackState models.PlaybackState
	CurrentIndex  int
	Position      time.Duration
	Length        time.Duration
	Volume        float64
	Muted         bool
	RepeatMode    models.PlayMode
	Shuffle       bool
}

// CurrentTrack is the currently loaded/playing track returned by Controller.GetCurrentTrack.
type CurrentTrack struct {
	Index int
	Song  models.Song
}

// EventPublisher is implemented by the gRPC server and consumed by the UI so
// that playback/state changes can be pushed to connected gRPC clients.
type EventPublisher interface {
	Publish(event *playerv1.PlayerEvent)
}

// Server wraps a gRPC server exposing the PlayerService.
type Server struct {
	playerv1.UnimplementedPlayerServiceServer

	grpcServer *grpc.Server
	listener   net.Listener
	ctrl       Controller

	subMu       sync.RWMutex
	subscribers map[*subscriber]struct{}
}

type subscriber struct {
	ch chan *playerv1.PlayerEvent
}

// NewServer creates a gRPC server that will listen on addr and use ctrl for
// all application interaction.
func NewServer(addr string, ctrl Controller) (*Server, error) {
	if addr == "" {
		addr = ":50051"
	}
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("listen %s: %w", addr, err)
	}

	s := &Server{
		listener:    lis,
		ctrl:        ctrl,
		subscribers: make(map[*subscriber]struct{}),
	}
	s.grpcServer = grpc.NewServer()
	playerv1.RegisterPlayerServiceServer(s.grpcServer, s)
	return s, nil
}

// Addr returns the address the server is listening on.
func (s *Server) Addr() string {
	if s.listener == nil {
		return ""
	}
	return s.listener.Addr().String()
}

// Start runs the gRPC server in a background goroutine.
func (s *Server) Start() {
	go func() {
		_ = s.grpcServer.Serve(s.listener)
	}()
}

// Close stops the gRPC server.
func (s *Server) Close() {
	if s.grpcServer != nil {
		s.grpcServer.Stop()
	}
}

// Publish pushes an event to all connected subscribers.
func (s *Server) Publish(event *playerv1.PlayerEvent) {
	s.subMu.RLock()
	subs := make([]*subscriber, 0, len(s.subscribers))
	for sub := range s.subscribers {
		subs = append(subs, sub)
	}
	s.subMu.RUnlock()

	for _, sub := range subs {
		select {
		case sub.ch <- event:
		default:
			// Subscriber is slow; drop the event to avoid blocking the UI.
		}
	}
}

func (s *Server) subscribe() *subscriber {
	sub := &subscriber{ch: make(chan *playerv1.PlayerEvent, 16)}
	s.subMu.Lock()
	s.subscribers[sub] = struct{}{}
	s.subMu.Unlock()
	return sub
}

func (s *Server) unsubscribe(sub *subscriber) {
	s.subMu.Lock()
	delete(s.subscribers, sub)
	s.subMu.Unlock()
	close(sub.ch)
}

// ---------------------------------------------------------------------------
// Playback control
// ---------------------------------------------------------------------------

func (s *Server) Play(ctx context.Context, _ *playerv1.Empty) (*playerv1.Empty, error) {
	s.ctrl.Play()
	return &playerv1.Empty{}, nil
}

func (s *Server) Pause(ctx context.Context, _ *playerv1.Empty) (*playerv1.Empty, error) {
	s.ctrl.Pause()
	return &playerv1.Empty{}, nil
}

func (s *Server) Stop(ctx context.Context, _ *playerv1.Empty) (*playerv1.Empty, error) {
	s.ctrl.Stop()
	return &playerv1.Empty{}, nil
}

func (s *Server) Next(ctx context.Context, _ *playerv1.Empty) (*playerv1.Empty, error) {
	s.ctrl.Next()
	return &playerv1.Empty{}, nil
}

func (s *Server) Previous(ctx context.Context, _ *playerv1.Empty) (*playerv1.Empty, error) {
	s.ctrl.Previous()
	return &playerv1.Empty{}, nil
}

func (s *Server) PlayIndex(ctx context.Context, req *playerv1.PlayIndexRequest) (*playerv1.Empty, error) {
	s.ctrl.PlayIndex(int(req.Index))
	return &playerv1.Empty{}, nil
}

// ---------------------------------------------------------------------------
// Transport
// ---------------------------------------------------------------------------

func (s *Server) Seek(ctx context.Context, req *playerv1.Position) (*playerv1.Empty, error) {
	s.ctrl.Seek(time.Duration(req.PositionMs) * time.Millisecond)
	return &playerv1.Empty{}, nil
}

func (s *Server) SetVolume(ctx context.Context, req *playerv1.Volume) (*playerv1.Empty, error) {
	s.ctrl.SetVolume(req.Volume)
	return &playerv1.Empty{}, nil
}

func (s *Server) GetVolume(ctx context.Context, _ *playerv1.Empty) (*playerv1.Volume, error) {
	return &playerv1.Volume{Volume: s.ctrl.GetVolume()}, nil
}

func (s *Server) SetMute(ctx context.Context, req *playerv1.Mute) (*playerv1.Empty, error) {
	s.ctrl.SetMute(req.Muted)
	return &playerv1.Empty{}, nil
}

func (s *Server) GetMute(ctx context.Context, _ *playerv1.Empty) (*playerv1.Mute, error) {
	return &playerv1.Mute{Muted: s.ctrl.GetMute()}, nil
}

// ---------------------------------------------------------------------------
// Queue / playlist
// ---------------------------------------------------------------------------

func (s *Server) GetQueue(ctx context.Context, _ *playerv1.Empty) (*playerv1.Queue, error) {
	songs := s.ctrl.GetQueue()
	status := s.ctrl.GetStatus()
	queue := &playerv1.Queue{
		Tracks:       make([]*playerv1.Track, 0, len(songs)),
		CurrentIndex: int32(status.CurrentIndex),
	}
	for i, song := range songs {
		queue.Tracks = append(queue.Tracks, SongToProto(i, song))
	}
	return queue, nil
}

func (s *Server) AddToQueue(ctx context.Context, req *playerv1.QueueRequest) (*playerv1.Empty, error) {
	s.ctrl.AddToQueue(req.Paths)
	return &playerv1.Empty{}, nil
}

func (s *Server) RemoveFromQueue(ctx context.Context, req *playerv1.RemoveRequest) (*playerv1.Empty, error) {
	indices := make([]int, 0, len(req.Indices))
	for _, idx := range req.Indices {
		indices = append(indices, int(idx))
	}
	s.ctrl.RemoveFromQueue(indices)
	return &playerv1.Empty{}, nil
}

func (s *Server) ClearQueue(ctx context.Context, _ *playerv1.Empty) (*playerv1.Empty, error) {
	s.ctrl.ClearQueue()
	return &playerv1.Empty{}, nil
}

func (s *Server) LoadPlaylist(ctx context.Context, req *playerv1.PlaylistRequest) (*playerv1.Empty, error) {
	s.ctrl.LoadPlaylist(req.Path)
	return &playerv1.Empty{}, nil
}

func (s *Server) SavePlaylist(ctx context.Context, req *playerv1.PlaylistRequest) (*playerv1.Empty, error) {
	s.ctrl.SavePlaylist(req.Path)
	return &playerv1.Empty{}, nil
}

// ---------------------------------------------------------------------------
// Modes
// ---------------------------------------------------------------------------

func (s *Server) SetRepeatMode(ctx context.Context, req *playerv1.RepeatModeRequest) (*playerv1.Empty, error) {
	s.ctrl.SetRepeatMode(protoToRepeatMode(req.Mode))
	return &playerv1.Empty{}, nil
}

func (s *Server) GetRepeatMode(ctx context.Context, _ *playerv1.Empty) (*playerv1.RepeatModeRequest, error) {
	return &playerv1.RepeatModeRequest{Mode: repeatModeToProto(s.ctrl.GetRepeatMode())}, nil
}

func (s *Server) SetShuffle(ctx context.Context, req *playerv1.ShuffleRequest) (*playerv1.Empty, error) {
	s.ctrl.SetShuffle(req.Enabled)
	return &playerv1.Empty{}, nil
}

func (s *Server) GetShuffle(ctx context.Context, _ *playerv1.Empty) (*playerv1.ShuffleRequest, error) {
	return &playerv1.ShuffleRequest{Enabled: s.ctrl.GetShuffle()}, nil
}

// ---------------------------------------------------------------------------
// Status
// ---------------------------------------------------------------------------

func (s *Server) GetStatus(ctx context.Context, _ *playerv1.Empty) (*playerv1.Status, error) {
	return StatusToProto(s.ctrl.GetStatus()), nil
}

func (s *Server) GetCurrentTrack(ctx context.Context, _ *playerv1.Empty) (*playerv1.Track, error) {
	ct := s.ctrl.GetCurrentTrack()
	if ct.Index < 0 {
		return &playerv1.Track{}, nil
	}
	return SongToProto(ct.Index, ct.Song), nil
}

// ---------------------------------------------------------------------------
// CD
// ---------------------------------------------------------------------------

func (s *Server) GetCDDevices(ctx context.Context, _ *playerv1.GetCDDevicesRequest) (*playerv1.GetCDDevicesResponse, error) {
	resp := &playerv1.GetCDDevicesResponse{Devices: make([]*playerv1.CDDevice, 0)}
	for _, d := range s.ctrl.GetCDDevices() {
		resp.Devices = append(resp.Devices, &playerv1.CDDevice{
			Device:    d.Device,
			Supported: d.Supported,
		})
	}
	return resp, nil
}

func (s *Server) ReadCD(ctx context.Context, req *playerv1.ReadCDRequest) (*playerv1.ReadCDResponse, error) {
	tracks, err := s.ctrl.ReadCD(req.Device)
	if err != nil {
		return nil, err
	}
	resp := &playerv1.ReadCDResponse{
		Device: req.Device,
		Tracks: make([]*playerv1.CDTrack, 0, len(tracks)),
	}
	for _, t := range tracks {
		resp.Tracks = append(resp.Tracks, &playerv1.CDTrack{
			Number:       int32(t.Number),
			StartLba:     int32(t.StartLBA),
			EndLba:       int32(t.EndLBA),
			LengthFrames: int32(t.Length),
			IsAudio:      t.IsAudio,
		})
	}
	return resp, nil
}

func (s *Server) LoadCD(ctx context.Context, req *playerv1.LoadCDRequest) (*playerv1.Empty, error) {
	s.ctrl.LoadCD(req.Device)
	return &playerv1.Empty{}, nil
}

// ---------------------------------------------------------------------------
// Music Provider
// ---------------------------------------------------------------------------

func (s *Server) ListProviders(ctx context.Context, _ *playerv1.ListProvidersRequest) (*playerv1.ListProvidersResponse, error) {
	providers := s.ctrl.ListProviders()
	resp := &playerv1.ListProvidersResponse{Providers: make([]*playerv1.ProviderInfo, 0, len(providers))}
	for _, p := range providers {
		resp.Providers = append(resp.Providers, &playerv1.ProviderInfo{
			Id:      p.ID,
			Name:    p.Name,
			Address: p.Address,
		})
	}
	return resp, nil
}

func (s *Server) SearchTracks(ctx context.Context, req *playerv1.SearchTracksRequest) (*playerv1.SearchTracksResponse, error) {
	tracks, err := s.ctrl.SearchTracks(req.ProviderId, req.Query, req.Limit)
	if err != nil {
		return nil, err
	}
	return &playerv1.SearchTracksResponse{Tracks: tracks}, nil
}

func (s *Server) SearchPlaylists(ctx context.Context, req *playerv1.SearchPlaylistsRequest) (*playerv1.SearchPlaylistsResponse, error) {
	playlists, err := s.ctrl.SearchPlaylists(req.ProviderId, req.Query, req.Limit)
	if err != nil {
		return nil, err
	}
	return &playerv1.SearchPlaylistsResponse{Playlists: playlists}, nil
}

func (s *Server) GetProviderTrackDetails(ctx context.Context, req *playerv1.ProviderTrackRequest) (*playerv1.GetProviderTrackDetailsResponse, error) {
	track, err := s.ctrl.GetProviderTrackDetails(req.ProviderId, req.TrackId)
	if err != nil {
		return nil, err
	}
	return &playerv1.GetProviderTrackDetailsResponse{Track: track}, nil
}

func (s *Server) ResolveProviderStream(ctx context.Context, req *playerv1.ProviderTrackRequest) (*playerv1.ResolveProviderStreamResponse, error) {
	url, headers, err := s.ctrl.ResolveProviderStream(req.ProviderId, req.TrackId)
	if err != nil {
		return nil, err
	}
	return &playerv1.ResolveProviderStreamResponse{StreamUrl: url, Headers: headers}, nil
}

func (s *Server) GetProviderPlaylistDetails(ctx context.Context, req *playerv1.GetProviderPlaylistDetailsRequest) (*playerv1.GetProviderPlaylistDetailsResponse, error) {
	playlist, tracks, err := s.ctrl.GetProviderPlaylistDetails(req.ProviderId, req.PlaylistId, req.Limit)
	if err != nil {
		return nil, err
	}
	return &playerv1.GetProviderPlaylistDetailsResponse{Playlist: playlist, Tracks: tracks}, nil
}

func (s *Server) AddProviderTrack(ctx context.Context, req *playerv1.AddProviderTrackRequest) (*playerv1.Empty, error) {
	if err := s.ctrl.AddProviderTrack(req.ProviderId, req.TrackId); err != nil {
		return nil, err
	}
	return &playerv1.Empty{}, nil
}

// ---------------------------------------------------------------------------
// Events
// ---------------------------------------------------------------------------

func (s *Server) SubscribeEvents(req *playerv1.SubscribeRequest, stream playerv1.PlayerService_SubscribeEventsServer) error {
	sub := s.subscribe()
	defer s.unsubscribe(sub)

	for {
		select {
		case event := <-sub.ch:
			if event == nil {
				return nil
			}
			if err := stream.Send(event); err != nil {
				return err
			}
		case <-stream.Context().Done():
			return stream.Context().Err()
		}
	}
}

// ---------------------------------------------------------------------------
// Conversion helpers
// ---------------------------------------------------------------------------

func SongToProto(idx int, s models.Song) *playerv1.Track {
	return &playerv1.Track{
		Index:           int32(idx),
		Name:            s.Name,
		Location:        s.Location,
		Device:          s.Device,
		TrackNum:        int32(s.TrackNum),
		IsCd:            s.IsCD,
		Artist:          s.Artist,
		Title:           s.Title,
		Album:           s.Album,
		AlbumYear:       s.AlbumYear,
		DurationSeconds: int32(s.Duration),
		CoverArtUrl:     s.CoverArtURL,
		StartOffset:     int32(s.StartOffset),
		EndOffset:       int32(s.EndOffset),
		ProviderId:      s.ProviderID,
		ProviderName:    s.ProviderName,
		ProviderTrackId: s.ProviderTrackID,
		StreamUrl:       s.StreamURL,
	}
}

func StatusToProto(st Status) *playerv1.Status {
	return &playerv1.Status{
		PlaybackState: playbackStateToProto(st.PlaybackState),
		CurrentIndex:  int32(st.CurrentIndex),
		PositionMs:    st.Position.Milliseconds(),
		LengthMs:      st.Length.Milliseconds(),
		Volume:        st.Volume,
		Muted:         st.Muted,
		RepeatMode:    repeatModeToProto(st.RepeatMode),
		Shuffle:       st.Shuffle,
	}
}

func playbackStateToProto(state models.PlaybackState) playerv1.PlaybackState {
	switch state {
	case models.PlaybackStatePlaying:
		return playerv1.PlaybackState_PLAYBACK_STATE_PLAYING
	case models.PlaybackStatePaused:
		return playerv1.PlaybackState_PLAYBACK_STATE_PAUSED
	case models.PlaybackStateStopped:
		return playerv1.PlaybackState_PLAYBACK_STATE_STOPPED
	default:
		return playerv1.PlaybackState_PLAYBACK_STATE_UNSPECIFIED
	}
}

func repeatModeToProto(mode models.PlayMode) playerv1.RepeatMode {
	switch mode {
	case models.PlayModeSequential:
		return playerv1.RepeatMode_REPEAT_MODE_SEQUENTIAL
	case models.PlayModeRepeatAll:
		return playerv1.RepeatMode_REPEAT_MODE_REPEAT_ALL
	case models.PlayModeRepeatOne:
		return playerv1.RepeatMode_REPEAT_MODE_REPEAT_ONE
	default:
		return playerv1.RepeatMode_REPEAT_MODE_UNSPECIFIED
	}
}

func protoToRepeatMode(mode playerv1.RepeatMode) models.PlayMode {
	switch mode {
	case playerv1.RepeatMode_REPEAT_MODE_SEQUENTIAL:
		return models.PlayModeSequential
	case playerv1.RepeatMode_REPEAT_MODE_REPEAT_ALL:
		return models.PlayModeRepeatAll
	case playerv1.RepeatMode_REPEAT_MODE_REPEAT_ONE:
		return models.PlayModeRepeatOne
	default:
		return models.PlayModeSequential
	}
}
