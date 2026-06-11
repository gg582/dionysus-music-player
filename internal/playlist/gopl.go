// Package playlist implements save/load for Gozik's native .gopl playlist
// format. A .gopl file is an XML document that can mix local files, CD
// tracks, remote provider tracks (YT Music / generic Music Provider gRPC API),
// HTTP(S) streams, and CUE segment bounds in a single playlist.
package playlist

import (
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gg582/gozik/internal/models"
)

const goplVersion = "1"

// goplPlaylist is the root XML document. It mirrors models.Song without
// exposing serialization details to the models package.
type goplPlaylist struct {
	XMLName xml.Name    `xml:"GozikPlaylist"`
	Version string      `xml:"version,attr"`
	Name    string      `xml:"name,attr,omitempty"`
	Tracks  []goplTrack `xml:"Track"`
}

// goplTrack stores one queue item.
type goplTrack struct {
	Name          string        `xml:"Name,omitempty"`
	Location      string        `xml:"Location,omitempty"`
	Device        string        `xml:"Device,omitempty"`
	TrackNum      int           `xml:"TrackNum,omitempty"`
	IsCD          bool          `xml:"IsCD,omitempty"`
	Artist        string        `xml:"Artist,omitempty"`
	Title         string        `xml:"Title,omitempty"`
	Album         string        `xml:"Album,omitempty"`
	AlbumYear     string        `xml:"AlbumYear,omitempty"`
	Duration      int           `xml:"Duration,omitempty"`
	Lyrics        string        `xml:"Lyrics,omitempty"`
	CoverArtURL   string        `xml:"CoverArtURL,omitempty"`
	CoverData     string        `xml:"CoverData,omitempty"`
	StartOffset   int           `xml:"StartOffset,omitempty"`
	EndOffset     int           `xml:"EndOffset,omitempty"`
	Provider      *goplProvider `xml:"Provider,omitempty"`
	StreamURL     string        `xml:"StreamURL,omitempty"`
	StreamHeaders []goplHeader  `xml:"StreamHeaders>Header,omitempty"`
}

type goplProvider struct {
	ID      string `xml:"ID,omitempty"`
	Address string `xml:"Address,omitempty"`
	TrackID string `xml:"TrackID,omitempty"`
	Name    string `xml:"Name,omitempty"`
}

type goplHeader struct {
	Name  string `xml:"name,attr"`
	Value string `xml:",chardata"`
}

// Save writes the given songs to path as a .gopl XML playlist.
// The display name is stored as the playlist name attribute.
func Save(path, name string, songs []models.Song) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create playlist dir: %w", err)
	}

	baseDir := filepath.Dir(path)
	pl := goplPlaylist{
		Version: goplVersion,
		Name:    name,
		Tracks:  make([]goplTrack, 0, len(songs)),
	}

	for _, s := range songs {
		track := goplTrack{
			Name:        s.Name,
			Location:    s.Location,
			Device:      s.Device,
			TrackNum:    s.TrackNum,
			IsCD:        s.IsCD,
			Artist:      s.Artist,
			Title:       s.Title,
			Album:       s.Album,
			AlbumYear:   s.AlbumYear,
			Duration:    s.Duration,
			Lyrics:      s.Lyrics,
			CoverArtURL: s.CoverArtURL,
			StartOffset: s.StartOffset,
			EndOffset:   s.EndOffset,
		}

		// Store cover art as base64; keep it empty when there is none.
		if len(s.CoverData) > 0 {
			track.CoverData = base64.StdEncoding.EncodeToString(s.CoverData)
		}

		// Prefer a path relative to the playlist file so playlists stay
		// portable when moved with their media.
		if track.Location != "" && filepath.IsAbs(track.Location) {
			if rel, err := filepath.Rel(baseDir, track.Location); err == nil && !strings.HasPrefix(rel, "..") {
				track.Location = rel
			}
		}

		if s.ProviderID != "" || s.ProviderAddress != "" || s.ProviderTrackID != "" || s.ProviderName != "" {
			track.Provider = &goplProvider{
				ID:      s.ProviderID,
				Address: s.ProviderAddress,
				TrackID: s.ProviderTrackID,
				Name:    s.ProviderName,
			}
		}

		// StreamURL is resolved at playback time; do not persist it.
		if len(s.StreamHeaders) > 0 {
			for k, v := range s.StreamHeaders {
				track.StreamHeaders = append(track.StreamHeaders, goplHeader{Name: k, Value: v})
			}
		}

		pl.Tracks = append(pl.Tracks, track)
	}

	out, err := xml.MarshalIndent(pl, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal gopl playlist: %w", err)
	}

	header := []byte(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	if err := os.WriteFile(path, append(header, out...), 0644); err != nil {
		return fmt.Errorf("write gopl playlist: %w", err)
	}
	return nil
}

// Load reads a .gopl playlist from path and returns the display name and
// restored songs. Relative file locations are resolved against the playlist
// file's directory.
func Load(path string) (string, []models.Song, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", nil, fmt.Errorf("read gopl playlist: %w", err)
	}

	var pl goplPlaylist
	if err := xml.Unmarshal(data, &pl); err != nil {
		return "", nil, fmt.Errorf("parse gopl playlist: %w", err)
	}

	baseDir := filepath.Dir(path)
	songs := make([]models.Song, 0, len(pl.Tracks))
	for _, t := range pl.Tracks {
		song := models.Song{
			Name:        t.Name,
			Location:    t.Location,
			Device:      t.Device,
			TrackNum:    t.TrackNum,
			IsCD:        t.IsCD,
			Artist:      t.Artist,
			Title:       t.Title,
			Album:       t.Album,
			AlbumYear:   t.AlbumYear,
			Duration:    t.Duration,
			Lyrics:      t.Lyrics,
			CoverArtURL: t.CoverArtURL,
			StartOffset: t.StartOffset,
			EndOffset:   t.EndOffset,
		}

		if t.CoverData != "" {
			if decoded, err := base64.StdEncoding.DecodeString(t.CoverData); err == nil {
				song.CoverData = decoded
			}
		}

		if song.Location != "" && !filepath.IsAbs(song.Location) {
			song.Location = filepath.Join(baseDir, song.Location)
		}

		if t.Provider != nil {
			song.ProviderID = t.Provider.ID
			song.ProviderAddress = t.Provider.Address
			song.ProviderTrackID = t.Provider.TrackID
			song.ProviderName = t.Provider.Name
		}

		if len(t.StreamHeaders) > 0 {
			song.StreamHeaders = make(map[string]string, len(t.StreamHeaders))
			for _, h := range t.StreamHeaders {
				song.StreamHeaders[h.Name] = h.Value
			}
		}

		songs = append(songs, song)
	}

	return pl.Name, songs, nil
}
