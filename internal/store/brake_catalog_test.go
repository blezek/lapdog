package store

import (
	"path/filepath"
	"strings"
	"testing"
)

const validBrakeCatalog = `{
  "catalogVersion": 1,
  "generatedAt": "2026-09-16T18:00:00Z",
  "sourceProvider": "garage61",
  "carsRequested": ["Mazda MX-5"],
  "scenarios": [{
    "id": "garage61-iracing-track-40-car-67-zone-1",
    "name": "Road Atlanta Mazda braking zone 1",
    "description": "Aggregate target from 12 visible laps.",
    "approachMs": 2200,
    "acceleratorFallTargetMs": 280,
    "brakeRiseTargetMs": 420,
    "targetBrakePercent": 76,
    "brakeTolerancePercent": 6,
    "brakeHoldMs": 1050,
    "trailBrakeReleaseMs": 1850,
    "transitionEnabled": false,
    "transitionMode": "coast",
    "transitionDurationMs": 600,
    "transitionBrakePercent": 5,
    "acceleratorRampMs": 1700,
    "carName": "Global Mazda MX-5 Cup",
    "trackName": "Road Atlanta Full Course",
    "createdAt": "2026-09-16T18:00:00Z",
    "updatedAt": "2026-09-16T18:00:00Z",
    "source": {
      "provider": "garage61",
      "method": "brake-event clustering by lap-distance percentage",
      "model": {"sourceLapCount": 12, "targetBrakeMedianPercent": 76}
    }
  }]
}`

func TestBrakeCatalogPersistsSearchMetadataInServerDatabase(t *testing.T) {
	catalog, err := DecodeBrakeCatalog(strings.NewReader(validBrakeCatalog))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "lapdog.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if count, err := st.ImportBrakeCatalog(catalog); err != nil || count != 1 {
		t.Fatalf("ImportBrakeCatalog count=%d err=%v, want 1, nil", count, err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	st, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	rows, err := st.ListBrakeScenarios()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("ListBrakeScenarios returned %d rows, want seeded plus imported", len(rows))
	}
	got := rows[1]
	if got.CarName == nil || *got.CarName != "Global Mazda MX-5 Cup" ||
		got.TrackName == nil || *got.TrackName != "Road Atlanta Full Course" ||
		got.SourceProvider == nil || *got.SourceProvider != "garage61" {
		t.Fatalf("imported search metadata = %+v", got)
	}
	if got.Origin != "builtin" {
		t.Fatalf("imported origin = %q, want read-only builtin", got.Origin)
	}
}

func TestBrakeCatalogRejectsLapIdentityBeforeImport(t *testing.T) {
	private := strings.Replace(
		validBrakeCatalog,
		`"sourceLapCount": 12`,
		`"sourceLapCount": 12, "lapId": "private-lap"`,
		1,
	)
	_, err := DecodeBrakeCatalog(strings.NewReader(private))
	if err == nil || !strings.Contains(err.Error(), "private lap-level field") {
		t.Fatalf("DecodeBrakeCatalog private data error = %v", err)
	}
}

func TestBrakeCatalogRejectsRawTelemetryBeforeImport(t *testing.T) {
	raw := strings.Replace(
		validBrakeCatalog,
		`"sourceLapCount": 12`,
		`"sourceLapCount": 12, "samples": [0.1, 0.2]`,
		1,
	)
	_, err := DecodeBrakeCatalog(strings.NewReader(raw))
	if err == nil || !strings.Contains(err.Error(), "private lap-level field") {
		t.Fatalf("DecodeBrakeCatalog raw telemetry error = %v", err)
	}
}

func TestBrakeCatalogUpsertCannotReplaceSyntheticBuiltin(t *testing.T) {
	catalog, err := DecodeBrakeCatalog(strings.NewReader(validBrakeCatalog))
	if err != nil {
		t.Fatal(err)
	}
	st := openTemp(t)
	collision := catalog.Scenarios[0].BrakeScenario
	if err := st.CreateBrakeScenario(&collision); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ImportBrakeCatalog(catalog); err == nil || !strings.Contains(err.Error(), "conflicts") {
		t.Fatalf("ImportBrakeCatalog custom collision = %v, want conflict", err)
	}
}
