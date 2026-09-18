package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

// ErrReadOnly indicates an attempt to change a built-in Brake-it scenario.
var ErrReadOnly = errors.New("store: built-in brake scenario is read-only")

// BrakeScenario is one pedal-training target.
type BrakeScenario struct {
	ID                      string          `json:"id"`
	Name                    string          `json:"name"`
	Description             string          `json:"description"`
	ApproachMS              int             `json:"approachMs"`
	AcceleratorFallTargetMS int             `json:"acceleratorFallTargetMs"`
	BrakeRiseTargetMS       int             `json:"brakeRiseTargetMs"`
	TargetBrakePercent      float64         `json:"targetBrakePercent"`
	BrakeTolerancePercent   float64         `json:"brakeTolerancePercent"`
	BrakeHoldMS             int             `json:"brakeHoldMs"`
	TrailBrakeReleaseMS     int             `json:"trailBrakeReleaseMs"`
	TransitionEnabled       bool            `json:"transitionEnabled"`
	TransitionMode          *string         `json:"transitionMode"`
	TransitionDurationMS    *int            `json:"transitionDurationMs"`
	TransitionBrakePercent  *float64        `json:"transitionBrakePercent"`
	AcceleratorRampMS       int             `json:"acceleratorRampMs"`
	Origin                  string          `json:"origin"`
	CatalogVersion          *int            `json:"catalogVersion"`
	CarName                 *string         `json:"carName"`
	TrackName               *string         `json:"trackName"`
	SourceProvider          *string         `json:"sourceProvider"`
	Source                  json.RawMessage `json:"source,omitempty"`
	Retired                 bool            `json:"retired"`
	CreatedAt               string          `json:"createdAt"`
	UpdatedAt               string          `json:"updatedAt"`
}

const brakeScenarioColumns = `
	id, name, description, approach_ms, accelerator_fall_target_ms,
	brake_rise_target_ms, target_brake_percent, brake_tolerance_percent,
	brake_hold_ms, trail_brake_release_ms, transition_enabled,
	transition_mode, transition_duration_ms, transition_brake_percent,
	accelerator_ramp_ms, origin, catalog_version, car_name, track_name,
	source_provider, source_json, retired, created_at, updated_at`

func scanBrakeScenario(row rowScanner) (BrakeScenario, error) {
	var out BrakeScenario
	var source sql.NullString
	err := row.Scan(
		&out.ID, &out.Name, &out.Description, &out.ApproachMS,
		&out.AcceleratorFallTargetMS, &out.BrakeRiseTargetMS,
		&out.TargetBrakePercent, &out.BrakeTolerancePercent, &out.BrakeHoldMS,
		&out.TrailBrakeReleaseMS, &out.TransitionEnabled, &out.TransitionMode,
		&out.TransitionDurationMS, &out.TransitionBrakePercent,
		&out.AcceleratorRampMS, &out.Origin, &out.CatalogVersion, &out.CarName,
		&out.TrackName, &out.SourceProvider, &source, &out.Retired,
		&out.CreatedAt, &out.UpdatedAt,
	)
	if err == nil && source.Valid {
		out.Source = json.RawMessage(source.String)
	}
	return out, err
}

// ListBrakeScenarios returns active scenarios with built-ins first.
func (s *Store) ListBrakeScenarios() ([]BrakeScenario, error) {
	rows, err := s.reader.Query(`SELECT ` + brakeScenarioColumns + `
		FROM brake_scenarios WHERE retired = 0
		ORDER BY CASE origin WHEN 'builtin' THEN 0 ELSE 1 END,
			COALESCE(car_name, ''), COALESCE(track_name, ''), name, id`)
	if err != nil {
		return nil, fmt.Errorf("store: list brake scenarios: %w", err)
	}
	defer rows.Close()
	out := []BrakeScenario{}
	for rows.Next() {
		rec, err := scanBrakeScenario(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan brake scenario: %w", err)
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

// BrakeScenarioByID returns one scenario, including a retired one.
func (s *Store) BrakeScenarioByID(id string) (BrakeScenario, error) {
	rec, err := scanBrakeScenario(s.reader.QueryRow(
		`SELECT `+brakeScenarioColumns+` FROM brake_scenarios WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return BrakeScenario{}, fmt.Errorf("%w: brake scenario %q", ErrNotFound, id)
	}
	if err != nil {
		return BrakeScenario{}, fmt.Errorf("store: read brake scenario %q: %w", id, err)
	}
	return rec, nil
}

// CreateBrakeScenario inserts a custom scenario.
func (s *Store) CreateBrakeScenario(rec *BrakeScenario) error {
	if rec == nil {
		return errors.New("store: CreateBrakeScenario called with nil record")
	}
	rec.Origin = "custom"
	rec.CatalogVersion = nil
	rec.SourceProvider = nil
	rec.Source = nil
	rec.Retired = false
	rec.CreatedAt = Now()
	rec.UpdatedAt = rec.CreatedAt
	_, err := s.writer.Exec(`INSERT INTO brake_scenarios (`+brakeScenarioColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		rec.ID, rec.Name, rec.Description, rec.ApproachMS, rec.AcceleratorFallTargetMS,
		rec.BrakeRiseTargetMS, rec.TargetBrakePercent, rec.BrakeTolerancePercent,
		rec.BrakeHoldMS, rec.TrailBrakeReleaseMS, rec.TransitionEnabled,
		rec.TransitionMode, rec.TransitionDurationMS, rec.TransitionBrakePercent,
		rec.AcceleratorRampMS, rec.Origin, rec.CatalogVersion, rec.CarName,
		rec.TrackName, rec.SourceProvider, rec.Source, rec.Retired,
		rec.CreatedAt, rec.UpdatedAt)
	if err != nil {
		return fmt.Errorf("store: create brake scenario %q: %w", rec.ID, err)
	}
	return nil
}

// UpdateBrakeScenario replaces an existing custom scenario.
func (s *Store) UpdateBrakeScenario(rec *BrakeScenario) error {
	if rec == nil {
		return errors.New("store: UpdateBrakeScenario called with nil record")
	}
	existing, err := s.BrakeScenarioByID(rec.ID)
	if err != nil {
		return err
	}
	if existing.Origin != "custom" {
		return fmt.Errorf("%w: %s", ErrReadOnly, rec.ID)
	}
	rec.Origin = "custom"
	rec.CatalogVersion = nil
	rec.SourceProvider = nil
	rec.Source = nil
	rec.Retired = false
	rec.CreatedAt = existing.CreatedAt
	rec.UpdatedAt = Now()
	res, err := s.writer.Exec(`UPDATE brake_scenarios SET
		name=?, description=?, approach_ms=?, accelerator_fall_target_ms=?,
		brake_rise_target_ms=?, target_brake_percent=?, brake_tolerance_percent=?,
		brake_hold_ms=?, trail_brake_release_ms=?, transition_enabled=?,
		transition_mode=?, transition_duration_ms=?, transition_brake_percent=?,
		accelerator_ramp_ms=?, car_name=?, track_name=?, source_provider=NULL, source_json=NULL,
		updated_at=? WHERE id=? AND origin='custom'`,
		rec.Name, rec.Description, rec.ApproachMS, rec.AcceleratorFallTargetMS,
		rec.BrakeRiseTargetMS, rec.TargetBrakePercent, rec.BrakeTolerancePercent,
		rec.BrakeHoldMS, rec.TrailBrakeReleaseMS, rec.TransitionEnabled,
		rec.TransitionMode, rec.TransitionDurationMS, rec.TransitionBrakePercent,
		rec.AcceleratorRampMS, rec.CarName, rec.TrackName, rec.UpdatedAt, rec.ID)
	if err != nil {
		return fmt.Errorf("store: update brake scenario %q: %w", rec.ID, err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("%w: brake scenario %q", ErrNotFound, rec.ID)
	}
	return nil
}

// DeleteBrakeScenario deletes a custom scenario with no recorded runs.
func (s *Store) DeleteBrakeScenario(id string) error {
	rec, err := s.BrakeScenarioByID(id)
	if err != nil {
		return err
	}
	if rec.Origin != "custom" {
		return fmt.Errorf("%w: %s", ErrReadOnly, id)
	}
	if _, err := s.writer.Exec(`DELETE FROM brake_scenarios WHERE id=?`, id); err != nil {
		return fmt.Errorf("store: delete brake scenario %q: %w", id, err)
	}
	return nil
}

// BrakeSample is one sampled pedal position in a completed run.
type BrakeSample struct {
	TimeMS      float64 `json:"timeMs"`
	Accelerator float64 `json:"accelerator"`
	Brake       float64 `json:"brake"`
	Source      string  `json:"source"`
}

// BrakeRunMetrics is the scored result of a completed run.
type BrakeRunMetrics struct {
	Score                        float64  `json:"score"`
	AcceleratorFallMS            *float64 `json:"acceleratorFallMs"`
	BrakeRiseMS                  *float64 `json:"brakeRiseMs"`
	AverageBrakeDeviationPercent float64  `json:"averageBrakeDeviationPercent"`
	HoldTimeInBandMS             float64  `json:"holdTimeInBandMs"`
	TrailErrorPercent            float64  `json:"trailErrorPercent"`
	TransitionErrorPercent       *float64 `json:"transitionErrorPercent"`
	AcceleratorRampErrorPercent  *float64 `json:"acceleratorRampErrorPercent"`
}

// BrakeRun is a completed Brake-it exercise and its samples.
type BrakeRun struct {
	ID                   string          `json:"id"`
	ScenarioID           string          `json:"scenarioId"`
	ScenarioName         string          `json:"scenarioName"`
	DeviceLabel          string          `json:"deviceLabel"`
	CreatedAt            string          `json:"createdAt"`
	ScoringVersion       int             `json:"scoringVersion"`
	AccelerationIncluded bool            `json:"accelerationIncluded"`
	Metrics              BrakeRunMetrics `json:"metrics"`
	Samples              []BrakeSample   `json:"samples"`
	ScenarioSnapshot     string          `json:"-"`
}

// InsertBrakeRun atomically stores a run and every sample.
func (s *Store) InsertBrakeRun(run *BrakeRun) error {
	if run == nil {
		return errors.New("store: InsertBrakeRun called with nil run")
	}
	tx, err := s.writer.Begin()
	if err != nil {
		return fmt.Errorf("store: begin brake run: %w", err)
	}
	defer tx.Rollback()
	legacyAcceleratorRampError := 0.0
	if run.Metrics.AcceleratorRampErrorPercent != nil {
		legacyAcceleratorRampError = *run.Metrics.AcceleratorRampErrorPercent
	}
	_, err = tx.Exec(`INSERT INTO brake_runs (
		id, scenario_id, scenario_name, scenario_snapshot_json, device_label,
		scoring_version, score, accelerator_fall_ms, brake_rise_ms,
		average_brake_deviation_percent, hold_time_in_band_ms, trail_error_percent,
		transition_error_percent, accelerator_ramp_error_percent_v1,
		acceleration_included, accelerator_ramp_error_percent, created_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		run.ID, run.ScenarioID, run.ScenarioName, run.ScenarioSnapshot,
		run.DeviceLabel, run.ScoringVersion, run.Metrics.Score,
		run.Metrics.AcceleratorFallMS, run.Metrics.BrakeRiseMS,
		run.Metrics.AverageBrakeDeviationPercent, run.Metrics.HoldTimeInBandMS,
		run.Metrics.TrailErrorPercent, run.Metrics.TransitionErrorPercent,
		legacyAcceleratorRampError, run.AccelerationIncluded,
		run.Metrics.AcceleratorRampErrorPercent, run.CreatedAt)
	if err != nil {
		return fmt.Errorf("store: insert brake run %q: %w", run.ID, err)
	}
	stmt, err := tx.Prepare(`INSERT INTO brake_samples
		(run_id, sequence, time_ms, accelerator_percent, brake_percent, source)
		VALUES (?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("store: prepare brake samples: %w", err)
	}
	defer stmt.Close()
	for i, sample := range run.Samples {
		if _, err := stmt.Exec(run.ID, i, sample.TimeMS, sample.Accelerator, sample.Brake, sample.Source); err != nil {
			return fmt.Errorf("store: insert brake sample %d: %w", i, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit brake run %q: %w", run.ID, err)
	}
	return nil
}

// ListBrakeRuns returns newest runs first, including their sampled traces.
func (s *Store) ListBrakeRuns() ([]BrakeRun, error) {
	rows, err := s.reader.Query(`SELECT id, scenario_id, scenario_name, device_label,
		created_at, scoring_version, score, accelerator_fall_ms, brake_rise_ms,
		average_brake_deviation_percent, hold_time_in_band_ms, trail_error_percent,
		transition_error_percent, acceleration_included, accelerator_ramp_error_percent
		FROM brake_runs ORDER BY created_at DESC, id DESC`)
	if err != nil {
		return nil, fmt.Errorf("store: list brake runs: %w", err)
	}
	defer rows.Close()
	out := []BrakeRun{}
	for rows.Next() {
		var run BrakeRun
		if err := rows.Scan(&run.ID, &run.ScenarioID, &run.ScenarioName,
			&run.DeviceLabel, &run.CreatedAt, &run.ScoringVersion,
			&run.Metrics.Score, &run.Metrics.AcceleratorFallMS,
			&run.Metrics.BrakeRiseMS, &run.Metrics.AverageBrakeDeviationPercent,
			&run.Metrics.HoldTimeInBandMS, &run.Metrics.TrailErrorPercent,
			&run.Metrics.TransitionErrorPercent,
			&run.AccelerationIncluded,
			&run.Metrics.AcceleratorRampErrorPercent); err != nil {
			return nil, fmt.Errorf("store: scan brake run: %w", err)
		}
		samples, err := s.brakeSamples(run.ID)
		if err != nil {
			return nil, err
		}
		run.Samples = samples
		out = append(out, run)
	}
	return out, rows.Err()
}

func (s *Store) brakeSamples(runID string) ([]BrakeSample, error) {
	rows, err := s.reader.Query(`SELECT time_ms, accelerator_percent, brake_percent, source
		FROM brake_samples WHERE run_id=? ORDER BY sequence`, runID)
	if err != nil {
		return nil, fmt.Errorf("store: list samples for brake run %q: %w", runID, err)
	}
	defer rows.Close()
	out := []BrakeSample{}
	for rows.Next() {
		var sample BrakeSample
		if err := rows.Scan(&sample.TimeMS, &sample.Accelerator, &sample.Brake, &sample.Source); err != nil {
			return nil, fmt.Errorf("store: scan brake sample: %w", err)
		}
		out = append(out, sample)
	}
	return out, rows.Err()
}

// BrakeSettings are the durable, non-permission parts of device setup.
type BrakeSettings struct {
	SelectedScenarioID *string `json:"selectedScenarioId"`
	BaudRate           int     `json:"baudRate"`
	USBVendorID        *string `json:"usbVendorId"`
	USBProductID       *string `json:"usbProductId"`
}

// GetBrakeSettings returns the singleton settings row.
func (s *Store) GetBrakeSettings() (BrakeSettings, error) {
	var out BrakeSettings
	err := s.reader.QueryRow(`SELECT selected_scenario_id, baud_rate, usb_vendor_id, usb_product_id
		FROM brake_settings WHERE id=1`).Scan(
		&out.SelectedScenarioID, &out.BaudRate, &out.USBVendorID, &out.USBProductID)
	if err != nil {
		return BrakeSettings{}, fmt.Errorf("store: read brake settings: %w", err)
	}
	return out, nil
}

// SetBrakeSettings replaces the singleton settings row.
func (s *Store) SetBrakeSettings(settings BrakeSettings) error {
	_, err := s.writer.Exec(`UPDATE brake_settings SET
		selected_scenario_id=?, baud_rate=?, usb_vendor_id=?, usb_product_id=?, updated_at=?
		WHERE id=1`, settings.SelectedScenarioID, settings.BaudRate,
		settings.USBVendorID, settings.USBProductID, Now())
	if err != nil {
		return fmt.Errorf("store: update brake settings: %w", err)
	}
	return nil
}
