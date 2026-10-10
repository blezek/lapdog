package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/blezek/lapdog/internal/garage61"
	"github.com/blezek/lapdog/internal/store"
)

type garageRefreshStatus struct {
	Combinations []store.Garage61Combination `json:"combinations"`
	DueCount     int                         `json:"dueCount"`
	ReminderDue  bool                        `json:"reminderDue"`
	SnoozeUntil  *time.Time                  `json:"snoozeUntil"`
}

func (s *Server) garageRefreshStatus(now time.Time) (garageRefreshStatus, error) {
	combinations, err := s.st.Garage61Combinations()
	if err != nil {
		return garageRefreshStatus{}, err
	}
	snooze, err := s.st.Garage61SnoozeUntil()
	if err != nil {
		return garageRefreshStatus{}, err
	}
	result := garageRefreshStatus{Combinations: combinations, SnoozeUntil: snooze}
	for _, c := range combinations {
		if store.Garage61CombinationDue(c.PreparedAt, now) {
			result.DueCount++
		}
	}
	result.ReminderDue = result.DueCount > 0 && (snooze == nil || !snooze.After(now))
	return result, nil
}
func (s *Server) handleGarageRefresh(w http.ResponseWriter, r *http.Request) {
	status, err := s.garageRefreshStatus(time.Now().UTC())
	if err != nil {
		s.fail(w, http.StatusInternalServerError, err)
		return
	}
	s.writeJSON(w, status)
}
func (s *Server) handleGarageSnooze(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Until string `json:"until"`
	}
	if !s.decodeJSONRequest(w, r, &input) {
		return
	}
	now := time.Now().UTC()
	var until *time.Time
	if strings.TrimSpace(input.Until) != "" {
		parsed, err := time.Parse(time.RFC3339, input.Until)
		if err != nil || !parsed.After(now) || parsed.After(now.Add(90*24*time.Hour)) {
			s.fail(w, http.StatusBadRequest, errors.New("choose a reminder date within the next 90 days"))
			return
		}
		until = &parsed
	}
	if err := s.st.SetGarage61SnoozeUntil(until); err != nil {
		s.fail(w, http.StatusInternalServerError, err)
		return
	}
	s.handleGarageRefresh(w, r)
}
func (s *Server) handleMyGarageCombinations(w http.ResponseWriter, r *http.Request) {
	combinations, err := s.st.MyDrivenCombinations()
	if err != nil {
		s.fail(w, http.StatusInternalServerError, err)
		return
	}
	s.writeJSON(w, combinations)
}

func (s *Server) garageCatalog(ctx context.Context) (garage61.Catalog, error) {
	s.garageState.mu.Lock()
	catalog, loaded, epoch := s.garageState.catalog, s.garageState.loaded, s.garageState.catalogEpoch
	s.garageState.mu.Unlock()
	if loaded {
		return catalog, nil
	}
	catalog, err := s.garage.Catalog(ctx)
	s.garageState.mu.Lock()
	if s.garageState.catalogEpoch != epoch {
		s.garageState.mu.Unlock()
		return s.garageCatalog(ctx)
	}
	if err != nil {
		s.garageState.mu.Unlock()
		return garage61.Catalog{}, err
	}
	s.garageState.catalog = catalog
	s.garageState.loaded = true
	s.garageState.mu.Unlock()
	return catalog, nil
}

// resolveSavedGarageCombination never guesses a car or layout. Scenario IDs
// normally carry Garage61 IDs; legacy rows fall back to exact catalog names.
func resolveSavedGarageCombination(saved store.Garage61Combination, catalog garage61.Catalog) (garage61.Entity, garage61.Entity, error) {
	cars, tracks := []garage61.Entity{}, []garage61.Entity{}
	for _, car := range catalog.Cars {
		if saved.CarID > 0 && car.ID == saved.CarID || saved.CarID == 0 && strings.EqualFold(car.Name, saved.CarName) {
			cars = append(cars, car)
		}
	}
	for _, track := range catalog.Tracks {
		if saved.TrackID > 0 && track.ID == saved.TrackID || saved.TrackID == 0 && strings.EqualFold(track.Label(), saved.TrackName) {
			tracks = append(tracks, track)
		}
	}
	if len(cars) != 1 || len(tracks) != 1 {
		return garage61.Entity{}, garage61.Entity{}, fmt.Errorf("%s / %s could not be matched uniquely to Garage61", saved.CarName, saved.TrackName)
	}
	return cars[0], tracks[0], nil
}
