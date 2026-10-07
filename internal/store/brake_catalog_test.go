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
    "id": "garage61-test-road-atlanta-mx5-zone-1",
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
      "model": {"sourceLapCount": 12, "targetBrakeMedianPercent": 76},
      "sourceLaps": [{
        "lapId": "lap-one",
        "garage61Url": "https://garage61.net/app/analyze;t=lap-one",
        "lapTimeSec": 87.123,
        "contribution": {"zone": 1}
      }]
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
	before, err := st.ListBrakeScenarios()
	if err != nil {
		t.Fatal(err)
	}
	if count, err := st.ImportBrakeCatalog(catalog); err != nil || count != 1 {
		t.Fatalf("ImportBrakeCatalog count=%d err=%v, want 1, nil", count, err)
	}
	if count, err := st.ImportBrakeCatalog(catalog); err != nil || count != 1 {
		t.Fatalf("second ImportBrakeCatalog count=%d err=%v, want 1, nil", count, err)
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
	if len(rows) != len(before)+1 {
		t.Fatalf("ListBrakeScenarios returned %d rows, want %d", len(rows), len(before)+1)
	}
	got, err := st.BrakeScenarioByID("garage61-test-road-atlanta-mx5-zone-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.CarName == nil || *got.CarName != "Global Mazda MX-5 Cup" ||
		got.TrackName == nil || *got.TrackName != "Road Atlanta Full Course" ||
		got.SourceProvider == nil || *got.SourceProvider != "garage61" {
		t.Fatalf("imported search metadata = %+v", got)
	}
	if got.Origin != "builtin" {
		t.Fatalf("imported origin = %q, want read-only builtin", got.Origin)
	}
	if !strings.Contains(string(got.Source), `https://garage61.net/app/analyze;t=lap-one`) {
		t.Fatalf("imported source lost Garage61 lap citation: %s", got.Source)
	}
}

func TestPackagedBrakeCatalogIsReconciled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lapdog.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	before, err := st.ListBrakeScenarios()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.importPackagedBrakeCatalogBytes([]byte(validBrakeCatalog)); err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	rows, err := st.ListBrakeScenarios()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != len(before)+1 {
		t.Fatalf("reconciled scenarios = %+v", rows)
	}
}

func TestBrakeCatalogRejectsDriverIdentityBeforeImport(t *testing.T) {
	private := strings.Replace(
		validBrakeCatalog,
		`"sourceLapCount": 12`,
		`"sourceLapCount": 12, "driver": {"name": "Private"}`,
		1,
	)
	_, err := DecodeBrakeCatalog(strings.NewReader(private))
	if err == nil || !strings.Contains(err.Error(), "private lap-level field") {
		t.Fatalf("DecodeBrakeCatalog private data error = %v", err)
	}
}

func TestBrakeCatalogRejectsNonGarage61CitationURL(t *testing.T) {
	private := strings.Replace(
		validBrakeCatalog,
		`https://garage61.net/app/analyze;t=lap-one`,
		`https://example.com/app/analyze;t=lap-one`,
		1,
	)
	_, err := DecodeBrakeCatalog(strings.NewReader(private))
	if err == nil || !strings.Contains(err.Error(), "invalid Garage61 citation URL") {
		t.Fatalf("DecodeBrakeCatalog external URL error = %v", err)
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

func TestLocalBrakeCatalogReplacementSurvivesPackagedImport(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "local.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	catalog, err := DecodeBrakeCatalog(strings.NewReader(validBrakeCatalog))
	if err != nil {
		t.Fatal(err)
	}
	first := catalog.Scenarios[0]
	first.ID = "garage61-iracing-track-202-car-101-zone-1"
	second := first
	second.ID = "garage61-iracing-track-202-car-101-zone-2"
	catalog.Scenarios = []BrakeCatalogScenario{first, second}
	if _, err := st.ImportBrakeCatalog(catalog); err != nil {
		t.Fatal(err)
	}
	all, err := st.Garage61Combinations()
	if err != nil {
		t.Fatal(err)
	}
	foundPackagedIDs := false
	for _, combo := range all {
		if combo.CarName == *first.CarName && combo.TrackName == *first.TrackName && combo.CarID == 101 && combo.TrackID == 202 {
			foundPackagedIDs = true
		}
	}
	if !foundPackagedIDs {
		t.Fatalf("packaged combination IDs were not recovered: %+v", all)
	}
	local := catalog
	local.Scenarios = []BrakeCatalogScenario{first}
	local.Scenarios[0].TargetBrakePercent = 55
	if count, err := st.ImportLocalBrakeCatalog(local, 101, 202); err != nil || count != 1 {
		t.Fatalf("local import=%d,%v", count, err)
	}
	// A bundled catalog must not restore an obsolete zone or introduce another
	// old zone into a combination the user already regenerated locally.
	third := first
	third.ID = "garage61-iracing-track-202-car-101-zone-3"
	catalog.Scenarios = append(catalog.Scenarios, third)
	if count, err := st.ImportBrakeCatalog(catalog); err != nil || count != 0 {
		t.Fatalf("packaged import=%d,%v", count, err)
	}
	got, err := st.BrakeScenarioByID(first.ID)
	if err != nil || got.TargetBrakePercent != 55 {
		t.Fatalf("local target=%+v err=%v", got, err)
	}
	retired, err := st.BrakeScenarioByID(second.ID)
	if err != nil || !retired.Retired || len(retired.Source) != 0 {
		t.Fatalf("obsolete zone=%+v err=%v", retired, err)
	}
	if _, err := st.BrakeScenarioByID(third.ID); err == nil {
		t.Fatal("packaged zone was added to local combination")
	}
	// A mid-import conflict rolls back retirement as well as scenario changes.
	custom := first.BrakeScenario
	custom.ID = "garage61-iracing-track-202-car-101-zone-4"
	custom.Name = "User-owned collision"
	if err := st.CreateBrakeScenario(&custom); err != nil {
		t.Fatal(err)
	}
	conflict := first
	conflict.ID = custom.ID
	local.Scenarios = append(local.Scenarios, conflict)
	if _, err := st.ImportLocalBrakeCatalog(local, 101, 202); err == nil {
		t.Fatal("expected conflict")
	}
	got, err = st.BrakeScenarioByID(first.ID)
	if err != nil || got.Retired {
		t.Fatalf("failed import retired prior scenario: %+v %v", got, err)
	}
	empty := BrakeCatalog{CatalogVersion: BrakeCatalogVersion, SourceProvider: "garage61", GeneratedAt: "2026-10-06T13:00:00Z", Scenarios: []BrakeCatalogScenario{}}
	if _, err := st.ImportLocalBrakeCatalog(empty, 101, 202); err == nil {
		t.Fatal("empty refresh was imported")
	}
	if affected, err := st.FlagGarageCombination(101, 202); err != nil || affected != 1 {
		t.Fatalf("flag saved combination: affected=%d err=%v", affected, err)
	}
	kept, err := st.BrakeScenarioByID(first.ID)
	if err != nil || kept.Retired || len(kept.Source) == 0 {
		t.Fatalf("unavailable source retired saved scenario: %+v err=%v", kept, err)
	}
	combos, err := st.Garage61Combinations()
	if err != nil || !hasReviewFlag(combos, 101, 202, true) {
		t.Fatalf("review flag missing: %+v err=%v", combos, err)
	}
	if count, err := st.ImportBrakeCatalog(catalog); err != nil || count != 0 {
		t.Fatalf("packaged catalog resurrected retired zone: count=%d err=%v", count, err)
	}
	local.Scenarios = []BrakeCatalogScenario{first}
	if count, err := st.ImportLocalBrakeCatalog(local, 101, 202); err != nil || count != 1 {
		t.Fatalf("successful refresh: count=%d err=%v", count, err)
	}
	combos, err = st.Garage61Combinations()
	if err != nil || !hasReviewFlag(combos, 101, 202, false) {
		t.Fatalf("successful refresh retained review flag: %+v err=%v", combos, err)
	}
}

func hasReviewFlag(combos []Garage61Combination, carID, trackID int, want bool) bool {
	for _, combo := range combos {
		if combo.CarID == carID && combo.TrackID == trackID {
			return combo.ReviewNeeded == want
		}
	}
	return false
}
