// Package garage61 retrieves telemetry and builds local Brake-It targets.
package garage61

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// TokenSource is deliberately independent of HTTP and scenario generation so
// a future OAuth token manager can supply and refresh credentials server-side.
type TokenSource func(context.Context) (string, error)

func EnvironmentToken(context.Context) (string, error) {
	token := strings.TrimSpace(os.Getenv("GARAGE61_TOKEN"))
	if token == "" {
		return "", errors.New("set GARAGE61_TOKEN in the LapDog process environment to connect to Garage61")
	}
	return token, nil
}

type Entity struct {
	ID         int    `json:"id"`
	Name       string `json:"name"`
	Variant    string `json:"variant,omitempty"`
	Platform   string `json:"platform"`
	PlatformID string `json:"platform_id,omitempty"`
}

func (e Entity) Label() string { return strings.TrimSpace(e.Name + " " + e.Variant) }

type Catalog struct {
	Cars   []Entity `json:"cars"`
	Tracks []Entity `json:"tracks"`
}

type lap struct {
	ID               string  `json:"id"`
	LapTime          float64 `json:"lapTime"`
	CanViewTelemetry bool    `json:"canViewTelemetry"`
}

type Client struct {
	baseURL  string
	http     *http.Client
	token    TokenSource
	mu       sync.Mutex
	next     time.Time
	interval time.Duration
}

func New(token TokenSource) *Client {
	return &Client{baseURL: "https://garage61.net/api/v1", token: token,
		http: &http.Client{Timeout: 60 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, interval: time.Second}
}

func (c *Client) Configured(ctx context.Context) bool {
	token, err := c.token(ctx)
	return err == nil && token != ""
}

// Serialize requests, including retries, to share the server's capacity budget
// between catalog browsing and generation. No response bodies or tokens enter logs.
func (c *Client) get(ctx context.Context, path string, query url.Values, csv bool) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for attempt := 0; attempt < 8; attempt++ {
		if delay := time.Until(c.next); delay > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case <-timer.C:
			}
		}
		token, err := c.token(ctx)
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+path+"?"+query.Encode(), nil)
		if err != nil {
			return nil, errors.New("invalid Garage61 request")
		}
		req.Header.Set("Authorization", "Bearer "+token)
		c.next = time.Now().Add(c.interval)
		res, err := c.http.Do(req)
		if err != nil {
			return nil, errors.New("Garage61 request failed; check your connection and retry")
		}
		body, readErr := io.ReadAll(io.LimitReader(res.Body, 32*1024*1024+1))
		res.Body.Close()
		if readErr != nil {
			return nil, errors.New("could not read Garage61 response")
		}
		if len(body) > 32*1024*1024 {
			return nil, errors.New("Garage61 response exceeds 32 MiB")
		}
		if delay, ok := retryDelay(res, body); ok {
			c.next = maxTime(c.next, time.Now().Add(delay+time.Duration(rand.Float64()*float64(time.Second))))
		}
		if res.StatusCode == 429 {
			continue
		}
		if csv && (res.StatusCode == 403 || res.StatusCode == 404) {
			return nil, nil
		}
		if res.StatusCode == 401 {
			return nil, errors.New("Garage61 rejected the access token; update GARAGE61_TOKEN and restart LapDog")
		}
		if res.StatusCode < 200 || res.StatusCode >= 300 {
			return nil, fmt.Errorf("Garage61 returned HTTP %d", res.StatusCode)
		}
		return body, nil
	}
	return nil, errors.New("Garage61 rate limit did not clear; try again later")
}

func maxTime(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

func retryDelay(res *http.Response, body []byte) (time.Duration, bool) {
	seconds := -1.0
	if v, err := strconv.ParseFloat(res.Header.Get("Retry-After"), 64); err == nil && !math.IsInf(v, 0) && !math.IsNaN(v) && v >= 0 {
		seconds = v
	} else if date, err := http.ParseTime(res.Header.Get("Retry-After")); err == nil {
		seconds = math.Max(0, time.Until(date).Seconds())
	}
	if res.StatusCode == 429 {
		var data struct {
			Details struct {
				RetryAfterSeconds *float64 `json:"retryAfterSeconds"`
			} `json:"details"`
		}
		if json.Unmarshal(body, &data) == nil && data.Details.RetryAfterSeconds != nil {
			seconds = math.Max(seconds, *data.Details.RetryAfterSeconds)
		}
		if seconds < 0 {
			seconds = 10
		}
	}
	// Cap only the representable duration, not the server's requested wait.
	if seconds < 0 || math.IsNaN(seconds) || math.IsInf(seconds, 0) {
		return 0, false
	}
	return time.Duration(math.Min(seconds*float64(time.Second), float64(math.MaxInt64/2))), true
}

func decodeItems[T any](body []byte) ([]T, int, error) {
	var items []T
	if strings.HasPrefix(strings.TrimSpace(string(body)), "[") {
		err := json.Unmarshal(body, &items)
		return items, len(items), err
	}
	var page struct {
		Items []T  `json:"items"`
		Total *int `json:"total"`
	}
	if err := json.Unmarshal(body, &page); err != nil {
		return nil, 0, errors.New("invalid Garage61 response")
	}
	if page.Items == nil {
		return nil, 0, errors.New("Garage61 response has no items array")
	}
	total := len(page.Items)
	if page.Total != nil {
		total = *page.Total
	}
	return page.Items, total, nil
}

func (c *Client) Catalog(ctx context.Context) (Catalog, error) {
	out := Catalog{Cars: []Entity{}, Tracks: []Entity{}}
	for _, target := range []struct {
		path string
		dst  *[]Entity
	}{{"/cars", &out.Cars}, {"/tracks", &out.Tracks}} {
		seen := map[int]bool{}
		for offset := 0; ; {
			query := url.Values{"limit": {"100"}, "offset": {strconv.Itoa(offset)}}
			body, err := c.get(ctx, target.path, query, false)
			if err != nil {
				return Catalog{}, err
			}
			items, total, err := decodeItems[Entity](body)
			if err != nil {
				return Catalog{}, err
			}
			if len(items) == 0 && offset < total {
				return Catalog{}, fmt.Errorf("Garage61 %s catalog ended before all entries were returned", target.path)
			}
			for _, e := range items {
				if seen[e.ID] {
					return Catalog{}, fmt.Errorf("Garage61 %s catalog repeated an entry", target.path)
				}
				seen[e.ID] = true
				if strings.EqualFold(e.Platform, "iracing") && e.ID > 0 && strings.TrimSpace(e.Name) != "" {
					*target.dst = append(*target.dst, e)
				}
			}
			offset += len(items)
			if offset >= total {
				break
			}
		}
		sort.SliceStable(*target.dst, func(i, j int) bool { return (*target.dst)[i].Label() < (*target.dst)[j].Label() })
	}
	return out, nil
}

func (c *Client) laps(ctx context.Context, car, track int) ([]lap, error) {
	visible := []lap{}
	previous := -1.0
	seen := map[string]bool{}
	for offset, blocks := 0, 0; ; blocks++ {
		q := url.Values{"cars": {strconv.Itoa(car)}, "tracks": {strconv.Itoa(track)}, "group": {"driver-car"}, "limit": {"12"}, "offset": {strconv.Itoa(offset)}, "lapTypes": {"1"}}
		body, err := c.get(ctx, "/laps", q, false)
		if err != nil {
			return nil, err
		}
		page, total, err := decodeItems[lap](body)
		if err != nil {
			return nil, err
		}
		for _, l := range page {
			if l.ID == "" || l.LapTime <= 0 || math.IsNaN(l.LapTime) || math.IsInf(l.LapTime, 0) {
				return nil, errors.New("Garage61 lap has no valid ID or lap time")
			}
			if l.LapTime < previous {
				return nil, errors.New("Garage61 laps are not ordered fastest-first")
			}
			previous = l.LapTime
			if seen[l.ID] {
				return nil, errors.New("Garage61 pagination repeated a lap")
			}
			seen[l.ID] = true
			if l.CanViewTelemetry && len(visible) < 12 {
				visible = append(visible, l)
			}
		}
		offset += len(page)
		if len(page) == 0 || offset >= total || (blocks >= 2 && len(visible) >= 12) {
			return visible, nil
		}
	}
}
