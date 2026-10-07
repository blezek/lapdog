package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"strings"
)

const BrakeCatalogVersion = 1

type brakeCatalogFiles interface {
	ReadFile(string) ([]byte, error)
}

// BrakeCatalog is the privacy-screened interchange format produced by the
// local Garage61 generator. It contains aggregate targets and Garage61 lap
// citations, but never raw telemetry, credentials, or driver identity.
type BrakeCatalog struct {
	CatalogVersion int                    `json:"catalogVersion"`
	GeneratedAt    string                 `json:"generatedAt"`
	SourceProvider string                 `json:"sourceProvider"`
	CarsRequested  []string               `json:"carsRequested"`
	Scenarios      []BrakeCatalogScenario `json:"scenarios"`
}

// BrakeCatalogScenario adds aggregate generation provenance to a scenario.
type BrakeCatalogScenario struct {
	BrakeScenario
	Source json.RawMessage `json:"source"`
}

// DecodeBrakeCatalog reads and validates a generated catalog before it reaches
// SQLite. The second decode rejects trailing JSON values just as the HTTP API
// does.
func DecodeBrakeCatalog(r io.Reader) (BrakeCatalog, error) {
	var catalog BrakeCatalog
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&catalog); err != nil {
		return BrakeCatalog{}, fmt.Errorf("store: decode brake catalog: %w", err)
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return BrakeCatalog{}, errors.New("store: decode brake catalog: multiple JSON values")
		}
		return BrakeCatalog{}, fmt.Errorf("store: decode brake catalog: %w", err)
	}
	if err := validateBrakeCatalog(catalog); err != nil {
		return BrakeCatalog{}, err
	}
	return catalog, nil
}

// importPackagedBrakeCatalog reconciles the catalog captured when this binary
// was built. A clean checkout has no catalog.json and retains only the
// synthetic scenario seeded by the migration.
func (s *Store) importPackagedBrakeCatalog() error {
	body, err := packagedBrakeCatalogFS.ReadFile("brake_catalog_data/catalog.json")
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("store: read packaged brake catalog: %w", err)
	}
	return s.importPackagedBrakeCatalogBytes(body)
}

func (s *Store) importPackagedBrakeCatalogBytes(body []byte) error {
	catalog, err := DecodeBrakeCatalog(strings.NewReader(string(body)))
	if err != nil {
		return fmt.Errorf("store: packaged brake catalog: %w", err)
	}
	if _, err := s.ImportBrakeCatalog(catalog); err != nil {
		return fmt.Errorf("store: reconcile packaged brake catalog: %w", err)
	}
	return nil
}

func validateBrakeCatalog(catalog BrakeCatalog) error {
	if catalog.CatalogVersion != BrakeCatalogVersion {
		return fmt.Errorf("store: brake catalog version %d, want %d", catalog.CatalogVersion, BrakeCatalogVersion)
	}
	if catalog.SourceProvider != "garage61" {
		return fmt.Errorf("store: brake catalog provider %q, want garage61", catalog.SourceProvider)
	}
	if strings.TrimSpace(catalog.GeneratedAt) == "" {
		return errors.New("store: brake catalog generatedAt is required")
	}
	if len(catalog.Scenarios) == 0 {
		return errors.New("store: brake catalog has no scenarios")
	}
	seen := make(map[string]struct{}, len(catalog.Scenarios))
	for i, item := range catalog.Scenarios {
		rec := item.BrakeScenario
		if !strings.HasPrefix(rec.ID, "garage61-") || strings.TrimSpace(rec.Name) == "" {
			return fmt.Errorf("store: brake catalog scenario %d has an invalid id or name", i)
		}
		if _, exists := seen[rec.ID]; exists {
			return fmt.Errorf("store: duplicate brake catalog scenario %q", rec.ID)
		}
		seen[rec.ID] = struct{}{}
		if rec.CarName == nil || strings.TrimSpace(*rec.CarName) == "" ||
			rec.TrackName == nil || strings.TrimSpace(*rec.TrackName) == "" {
			return fmt.Errorf("store: brake catalog scenario %q needs carName and trackName", rec.ID)
		}
		if len(item.Source) == 0 || !json.Valid(item.Source) {
			return fmt.Errorf("store: brake catalog scenario %q has invalid source JSON", rec.ID)
		}
		var source any
		if err := json.Unmarshal(item.Source, &source); err != nil {
			return fmt.Errorf("store: brake catalog scenario %q source: %w", rec.ID, err)
		}
		if err := rejectPrivateBrakeCatalogData(source, "source"); err != nil {
			return fmt.Errorf("store: brake catalog scenario %q: %w", rec.ID, err)
		}
	}
	return nil
}

func rejectPrivateBrakeCatalogData(value any, path string) error {
	banned := map[string]struct{}{
		"driver": {}, "drivers": {}, "driverid": {}, "driverslug": {},
		"laps": {}, "samples": {},
		"telemetry": {}, "rows": {}, "csv": {}, "raw": {},
		"apilapurl": {}, "apicsvurl": {},
		"authorization": {}, "token": {},
	}
	allowedURLFields := map[string]struct{}{
		"garage61url": {}, "garage61telemetryurl": {},
		"garage61analysisurl": {}, "garage61analyzeurl": {},
	}
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			folded := strings.Map(func(r rune) rune {
				if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
					return r
				}
				if r >= 'A' && r <= 'Z' {
					return r + ('a' - 'A')
				}
				return -1
			}, key)
			if _, found := banned[folded]; found {
				return fmt.Errorf("private lap-level field %s.%s is not allowed", path, key)
			}
			if _, isURL := allowedURLFields[folded]; isURL {
				text, ok := child.(string)
				if !ok || !validGarage61CitationURL(text) {
					return fmt.Errorf("invalid Garage61 citation URL at %s.%s", path, key)
				}
				continue
			}
			if err := rejectPrivateBrakeCatalogData(child, path+"."+key); err != nil {
				return err
			}
		}
	case []any:
		for i, child := range typed {
			if err := rejectPrivateBrakeCatalogData(child, fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
	case string:
		folded := strings.ToLower(typed)
		if strings.Contains(folded, "authorization: bearer") || strings.Contains(folded, "/api/v1/laps/") ||
			strings.Contains(folded, "https://") || strings.Contains(folded, "http://") {
			return fmt.Errorf("private lap-level value at %s is not allowed", path)
		}
	}
	return nil
}

func validGarage61CitationURL(value string) bool {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.Host != "garage61.net" || parsed.User != nil {
		return false
	}
	return strings.HasPrefix(parsed.Path, "/app/analyze") ||
		strings.HasPrefix(parsed.Path, "/app/analysis/laps/")
}

// ImportBrakeCatalog atomically upserts generated scenarios into the same
// SQLite database used by the LapDog server. Catalog rows are read-only in the
// HTTP API because they use the existing built-in origin.
func (s *Store) ImportBrakeCatalog(catalog BrakeCatalog) (int, error) {
	if err := validateBrakeCatalog(catalog); err != nil {
		return 0, err
	}
	tx, err := s.writer.Begin()
	if err != nil {
		return 0, fmt.Errorf("store: begin brake catalog import: %w", err)
	}
	defer tx.Rollback()

	for _, item := range catalog.Scenarios {
		rec := item.BrakeScenario
		provider := catalog.SourceProvider
		rec.Origin = "builtin"
		rec.CatalogVersion = &catalog.CatalogVersion
		rec.SourceProvider = &provider
		rec.Retired = false
		rec.CreatedAt = catalog.GeneratedAt
		rec.UpdatedAt = catalog.GeneratedAt
		result, err := tx.Exec(`INSERT INTO brake_scenarios (
			id, name, description, approach_ms, accelerator_fall_target_ms,
			brake_rise_target_ms, target_brake_percent, brake_tolerance_percent,
			brake_hold_ms, trail_brake_release_ms, transition_enabled,
			transition_mode, transition_duration_ms, transition_brake_percent,
			accelerator_ramp_ms, origin, catalog_version, retired, source_json,
			created_at, updated_at, car_name, track_name, source_provider
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name=excluded.name, description=excluded.description,
			approach_ms=excluded.approach_ms,
			accelerator_fall_target_ms=excluded.accelerator_fall_target_ms,
			brake_rise_target_ms=excluded.brake_rise_target_ms,
			target_brake_percent=excluded.target_brake_percent,
			brake_tolerance_percent=excluded.brake_tolerance_percent,
			brake_hold_ms=excluded.brake_hold_ms,
			trail_brake_release_ms=excluded.trail_brake_release_ms,
			transition_enabled=excluded.transition_enabled,
			transition_mode=excluded.transition_mode,
			transition_duration_ms=excluded.transition_duration_ms,
			transition_brake_percent=excluded.transition_brake_percent,
			accelerator_ramp_ms=excluded.accelerator_ramp_ms,
			catalog_version=excluded.catalog_version, retired=0,
			source_json=excluded.source_json, updated_at=excluded.updated_at,
			car_name=excluded.car_name, track_name=excluded.track_name,
			source_provider=excluded.source_provider
		WHERE brake_scenarios.origin='builtin'
			AND brake_scenarios.source_provider='garage61'`,
			rec.ID, rec.Name, rec.Description, rec.ApproachMS,
			rec.AcceleratorFallTargetMS, rec.BrakeRiseTargetMS,
			rec.TargetBrakePercent, rec.BrakeTolerancePercent, rec.BrakeHoldMS,
			rec.TrailBrakeReleaseMS, rec.TransitionEnabled, rec.TransitionMode,
			rec.TransitionDurationMS, rec.TransitionBrakePercent,
			rec.AcceleratorRampMS, rec.Origin, rec.CatalogVersion, rec.Retired,
			string(item.Source), rec.CreatedAt, rec.UpdatedAt, rec.CarName,
			rec.TrackName, rec.SourceProvider)
		if err != nil {
			return 0, fmt.Errorf("store: import brake catalog scenario %q: %w", rec.ID, err)
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return 0, fmt.Errorf("store: count brake catalog scenario %q: %w", rec.ID, err)
		}
		if changed != 1 {
			return 0, fmt.Errorf("store: brake catalog id %q conflicts with a non-catalog scenario", rec.ID)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("store: commit brake catalog import: %w", err)
	}
	return len(catalog.Scenarios), nil
}
