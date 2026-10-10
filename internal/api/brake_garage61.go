package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/blezek/lapdog/internal/garage61"
	"github.com/blezek/lapdog/internal/store"
)

type garageProvider interface {
	Configured(context.Context) bool
	Catalog(context.Context) (garage61.Catalog, error)
	Generate(context.Context, garage61.Entity, garage61.Entity, func(string)) (store.BrakeCatalog, error)
}
type garageState struct {
	mu           sync.Mutex
	catalog      garage61.Catalog
	loaded       bool
	catalogEpoch uint64
	job          garageJob
	cancel       context.CancelFunc
	nextRunID    uint64
}
type garageJob struct {
	RunID        uint64   `json:"runId"`
	QueueItemID  int64    `json:"queueItemId,omitempty"`
	State        string   `json:"state"`
	Message      string   `json:"message"`
	CarID        int      `json:"carId"`
	TrackID      int      `json:"trackId"`
	Count        int      `json:"count"`
	ScenarioID   string   `json:"scenarioId,omitempty"`
	All          bool     `json:"all"`
	Index        int      `json:"index"`
	Total        int      `json:"total"`
	Succeeded    int      `json:"succeeded"`
	ReviewNeeded int      `json:"reviewNeeded"`
	Errors       []string `json:"errors"`
}
type garagePair struct{ car, track garage61.Entity }

func (s *Server) handleGarageCatalog(w http.ResponseWriter, r *http.Request) {
	if !s.garage.Configured(r.Context()) {
		s.writeJSON(w, map[string]any{"configured": false, "cars": []garage61.Entity{}, "tracks": []garage61.Entity{}})
		return
	}
	catalog, err := s.garageCatalog(r.Context())
	if err != nil {
		s.fail(w, http.StatusBadGateway, err)
		return
	}
	s.writeJSON(w, map[string]any{"configured": true, "cars": catalog.Cars, "tracks": catalog.Tracks})
}
func (s *Server) handleGarageJob(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.garageState.mu.Lock()
		job := s.garageState.job
		s.garageState.mu.Unlock()
		if job.State == "" {
			job.State = "idle"
		}
		if job.Errors == nil {
			job.Errors = []string{}
		}
		s.writeJSON(w, job)
	case http.MethodDelete:
		s.garageState.mu.Lock()
		if s.garageState.cancel != nil {
			s.garageState.cancel()
		}
		s.garageState.mu.Unlock()
		s.writeJSON(w, map[string]bool{"ok": true})
	case http.MethodPost:
		var input struct {
			CarID   int  `json:"carId"`
			TrackID int  `json:"trackId"`
			All     bool `json:"all"`
		}
		if !s.decodeJSONRequest(w, r, &input) {
			return
		}
		if !s.garage.Configured(r.Context()) {
			s.fail(w, http.StatusConflict, errors.New("add a Garage61 token on the Garage61 tab or set GARAGE61_TOKEN before processing scenarios"))
			return
		}
		catalog, err := s.garageCatalog(r.Context())
		if err != nil {
			s.fail(w, http.StatusBadGateway, err)
			return
		}
		pairs, unresolved, err := s.garagePairs(input.All, input.CarID, input.TrackID, catalog)
		if err != nil {
			s.fail(w, http.StatusBadRequest, err)
			return
		}
		s.garageState.mu.Lock()
		if s.garageState.job.State == "running" {
			s.garageState.mu.Unlock()
			s.fail(w, http.StatusConflict, errors.New("a Garage61 job is already processing"))
			return
		}
		timeout := 30 * time.Minute
		if input.All {
			timeout = 2 * time.Hour
		}
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		s.garageState.nextRunID++
		job := garageJob{RunID: s.garageState.nextRunID, State: "running", Message: "Finding viewable laps", All: input.All, Total: len(pairs), Errors: unresolved}
		if !input.All {
			job.CarID = input.CarID
			job.TrackID = input.TrackID
		}
		s.garageState.job = job
		s.garageState.cancel = cancel
		s.garageState.mu.Unlock()
		go s.runGarageJob(ctx, cancel, pairs)
		s.writeJSONStatus(w, http.StatusAccepted, job)
	default:
		w.Header().Set("Allow", "GET, POST, DELETE")
		s.fail(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
	}
}
func (s *Server) garagePairs(all bool, carID, trackID int, catalog garage61.Catalog) ([]garagePair, []string, error) {
	if !all {
		var car, track garage61.Entity
		for _, e := range catalog.Cars {
			if e.ID == carID {
				car = e
				break
			}
		}
		for _, e := range catalog.Tracks {
			if e.ID == trackID {
				track = e
				break
			}
		}
		if car.ID == 0 || track.ID == 0 {
			return nil, nil, errors.New("select a car and track from the Garage61 catalog")
		}
		return []garagePair{{car, track}}, []string{}, nil
	}
	saved, err := s.st.Garage61Combinations()
	if err != nil {
		return nil, nil, err
	}
	if len(saved) == 0 {
		return nil, nil, errors.New("no Garage61 combinations have been prepared yet")
	}
	pairs, unresolved := []garagePair{}, []string{}
	seen := map[[2]int]bool{}
	for _, item := range saved {
		car, track, err := resolveSavedGarageCombination(item, catalog)
		if err != nil {
			unresolved = append(unresolved, err.Error())
			continue
		}
		key := [2]int{car.ID, track.ID}
		if seen[key] {
			continue
		}
		seen[key] = true
		pairs = append(pairs, garagePair{car, track})
	}
	if len(pairs) == 0 {
		return nil, nil, fmt.Errorf("no saved combinations matched the current Garage61 catalog; choose a car and layout individually")
	}
	return pairs, unresolved, nil
}
func (s *Server) runGarageJob(ctx context.Context, cancel context.CancelFunc, pairs []garagePair) {
	defer cancel()
	for i, pair := range pairs {
		if ctx.Err() != nil {
			break
		}
		s.garageState.mu.Lock()
		s.garageState.job.Index = i + 1
		s.garageState.job.CarID = pair.car.ID
		s.garageState.job.TrackID = pair.track.ID
		s.garageState.job.Message = fmt.Sprintf("Combination %d of %d: %s / %s", i+1, len(pairs), pair.car.Name, pair.track.Label())
		s.garageState.mu.Unlock()
		catalog, err := s.garage.Generate(ctx, pair.car, pair.track, func(message string) {
			s.garageState.mu.Lock()
			s.garageState.job.Message = fmt.Sprintf("%d/%d %s / %s: %s", i+1, len(pairs), pair.car.Name, pair.track.Label(), message)
			s.garageState.mu.Unlock()
		})
		if err == nil {
			err = ctx.Err()
		}
		count := 0
		flagged := false
		if err == nil && len(catalog.Scenarios) == 0 {
			var affected int64
			affected, err = s.st.FlagGarageCombination(pair.car.ID, pair.track.ID)
			flagged = err == nil && affected > 0
			if err == nil && affected == 0 {
				err = errors.New("no supported zones and no saved scenarios to review")
			}
		} else if err == nil {
			count, err = s.st.ImportLocalBrakeCatalog(catalog, pair.car.ID, pair.track.ID)
		}
		s.garageState.mu.Lock()
		if err != nil {
			if ctx.Err() == nil {
				s.garageState.job.Errors = append(s.garageState.job.Errors, fmt.Sprintf("%s / %s: %s", pair.car.Name, pair.track.Label(), err))
			}
		} else if flagged {
			s.garageState.job.ReviewNeeded++
			s.garageState.job.Errors = append(s.garageState.job.Errors, fmt.Sprintf("%s / %s: no currently supported braking zones; saved scenarios kept for review", pair.car.Name, pair.track.Label()))
		} else {
			s.garageState.job.Count += count
			s.garageState.job.Succeeded++
			if s.garageState.job.ScenarioID == "" && len(catalog.Scenarios) > 0 {
				s.garageState.job.ScenarioID = catalog.Scenarios[0].ID
			}
		}
		s.garageState.mu.Unlock()
	}
	s.garageState.mu.Lock()
	defer s.garageState.mu.Unlock()
	s.garageState.cancel = nil
	job := &s.garageState.job
	switch {
	case ctx.Err() != nil:
		job.State = "cancelled"
		job.Message = fmt.Sprintf("Stopped after %d of %d combinations; completed updates were saved", job.Succeeded, job.Total)
	case job.ReviewNeeded > 0 && job.Succeeded == 0:
		job.State = "review"
		job.Message = fmt.Sprintf("%d combination(s) need review; saved scenarios were kept", job.ReviewNeeded)
	case len(job.Errors) > 0 && job.Succeeded == 0:
		job.State = "failed"
		job.Message = "No combinations were updated"
	case len(job.Errors) > 0:
		job.State = "partial"
		job.Message = fmt.Sprintf("Updated %d combinations; %d need attention", job.Succeeded, len(job.Errors))
	default:
		job.State = "complete"
		job.Message = fmt.Sprintf("Updated %d combinations", job.Succeeded)
	}
}
