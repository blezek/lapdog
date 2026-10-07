package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/blezek/lapdog/internal/garage61"
	"github.com/blezek/lapdog/internal/store"
)

func TestGarage61WeeklyStatusSnoozeAndRefreshAll(t *testing.T) {
	_, st, cfg := newTestServer(t)
	data, err := os.ReadFile("../garage61/testdata/python_parity.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Car, Track garage61.Entity
		Scenarios  []store.BrakeCatalogScenario
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	fixture.Car.PlatformID = "67"
	fixture.Track.PlatformID = "523"
	old := store.BrakeCatalog{CatalogVersion: 1, SourceProvider: "garage61", GeneratedAt: "2026-09-01T00:00:00Z", Scenarios: fixture.Scenarios}
	for i := range old.Scenarios {
		old.Scenarios[i].UpdatedAt = old.GeneratedAt
	}
	if _, err := st.ImportBrakeCatalog(old); err != nil {
		t.Fatal(err)
	}
	current := old
	current.GeneratedAt = time.Now().UTC().Format(time.RFC3339)
	fake := &fakeGarage{catalog: garage61.Catalog{Cars: []garage61.Entity{fixture.Car}, Tracks: []garage61.Entity{fixture.Track}}, output: current, release: make(chan struct{})}
	srv := New(st, fakeStatus{}, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	srv.garage = fake
	h, err := srv.Handler()
	if err != nil {
		t.Fatal(err)
	}
	var status garageRefreshStatus
	get(t, h, "/api/brake-it/garage61/refresh", &status)
	if !status.ReminderDue || status.DueCount < 1 || len(status.Combinations) < 1 {
		t.Fatalf("weekly status=%+v", status)
	}
	var mine []store.MyDrivenCombination
	get(t, h, "/api/brake-it/garage61/my-combinations", &mine)
	if len(mine) == 0 || mine[0].CarPlatformID == 0 || mine[0].TrackPlatformID == 0 {
		t.Fatalf("recorded combos=%+v", mine)
	}
	tomorrow := time.Now().UTC().Add(24 * time.Hour).Format(time.RFC3339)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, jsonRequest(http.MethodPut, "/api/brake-it/garage61/snooze", strings.NewReader(`{"until":"`+tomorrow+`"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("snooze: %d %s", rec.Code, rec.Body)
	}
	get(t, h, "/api/brake-it/garage61/refresh", &status)
	if status.ReminderDue || status.SnoozeUntil == nil {
		t.Fatalf("snoozed status=%+v", status)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, jsonRequest(http.MethodPut, "/api/brake-it/garage61/snooze", strings.NewReader(`{"until":"2050-01-01T00:00:00Z"}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unbounded snooze: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, jsonRequest(http.MethodPost, "/api/brake-it/garage61/job", strings.NewReader(`{"all":true}`)))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("refresh all: %d %s", rec.Code, rec.Body)
	}
	close(fake.release)
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		var job garageJob
		get(t, h, "/api/brake-it/garage61/job", &job)
		if job.State == "partial" {
			if job.Total != 1 || job.Succeeded != 1 || job.Count != 2 {
				t.Fatalf("batch job=%+v", job)
			}
			if len(job.Errors) == 0 {
				t.Fatalf("unmatched saved combinations were not reported: %+v", job)
			}
			return
		}
		if job.State == "failed" {
			t.Fatalf("batch failed: %+v", job)
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("batch did not finish")
}
func TestGarage61SavedCombinationResolutionIsExact(t *testing.T) {
	catalog := garage61.Catalog{Cars: []garage61.Entity{{ID: 8, Name: "Mazda", Platform: "iracing"}}, Tracks: []garage61.Entity{{ID: 444, Name: "Spa", Variant: "Grand Prix Pits", Platform: "iracing"}, {ID: 445, Name: "Spa", Variant: "Bike", Platform: "iracing"}}}
	car, track, err := resolveSavedGarageCombination(store.Garage61Combination{CarName: "Mazda", TrackName: "Spa Grand Prix Pits"}, catalog)
	if err != nil || car.ID != 8 || track.ID != 444 {
		t.Fatalf("resolved=%+v %+v err=%v", car, track, err)
	}
	_, _, err = resolveSavedGarageCombination(store.Garage61Combination{CarName: "Mazda", TrackName: "Spa Grand Prix"}, catalog)
	if err == nil {
		t.Fatal("similar but non-exact layout was selected")
	}
}
