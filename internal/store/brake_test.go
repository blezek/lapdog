package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
)

func TestBrakeMigrationSeedsBuiltinScenarioAndSettings(t *testing.T) {
	s := openTemp(t)
	rows, err := s.ListBrakeScenarios()
	if err != nil {
		t.Fatal(err)
	}
	var seeded *BrakeScenario
	for i := range rows {
		if rows[i].ID == "builtin-threshold-to-trail-baseline" {
			seeded = &rows[i]
			break
		}
	}
	if seeded == nil || seeded.Origin != "builtin" {
		t.Fatalf("synthetic seeded scenario missing from %+v", rows)
	}
	settings, err := s.GetBrakeSettings()
	if err != nil {
		t.Fatal(err)
	}
	if settings.SelectedScenarioID == nil || *settings.SelectedScenarioID != seeded.ID {
		t.Errorf("selected scenario = %v, want %q", settings.SelectedScenarioID, seeded.ID)
	}
	if settings.BaudRate != 115200 {
		t.Errorf("baud rate = %d, want 115200", settings.BaudRate)
	}
}

func TestBuiltinBrakeScenarioIsReadOnly(t *testing.T) {
	s := openTemp(t)
	rec, err := s.BrakeScenarioByID("builtin-threshold-to-trail-baseline")
	if err != nil {
		t.Fatal(err)
	}
	rec.Name = "Changed"
	if err := s.UpdateBrakeScenario(&rec); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("UpdateBrakeScenario built-in = %v, want ErrReadOnly", err)
	}
	if err := s.DeleteBrakeScenario(rec.ID); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("DeleteBrakeScenario built-in = %v, want ErrReadOnly", err)
	}
}

func TestCustomBrakeScenarioAndRunRoundTrip(t *testing.T) {
	s := openTemp(t)
	rec := &BrakeScenario{
		ID: "custom-one", Name: "Custom", Description: "A custom target",
		ApproachMS: 1000, AcceleratorFallTargetMS: 200, BrakeRiseTargetMS: 300,
		TargetBrakePercent: 70, BrakeTolerancePercent: 5, BrakeHoldMS: 500,
		TrailBrakeReleaseMS: 900, AcceleratorRampMS: 1000,
	}
	if err := s.CreateBrakeScenario(rec); err != nil {
		t.Fatal(err)
	}
	rec.Name = "Updated custom"
	if err := s.UpdateBrakeScenario(rec); err != nil {
		t.Fatal(err)
	}
	got, err := s.BrakeScenarioByID(rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Updated custom" || got.Origin != "custom" {
		t.Errorf("updated scenario = %+v", got)
	}

	fall := 190.0
	rampError := 4.0
	run := &BrakeRun{
		ID: "run-one", ScenarioID: rec.ID, ScenarioName: rec.Name,
		ScenarioSnapshot: `{"id":"custom-one"}`, DeviceLabel: "Keyboard",
		CreatedAt: Now(), ScoringVersion: 2, AccelerationIncluded: true,
		Metrics: BrakeRunMetrics{
			Score: 91, AcceleratorFallMS: &fall, AverageBrakeDeviationPercent: 2,
			HoldTimeInBandMS: 450, TrailErrorPercent: 3,
			AcceleratorRampErrorPercent: &rampError,
		},
		Samples: []BrakeSample{
			{TimeMS: 0, Accelerator: 100, Brake: 0, Source: "keyboard"},
			{TimeMS: 100, Accelerator: 50, Brake: 30, Source: "keyboard"},
			{TimeMS: 200, Accelerator: 0, Brake: 70, Source: "keyboard"},
		},
	}
	if err := s.InsertBrakeRun(run); err != nil {
		t.Fatal(err)
	}
	runs, err := s.ListBrakeRuns()
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || len(runs[0].Samples) != 3 {
		t.Fatalf("runs = %+v, want one run with three samples", runs)
	}
	if runs[0].Metrics.BrakeRiseMS != nil {
		t.Errorf("absent brake rise became %v", *runs[0].Metrics.BrakeRiseMS)
	}
	if !runs[0].AccelerationIncluded || runs[0].Metrics.AcceleratorRampErrorPercent == nil || *runs[0].Metrics.AcceleratorRampErrorPercent != 4 {
		t.Errorf("acceleration facts = included %v, error %v", runs[0].AccelerationIncluded, runs[0].Metrics.AcceleratorRampErrorPercent)
	}
	if err := s.DeleteBrakeScenario(rec.ID); err == nil {
		t.Fatal("deleted a scenario referenced by a run")
	}
}

func TestBrakingOnlyRunKeepsAccelerationMetricAbsent(t *testing.T) {
	s := openTemp(t)
	run := &BrakeRun{
		ID: "braking-only", ScenarioID: "builtin-threshold-to-trail-baseline",
		ScenarioName: "Baseline", ScenarioSnapshot: `{}`, DeviceLabel: "Keyboard",
		CreatedAt: Now(), ScoringVersion: 2, AccelerationIncluded: false,
		Metrics: BrakeRunMetrics{Score: 90},
		Samples: []BrakeSample{
			{TimeMS: 0, Accelerator: 100, Brake: 0, Source: "keyboard"},
			{TimeMS: 100, Accelerator: 0, Brake: 70, Source: "keyboard"},
			{TimeMS: 200, Accelerator: 0, Brake: 0, Source: "keyboard"},
		},
	}
	if err := s.InsertBrakeRun(run); err != nil {
		t.Fatal(err)
	}
	runs, err := s.ListBrakeRuns()
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || runs[0].AccelerationIncluded || runs[0].Metrics.AcceleratorRampErrorPercent != nil {
		t.Fatalf("braking-only run = %+v", runs)
	}
}

func TestBrakePracticeMigrationPreservesAccelerationMetric(t *testing.T) {
	path := filepath.Join(t.TempDir(), "version-five.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for version, name := range []string{
		"0001_init.sql", "0002_driver_identity.sql", "0003_rating_category.sql",
		"0004_brake_it.sql", "0005_brake_catalog.sql",
	} {
		body, readErr := migrationFS.ReadFile("migrations/" + name)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if _, execErr := db.Exec(string(body)); execErr != nil {
			t.Fatalf("apply %s: %v", name, execErr)
		}
		if version > 0 {
			if _, execErr := db.Exec(`UPDATE schema_version SET version=?`, version+1); execErr != nil {
				t.Fatal(execErr)
			}
		}
	}
	if _, err := db.Exec(`INSERT INTO brake_runs (
		id, scenario_id, scenario_name, scenario_snapshot_json, device_label,
		scoring_version, score, average_brake_deviation_percent,
		hold_time_in_band_ms, trail_error_percent,
		accelerator_ramp_error_percent, created_at
	) VALUES ('old-run', 'builtin-threshold-to-trail-baseline', 'Baseline', '{}',
		'Keyboard', 1, 90, 2, 400, 3, 4, ?)`, Now()); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	runs, err := s.ListBrakeRuns()
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || !runs[0].AccelerationIncluded || runs[0].Metrics.AcceleratorRampErrorPercent == nil || *runs[0].Metrics.AcceleratorRampErrorPercent != 4 {
		t.Fatalf("upgraded run = %+v", runs)
	}
}

func TestBrakeRunWriteIsAtomicAndSamplesCascade(t *testing.T) {
	s := openTemp(t)
	run := &BrakeRun{
		ID: "bad-run", ScenarioID: "builtin-threshold-to-trail-baseline",
		ScenarioName: "Baseline", ScenarioSnapshot: `{}`, DeviceLabel: "Keyboard",
		CreatedAt: Now(), ScoringVersion: 1,
		Samples: []BrakeSample{
			{TimeMS: 0, Accelerator: 100, Brake: 0, Source: "keyboard"},
			{TimeMS: 100, Accelerator: 0, Brake: 101, Source: "keyboard"},
		},
	}
	if err := s.InsertBrakeRun(run); err == nil {
		t.Fatal("invalid sample did not fail the run transaction")
	}
	var count int
	if err := s.Reader().QueryRow(`SELECT count(*) FROM brake_runs WHERE id=?`, run.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("failed transaction left %d run rows, want 0", count)
	}

	run.ID = "cascade-run"
	run.Samples[1].Brake = 70
	if err := s.InsertBrakeRun(run); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Writer().Exec(`DELETE FROM brake_runs WHERE id=?`, run.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.Reader().QueryRow(`SELECT count(*) FROM brake_samples WHERE run_id=?`, run.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("run deletion left %d samples, want 0", count)
	}
}
