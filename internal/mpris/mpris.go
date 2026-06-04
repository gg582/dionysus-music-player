package mpris

import (
	"fmt"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/prop"
)

const (
	baseInterface   = "org.mpris.MediaPlayer2"
	playerInterface = "org.mpris.MediaPlayer2.Player"
	objectPath      = "/org/mpris/MediaPlayer2"
)

// Controller provides the minimal surface the MPRIS server needs from the
// application.  All methods will be called from the DBus event loop and may
// originate from external clients (media keys, GNOME Shell, etc.).
type Controller interface {
	Play()
	Pause()
	Stop()
	Next()
	Previous()
	IsPlaying() bool
	IsPaused() bool
	CurrentTitle() string
	CurrentArtist() string
	CurrentAlbum() string
	CurrentLengthMicros() int64
}

// Server exposes an MPRIS2 interface on the session bus.
type Server struct {
	conn  *dbus.Conn
	props *prop.Properties
}

// NewServer registers the MPRIS2 service on the session bus.
// appID should be a unique identifier like "Gozik".
func NewServer(appID string, ctrl Controller) (*Server, error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, fmt.Errorf("dbus session: %w", err)
	}

	name := fmt.Sprintf("%s.%s", baseInterface, appID)
	reply, err := conn.RequestName(name, dbus.NameFlagDoNotQueue)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("request name: %w", err)
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		conn.Close()
		return nil, fmt.Errorf("bus name already taken")
	}

	s := &Server{conn: conn}

	// Export methods on the controller.
	conn.Export(ctrl, dbus.ObjectPath(objectPath), baseInterface)
	conn.Export(ctrl, dbus.ObjectPath(objectPath), playerInterface)

	// Minimal root interface properties.
	rootProps := map[string]*prop.Prop{
		"Identity":        {Value: appID, Writable: false, Emit: prop.EmitTrue},
		"DesktopEntry":    {Value: "gozik", Writable: false, Emit: prop.EmitTrue},
		"CanQuit":         {Value: true, Writable: false, Emit: prop.EmitTrue},
		"CanRaise":        {Value: false, Writable: false, Emit: prop.EmitTrue},
		"HasTrackList":    {Value: false, Writable: false, Emit: prop.EmitTrue},
		"SupportedUriSchemes": {Value: []string{"file", "http", "https"}, Writable: false, Emit: prop.EmitTrue},
		"SupportedMimeTypes":  {Value: []string{}, Writable: false, Emit: prop.EmitTrue},
	}

	// Player interface properties.
	playerProps := map[string]*prop.Prop{
		"PlaybackStatus": {Value: "Stopped", Writable: true, Emit: prop.EmitTrue},
		"Metadata":       {Value: map[string]interface{}{}, Writable: true, Emit: prop.EmitTrue},
		"Rate":           {Value: 1.0, Writable: true, Emit: prop.EmitTrue},
		"Volume":         {Value: 1.0, Writable: true, Emit: prop.EmitTrue},
		"Position":       {Value: int64(0), Writable: false, Emit: prop.EmitTrue},
		"MinimumRate":    {Value: 1.0, Writable: false, Emit: prop.EmitTrue},
		"MaximumRate":    {Value: 1.0, Writable: false, Emit: prop.EmitTrue},
		"CanGoNext":      {Value: true, Writable: false, Emit: prop.EmitTrue},
		"CanGoPrevious":  {Value: true, Writable: false, Emit: prop.EmitTrue},
		"CanPlay":        {Value: true, Writable: false, Emit: prop.EmitTrue},
		"CanPause":       {Value: true, Writable: false, Emit: prop.EmitTrue},
		"CanSeek":        {Value: true, Writable: false, Emit: prop.EmitTrue},
		"CanControl":     {Value: true, Writable: false, Emit: prop.EmitTrue},
	}

	propsSpec := map[string]map[string]*prop.Prop{
		baseInterface:   rootProps,
		playerInterface: playerProps,
	}

	props, err := prop.Export(conn, dbus.ObjectPath(objectPath), propsSpec)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("export props: %w", err)
	}
	s.props = props

	return s, nil
}

func (s *Server) SetPlaybackStatus(status string) {
	if s.props != nil {
		s.props.SetMust(playerInterface, "PlaybackStatus", status)
	}
}

func (s *Server) SetMetadata(meta map[string]interface{}) {
	if s.props != nil {
		s.props.SetMust(playerInterface, "Metadata", meta)
	}
}

func (s *Server) SetPosition(posMicros int64) {
	if s.props != nil {
		s.props.SetMust(playerInterface, "Position", posMicros)
	}
}

func (s *Server) Close() {
	if s.conn != nil {
		s.conn.Close()
	}
}
