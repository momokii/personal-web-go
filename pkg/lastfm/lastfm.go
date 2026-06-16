package lastfm

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"
)

const (
	BASE_LASTFM_URL = "https://ws.audioscrobbler.com/2.0/"

	// widget states surfaced to the browser
	StateActive   = "active"   // track.@attr.nowplaying == "true"
	StateIdle     = "idle"     // a recent track exists but is not currently playing
	StateFallback = "fallback" // recenttracks empty/failed -> this week's top artist
	StateOff      = "off"      // no data / missing creds -> widget hides

	cacheTTL    = 60 * time.Second
	httpTimeout = 10 * time.Second
)

// NowPlaying is the normalized payload served to the browser. It is flat on
// purpose so the client can rely on simple optional chaining.
type NowPlaying struct {
	State     string `json:"state"`     // active | idle | fallback | off
	Artist    string `json:"artist"`    // "" when off
	Track     string `json:"track"`     // "" for fallback (top artist has no track)
	Album     string `json:"album"`     // best-effort, often ""
	ImageURL  string `json:"image_url"` // artwork; "" -> client swaps to SVG
	URL       string `json:"url"`       // Last.fm page for the track/artist
	Timestamp int64  `json:"timestamp"` // scrobble unix seconds; 0 when unknown

	// Enrichment lists for the expand panel. omitempty so StateOff and old
	// cached payloads serialize without them.
	TopArtists   []ArtistItem `json:"top_artists,omitempty"`
	RecentTracks []TrackItem  `json:"recent_tracks,omitempty"`
}

// ArtistItem is a single entry in the weekly top-artists list.
type ArtistItem struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// TrackItem is a single entry in the recent-scrobbles list.
type TrackItem struct {
	Artist string `json:"artist"`
	Name   string `json:"name"`
	URL    string `json:"url"`
}

// --- raw Last.fm response structs (decode only) ---
//
// NOTE the two load-bearing JSON quirks: the nowplaying flag lives under the
// "@attr" object, and inline text values use the "#text" key. Get these tags
// exactly right or nowplaying silently reads empty (active plays degrade to
// idle) and image/artist fields come back blank.
type lfImage struct {
	Size string `json:"size"`
	Text string `json:"#text"`
}

type recentTracksResp struct {
	RecentTracks struct {
		Track []struct {
			Artist struct {
				Text string `json:"#text"`
			} `json:"artist"`
			Album struct {
				Text string `json:"#text"`
			} `json:"album"`
			Name string `json:"name"`
			URL  string `json:"url"`
			Date struct {
				Uts string `json:"uts"`
			} `json:"date"`
			Attr struct {
				NowPlaying string `json:"nowplaying"`
			} `json:"@attr"`
			Image []lfImage `json:"image"`
		} `json:"track"`
	} `json:"recenttracks"`
}

type topArtistsResp struct {
	TopArtists struct {
		Artist []struct {
			Name  string    `json:"name"`
			URL   string    `json:"url"`
			Image []lfImage `json:"image"`
		} `json:"artist"`
	} `json:"topartists"`
}

// Client is a concurrency-safe, lazily-cached Last.fm reader. It never panics
// and never returns an error: missing credentials or upstream failures degrade
// to StateOff so the caller and the app always stay healthy.
type Client struct {
	apiKey     string
	username   string
	httpClient *http.Client

	mu       sync.Mutex
	cached   *NowPlaying
	cachedAt time.Time
}

// New does NOT error on missing credentials — the app must always boot. Empty
// apiKey/username simply makes every NowPlaying() return StateOff without ever
// touching the network.
func New(apiKey, username string) *Client {
	return &Client{
		apiKey:     apiKey,
		username:   username,
		httpClient: &http.Client{Timeout: httpTimeout},
	}
}

// NowPlaying returns the current normalized state, serving from a lazy 60s
// cache when fresh. The network fetch happens outside the lock so a slow
// Last.fm response never blocks other visitors. It always returns a usable
// payload (never an error).
func (c *Client) NowPlaying() NowPlaying {
	c.mu.Lock()
	if c.cached != nil && time.Since(c.cachedAt) < cacheTTL {
		out := *c.cached
		c.mu.Unlock()
		return out
	}
	c.mu.Unlock()

	fresh := c.fetchFresh()

	c.mu.Lock()
	c.cached = &fresh
	c.cachedAt = time.Now()
	c.mu.Unlock()

	return fresh
}

// fetchFresh is the state machine. Errors are swallowed (logged) and degrade
// to StateOff.
func (c *Client) fetchFresh() NowPlaying {
	if c.apiKey == "" || c.username == "" {
		return NowPlaying{State: StateOff}
	}

	// primary: recent tracks (active / idle)
	if np, recent, ok := c.getRecentTracks(); ok {
		np.RecentTracks = recent
		// enrich with this week's top artists for the expand panel
		if _, top, ok2 := c.getTopArtists(); ok2 {
			np.TopArtists = top
		}
		return np
	}

	// fallback: this week's top artist (list comes from the same response)
	if np, top, ok := c.getTopArtists(); ok {
		np.TopArtists = top
		return np
	}

	return NowPlaying{State: StateOff}
}

func (c *Client) getRecentTracks() (np NowPlaying, recent []TrackItem, ok bool) {
	u := fmt.Sprintf(
		"%s?method=user.getrecenttracks&user=%s&api_key=%s&format=json&limit=4",
		BASE_LASTFM_URL,
		url.QueryEscape(c.username),
		url.QueryEscape(c.apiKey),
	)

	body, err := c.doGet(u)
	if err != nil {
		log.Println("lastfm recenttracks error: ", err)
		return NowPlaying{}, nil, false
	}
	if body == nil {
		return NowPlaying{}, nil, false // non-200 -> fall through to fallback
	}

	var raw recentTracksResp
	if err := json.Unmarshal(body, &raw); err != nil {
		log.Println("lastfm recenttracks decode error: ", err)
		return NowPlaying{}, nil, false
	}

	tracks := raw.RecentTracks.Track
	if len(tracks) == 0 {
		return NowPlaying{}, nil, false
	}

	t := tracks[0]
	state := StateIdle
	if t.Attr.NowPlaying == "true" {
		state = StateActive
	}

	// build the recent-scrobbles list from tracks[1:], capped at 3
	recent = make([]TrackItem, 0, 3)
	for _, rt := range tracks[1:] {
		if len(recent) >= 3 {
			break
		}
		if rt.Name == "" {
			continue
		}
		recent = append(recent, TrackItem{
			Artist: rt.Artist.Text,
			Name:   rt.Name,
			URL:    rt.URL,
		})
	}

	return NowPlaying{
		State:     state,
		Artist:    t.Artist.Text,
		Track:     t.Name,
		Album:     t.Album.Text,
		ImageURL:  pickImage(t.Image),
		URL:       t.URL,
		Timestamp: parseUnix(t.Date.Uts),
	}, recent, true
}

func (c *Client) getTopArtists() (np NowPlaying, top []ArtistItem, ok bool) {
	u := fmt.Sprintf(
		"%s?method=user.gettopartists&user=%s&api_key=%s&format=json&limit=3&period=7day",
		BASE_LASTFM_URL,
		url.QueryEscape(c.username),
		url.QueryEscape(c.apiKey),
	)

	body, err := c.doGet(u)
	if err != nil {
		log.Println("lastfm topartists error: ", err)
		return NowPlaying{}, nil, false
	}
	if body == nil {
		return NowPlaying{}, nil, false
	}

	var raw topArtistsResp
	if err := json.Unmarshal(body, &raw); err != nil {
		log.Println("lastfm topartists decode error: ", err)
		return NowPlaying{}, nil, false
	}

	artists := raw.TopArtists.Artist
	if len(artists) == 0 {
		return NowPlaying{}, nil, false
	}

	// build the top-artists list, capped at 3
	top = make([]ArtistItem, 0, 3)
	for _, a := range artists {
		if len(top) >= 3 {
			break
		}
		if a.Name == "" {
			continue
		}
		top = append(top, ArtistItem{Name: a.Name, URL: a.URL})
	}

	a := artists[0]
	return NowPlaying{
		State:    StateFallback,
		Artist:   a.Name,
		ImageURL: pickImage(a.Image),
		URL:      a.URL,
	}, top, true
}

// doGet performs a GET and returns the fully-read body. A nil body with a nil
// error signals a non-200 response (so the caller can fall back silently).
func (c *Client) doGet(rawURL string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	res, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		// drain + close so the underlying connection can be reused
		_, _ = io.Copy(io.Discard, res.Body)
		res.Body.Close()
	}()

	if res.StatusCode != http.StatusOK {
		return nil, nil
	}

	return io.ReadAll(res.Body)
}

// pickImage returns the best available artwork URL, preferring the large/
// extralarge sizes and otherwise falling back to the last non-empty entry.
func pickImage(images []lfImage) string {
	var last string
	for _, img := range images {
		if img.Text == "" {
			continue
		}
		last = img.Text
		if img.Size == "extralarge" || img.Size == "large" {
			return img.Text
		}
	}
	return last
}

func parseUnix(s string) int64 {
	if s == "" {
		return 0
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0
	}
	return n
}
