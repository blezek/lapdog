package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/blezek/lapdog/internal/garage61"
	"github.com/blezek/lapdog/internal/store"
)

func TestGarageQueueProcessesAndDeletionSuppressesAutomaticDownload(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "queue.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	body, err := os.ReadFile("../garage61/testdata/python_parity.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Car, Track garage61.Entity
		Scenarios  []store.BrakeCatalogScenario
	}
	if err := json.Unmarshal(body, &fixture); err != nil {
		t.Fatal(err)
	}
	fixture.Car.PlatformID, fixture.Track.PlatformID = "67", "523"
	release := make(chan struct{})
	close(release)
	fake := &fakeGarage{catalog: garage61.Catalog{Cars: []garage61.Entity{fixture.Car}, Tracks: []garage61.Entity{fixture.Track}},
		output: store.BrakeCatalog{CatalogVersion: 1, SourceProvider: "garage61", GeneratedAt: time.Now().UTC().Format(time.RFC3339), Scenarios: fixture.Scenarios}, release: release}
	srv := New(st, fakeStatus{}, &fakeConfig{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	srv.garage = fake
	h, err := srv.Handler()
	if err != nil {
		t.Fatal(err)
	}
	post := func() {
		t.Helper()
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, jsonRequest(http.MethodPost, "/api/brake-it/garage61/queue", strings.NewReader(`{"carId":101,"trackId":202}`)))
		if rec.Code != http.StatusAccepted {
			t.Fatalf("enqueue: %d %s", rec.Code, rec.Body)
		}
	}
	post()
	worked, err := srv.processNextGarageQueue(context.Background())
	if err != nil || !worked {
		t.Fatalf("process queued combination: worked=%v err=%v", worked, err)
	}
	var queue []store.GarageQueueItem
	get(t, h, "/api/brake-it/garage61/queue", &queue)
	if len(queue) != 1 || queue[0].State != "done" || queue[0].GarageCarID != 101 || queue[0].GarageTrackID != 202 {
		t.Fatalf("finished queue=%+v", queue)
	}
	scenarioID := fixture.Scenarios[0].ID
	active, err := st.BrakeScenarioByID(scenarioID)
	if err != nil || active.Retired || len(active.Source) == 0 {
		t.Fatalf("prepared scenario=%+v err=%v", active, err)
	}
	snapshot, err := json.Marshal(active)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.InsertBrakeRun(&store.BrakeRun{ID: "before-deletion", ScenarioID: scenarioID,
		ScenarioName: active.Name, DeviceLabel: "test pedals", ScoringVersion: 1,
		ScenarioSnapshot: string(snapshot), CreatedAt: time.Now().UTC().Format(time.RFC3339),
		Metrics: store.BrakeRunMetrics{Score: 80}}); err != nil {
		t.Fatal(err)
	}
	srv.garageState.job = garageJob{State: "running", CarID: 101, TrackID: 202}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, jsonRequest(http.MethodDelete, "/api/brake-it/garage61/combinations/101/202", strings.NewReader(`{}`)))
	if rec.Code != http.StatusConflict {
		t.Fatalf("deleted combination during its own processing: %d %s", rec.Code, rec.Body)
	}
	srv.garageState.job = garageJob{State: "running", CarID: 999, TrackID: 888}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, jsonRequest(http.MethodDelete, "/api/brake-it/garage61/combinations/101/202", strings.NewReader(`{}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("delete downloaded scenarios: %d %s", rec.Code, rec.Body)
	}
	srv.garageState.job = garageJob{State: "idle"}
	removed, err := st.BrakeScenarioByID(scenarioID)
	if err != nil || !removed.Retired || len(removed.Source) != 0 {
		t.Fatalf("deleted scenario retained download: %+v err=%v", removed, err)
	}
	runs, err := st.ListBrakeRuns()
	if err != nil || len(runs) != 1 || runs[0].ScenarioID != scenarioID {
		t.Fatalf("deletion lost prior practice result: %+v err=%v", runs, err)
	}
	var savedCitationCount int
	if err := st.Reader().QueryRow(`SELECT COUNT(*) FROM brake_runs WHERE id='before-deletion'
 AND json_type(scenario_snapshot_json,'$.source') IS NOT NULL`).Scan(&savedCitationCount); err != nil || savedCitationCount != 0 {
		t.Fatalf("deletion retained citations in practice snapshot: count=%d err=%v", savedCitationCount, err)
	}
	get(t, h, "/api/brake-it/garage61/queue", &queue)
	if len(queue) != 1 || queue[0].State != "deleted" {
		t.Fatalf("deleted combination stayed queued: %+v", queue)
	}
	deleted, err := st.GarageCombinationDeleted(101, 202)
	if err != nil || !deleted {
		t.Fatalf("deletion marker=%v err=%v", deleted, err)
	}
	post() // An explicit user request may restore the combination.
	deleted, err = st.GarageCombinationDeleted(101, 202)
	if err != nil || deleted {
		t.Fatalf("explicit requeue retained deletion marker=%v err=%v", deleted, err)
	}
	worked, err = srv.processNextGarageQueue(context.Background())
	if err != nil || !worked {
		t.Fatalf("explicit reprocessing: worked=%v err=%v", worked, err)
	}
	active, err = st.BrakeScenarioByID(scenarioID)
	if err != nil || active.Retired || len(active.Source) == 0 {
		t.Fatalf("reprocessed scenario=%+v err=%v", active, err)
	}
}

func TestGarageQueueWaitsForClickOnRecordedPractice(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "manual.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	body, err := os.ReadFile("../garage61/testdata/python_parity.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Car, Track garage61.Entity
		Scenarios  []store.BrakeCatalogScenario
	}
	if err := json.Unmarshal(body, &fixture); err != nil {
		t.Fatal(err)
	}
	fixture.Car.PlatformID, fixture.Track.PlatformID = "67", "523"
	release := make(chan struct{})
	close(release)
	srv := New(st, fakeStatus{}, &fakeConfig{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	srv.garage = &fakeGarage{catalog: garage61.Catalog{Cars: []garage61.Entity{fixture.Car}, Tracks: []garage61.Entity{fixture.Track}},
		output: store.BrakeCatalog{CatalogVersion: 1, SourceProvider: "garage61", GeneratedAt: time.Now().UTC().Format(time.RFC3339), Scenarios: fixture.Scenarios}, release: release}
	h, err := srv.Handler()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { srv.runGarageQueue(ctx); close(done) }()
	defer func() { cancel(); <-done }()
	car, track := 67, 523
	carName, trackName := "Fixture car", "Fixture circuit"
	if _, err := st.UpsertSession(&store.Session{SessionKey: "recorded-practice", SessionType: "Practice", EventContext: "Test",
		StartedAt: time.Now().UTC().Format(time.RFC3339), CarID: &car, TrackID: &track,
		CarName: &carName, TrackName: &trackName}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2200 * time.Millisecond) // Cross one worker poll with a configured token.
	queue, err := st.ListGarageQueue()
	if err != nil || len(queue) != 0 {
		t.Fatalf("recorded practice queued without a click: %+v %v", queue, err)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, jsonRequest(http.MethodPost, "/api/brake-it/garage61/queue", strings.NewReader(`{"carId":101,"trackId":202}`)))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("manual enqueue: %d %s", rec.Code, rec.Body)
	}
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		queue, err = st.ListGarageQueue()
		if err != nil {
			t.Fatal(err)
		}
		if len(queue) == 1 && queue[0].State == "done" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("clicked queue did not complete: %+v", queue)
}

func TestDeleteMultipleGarageCombinationsIsAtomic(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "bulk-delete.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	body, err := os.ReadFile("../garage61/testdata/python_parity.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct{ Scenarios []store.BrakeCatalogScenario }
	if err := json.Unmarshal(body, &fixture); err != nil {
		t.Fatal(err)
	}
	first := fixture.Scenarios[0]
	second := first
	second.ID = strings.Replace(first.ID, "track-202-", "track-203-", 1)
	secondTrack := "Second layout"
	second.TrackName = &secondTrack
	for _, item := range []struct {
		trackID  int
		scenario store.BrakeCatalogScenario
	}{{202, first}, {203, second}} {
		catalog := store.BrakeCatalog{CatalogVersion: 1, SourceProvider: "garage61",
			GeneratedAt: time.Now().UTC().Format(time.RFC3339), Scenarios: []store.BrakeCatalogScenario{item.scenario}}
		if _, err := st.ImportLocalBrakeCatalog(catalog, 101, item.trackID); err != nil {
			t.Fatal(err)
		}
	}
	srv := New(st, fakeStatus{}, &fakeConfig{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	h, err := srv.Handler()
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/brake-it/garage61/combinations/delete"
	srv.garageState.job = garageJob{State: "running", CarID: 101, TrackID: 202}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, jsonRequest(http.MethodPost, path, strings.NewReader(`{"combinations":[{"carId":101,"trackId":202},{"carId":101,"trackId":203}]}`)))
	if rec.Code != http.StatusConflict {
		t.Fatalf("deleted processing combination in batch: %d %s", rec.Code, rec.Body)
	}
	srv.garageState.job = garageJob{State: "idle"}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, jsonRequest(http.MethodPost, path, strings.NewReader(`{"combinations":[{"carId":101,"trackId":202},{"carId":101,"trackId":999}]}`)))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("invalid batch status: %d %s", rec.Code, rec.Body)
	}
	stillReady, err := st.BrakeScenarioByID(first.ID)
	if err != nil || stillReady.Retired {
		t.Fatalf("invalid batch partially deleted scenarios: %+v %v", stillReady, err)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, jsonRequest(http.MethodPost, path, strings.NewReader(`{"combinations":[{"carId":101,"trackId":202},{"carId":101,"trackId":203}]}`)))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"deleted":2`) {
		t.Fatalf("bulk delete: %d %s", rec.Code, rec.Body)
	}
	for _, id := range []string{first.ID, second.ID} {
		removed, err := st.BrakeScenarioByID(id)
		if err != nil || !removed.Retired || len(removed.Source) != 0 {
			t.Fatalf("bulk delete kept downloaded scenario %q: %+v %v", id, removed, err)
		}
	}
}
