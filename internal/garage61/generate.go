package garage61

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/blezek/lapdog/internal/store"
)

type row struct {
	speed, pct, brake, throttle float64
	position                    int
}
type event struct {
	lap                                   lap
	start, threshold, trail, acceleration float64
	peak, rise, fall, hold, release       float64
	transition, transitionBrake           float64
	entrySpeed, minSpeed                  float64
	ramp                                  *float64
}
type cluster struct {
	center float64
	events []event
}

func loadCSV(body []byte) ([]row, error) {
	r := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(body), "\ufeff")))
	header, err := r.Read()
	if err != nil {
		return nil, errors.New("Garage61 telemetry has no CSV header")
	}
	columns := map[string]int{}
	for i, h := range header {
		columns[h] = i
	}
	for _, key := range []string{"Speed", "LapDistPct", "Brake", "Throttle", "PositionType"} {
		if _, ok := columns[key]; !ok {
			return nil, fmt.Errorf("Garage61 telemetry is missing %s", key)
		}
	}
	rows := []row{}
	for {
		record, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, errors.New("invalid Garage61 telemetry CSV")
		}
		values := make([]float64, 5)
		valid := true
		for i, key := range []string{"Speed", "LapDistPct", "Brake", "Throttle", "PositionType"} {
			v, err := strconv.ParseFloat(record[columns[key]], 64)
			if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
				valid = false
				break
			}
			values[i] = v
		}
		if valid {
			rows = append(rows, row{values[0], values[1], values[2], values[3], int(values[4])})
		}
	}
	return rows, nil
}
func smooth(values []float64) []float64 {
	out := make([]float64, len(values))
	for i := range values {
		start, end := max(0, i-3), min(len(values), i+4)
		for _, v := range values[start:end] {
			out[i] += v
		}
		out[i] /= float64(end - start)
	}
	return out
}
func quantile(values []float64, p float64) float64 {
	if len(values) == 0 {
		return 0
	}
	values = append([]float64(nil), values...)
	sort.Float64s(values)
	rank := float64(len(values)-1) * p
	lo, hi := int(math.Floor(rank)), int(math.Ceil(rank))
	return values[lo] + (values[hi]-values[lo])*(rank-float64(lo))
}
func median(v []float64) float64 { return quantile(v, 0.5) }
func rounded(v float64) int      { return int(math.RoundToEven(v)) }
func roundedPlaces(v float64, places int) float64 {
	scale := math.Pow10(places)
	return math.RoundToEven(v*scale) / scale
}
func fallback(v, def float64) float64 {
	if v == 0 {
		return def
	}
	return v
}
func distance(a, b float64) float64 { d := math.Abs(a - b); return math.Min(d, 1-d) }
func first(values []float64, start, end int, predicate func(float64, int) bool, otherwise int) int {
	for i := max(0, start); i < min(len(values), end); i++ {
		if predicate(values[i], i) {
			return i
		}
	}
	return otherwise
}

// detectEvents preserves the Python model's uniform sampling assumption: the
// CSV has no time column, so lapTime/(sample count-1) estimates sample spacing.
func detectEvents(l lap, rows []row) []event {
	if len(rows) < 2 || l.LapTime <= 0 {
		return nil
	}
	dt := l.LapTime / float64(len(rows)-1)
	brake, throttle := make([]float64, len(rows)), make([]float64, len(rows))
	for i, r := range rows {
		brake[i], throttle[i] = r.brake, r.throttle
	}
	brake, throttle = smooth(brake), smooth(throttle)
	gap, minSamples := max(3, rounded(.16/dt)), max(6, rounded(.28/dt))
	windows := [][2]int{}
	start, last := -1, -1
	for i, v := range brake {
		if v > .04 && rows[i].position == 3 {
			if start < 0 {
				start = i
			}
			last = i
		} else if start >= 0 && last >= 0 && i-last > gap {
			if last-start >= minSamples {
				windows = append(windows, [2]int{start, last})
			}
			start, last = -1, -1
		}
	}
	if start >= 0 && last >= 0 && last-start >= minSamples {
		windows = append(windows, [2]int{start, last})
	}
	events := []event{}
	for _, w := range windows {
		s, e := w[0], w[1]
		peakIndex := s
		for i := s; i <= e; i++ {
			if brake[i] > brake[peakIndex] {
				peakIndex = i
			}
		}
		peak := brake[peakIndex]
		if peak < .18 {
			continue
		}
		rise := first(brake, s, e+1, func(v float64, _ int) bool { return v >= peak*.9 }, peakIndex)
		threshold := first(brake, rise, e+1, func(v float64, i int) bool { return i > peakIndex && v < math.Max(.12, peak*.82) }, peakIndex)
		fallStart := s
		for i := max(0, s-rounded(1.5/dt)) + 1; i <= min(len(throttle)-1, s+rounded(.25/dt)); i++ {
			if throttle[i-1] >= .92 && throttle[i] < .92 {
				fallStart = i - 1
				break
			}
		}
		fallEnd := first(throttle, fallStart, min(len(throttle), s+rounded(1.5/dt)), func(v float64, _ int) bool { return v <= .12 }, s)
		accel := first(throttle, e, len(throttle), func(v float64, _ int) bool { return v >= .12 }, e)
		full := first(throttle, accel, len(throttle), func(v float64, _ int) bool { return v >= .95 }, -1)
		ms := func(n int) float64 { return math.Max(0, float64(n)*dt*1000) }
		transitionBrake := 0.0
		if accel > e {
			values := []float64{}
			for _, v := range brake[e:accel] {
				values = append(values, v*100)
			}
			transitionBrake = median(values)
		}
		minSpeed := rows[s].speed
		for _, r := range rows[s : e+1] {
			minSpeed = math.Min(minSpeed, r.speed)
		}
		ev := event{
			lap:             l,
			start:           rows[s].pct,
			threshold:       rows[threshold].pct,
			trail:           rows[e].pct,
			acceleration:    rows[accel].pct,
			peak:            peak * 100,
			rise:            ms(rise - s),
			fall:            ms(fallEnd - fallStart),
			hold:            ms(threshold - max(rise, fallEnd)),
			release:         ms(e - threshold),
			transition:      ms(accel - e),
			transitionBrake: transitionBrake,
			entrySpeed:      rows[s].speed,
			minSpeed:        minSpeed,
		}
		if full >= 0 {
			v := ms(full - accel)
			ev.ramp = &v
		}
		events = append(events, ev)
	}
	return events
}
func clusterEvents(events []event) []cluster {
	sort.SliceStable(events, func(i, j int) bool { return events[i].start < events[j].start })
	clusters := []cluster{}
	for _, e := range events {
		match := -1
		for i, c := range clusters {
			if distance(c.center, e.start) <= .018 {
				match = i
				break
			}
		}
		if match < 0 {
			clusters = append(clusters, cluster{e.start, []event{e}})
		} else {
			c := &clusters[match]
			c.events = append(c.events, e)
			values := []float64{}
			for _, item := range c.events {
				values = append(values, item.start)
			}
			c.center = fallback(median(values), c.center)
		}
	}
	return clusters
}

// Generate downloads at most twelve viewable laps and returns only aggregates
// and citations. Raw telemetry and driver identity are never persisted.
func (c *Client) Generate(ctx context.Context, car, track Entity, progress func(string)) (store.BrakeCatalog, error) {
	progress("Finding viewable laps")
	laps, err := c.laps(ctx, car.ID, track.ID)
	if err != nil {
		return store.BrakeCatalog{}, err
	}
	events := []event{}
	analyzed := 0
	for i, l := range laps {
		progress(fmt.Sprintf("Processing telemetry %d of %d", i+1, len(laps)))
		body, err := c.get(ctx, "/laps/"+url.PathEscape(l.ID)+"/csv", nil, true)
		if err != nil {
			return store.BrakeCatalog{}, err
		}
		if body == nil {
			continue
		}
		rows, err := loadCSV(body)
		if err != nil {
			return store.BrakeCatalog{}, err
		}
		if len(rows) < 100 {
			continue
		}
		found := detectEvents(l, rows)
		if len(found) > 0 {
			analyzed++
			events = append(events, found...)
		}
	}
	minimum := max(3, rounded(float64(analyzed)*.35))
	clusters := []cluster{}
	for _, c := range clusterEvents(events) {
		ids := map[string]bool{}
		for _, e := range c.events {
			ids[e.lap.ID] = true
		}
		if len(ids) >= minimum {
			clusters = append(clusters, c)
		}
	}
	sort.SliceStable(clusters, func(i, j int) bool { return clusters[i].center < clusters[j].center })
	if len(clusters) == 0 {
		// The request completed without enough supported zones. The caller keeps
		// saved scenarios and flags the combination for review.
		return store.BrakeCatalog{CatalogVersion: store.BrakeCatalogVersion,
			GeneratedAt:    time.Now().UTC().Format(time.RFC3339),
			SourceProvider: "garage61", CarsRequested: []string{car.Name},
			Scenarios: []store.BrakeCatalogScenario{}}, nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	catalog := store.BrakeCatalog{CatalogVersion: store.BrakeCatalogVersion, GeneratedAt: now, SourceProvider: "garage61", CarsRequested: []string{car.Name}, Scenarios: []store.BrakeCatalogScenario{}}
	for i, c := range clusters {
		catalog.Scenarios = append(catalog.Scenarios, makeScenario(c, i+1, analyzed, car, track, now))
	}
	return catalog, nil
}

func makeScenario(c cluster, zone, lapCount int, car, track Entity, now string) store.BrakeCatalogScenario {
	byLap := map[string]event{}
	for _, e := range c.events {
		old, ok := byLap[e.lap.ID]
		if !ok || distance(c.center, e.start) < distance(c.center, old.start) {
			byLap[e.lap.ID] = e
		}
	}
	contributors := []event{}
	for _, e := range byLap {
		contributors = append(contributors, e)
	}
	sort.Slice(contributors, func(i, j int) bool {
		if contributors[i].lap.LapTime == contributors[j].lap.LapTime {
			return contributors[i].lap.ID < contributors[j].lap.ID
		}
		return contributors[i].lap.LapTime < contributors[j].lap.LapTime
	})
	values := func(f func(event) float64) []float64 {
		v := []float64{}
		for _, e := range contributors {
			v = append(v, f(e))
		}
		return v
	}
	med := func(f func(event) float64) float64 { return median(values(f)) }
	peaks := values(func(e event) float64 { return e.peak })
	target := fallback(median(peaks), 75)
	p10, p90 := fallback(quantile(peaks, .1), target), fallback(quantile(peaks, .9), target)
	variation := math.Max(math.Abs(target-p10), math.Abs(p90-target))
	deviations := []float64{}
	for _, p := range peaks {
		deviations = append(deviations, math.Abs(p-target))
	}
	transition := med(func(e event) float64 { return e.transition })
	transitionBrake := med(func(e event) float64 { return e.transitionBrake })
	ramps := []float64{}
	for _, e := range contributors {
		if e.ramp != nil {
			ramps = append(ramps, *e.ramp)
		}
	}
	mode := "coast"
	if transition >= 250 && transitionBrake >= 2 {
		mode = "low-brake"
	}
	bounded := func(v, def, lo, hi float64) int { return rounded(math.Min(hi, math.Max(lo, fallback(v, def)))) }
	duration := bounded(transition, 600, 150, 2500)
	lowBrake := float64(bounded(transitionBrake, 5, 1, 25))
	start := fallback(med(func(e event) float64 { return e.start }), c.center)
	threshold := fallback(med(func(e event) float64 { return e.threshold }), start)
	trail := fallback(med(func(e event) float64 { return e.trail }), threshold)
	acceleration := fallback(med(func(e event) float64 { return e.acceleration }), trail)
	trackName := track.Label()
	rec := store.BrakeScenario{
		ID:                      fmt.Sprintf("garage61-%s-track-%d-car-%d-zone-%d", track.Platform, track.ID, car.ID, zone),
		Name:                    fmt.Sprintf("%s %s braking zone %d", trackName, car.Name, zone),
		Description:             fmt.Sprintf("Generated from %d Garage 61 laps. Start %.2f%% lap, trail ends %.2f%% lap.", len(contributors), start*100, trail*100),
		ApproachMS:              2200,
		AcceleratorFallTargetMS: bounded(med(func(e event) float64 { return e.fall }), 280, 80, 1600),
		BrakeRiseTargetMS:       bounded(med(func(e event) float64 { return e.rise }), 420, 120, 2200),
		TargetBrakePercent:      float64(bounded(target, 75, 20, 100)),
		BrakeTolerancePercent:   float64(bounded(variation, 0, 4, 35)),
		BrakeHoldMS:             bounded(med(func(e event) float64 { return e.hold }), 500, 250, 3500),
		TrailBrakeReleaseMS:     bounded(med(func(e event) float64 { return e.release }), 1200, 350, 4500),
		TransitionEnabled:       transition >= 250,
		TransitionMode:          &mode,
		TransitionDurationMS:    &duration,
		TransitionBrakePercent:  &lowBrake,
		AcceleratorRampMS:       bounded(median(ramps), 1700, 400, 4500),
		CreatedAt:               now,
		UpdatedAt:               now,
		CarName:                 &car.Name,
		TrackName:               &trackName,
	}
	citations := []any{}
	for _, e := range contributors {
		link := "https://garage61.net/app/analyze;t=" + url.PathEscape(e.lap.ID)
		contribution := map[string]any{
			"zone":                             zone,
			"weight":                           1.0,
			"eventStartLapPercent":             roundedPlaces(e.start*100, 4),
			"eventThresholdEndLapPercent":      roundedPlaces(e.threshold*100, 4),
			"eventTrailEndLapPercent":          roundedPlaces(e.trail*100, 4),
			"eventAccelerationStartLapPercent": roundedPlaces(e.acceleration*100, 4),
			"telemetryWindowStartLapPercent":   roundedPlaces(e.start*100, 4),
			"telemetryWindowEndLapPercent":     roundedPlaces(max(e.threshold, e.trail, e.acceleration)*100, 4),
			"peakBrakePercent":                 roundedPlaces(e.peak, 3),
			"clusterDistanceLapPercent":        roundedPlaces(distance(c.center, e.start)*100, 4),
		}
		citations = append(citations, map[string]any{"lapId": e.lap.ID, "garage61Url": link, "garage61AnalyzeUrl": link, "lapTimeSec": roundedPlaces(e.lap.LapTime, 6), "contribution": contribution})
	}
	trackSource := map[string]any{"name": track.Name}
	if track.Variant != "" {
		trackSource["variant"] = track.Variant
	}
	source := map[string]any{
		"provider":         "garage61",
		"generatedAt":      now,
		"generatorVersion": "0.2.0",
		"method":           "brake-event clustering by lap-distance percentage",
		"car":              map[string]any{"name": car.Name},
		"track":            trackSource,
		"sourceLaps":       citations,
		"model": map[string]any{
			"zone":                        zone,
			"observations":                len(c.events),
			"sourceLapCount":              len(contributors),
			"lapCoveragePercent":          roundedPlaces(float64(len(contributors))/float64(lapCount)*100, 2),
			"garage61TelemetryView":       "driving-style",
			"clusterCenterLapPercent":     roundedPlaces(c.center*100, 4),
			"startLapPercent":             roundedPlaces(start*100, 4),
			"thresholdEndLapPercent":      roundedPlaces(threshold*100, 4),
			"trailEndLapPercent":          roundedPlaces(trail*100, 4),
			"accelerationStartLapPercent": roundedPlaces(acceleration*100, 4),
			"targetBrakeMedianPercent":    roundedPlaces(target, 3),
			"targetBrakeP10Percent":       roundedPlaces(p10, 3),
			"targetBrakeP90Percent":       roundedPlaces(p90, 3),
			"targetBrakeVariationPercent": roundedPlaces(variation, 3),
			"targetBrakeMadPercent":       roundedPlaces(fallback(median(deviations), 4), 3),
			"brakeToleranceBasis":         "Garage 61 peak-brake p10-p90 envelope",
			"entrySpeedMedian":            roundedPlaces(med(func(e event) float64 { return e.entrySpeed }), 3),
			"minSpeedMedian":              roundedPlaces(med(func(e event) float64 { return e.minSpeed }), 3),
		},
	}
	body, _ := json.Marshal(source)
	return store.BrakeCatalogScenario{BrakeScenario: rec, Source: body}
}
