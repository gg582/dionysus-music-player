package provider

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	musicv1 "github.com/gg582/gozik/api/music/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// DiscoveredProvider holds metadata and a live gRPC connection for a provider
// found via port scan.
type DiscoveredProvider struct {
	Address      string
	ProviderID   string
	DisplayName  string
	AuthStatus   musicv1.AuthStatus
	Capabilities []musicv1.ProviderCapability
	conn         *grpc.ClientConn
	client       musicv1.MusicProviderServiceClient
}

// Manager scans for local MusicProviderService instances and exposes thin
// wrappers for the UI layer. All RPCs must be invoked from background
// goroutines to avoid blocking the GTK main loop.
type Manager struct {
	providersMu sync.RWMutex
	providers   []DiscoveredProvider
}

// NewManager creates a manager in discovery-ready mode.
func NewManager() *Manager {
	return &Manager{}
}

// Close tears down all provider gRPC connections.
func (m *Manager) Close() error {
	m.providersMu.Lock()
	defer m.providersMu.Unlock()
	var firstErr error
	for i := range m.providers {
		if m.providers[i].conn != nil {
			if err := m.providers[i].conn.Close(); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}
	m.providers = nil
	return firstErr
}

// DiscoverServices scans localhost ports 50000–50100 for gRPC services that
// implement MusicProviderService and stores live connections to them.
func (m *Manager) DiscoverServices(ctx context.Context) {
	const startPort = 50000
	const endPort = 50100

	var mu sync.Mutex
	var found []DiscoveredProvider
	var wg sync.WaitGroup

	for port := startPort; port <= endPort; port++ {
		wg.Add(1)
		go func(p int) {
			defer wg.Done()
			addr := fmt.Sprintf("127.0.0.1:%d", p)
			if svc, ok := m.probeService(ctx, addr); ok {
				mu.Lock()
				found = append(found, svc)
				mu.Unlock()
			}
		}(port)
	}
	wg.Wait()

	m.providersMu.Lock()
	// Close connections for providers that disappeared.
	old := m.providers
	m.providers = found
	m.providersMu.Unlock()

	for _, o := range old {
		stillThere := false
		for _, n := range found {
			if n.Address == o.Address {
				stillThere = true
				break
			}
		}
		if !stillThere && o.conn != nil {
			_ = o.conn.Close()
		}
	}
}

func (m *Manager) probeService(ctx context.Context, addr string) (DiscoveredProvider, bool) {
	if !isPortOpen(addr, 300*time.Millisecond) {
		return DiscoveredProvider{}, false
	}

	dialCtx, cancel := context.WithTimeout(ctx, 800*time.Millisecond)
	defer cancel()

	conn, err := grpc.DialContext(dialCtx, addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		return DiscoveredProvider{}, false
	}

	client := musicv1.NewMusicProviderServiceClient(conn)
	metaCtx, metaCancel := context.WithTimeout(ctx, 800*time.Millisecond)
	defer metaCancel()

	res, err := client.GetProviderMetadata(metaCtx, &musicv1.GetProviderMetadataRequest{})
	if err != nil {
		_ = conn.Close()
		return DiscoveredProvider{}, false
	}

	return DiscoveredProvider{
		Address:      addr,
		ProviderID:   res.ProviderId,
		DisplayName:  res.DisplayName,
		AuthStatus:   res.AuthStatus,
		Capabilities: res.Capabilities,
		conn:         conn,
		client:       client,
	}, true
}

func isPortOpen(addr string, timeout time.Duration) bool {
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// Providers returns a snapshot of the currently discovered providers.
// The returned slice is a copy; callers should not modify it.
func (m *Manager) Providers() []DiscoveredProvider {
	m.providersMu.RLock()
	defer m.providersMu.RUnlock()
	out := make([]DiscoveredProvider, len(m.providers))
	copy(out, m.providers)
	return out
}

// Count returns the number of currently discovered providers.
func (m *Manager) Count() int {
	m.providersMu.RLock()
	defer m.providersMu.RUnlock()
	return len(m.providers)
}

// providerByID returns a pointer to the provider with the given provider ID.
// The pointer is only valid while providersMu is held.
func (m *Manager) providerByID(id string) *DiscoveredProvider {
	for i := range m.providers {
		if m.providers[i].ProviderID == id {
			return &m.providers[i]
		}
	}
	return nil
}

// providerByAddress returns a pointer to the provider at the given address.
func (m *Manager) providerByAddress(addr string) *DiscoveredProvider {
	for i := range m.providers {
		if m.providers[i].Address == addr {
			return &m.providers[i]
		}
	}
	return nil
}

// SearchTracks searches a specific provider for tracks.
func (m *Manager) SearchTracks(ctx context.Context, providerID, query string, limit int32) ([]*musicv1.Track, error) {
	m.providersMu.RLock()
	p := m.providerByID(providerID)
	m.providersMu.RUnlock()
	if p == nil || p.client == nil {
		return nil, fmt.Errorf("provider %s not available", providerID)
	}
	if limit <= 0 {
		limit = 20
	}
	res, err := p.client.Search(ctx, &musicv1.SearchRequest{
		Query: query,
		Types: []musicv1.MediaType{musicv1.MediaType_MEDIA_TYPE_TRACK},
		Limit: limit,
	})
	if err != nil {
		return nil, err
	}
	return res.Tracks, nil
}

// SearchPlaylists searches a specific provider for playlists.
func (m *Manager) SearchPlaylists(ctx context.Context, providerID, query string, limit int32) ([]*musicv1.Playlist, error) {
	m.providersMu.RLock()
	p := m.providerByID(providerID)
	m.providersMu.RUnlock()
	if p == nil || p.client == nil {
		return nil, fmt.Errorf("provider %s not available", providerID)
	}
	if limit <= 0 {
		limit = 20
	}
	res, err := p.client.Search(ctx, &musicv1.SearchRequest{
		Query: query,
		Types: []musicv1.MediaType{musicv1.MediaType_MEDIA_TYPE_PLAYLIST},
		Limit: limit,
	})
	if err != nil {
		return nil, err
	}
	return res.Playlists, nil
}

// GetTrackMetadata fetches detailed metadata for a track.
func (m *Manager) GetTrackMetadata(ctx context.Context, providerID, trackID string) (*musicv1.Track, error) {
	m.providersMu.RLock()
	p := m.providerByID(providerID)
	m.providersMu.RUnlock()
	if p == nil || p.client == nil {
		return nil, fmt.Errorf("provider %s not available", providerID)
	}
	res, err := p.client.GetTrackDetails(ctx, &musicv1.GetTrackDetailsRequest{TrackId: trackID})
	if err != nil {
		return nil, err
	}
	return res.Track, nil
}

// ResolveStream resolves a playable stream URL for a track.
func (m *Manager) ResolveStream(ctx context.Context, providerID, trackID string) (string, map[string]string, error) {
	m.providersMu.RLock()
	p := m.providerByID(providerID)
	m.providersMu.RUnlock()
	if p == nil || p.client == nil {
		return "", nil, fmt.Errorf("provider %s not available", providerID)
	}
	res, err := p.client.ResolveStream(ctx, &musicv1.ResolveStreamRequest{
		TrackId:          trackID,
		PreferredQuality: musicv1.AudioQuality_AUDIO_QUALITY_HIGH,
	})
	if err != nil {
		return "", nil, err
	}
	return res.StreamUrl, res.Headers, nil
}

// GetPlaylistDetails fetches a playlist and its tracks.
func (m *Manager) GetPlaylistDetails(ctx context.Context, providerID, playlistID string, limit int32) (*musicv1.Playlist, []*musicv1.Track, error) {
	m.providersMu.RLock()
	p := m.providerByID(providerID)
	m.providersMu.RUnlock()
	if p == nil || p.client == nil {
		return nil, nil, fmt.Errorf("provider %s not available", providerID)
	}
	if limit <= 0 {
		limit = 100
	}
	res, err := p.client.GetPlaylistDetails(ctx, &musicv1.GetPlaylistDetailsRequest{
		PlaylistId: playlistID,
		Limit:      limit,
	})
	if err != nil {
		return nil, nil, err
	}
	return res.Playlist, res.Tracks, nil
}

// InitiateAuth starts the provider's OAuth flow. Optional params are passed
// straight through to the provider (e.g. spotify uses params["client_id"]).
func (m *Manager) InitiateAuth(ctx context.Context, providerID string, params map[string]string) (*musicv1.InitiateAuthResponse, error) {
	m.providersMu.RLock()
	p := m.providerByID(providerID)
	m.providersMu.RUnlock()
	if p == nil || p.client == nil {
		return nil, fmt.Errorf("provider %s not available", providerID)
	}
	return p.client.InitiateAuth(ctx, &musicv1.InitiateAuthRequest{Params: params})
}

// CompleteAuth completes the provider's OAuth flow.
func (m *Manager) CompleteAuth(ctx context.Context, providerID string, params map[string]string) (*musicv1.CompleteAuthResponse, error) {
	m.providersMu.RLock()
	p := m.providerByID(providerID)
	m.providersMu.RUnlock()
	if p == nil || p.client == nil {
		return nil, fmt.Errorf("provider %s not available", providerID)
	}
	return p.client.CompleteAuth(ctx, &musicv1.CompleteAuthRequest{Params: params})
}
