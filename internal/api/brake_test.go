package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/blezek/lapdog/internal/store"
)

func TestBrakeScenarioAPIAndBuiltinProtection(t *testing.T) {
	h, _, _ := newTestServer(t)
	var scenarios []store.BrakeScenario
	rec := get(t, h, "/api/brake-it/scenarios", &scenarios)
	if rec.Code != http.StatusOK || len(scenarios) != 1 {
		t.Fatalf("GET scenarios status=%d rows=%d body=%s", rec.Code, len(scenarios), rec.Body.String())
	}

	builtin := scenarios[0]
	builtin.Name = "Must not change"
	body, _ := json.Marshal(builtin)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, jsonRequest(http.MethodPut, "/api/brake-it/scenarios/"+builtin.ID, strings.NewReader(string(body))))
	if rec.Code != http.StatusConflict {
		t.Fatalf("update built-in status=%d, want 409: %s", rec.Code, rec.Body.String())
	}

	custom := builtin
	custom.ID = "custom-api"
	custom.Name = "Custom API scenario"
	body, _ = json.Marshal(custom)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, jsonRequest(http.MethodPost, "/api/brake-it/scenarios", strings.NewReader(string(body))))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create custom status=%d: %s", rec.Code, rec.Body.String())
	}
	var created store.BrakeScenario
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Origin != "custom" || created.CatalogVersion != nil {
		t.Errorf("created scenario provenance = %+v", created)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, jsonRequest(http.MethodDelete, "/api/brake-it/scenarios/"+created.ID, strings.NewReader(`{}`)))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete custom status=%d, want 204: %s", rec.Code, rec.Body.String())
	}
}

func TestBrakeScenarioAPIExposesCatalogCarAndTrack(t *testing.T) {
	h, st, _ := newTestServer(t)
	car, track, provider := "Global Mazda MX-5 Cup", "Road Atlanta Full Course", "garage61"
	catalog := store.BrakeCatalog{
		CatalogVersion: 1, GeneratedAt: "2026-09-16T18:00:00Z", SourceProvider: provider,
		Scenarios: []store.BrakeCatalogScenario{{
			BrakeScenario: store.BrakeScenario{
				ID: "garage61-iracing-track-40-car-67-zone-1", Name: "Mazda zone 1",
				ApproachMS: 2200, AcceleratorFallTargetMS: 280, BrakeRiseTargetMS: 420,
				TargetBrakePercent: 76, BrakeTolerancePercent: 6, BrakeHoldMS: 1050,
				TrailBrakeReleaseMS: 1850, AcceleratorRampMS: 1700,
				CarName: &car, TrackName: &track, SourceProvider: &provider,
			},
			Source: json.RawMessage(`{"provider":"garage61","model":{"sourceLapCount":12}}`),
		}},
	}
	if _, err := st.ImportBrakeCatalog(catalog); err != nil {
		t.Fatal(err)
	}
	var scenarios []store.BrakeScenario
	rec := get(t, h, "/api/brake-it/scenarios", &scenarios)
	if rec.Code != http.StatusOK || len(scenarios) != 2 {
		t.Fatalf("GET scenarios status=%d rows=%d body=%s", rec.Code, len(scenarios), rec.Body.String())
	}
	got := scenarios[1]
	if got.CarName == nil || *got.CarName != car || got.TrackName == nil || *got.TrackName != track ||
		got.SourceProvider == nil || *got.SourceProvider != provider {
		t.Fatalf("catalog metadata through API = %+v", got)
	}
}

func TestBrakeRunAPIStoresServerScenarioFacts(t *testing.T) {
	h, _, _ := newTestServer(t)
	run := store.BrakeRun{
		ID: "api-run", ScenarioID: "builtin-threshold-to-trail-baseline",
		ScenarioName: "fabricated client name", DeviceLabel: "Keyboard simulator",
		Metrics: store.BrakeRunMetrics{
			Score: 88, AverageBrakeDeviationPercent: 2, HoldTimeInBandMS: 900,
			TrailErrorPercent: 3, AcceleratorRampErrorPercent: 4,
		},
		Samples: []store.BrakeSample{
			{TimeMS: 0, Accelerator: 100, Brake: 0, Source: "keyboard"},
			{TimeMS: 100, Accelerator: 50, Brake: 30, Source: "keyboard"},
			{TimeMS: 200, Accelerator: 0, Brake: 70, Source: "keyboard"},
		},
	}
	body, _ := json.Marshal(run)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, jsonRequest(http.MethodPost, "/api/brake-it/results", strings.NewReader(string(body))))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create result status=%d: %s", rec.Code, rec.Body.String())
	}
	var created store.BrakeRun
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.ScenarioName != "Threshold to Trail Baseline" {
		t.Errorf("scenario name = %q, want server-owned name", created.ScenarioName)
	}
	if created.CreatedAt == "" || created.ScoringVersion != 1 {
		t.Errorf("server facts missing: %+v", created)
	}

	var rows []store.BrakeRun
	rec = get(t, h, "/api/brake-it/results", &rows)
	if rec.Code != http.StatusOK || len(rows) != 1 || len(rows[0].Samples) != 3 {
		t.Fatalf("GET results status=%d rows=%+v", rec.Code, rows)
	}
}

func TestBrakeRunRejectsOutOfOrderSamples(t *testing.T) {
	h, _, _ := newTestServer(t)
	body := `{
		"scenarioId":"builtin-threshold-to-trail-baseline",
		"deviceLabel":"Keyboard",
		"metrics":{"score":50,"acceleratorFallMs":null,"brakeRiseMs":null,
		"averageBrakeDeviationPercent":0,"holdTimeInBandMs":0,"trailErrorPercent":0,
		"transitionErrorPercent":null,"acceleratorRampErrorPercent":0},
		"samples":[
		{"timeMs":0,"accelerator":100,"brake":0,"source":"keyboard"},
		{"timeMs":200,"accelerator":0,"brake":50,"source":"keyboard"},
		{"timeMs":100,"accelerator":0,"brake":70,"source":"keyboard"}]}`
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, jsonRequest(http.MethodPost, "/api/brake-it/results", strings.NewReader(body)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("out-of-order run status=%d, want 400: %s", rec.Code, rec.Body.String())
	}
}

func TestBrakeScenarioAPIReportsInvalidMissingAndOversizedRequests(t *testing.T) {
	h, _, _ := newTestServer(t)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, jsonRequest(http.MethodGet, "/api/brake-it/scenarios/missing", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing scenario status=%d, want 404: %s", rec.Code, rec.Body.String())
	}

	invalid := `{"name":"Invalid","approachMs":1000,"acceleratorFallTargetMs":200,
		"brakeRiseTargetMs":300,"targetBrakePercent":70,"brakeTolerancePercent":5,
		"brakeHoldMs":500,"trailBrakeReleaseMs":900,"transitionEnabled":false,
		"transitionMode":"invalid","acceleratorRampMs":1000}`
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, jsonRequest(http.MethodPost, "/api/brake-it/scenarios", strings.NewReader(invalid)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid scenario status=%d, want 400: %s", rec.Code, rec.Body.String())
	}

	oversized := `{"name":"` + strings.Repeat("x", brakeRequestLimit) + `"}`
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, jsonRequest(http.MethodPost, "/api/brake-it/scenarios", strings.NewReader(oversized)))
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized scenario status=%d, want 413: %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, jsonRequest(http.MethodPatch, "/api/brake-it/results", strings.NewReader(`{}`)))
	if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != "GET, POST" {
		t.Fatalf("wrong method status=%d allow=%q, want 405 and GET, POST", rec.Code, rec.Header().Get("Allow"))
	}
}
