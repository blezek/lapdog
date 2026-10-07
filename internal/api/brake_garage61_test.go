package api

import (
	"context"
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

type fakeGarage struct {
	catalog garage61.Catalog
	output  store.BrakeCatalog
	release chan struct{}
}

func (f *fakeGarage) Configured(context.Context) bool                   { return true }
func (f *fakeGarage) Catalog(context.Context) (garage61.Catalog, error) { return f.catalog, nil }
func (f *fakeGarage) Generate(ctx context.Context, car, track garage61.Entity, progress func(string)) (store.BrakeCatalog, error) {
	progress("Processing telemetry 1 of 6")
	select {
	case <-ctx.Done():
		return store.BrakeCatalog{}, ctx.Err()
	case <-f.release:
		return f.output, nil
	}
}

func TestGarageJobLifecycle(t *testing.T) {
	_, st, cfg := newTestServer(t)
	data, err := os.ReadFile("../garage61/testdata/python_parity.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Car, Track garage61.Entity
		Scenarios  []store.BrakeCatalogScenario
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	fake := &fakeGarage{catalog: garage61.Catalog{Cars: []garage61.Entity{fixture.Car}, Tracks: []garage61.Entity{fixture.Track}}, output: store.BrakeCatalog{CatalogVersion: 1, SourceProvider: "garage61", GeneratedAt: "2026-10-06T12:00:00Z", Scenarios: fixture.Scenarios}, release: make(chan struct{})}
	srv := New(st, fakeStatus{}, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	srv.garage = fake
	h, err := srv.Handler()
	if err != nil {
		t.Fatal(err)
	}
	get(t, h, "/api/brake-it/garage61/catalog", nil)
	post := func(body string) *httptest.ResponseRecorder {
		r := httptest.NewRecorder()
		h.ServeHTTP(r, jsonRequest("POST", "/api/brake-it/garage61/job", strings.NewReader(body)))
		return r
	}
	if r := post(`{"carId":999,"trackId":202}`); r.Code != http.StatusBadRequest {
		t.Fatalf("invalid combo: %d %s", r.Code, r.Body)
	}
	input := `{"carId":101,"trackId":202}`
	if r := post(input); r.Code != http.StatusAccepted {
		t.Fatalf("start: %d %s", r.Code, r.Body)
	}
	if r := post(input); r.Code != http.StatusConflict {
		t.Fatalf("duplicate job: %d", r.Code)
	}
	close(fake.release)
	wait := func(state string) garageJob {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			var job garageJob
			get(t, h, "/api/brake-it/garage61/job", &job)
			if job.State == state {
				return job
			}
			time.Sleep(time.Millisecond)
		}
		t.Fatalf("job never reached %s", state)
		return garageJob{}
	}
	job := wait("complete")
	if job.Count != 2 || job.RunID == 0 {
		t.Fatalf("completed job=%+v", job)
	}
	row, err := st.BrakeScenarioByID(job.ScenarioID)
	if err != nil || row.CarName == nil || *row.CarName != "Fixture car" {
		t.Fatalf("saved scenario=%+v err=%v", row, err)
	}
	fake.release = make(chan struct{})
	if r := post(input); r.Code != http.StatusAccepted {
		t.Fatalf("restart: %d", r.Code)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, jsonRequest("DELETE", "/api/brake-it/garage61/job", strings.NewReader(`{}`)))
	cancelled := wait("cancelled")
	if cancelled.RunID <= job.RunID {
		t.Fatalf("new job reused run ID: first=%d cancelled=%d", job.RunID, cancelled.RunID)
	}
	after, err := st.BrakeScenarioByID(job.ScenarioID)
	if err != nil || after.UpdatedAt != row.UpdatedAt || after.Retired {
		t.Fatalf("cancel changed saved scenario: %+v %v", after, err)
	}
	fake.output.Scenarios = nil
	close(fake.release)
	if r := post(input); r.Code != http.StatusAccepted {
		t.Fatalf("start unsupported refresh: %d %s", r.Code, r.Body)
	}
	review := wait("review")
	if review.RunID <= cancelled.RunID || review.ReviewNeeded != 1 || review.Succeeded != 0 {
		t.Fatalf("unsupported refresh=%+v", review)
	}
	after, err = st.BrakeScenarioByID(job.ScenarioID)
	if err != nil || after.Retired || len(after.Source) == 0 {
		t.Fatalf("unsupported refresh removed saved scenario: %+v %v", after, err)
	}
	var status garageRefreshStatus
	get(t, h, "/api/brake-it/garage61/refresh", &status)
	found := false
	for _, combo := range status.Combinations {
		if combo.CarName == *after.CarName && combo.TrackName == *after.TrackName {
			found = combo.ReviewNeeded
		}
	}
	if !found {
		t.Fatalf("unsupported combination lacks review flag: %+v", status)
	}
}

func TestGarageMissingTokenAndCrossSiteMutation(t *testing.T) {
	t.Setenv("GARAGE61_TOKEN", "")
	h, _, _ := newTestServer(t)
	var catalog struct {
		Configured bool
		Cars       []garage61.Entity
	}
	r := get(t, h, "/api/brake-it/garage61/catalog", &catalog)
	if r.Code != 200 || catalog.Configured || catalog.Cars == nil {
		t.Fatalf("missing token response: %d %s", r.Code, r.Body)
	}
	request := jsonRequest("POST", "/api/brake-it/garage61/job", strings.NewReader(`{"carId":1,"trackId":2}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, request)
	if rec.Code != http.StatusConflict {
		t.Fatalf("missing token start: %d", rec.Code)
	}
	request = jsonRequest("POST", "/api/brake-it/garage61/job", strings.NewReader(`{}`))
	request.Header.Set("Origin", "https://other.example")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, request)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("cross-site start: %d", rec.Code)
	}
}
