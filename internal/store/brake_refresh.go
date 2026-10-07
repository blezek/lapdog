package store

import (
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const Garage61RefreshInterval = 7 * 24 * time.Hour

// Garage61Combination is a saved, active Garage61 car/layout pairing. Older
// packaged and locally generated scenario IDs retain Garage61 entity IDs,
// which can be recovered without storing telemetry or driver information.
type Garage61Combination struct {
	CarName      string `json:"carName"`
	TrackName    string `json:"trackName"`
	PreparedAt   string `json:"preparedAt"`
	CarID        int    `json:"carId,omitempty"`
	TrackID      int    `json:"trackId,omitempty"`
	ReviewNeeded bool   `json:"reviewNeeded"`
}

var localGarage61ID = regexp.MustCompile(`^garage61-iracing-track-([0-9]+)-car-([0-9]+)-zone-[0-9]+$`)

func (s *Store) Garage61Combinations() ([]Garage61Combination, error) {
	rows, err := s.reader.Query(`SELECT MAX(car_name), MAX(track_name), MAX(updated_at), MAX(id), MAX(review_needed)
 FROM brake_scenarios WHERE source_provider='garage61' AND retired=0
 AND car_name IS NOT NULL AND track_name IS NOT NULL
 GROUP BY CASE WHEN instr(id,'-zone-')>0 THEN substr(id,1,instr(id,'-zone-')+5)
 ELSE car_name || char(31) || track_name END
 ORDER BY car_name, track_name`)
	if err != nil {
		return nil, fmt.Errorf("store: list Garage61 combinations: %w", err)
	}
	defer rows.Close()
	out := []Garage61Combination{}
	for rows.Next() {
		var c Garage61Combination
		var scenarioID string
		if err := rows.Scan(&c.CarName, &c.TrackName, &c.PreparedAt, &scenarioID, &c.ReviewNeeded); err != nil {
			return nil, fmt.Errorf("store: scan Garage61 combination: %w", err)
		}
		if match := localGarage61ID.FindStringSubmatch(scenarioID); match != nil {
			c.TrackID, _ = strconv.Atoi(match[1])
			c.CarID, _ = strconv.Atoi(match[2])
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// FlagGarageCombination keeps unsupported targets available while making the
// unresolved source-lap state visible after restarts and reminder delays.
func (s *Store) FlagGarageCombination(carID, trackID int) (int64, error) {
	if carID <= 0 || trackID <= 0 {
		return 0, errors.New("store: invalid Garage61 combination IDs")
	}
	prefix := fmt.Sprintf("garage61-iracing-track-%d-car-%d-zone-", trackID, carID)
	result, err := s.writer.Exec(`UPDATE brake_scenarios SET review_needed=1 WHERE source_provider='garage61' AND retired=0 AND substr(id,1,?)=?`, len(prefix), prefix)
	if err != nil {
		return 0, fmt.Errorf("store: flag Garage61 combination: %w", err)
	}
	return result.RowsAffected()
}

// MyDrivenCombinations lists every race or practice pairing recorded locally.
// Platform IDs are iRacing IDs, not Garage61 API IDs.
type MyDrivenCombination struct {
	CarPlatformID   int     `json:"carPlatformId"`
	TrackPlatformID int     `json:"trackPlatformId"`
	CarName         string  `json:"carName"`
	TrackName       string  `json:"trackName"`
	TrackConfig     string  `json:"trackConfig"`
	DrivingHours    float64 `json:"drivingHours"`
}

func (s *Store) MyDrivenCombinations() ([]MyDrivenCombination, error) {
	rows, err := s.reader.Query(`SELECT car_id, track_id, MAX(COALESCE(car_name,'')),
 MAX(COALESCE(track_name,'')), MAX(COALESCE(track_config,'')), SUM(driving_seconds)/3600.0
 FROM sessions WHERE session_type IN ('Race','Practice') AND car_id IS NOT NULL AND track_id IS NOT NULL
 GROUP BY car_id,track_id ORDER BY SUM(driving_seconds) DESC,car_id,track_id`)
	if err != nil {
		return nil, fmt.Errorf("store: list driven combinations: %w", err)
	}
	defer rows.Close()
	out := []MyDrivenCombination{}
	for rows.Next() {
		var c MyDrivenCombination
		if err := rows.Scan(&c.CarPlatformID, &c.TrackPlatformID, &c.CarName, &c.TrackName, &c.TrackConfig, &c.DrivingHours); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) Garage61SnoozeUntil() (*time.Time, error) {
	var raw sql.NullString
	if err := s.reader.QueryRow(`SELECT snooze_until FROM brake_garage61_refresh WHERE id=1`).Scan(&raw); err != nil {
		return nil, fmt.Errorf("store: read Garage61 reminder: %w", err)
	}
	if !raw.Valid {
		return nil, nil
	}
	value, err := time.Parse(time.RFC3339, raw.String)
	if err != nil {
		return nil, fmt.Errorf("store: invalid Garage61 reminder date: %w", err)
	}
	return &value, nil
}
func (s *Store) SetGarage61SnoozeUntil(until *time.Time) error {
	var value any
	if until != nil {
		value = until.UTC().Format(time.RFC3339)
	}
	result, err := s.writer.Exec(`UPDATE brake_garage61_refresh SET snooze_until=? WHERE id=1`, value)
	if err != nil {
		return fmt.Errorf("store: set Garage61 reminder: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return errors.New("store: Garage61 reminder row is missing")
	}
	return nil
}

func Garage61CombinationDue(preparedAt string, now time.Time) bool {
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(preparedAt))
	return err != nil || !t.Add(Garage61RefreshInterval).After(now)
}
