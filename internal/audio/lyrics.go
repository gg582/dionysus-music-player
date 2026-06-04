package audio

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sahilm/fuzzy"
)

const (
	lrclibSearchURL = "https://lrclib.net/api/search"
	lrclibGetURL    = "https://lrclib.net/api/get"
)

type lrclibResult struct {
	ID           int    `json:"id"`
	Name         string `json:"name"`
	TrackName    string `json:"trackName"`
	ArtistName   string `json:"artistName"`
	AlbumName    string `json:"albumName"`
	Duration     int    `json:"duration"`
	Instrumental bool   `json:"instrumental"`
	PlainLyrics  string `json:"plainLyrics"`
	SyncedLyrics string `json:"syncedLyrics"`
}

// SearchLyrics queries lrclib.net for lyrics using the track title, artist, album and duration.
// It first tries the direct /get endpoint (most accurate), then falls back to /search with fuzzy matching.
func SearchLyrics(title, artist, album string, durationSec int) (string, error) {
	if strings.TrimSpace(title) == "" {
		return "", fmt.Errorf("empty title")
	}

	// 1. Try direct lookup first (exact match by all available fields)
	q := url.Values{}
	q.Set("track_name", strings.TrimSpace(title))
	if strings.TrimSpace(artist) != "" {
		q.Set("artist_name", strings.TrimSpace(artist))
	}
	if strings.TrimSpace(album) != "" {
		q.Set("album_name", strings.TrimSpace(album))
	}
	if durationSec > 0 {
		q.Set("duration", strconv.Itoa(durationSec))
	}

	reqURL := fmt.Sprintf("%s?%s", lrclibGetURL, q.Encode())
	if lyrics, err := fetchSingleLyrics(reqURL); err == nil && lyrics != "" {
		return lyrics, nil
	}

	// 2. Fallback to search endpoint
	q = url.Values{}
	q.Set("track_name", strings.TrimSpace(title))
	if strings.TrimSpace(artist) != "" {
		q.Set("artist_name", strings.TrimSpace(artist))
	}
	if strings.TrimSpace(album) != "" {
		q.Set("album_name", strings.TrimSpace(album))
	}

	reqURL = fmt.Sprintf("%s?%s", lrclibSearchURL, q.Encode())
	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "GozikMusicPlayer/1.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("lrclib returned status %d", resp.StatusCode)
	}

	var results []lrclibResult
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		return "", err
	}

	if len(results) == 0 {
		return "", fmt.Errorf("no lyrics found")
	}

	best := pickBestResult(results, title, artist, album, durationSec)
	if best == nil {
		return "", fmt.Errorf("no matching result")
	}

	if best.Instrumental {
		return "(Instrumental)", nil
	}
	if best.PlainLyrics != "" {
		return best.PlainLyrics, nil
	}
	if best.SyncedLyrics != "" {
		return stripSyncTags(best.SyncedLyrics), nil
	}
	return "", fmt.Errorf("empty lyrics in result")
}

func fetchSingleLyrics(reqURL string) (string, error) {
	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "GozikMusicPlayer/1.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return "", fmt.Errorf("not found")
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("lrclib get returned %d", resp.StatusCode)
	}

	var result lrclibResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	if result.Instrumental {
		return "(Instrumental)", nil
	}
	if result.PlainLyrics != "" {
		return result.PlainLyrics, nil
	}
	if result.SyncedLyrics != "" {
		return stripSyncTags(result.SyncedLyrics), nil
	}
	return "", fmt.Errorf("empty lyrics")
}

func pickBestResult(results []lrclibResult, queryTitle, queryArtist, queryAlbum string, queryDuration int) *lrclibResult {
	qt := strings.ToLower(strings.TrimSpace(queryTitle))
	qa := strings.ToLower(strings.TrimSpace(queryArtist))
	qal := strings.ToLower(strings.TrimSpace(queryAlbum))

	// 1. Exact title + artist matches: pick best by album & duration
	var exactMatches []lrclibResult
	for i := range results {
		r := results[i]
		if strings.EqualFold(r.TrackName, queryTitle) && (qa == "" || strings.EqualFold(r.ArtistName, queryArtist)) {
			exactMatches = append(exactMatches, r)
		}
	}
	if len(exactMatches) > 0 {
		best := &exactMatches[0]
		bestScore := -1
		for i := range exactMatches {
			r := exactMatches[i]
			score := 0
			if qal != "" && strings.EqualFold(r.AlbumName, queryAlbum) {
				score += 2000
			} else if qal != "" {
				if s := matchScore(qal, r.AlbumName); s > 0 {
					score += s
				}
			}
			if queryDuration > 0 && r.Duration > 0 {
				diff := queryDuration - r.Duration
				if diff < 0 {
					diff = -diff
				}
				if diff <= 3 {
					score += 1500
				} else if diff <= 10 {
					score += 800
				} else if diff <= 30 {
					score += 300
				} else if diff <= 60 {
					score += 100
				}
			}
			if score > bestScore {
				bestScore = score
				best = &exactMatches[i]
			}
		}
		return best
	}

	// 2. Fuzzy scoring with heavy weight on artist/album/duration
	type scored struct {
		result lrclibResult
		score  int
	}
	var scoredResults []scored

	for i := range results {
		r := results[i]
		score := 0

		// Title is most important
		if s := matchScore(qt, r.TrackName); s > 0 {
			score += s * 3
		}

		// Artist match
		if qa != "" {
			if s := matchScore(qa, r.ArtistName); s > 0 {
				score += s * 2
			}
		}

		// Album match
		if qal != "" {
			if s := matchScore(qal, r.AlbumName); s > 0 {
				score += s * 2
			}
		}

		// Duration proximity (critical for blues/jazz alternate takes)
		if queryDuration > 0 && r.Duration > 0 {
			diff := queryDuration - r.Duration
			if diff < 0 {
				diff = -diff
			}
			if diff <= 3 {
				score += 1000
			} else if diff <= 10 {
				score += 500
			} else if diff <= 30 {
				score += 200
			} else if diff <= 60 {
				score += 50
			}
		}

		if score > 0 {
			scoredResults = append(scoredResults, scored{r, score})
		}
	}

	if len(scoredResults) == 0 {
		if len(results) > 0 {
			return &results[0]
		}
		return nil
	}

	sort.Slice(scoredResults, func(i, j int) bool {
		return scoredResults[i].score > scoredResults[j].score
	})

	return &scoredResults[0].result
}

func matchScore(pattern, str string) int {
	matches := fuzzy.Find(strings.ToLower(pattern), []string{strings.ToLower(str)})
	if len(matches) > 0 {
		return matches[0].Score
	}
	return 0
}

func stripSyncTags(synced string) string {
	lines := strings.Split(synced, "\n")
	var out []string
	for _, line := range lines {
		for {
			start := strings.Index(line, "[")
			end := strings.Index(line, "]")
			if start == -1 || end == -1 || end < start {
				break
			}
			line = line[:start] + line[end+1:]
		}
		out = append(out, strings.TrimSpace(line))
	}
	return strings.Join(out, "\n")
}

// LRCLine represents a single synced lyric line.
type LRCLine struct {
	Time time.Duration
	Text string
}

// ParseSyncedLyrics parses LRC-format synced lyrics into timed lines.
func ParseSyncedLyrics(synced string) []LRCLine {
	var lines []LRCLine
	for _, rawLine := range strings.Split(synced, "\n") {
		rawLine = strings.TrimSpace(rawLine)
		if rawLine == "" {
			continue
		}
		var timestamps []time.Duration
		text := rawLine
		for {
			start := strings.Index(text, "[")
			end := strings.Index(text, "]")
			if start == -1 || end == -1 || end < start {
				break
			}
			tag := text[start+1 : end]
			text = text[end+1:]
			if t, ok := parseLrcTime(tag); ok {
				timestamps = append(timestamps, t)
			}
		}
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		for _, t := range timestamps {
			lines = append(lines, LRCLine{Time: t, Text: text})
		}
	}
	sort.Slice(lines, func(i, j int) bool {
		return lines[i].Time < lines[j].Time
	})
	return lines
}

func parseLrcTime(tag string) (time.Duration, bool) {
	parts := strings.Split(tag, ":")
	if len(parts) != 2 {
		return 0, false
	}
	min, err1 := strconv.Atoi(parts[0])
	secParts := strings.Split(parts[1], ".")
	sec, err2 := strconv.Atoi(secParts[0])
	if err1 != nil || err2 != nil || min < 0 || sec < 0 || sec >= 60 {
		return 0, false
	}
	ms := 0
	if len(secParts) > 1 {
		msStr := secParts[1]
		if len(msStr) == 2 {
			ms, _ = strconv.Atoi(msStr)
			ms *= 10
		} else if len(msStr) >= 3 {
			ms, _ = strconv.Atoi(msStr[:3])
		}
	}
	return time.Duration(min)*time.Minute + time.Duration(sec)*time.Second + time.Duration(ms)*time.Millisecond, true
}
