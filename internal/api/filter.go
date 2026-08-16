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
	if f.TrackIDs, err = intListParam(q, "track_id"); err != nil {
		return f, err
	}
	if f.CarIDs, err = intListParam(q, "car_id"); err != nil {
		return f, err
	}
	if f.LeagueID, err = intParam(q, "league_id"); err != nil {
		return f, err
	}

	if f.HourFrom, err = hourParam(q, "hour_from"); err != nil {
		return f, err
	}
	if f.HourTo, err = hourParam(q, "hour_to"); err != nil {
		return f, err
	}
	if f.Weekdays, err = weekdayParam(q, "weekday"); err != nil {
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

func intListParam(q url.Values, key string) ([]int, error) {
	parts := listParam(q, key)
	out := make([]int, 0, len(parts))
	seen := map[int]bool{}
	for _, raw := range parts {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			return nil, fmt.Errorf("%w: %s must contain non-negative integers", ErrBadRequest, key)
		}
		if !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	return out, nil
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
// A bare date is expanded to cover the whole day in the server's local zone:
// "from" to its start and "to" to its final instant. The browser and server are
// the same desktop application, so this is the calendar day the driver selected.
// Advancing by a calendar day rather than 24 hours also preserves the boundary
// across daylight-saving transitions.
func normaliseTimeBound(key, raw string) (string, error) {
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t.UTC().Format(time.RFC3339), nil
	}
	if d, err := time.ParseInLocation("2006-01-02", raw, time.Local); err == nil {
		if key == "to" {
			d = d.AddDate(0, 0, 1).Add(-time.Second)
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

// hourParam parses an optional hour-of-day bound, which must be 0..23.
//
// Out of range is rejected rather than clamped: an hour of 24 is a mistake in the
// caller, not a value with an obvious safe interpretation, and silently treating
// it as 23 would hide the bug while quietly changing what the filter means.
func hourParam(q url.Values, key string) (*int, error) {
	raw := strings.TrimSpace(q.Get(key))
	if raw == "" {
		return nil, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 || n > 23 {
		return nil, fmt.Errorf("%w: %s must be an hour 0..23", ErrBadRequest, key)
	}
	return &n, nil
}

// weekdayParam parses an optional comma-separated set of weekdays, each 0 (Sunday)
// through 6 (Saturday) — the numbering strftime('%w') uses. Duplicates are folded
// so the SQL IN clause stays minimal.
func weekdayParam(q url.Values, key string) ([]int, error) {
	seen := map[int]bool{}
	var out []int
	for _, part := range listParam(q, key) {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 || n > 6 {
			return nil, fmt.Errorf("%w: %s must be weekdays 0..6", ErrBadRequest, key)
		}
		if !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	return out, nil
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
