package utils

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// MBDiscTrack holds track info from a MusicBrainz disc lookup.
type MBDiscTrack struct {
	Position int
	Title    string
	Artist   string
	Length   int // milliseconds
}

// MBDiscRelease holds release info from a MusicBrainz disc lookup.
type MBDiscRelease struct {
	ID          string
	Title       string
	Artist      string
	Year        string
	CoverArtURL string
	Tracks      []MBDiscTrack
}

// SearchMusicBrainzDisc performs a synchronous disc lookup by TOC.
func SearchMusicBrainzDisc(toc string) (*MBDiscRelease, error) {
	if strings.TrimSpace(toc) == "" {
		return nil, fmt.Errorf("empty toc")
	}

	q := url.Values{}
	q.Set("toc", toc)
	q.Set("inc", "artists+recordings")
	q.Set("fmt", "json")

	reqURL := fmt.Sprintf("%s/discid/-?%s", musicBrainzAPI, q.Encode())
	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "GozikMusicPlayer/1.0")

	mbReqMu.Lock()
	defer mbReqMu.Unlock()
	time.Sleep(1100 * time.Millisecond)

	resp, err := mbHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("musicbrainz disc lookup returned %d", resp.StatusCode)
	}

	var result struct {
		Releases []struct {
			ID           string `json:"id"`
			Title        string `json:"title"`
			Date         string `json:"date"`
			ArtistCredit []struct {
				Artist struct {
					Name string `json:"name"`
				} `json:"artist"`
			} `json:"artist-credit"`
			Media []struct {
				Tracks []struct {
					Position  int    `json:"position"`
					Title     string `json:"title"`
					Length    int    `json:"length"`
					Recording struct {
						Title        string `json:"title"`
						ArtistCredit []struct {
							Artist struct {
								Name string `json:"name"`
							} `json:"artist"`
						} `json:"artist-credit"`
					} `json:"recording"`
				} `json:"tracks"`
			} `json:"media"`
		} `json:"releases"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	if len(result.Releases) == 0 {
		return nil, fmt.Errorf("no releases found for disc")
	}

	release := result.Releases[0]
	disc := &MBDiscRelease{
		ID:    release.ID,
		Title: release.Title,
		Year:  extractYear(release.Date),
	}

	if len(release.ArtistCredit) > 0 {
		disc.Artist = release.ArtistCredit[0].Artist.Name
	}

	if len(release.Media) > 0 {
		for _, t := range release.Media[0].Tracks {
			track := MBDiscTrack{
				Position: t.Position,
				Title:    t.Title,
				Length:   t.Length,
			}
			if t.Recording.Title != "" {
				track.Title = t.Recording.Title
			}
			if len(t.Recording.ArtistCredit) > 0 {
				track.Artist = t.Recording.ArtistCredit[0].Artist.Name
			}
			disc.Tracks = append(disc.Tracks, track)
		}
	}

	if coverURL, err := fetchCoverArtURL(disc.ID); err == nil {
		disc.CoverArtURL = coverURL
	}

	return disc, nil
}
