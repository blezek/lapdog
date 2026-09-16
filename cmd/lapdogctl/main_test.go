package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/blezek/lapdog/internal/store"
)

func TestTimeFromName(t *testing.T) {
	cases := []struct {
		name string
		want time.Time
	}{
		{
			name: "20260812T014837Z-87900660-1.lpd",
			want: time.Date(2026, 8, 12, 1, 48, 37, 0, time.UTC),
		},
		{
			name: "20260812-014837-public-practice.lpd",
			want: time.Date(2026, 8, 12, 1, 48, 37, 0, time.UTC),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := timeFromName(tc.name); !got.Equal(tc.want) {
				t.Errorf("timeFromName(%q) = %s, want %s", tc.name, got, tc.want)
			}
		})
	}
}

func TestImportBrakeCatalogCommandWritesServerDatabase(t *testing.T) {
	car, track := "BMW M2 Racing (G87)", "Road Atlanta Full Course"
	catalog := store.BrakeCatalog{
		CatalogVersion: 1, GeneratedAt: "2026-09-16T18:00:00Z", SourceProvider: "garage61",
		Scenarios: []store.BrakeCatalogScenario{{
			BrakeScenario: store.BrakeScenario{
				ID: "garage61-iracing-track-40-car-68-zone-1", Name: "BMW M2 zone 1",
				ApproachMS: 2200, AcceleratorFallTargetMS: 280, BrakeRiseTargetMS: 420,
				TargetBrakePercent: 76, BrakeTolerancePercent: 6, BrakeHoldMS: 1050,
				TrailBrakeReleaseMS: 1850, AcceleratorRampMS: 1700,
				CarName: &car, TrackName: &track,
			},
			Source: json.RawMessage(`{"provider":"garage61","model":{"sourceLapCount":12}}`),
		}},
	}
	body, err := json.Marshal(catalog)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	catalogPath := filepath.Join(dir, "catalog.json")
	dbPath := filepath.Join(dir, "lapdog.db")
	if err := os.WriteFile(catalogPath, body, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := run("import-brake-catalog", []string{catalogPath, dbPath}); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	rows, err := st.ListBrakeScenarios()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[1].CarName == nil || *rows[1].CarName != car {
		t.Fatalf("imported scenarios = %+v", rows)
	}
}
