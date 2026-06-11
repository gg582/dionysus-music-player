package playlist

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/gg582/gozik/internal/models"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mix.gopl")

	songs := []models.Song{
		{
			Name:      "Local File",
			Location:  filepath.Join(dir, "music", "track.flac"),
			Artist:    "Artist A",
			Title:     "Track One",
			Album:     "Album",
			AlbumYear: "2021",
			Duration:  215,
			Lyrics:    "Line 1\nLine 2",
			CoverData: []byte{0x01, 0x02, 0x03},
		},
		{
			Name:     "CD Track",
			Device:   "/dev/cdrom",
			TrackNum: 3,
			IsCD:     true,
			Artist:   "Artist B",
			Title:    "CD Song",
			Duration: 198,
		},
		{
			Name:            "Provider Track",
			Artist:          "Artist C",
			Title:           "Stream Song",
			Duration:        180,
			CoverArtURL:     "https://example.com/cover.jpg",
			ProviderID:      "ytmusic",
			ProviderAddress: "127.0.0.1:50051",
			ProviderTrackID: "MPREb_abc123",
			ProviderName:    "YT Music",
			StreamHeaders: map[string]string{
				"Cookie": "value=1",
			},
		},
		{
			Name:        "CUE Segment",
			Location:    filepath.Join(dir, "music", "album.flac"),
			Title:       "Segment",
			StartOffset: 120,
			EndOffset:   240,
		},
	}

	if err := Save(path, "Mixed Playlist", songs); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	name, loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if name != "Mixed Playlist" {
		t.Errorf("playlist name = %q, want %q", name, "Mixed Playlist")
	}
	if len(loaded) != len(songs) {
		t.Fatalf("loaded %d songs, want %d", len(loaded), len(songs))
	}

	for i := range songs {
		// StreamURL is intentionally not persisted.
		want := songs[i]
		want.StreamURL = ""
		got := loaded[i]
		if !reflect.DeepEqual(got, want) {
			t.Errorf("song %d mismatch:\n got %+v\nwant %+v", i, got, want)
		}
	}
}

func TestRelativePathResolution(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "music"), 0755); err != nil {
		t.Fatal(err)
	}
	trackPath := filepath.Join(dir, "music", "track.flac")
	if err := os.WriteFile(trackPath, []byte("dummy"), 0644); err != nil {
		t.Fatal(err)
	}

	playlistDir := filepath.Join(dir, "playlists")
	if err := os.MkdirAll(playlistDir, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(playlistDir, "mix.gopl")

	songs := []models.Song{
		{Location: trackPath, Title: "Relative"},
	}
	if err := Save(path, "Relative", songs); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) == "" {
		t.Fatal("empty playlist file")
	}

	_, loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("loaded %d songs, want 1", len(loaded))
	}
	if loaded[0].Location != trackPath {
		t.Errorf("resolved location = %q, want %q", loaded[0].Location, trackPath)
	}
}

func TestLoadInvalidFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.gopl")
	if err := os.WriteFile(path, []byte("not xml"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Load(path); err == nil {
		t.Error("expected error loading invalid XML")
	}
}

func TestLoadMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.gopl")
	if _, _, err := Load(path); err == nil {
		t.Error("expected error loading missing file")
	}
}

func TestEmptyPlaylist(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.gopl")
	if err := Save(path, "Empty", nil); err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	name, loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if name != "Empty" {
		t.Errorf("name = %q, want %q", name, "Empty")
	}
	if len(loaded) != 0 {
		t.Errorf("loaded %d songs, want 0", len(loaded))
	}
}
