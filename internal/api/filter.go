// Package api serves LapDog's JSON API and the embedded user interface.
package api

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/blezek/lapdog/internal/store"
)

// ErrBadRequest indicates a malformed query parameter.
var ErrBadRequest = errors.New("api: bad request")

// MaxLimit caps how many rows one request may ask for. Exceeding it is clamped
// rather than rejected, so a bug in the interface degrades instead of failing.
const MaxLimit = 5000

// parseFilter builds a store.Filter from query parameters.
//
// Every list, aggregate and export endpoint uses this one function, which is what
// lets an export return exactly the rows the interface is displaying.
func parseFilter(q url.Values) (store.Filter, error) {
	var f store.Filter

	for _, key := range []string{"from", "to"} {
		raw := strings.TrimSpace(q.Get(key))
		if raw == "" {
			continue
		}
		norm, err := normaliseTimeBound(key, raw)
		if err != nil {
			return f, err
		}
		if key == "from" {
			f.From = norm
		} else {
			f.To = norm
		}
	}

	f.SessionType = listParam(q, "session_type")
	f.EventContext = listParam(q, "event_context")

	var err error
	if f.TrackID, err = intParam(q, "track_id"); err != nil {
		return f, err
	}
	if f.CarID, err = intParam(q, "car_id"); err != nil {
		return f, err
	}
	if f.LeagueID, err = intParam(q, "league_id"); err != nil {
		return f, err
	}

	excludeAI, err := boolParam(q, "exclude_ai")
	if err != nil {
		return f, err
	}
	f.ExcludeAI = excludeAI

	if raw := q.Get("limit"); raw != "" {
		n, convErr := strconv.Atoi(raw)
		if convErr != nil || n < 0 {
			return f, fmt.Errorf("%w: limit must be a non-negative integer", ErrBadRequest)
		}
		if n > MaxLimit {
			n = MaxLimit
		}
		f.Limit = n
	}
	if raw := q.Get("offset"); raw != "" {
		n, convErr := strconv.Atoi(raw)
		if convErr != nil || n < 0 {
			return f, fmt.Errorf("%w: offset must be a non-negative integer", ErrBadRequest)
		}
		f.Offset = n
	}
	return f, nil
}

func parseLapFilter(q url.Values) (store.LapFilter, error) {
	f, err := parseFilter(q)
	if err != nil {
		return store.LapFilter{}, err
	}
	cleanOnly, err := boolParam(q, "clean_laps")
	if err != nil {
		return store.LapFilter{}, err
	}
	return store.LapFilter{Filter: f, CleanOnly: cleanOnly}, nil
}

// normaliseTimeBound accepts either a full RFC3339 timestamp or a bare
// YYYY-MM-DD date.
//
// A bare date is expanded to cover the whole day: "from" to its start and "to" to
// its final instant. Without that, a range picker sending to=2026-08-04 would
// silently exclude everything that happened that day, since started_at carries a
// time of day.
func normaliseTimeBound(key, raw string) (string, error) {
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t.UTC().Format(time.RFC3339), nil
	}
	if d, err := time.Parse("2006-01-02", raw); err == nil {
		if key == "to" {
			return d.UTC().Add(24*time.Hour - time.Second).Format(time.RFC3339), nil
		}
		return d.UTC().Format(time.RFC3339), nil
	}
	return "", fmt.Errorf("%w: %s must be RFC3339 or YYYY-MM-DD, got %q", ErrBadRequest, key, raw)
}

// listParam collects a parameter given either as repeated keys or as a single
// comma-separated value; both forms are natural in a query string.
func listParam(q url.Values, key string) []string {
	var out []string
	for _, raw := range q[key] {
		for _, part := range strings.Split(raw, ",") {
			if p := strings.TrimSpace(part); p != "" {
				out = append(out, p)
			}
		}
	}
	return out
}

// intParam parses an optional integer parameter.
func intParam(q url.Values, key string) (*int, error) {
	raw := strings.TrimSpace(q.Get(key))
	if raw == "" {
		return nil, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: %s must be an integer", ErrBadRequest, key)
	}
	return &n, nil
}

func boolParam(q url.Values, key string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(q.Get(key))) {
	case "", "false", "0":
		return false, nil
	case "true", "1":
		return true, nil
	default:
		return false, fmt.Errorf("%w: %s must be true or false", ErrBadRequest, key)
	}
}
