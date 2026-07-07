package main

import (
	"fmt"
	"log"
	"os"
	"runtime"
	"runtime/debug"

	"github.com/gg582/gozik/internal/config"
	"github.com/gg582/gozik/internal/provider"
	"github.com/gg582/gozik/internal/ui"
	"github.com/gotk3/gotk3/glib"
	"github.com/gotk3/gotk3/gtk"
)

const appID = "com.gosuda.gozik.player"

func main() {
	if hasHelpFlag(os.Args[1:]) {
		printHelp()
		os.Exit(0)
	}

	config.SetupBundledEnvironment()
	configureRuntime()

	app, err := gtk.ApplicationNew(appID, glib.APPLICATION_FLAGS_NONE)
	if err != nil {
		log.Fatal("Could not create application:", err)
	}

	files := os.Args[1:]

	// Set up the provider manager; it will auto-discover local services.
	mgr := provider.NewManager()

	var mainWin *ui.MainWindow

	app.Connect("activate", func() {
		if mainWin != nil {
			mainWin.Present()
			return
		}
		win, err := ui.NewMainWindow(app, mgr)
		if err != nil {
			log.Fatal("Could not create main window:", err)
		}
		mainWin = win
		win.ShowAll()
		if len(files) > 0 {
			win.LoadFiles(files)
			files = nil
		}
	})

	if mgr != nil {
		defer mgr.Close()
	}

	// Only hand the program name to GtkApplication; file arguments are consumed
	// above (GtkApplication with FLAGS_NONE would otherwise reject extra args).
	os.Exit(app.Run(os.Args[:1]))
}

func configureRuntime() {
	if runtime.GOMAXPROCS(0) < 2 {
		runtime.GOMAXPROCS(2)
	}
	debug.SetGCPercent(200)
}

func hasHelpFlag(args []string) bool {
	for _, a := range args {
		if a == "--help" || a == "-h" {
			return true
		}
	}
	return false
}

func printHelp() {
	fmt.Print(`Gozik - GTK music player with gRPC remote control

Usage:
  gozik [file_or_playlist ...]

Remote control:
  Gozik exposes a gRPC server on 127.0.0.1:50051 by default.
  Set GOZIK_GRPC_ADDR to change the listen address.

  Proto: api/player/v1/player.proto

Available RPCs (player.v1.PlayerService):
  Playback:   Play, Pause, Stop, Next, Previous, PlayIndex
  Transport:  Seek, SetVolume, GetVolume, SetMute, GetMute
  Queue:      GetQueue, AddToQueue, RemoveFromQueue, ClearQueue
  Playlist:   LoadPlaylist, SavePlaylist
  Modes:      SetRepeatMode, GetRepeatMode, SetShuffle, GetShuffle
  Status:     GetStatus, GetCurrentTrack
  CD:         GetCDDevices, ReadCD, LoadCD
  Provider:   ListProviders, SearchTracks, SearchPlaylists,
              GetProviderTrackDetails, ResolveProviderStream,
              GetProviderPlaylistDetails, AddProviderTrack
  Events:     SubscribeEvents (server streaming)

Examples:
  # Playback
  grpcurl -plaintext 127.0.0.1:50051 player.v1.PlayerService/Play
  grpcurl -plaintext 127.0.0.1:50051 player.v1.PlayerService/Pause
  grpcurl -plaintext 127.0.0.1:50051 player.v1.PlayerService/GetStatus
  grpcurl -plaintext 127.0.0.1:50051 player.v1.PlayerService/GetQueue

  # Queue / playlist
  grpcurl -plaintext -d '{"paths":["/path/to/song.flac"]}' \
      127.0.0.1:50051 player.v1.PlayerService/AddToQueue
  grpcurl -plaintext -d '{"path":"/path/to/playlist.gopl"}' \
      127.0.0.1:50051 player.v1.PlayerService/SavePlaylist

  # CD
  grpcurl -plaintext 127.0.0.1:50051 player.v1.PlayerService/GetCDDevices
  grpcurl -plaintext -d '{"device":"/dev/sr0"}' \
      127.0.0.1:50051 player.v1.PlayerService/LoadCD

  # Music Provider
  grpcurl -plaintext 127.0.0.1:50051 player.v1.PlayerService/ListProviders
  grpcurl -plaintext -d '{"provider_id":"yt-music","query":"led zeppelin","limit":5}' \
      127.0.0.1:50051 player.v1.PlayerService/SearchTracks
  grpcurl -plaintext -d '{"provider_id":"yt-music","track_id":"TRACK_ID"}' \
      127.0.0.1:50051 player.v1.PlayerService/AddProviderTrack

  # Events
  grpcurl -plaintext 127.0.0.1:50051 player.v1.PlayerService/SubscribeEvents
`)
}
