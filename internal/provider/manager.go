package provider

import (
	"context"
	"fmt"
	"time"

	musicv1 "github.com/gg582/gozik/api/music/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Manager holds gRPC connections to the plugin coordinator and exposes
// helper methods for the UI layer. All RPCs are thin wrappers; callers must
// invoke them from background goroutines to avoid blocking the GTK main loop.
type Manager struct {
	linkClient musicv1.ProviderLinkServiceClient
	conn       *grpc.ClientConn
}

// NewManager dials the plugin coordinator at targetAddr.
// For a local subprocess, targetAddr is typically "localhost:<port>" or a unix socket.
func NewManager(targetAddr string) (*Manager, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := grpc.DialContext(ctx, targetAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		return nil, fmt.Errorf("dial plugin coordinator: %w", err)
	}

	return &Manager{
		linkClient: musicv1.NewProviderLinkServiceClient(conn),
		conn:       conn,
	}, nil
}

// Close tears down the underlying gRPC connection.
func (m *Manager) Close() error {
	if m.conn != nil {
		return m.conn.Close()
	}
	return nil
}

// CountLinkedPlugins returns the number of providers whose status is LINKED.
func (m *Manager) CountLinkedPlugins(ctx context.Context) (int, error) {
	res, err := m.linkClient.ListAvailablePlugins(ctx, &musicv1.ListAvailablePluginsRequest{})
	if err != nil {
		return 0, err
	}

	count := 0
	for _, p := range res.Plugins {
		if p.Status == musicv1.PluginStatus_PLUGIN_STATUS_LINKED {
			count++
		}
	}
	return count, nil
}

// ListAvailablePlugins fetches the full plugin list from the coordinator.
func (m *Manager) ListAvailablePlugins(ctx context.Context) (*musicv1.ListAvailablePluginsResponse, error) {
	return m.linkClient.ListAvailablePlugins(ctx, &musicv1.ListAvailablePluginsRequest{})
}

// InitiateLink starts the OAuth/link flow for a specific provider.
func (m *Manager) InitiateLink(ctx context.Context, providerID string) (*musicv1.InitiateLinkResponse, error) {
	return m.linkClient.InitiateLink(ctx, &musicv1.InitiateLinkRequest{ProviderId: providerID})
}

// CompleteFirebaseLink finalizes the Firebase credential binding.
func (m *Manager) CompleteFirebaseLink(ctx context.Context, req *musicv1.CompleteFirebaseLinkRequest) (*musicv1.CompleteFirebaseLinkResponse, error) {
	return m.linkClient.CompleteFirebaseLink(ctx, req)
}
