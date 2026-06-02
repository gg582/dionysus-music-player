package audio

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const musicBrainzAPI = "https://musicbrainz.org/ws/2"
const coverArtArchiveAPI = "https://coverartarchive.org/release"

var mbHTTPClient = &http.Client{Timeout: 15 * time.Second}

// MBRecording holds parsed MusicBrainz recording info.
type MBRecording struct {
	ID          string
	Title       string
	Artist      string
	Album       string
	Year        string
	Duration    int // milliseconds
	CoverArtURL string
}

type mbRecordingRaw struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	Length       int    `json:"length"`
	ArtistCredit []struct {
		Artist struct {
			Name     string `json:"name"`
			SortName string `json:"sort-name"`
		} `json:"artist"`
	} `json:"artist-credit"`
	Releases []struct {
		ID      string `json:"id"`
		Title   string `json:"title"`
		Date    string `json:"date"`
		Country string `json:"country"`
	} `json:"releases"`
}

// ---------------------------------------------------------------------------
// Queued / cached worker
// ---------------------------------------------------------------------------

type mbRequest struct {
	query    string
	callback func(*MBRecording, error)
}

type mbResult struct {
	rec *MBRecording
	err error
}

var (
	mbQueue     chan mbRequest
	mbCache     map[string]*mbResult
	mbCacheMu   sync.RWMutex
	mbPending   map[string]bool
	mbPendingMu sync.Mutex
	mbOnce      sync.Once
)

func ensureMBWorker() {
	mbOnce.Do(func() {
		mbQueue = make(chan mbRequest, 100)
		mbCache = make(map[string]*mbResult)
		mbPending = make(map[string]bool)
		go mbWorker()
	})
}

func mbWorker() {
	for req := range mbQueue {
		// 1) Cache hit → immediate callback
		mbCacheMu.RLock()
		cached, ok := mbCache[req.query]
		mbCacheMu.RUnlock()
		if ok {
			req.callback(cached.rec, cached.err)
			continue
		}

		// 2) Rate-limit: 1 req / 1.1 sec
		time.Sleep(1100 * time.Millisecond)

		// 3) Execute request
		rec, err := searchMusicBrainzDirect(req.query)

		// 4) Store in cache
		mbCacheMu.Lock()
		mbCache[req.query] = &mbResult{rec: rec, err: err}
		mbCacheMu.Unlock()

		// 5) Callback
		req.callback(rec, err)
	}
}

// QueueMusicBrainzSearch queues a MusicBrainz search request.
// Results are cached and reused. Duplicate queued requests are deduplicated.
func QueueMusicBrainzSearch(query string, callback func(*MBRecording, error)) {
	ensureMBWorker()
	query = sanitizeQuery(query)

	// Cache hit → callback immediately in a new goroutine
	mbCacheMu.RLock()
	if cached, ok := mbCache[query]; ok {
		mbCacheMu.RUnlock()
		go callback(cached.rec, cached.err)
		return
	}
	mbCacheMu.RUnlock()

	// Deduplication: already in queue?
	mbPendingMu.Lock()
	if mbPending[query] {
		mbPendingMu.Unlock()
		return
	}
	mbPending[query] = true
	mbPendingMu.Unlock()

	req := mbRequest{
		query: query,
		callback: func(rec *MBRecording, err error) {
			mbPendingMu.Lock()
			delete(mbPending, query)
			mbPendingMu.Unlock()
			callback(rec, err)
		},
	}

	mbQueue <- req // blocking; caller is expected to be in a goroutine
}

// SearchMusicBrainz performs a synchronous MusicBrainz search (bypasses queue).
func SearchMusicBrainz(query string) (*MBRecording, error) {
	return searchMusicBrainzDirect(query)
}

// ---------------------------------------------------------------------------
// Direct search implementation
// ---------------------------------------------------------------------------

func searchMusicBrainzDirect(query string) (*MBRecording, error) {
	if strings.TrimSpace(query) == "" {
		return nil, fmt.Errorf("empty query")
	}

	q := url.Values{}
	q.Set("query", sanitizeQuery(query))
	q.Set("fmt", "json")
	q.Set("limit", "10")

	reqURL := fmt.Sprintf("%s/recording?%s", musicBrainzAPI, q.Encode())
	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "DionysusMusicPlayer/1.0")

	resp, err := mbHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("musicbrainz returned %d", resp.StatusCode)
	}

	var result struct {
		Recordings []mbRecordingRaw `json:"recordings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	if len(result.Recordings) == 0 {
		return nil, fmt.Errorf("no recordings found")
	}

	best := pickBestRecording(result.Recordings, query)
	if best == nil {
		return nil, fmt.Errorf("no matching recording")
	}

	rec := &MBRecording{
		ID:       best.ID,
		Title:    best.Title,
		Duration: best.Length,
	}

	if len(best.ArtistCredit) > 0 {
		rec.Artist = best.ArtistCredit[0].Artist.Name
	}

	if len(best.Releases) > 0 {
		release := best.Releases[0]
		rec.Album = release.Title
		rec.Year = extractYear(release.Date)
		if coverURL, err := fetchCoverArtURL(release.ID); err == nil {
			rec.CoverArtURL = coverURL
		}
	}

	return rec, nil
}

func sanitizeQuery(q string) string {
	q = strings.TrimSpace(q)
	q = strings.TrimSuffix(q, filepath.Ext(q))
	q = strings.ReplaceAll(q, "_", " ")
	q = strings.ReplaceAll(q, "-", " ")
	q = strings.Join(strings.Fields(q), " ")
	return q
}

func pickBestRecording(recordings []mbRecordingRaw, query string) *mbRecordingRaw {
	q := strings.ToLower(strings.TrimSpace(query))

	type scored struct {
		rec   *mbRecordingRaw
		score int
	}
	var scoredResults []scored

	for i := range recordings {
		r := &recordings[i]
		score := 0

		if s := matchScore(q, r.Title); s > 0 {
			score += s * 3
		}
		if len(r.ArtistCredit) > 0 {
			if s := matchScore(q, r.ArtistCredit[0].Artist.Name); s > 0 {
				score += s * 2
			}
		}
		if len(r.Releases) > 0 {
			if s := matchScore(q, r.Releases[0].Title); s > 0 {
				score += s
			}
		}

		if score > 0 {
			scoredResults = append(scoredResults, scored{r, score})
		}
	}

	if len(scoredResults) == 0 {
		if len(recordings) > 0 {
			return &recordings[0]
		}
		return nil
	}

	sort.Slice(scoredResults, func(i, j int) bool {
		return scoredResults[i].score > scoredResults[j].score
	})

	return scoredResults[0].rec
}

func extractYear(date string) string {
	if len(date) >= 4 {
		if _, err := strconv.Atoi(date[:4]); err == nil {
			return date[:4]
		}
	}
	return ""
}

func fetchCoverArtURL(releaseID string) (string, error) {
	u := fmt.Sprintf("%s/%s/front", coverArtArchiveAPI, releaseID)
	req, err := http.NewRequest("HEAD", u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "DionysusMusicPlayer/1.0")

	resp, err := mbHTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusFound || resp.StatusCode == http.StatusMovedPermanently {
		if loc := resp.Header.Get("Location"); loc != "" {
			return loc, nil
		}
		return u, nil
	}
	return "", fmt.Errorf("no cover art")
}

// DownloadImage downloads image data from URL.
func DownloadImage(url string) ([]byte, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "DionysusMusicPlayer/1.0")

	resp, err := mbHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("image download failed: %d", resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}
