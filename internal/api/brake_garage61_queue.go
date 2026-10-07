package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/blezek/lapdog/internal/garage61"
	"github.com/blezek/lapdog/internal/store"
)

func (s *Server) wakeGarageQueue() {
	select {
	case s.garageWake <- struct{}{}:
	default:
	}
}

func (s *Server) runGarageQueue(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		worked := false
		if s.garage.Configured(ctx) {
			var err error
			worked, err = s.processNextGarageQueue(ctx)
			if err != nil {
				s.log.Error("Garage61 queue processing failed", "err", err)
			}
		}
		if worked {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-s.garageWake:
		}
	}
}

// processNextGarageQueue claims under the job lock so an explicit legacy job
// cannot start between the idle check and the queue worker's claim.
func (s *Server) processNextGarageQueue(parent context.Context) (bool, error) {
	s.garageState.mu.Lock()
	if s.garageState.job.State == "running" {
		s.garageState.mu.Unlock()
		return false, nil
	}
	item, err := s.st.ClaimNextGarageQueue()
	if err != nil || item == nil {
		s.garageState.mu.Unlock()
		return false, err
	}
	ctx, cancel := context.WithTimeout(parent, 30*time.Minute)
	s.garageState.nextRunID++
	s.garageState.job = garageJob{RunID: s.garageState.nextRunID, QueueItemID: item.ID,
		State: "running", Message: "Matching iRacing car and layout in Garage61", Total: 1, Errors: []string{}}
	s.garageState.cancel = cancel
	s.garageState.mu.Unlock()

	defer cancel()
	catalog, err := s.garageCatalog(ctx)
	if err != nil {
		state := "failed"
		if ctx.Err() != nil {
			state = "cancelled"
		}
		return true, s.finishGarageQueueEarly(parent, item.ID, state, err.Error())
	}
	car, track, err := resolveQueuedGaragePair(*item, catalog)
	if err != nil {
		return true, s.finishGarageQueueEarly(parent, item.ID, "unmatched", err.Error())
	}
	s.garageState.mu.Lock()
	s.garageState.job.CarID, s.garageState.job.TrackID = car.ID, track.ID
	deleted, err := s.st.GarageCombinationDeleted(car.ID, track.ID)
	if err != nil {
		s.garageState.mu.Unlock()
		return true, s.finishGarageQueueEarly(parent, item.ID, "failed", err.Error())
	}
	if deleted {
		s.garageState.mu.Unlock()
		return true, s.finishGarageQueueEarly(parent, item.ID, "deleted", "Scenarios were deleted locally")
	}
	if err := s.st.ResolveGarageQueue(item.ID, car.ID, track.ID, car.Name, track.Label()); err != nil {
		s.garageState.mu.Unlock()
		return true, s.finishGarageQueueEarly(parent, item.ID, "failed", err.Error())
	}
	s.garageState.mu.Unlock()
	s.runGarageJob(ctx, cancel, []garagePair{{car: car, track: track}})
	s.garageState.mu.Lock()
	defer s.garageState.mu.Unlock()
	job := s.garageState.job
	if parent.Err() != nil {
		// A stopped process leaves the row running; the next launch recovers it.
		return true, nil
	}
	state := job.State
	if state == "complete" {
		state = "done"
	}
	detail := ""
	if state != "done" {
		detail = job.Message
		if len(job.Errors) > 0 {
			detail = strings.Join(job.Errors, "; ")
		}
	}
	return true, s.st.FinishGarageQueue(item.ID, state, detail)
}

func (s *Server) finishGarageQueueEarly(parent context.Context, id int64, state, detail string) error {
	s.garageState.mu.Lock()
	defer s.garageState.mu.Unlock()
	s.garageState.job.State = state
	s.garageState.job.Message = detail
	s.garageState.job.Errors = []string{detail}
	s.garageState.cancel = nil
	if parent.Err() != nil {
		return nil
	}
	return s.st.FinishGarageQueue(id, state, detail)
}

func resolveQueuedGaragePair(item store.GarageQueueItem, catalog garage61.Catalog) (garage61.Entity, garage61.Entity, error) {
	cars, tracks := []garage61.Entity{}, []garage61.Entity{}
	for _, car := range catalog.Cars {
		if item.GarageCarID > 0 && car.ID == item.GarageCarID ||
			item.GarageCarID == 0 && item.CarPlatformID > 0 && car.PlatformID == strconv.Itoa(item.CarPlatformID) ||
			item.GarageCarID == 0 && item.CarPlatformID == 0 && strings.EqualFold(car.Name, item.CarName) {
			cars = append(cars, car)
		}
	}
	for _, track := range catalog.Tracks {
		if item.GarageTrackID > 0 && track.ID == item.GarageTrackID ||
			item.GarageTrackID == 0 && item.TrackPlatformID > 0 && track.PlatformID == strconv.Itoa(item.TrackPlatformID) ||
			item.GarageTrackID == 0 && item.TrackPlatformID == 0 && strings.EqualFold(track.Label(), item.TrackName) {
			tracks = append(tracks, track)
		}
	}
	if len(cars) != 1 || len(tracks) != 1 {
		return garage61.Entity{}, garage61.Entity{}, fmt.Errorf("%s / %s has no unique Garage61 match", item.CarName, item.TrackName)
	}
	return cars[0], tracks[0], nil
}

func (s *Server) handleGarageQueue(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		items, err := s.st.ListGarageQueue()
		if err != nil {
			s.fail(w, http.StatusInternalServerError, err)
			return
		}
		s.writeJSON(w, items)
	case http.MethodPost:
		var input struct {
			CarID   int  `json:"carId"`
			TrackID int  `json:"trackId"`
			All     bool `json:"all"`
		}
		if !s.decodeBrakeRequest(w, r, &input) {
			return
		}
		catalog, err := s.garageCatalog(r.Context())
		if err != nil {
			s.fail(w, http.StatusBadGateway, err)
			return
		}
		if input.All {
			saved, err := s.st.Garage61Combinations()
			if err != nil {
				s.fail(w, http.StatusInternalServerError, err)
				return
			}
			if len(saved) == 0 {
				s.fail(w, http.StatusBadRequest, errors.New("no Garage61 combinations have been prepared yet"))
				return
			}
			for _, item := range saved {
				carPlatform, trackPlatform, garageCar, garageTrack := 0, 0, item.CarID, item.TrackID
				carName, trackName := item.CarName, item.TrackName
				if car, track, err := resolveSavedGarageCombination(item, catalog); err == nil {
					carPlatform, _ = strconv.Atoi(car.PlatformID)
					trackPlatform, _ = strconv.Atoi(track.PlatformID)
					garageCar, garageTrack = car.ID, track.ID
					carName, trackName = car.Name, track.Label()
				}
				if _, err := s.st.EnqueueGaragePair(carPlatform, trackPlatform, garageCar, garageTrack,
					carName, trackName, "refresh"); err != nil {
					s.fail(w, http.StatusInternalServerError, err)
					return
				}
			}
		} else {
			pairs, _, err := s.garagePairs(false, input.CarID, input.TrackID, catalog)
			if err != nil {
				s.fail(w, http.StatusBadRequest, err)
				return
			}
			pair := pairs[0]
			carPlatform, _ := strconv.Atoi(pair.car.PlatformID)
			trackPlatform, _ := strconv.Atoi(pair.track.PlatformID)
			if _, err := s.st.EnqueueGaragePair(carPlatform, trackPlatform, pair.car.ID, pair.track.ID,
				pair.car.Name, pair.track.Label(), "manual"); err != nil {
				s.fail(w, http.StatusInternalServerError, err)
				return
			}
		}
		s.wakeGarageQueue()
		items, err := s.st.ListGarageQueue()
		if err != nil {
			s.fail(w, http.StatusInternalServerError, err)
			return
		}
		s.writeJSONStatus(w, http.StatusAccepted, items)
	default:
		w.Header().Set("Allow", "GET, POST")
		s.fail(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
	}
}

func (s *Server) handleGarageQueueRetry(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		s.fail(w, http.StatusBadRequest, errors.New("invalid queue item ID"))
		return
	}
	if err := s.st.RetryGarageQueue(id); err != nil {
		s.fail(w, http.StatusNotFound, err)
		return
	}
	s.wakeGarageQueue()
	items, err := s.st.ListGarageQueue()
	if err != nil {
		s.fail(w, http.StatusInternalServerError, err)
		return
	}
	s.writeJSON(w, items)
}

func (s *Server) handleGarageCombinationDelete(w http.ResponseWriter, r *http.Request) {
	carID, carErr := strconv.Atoi(r.PathValue("carId"))
	trackID, trackErr := strconv.Atoi(r.PathValue("trackId"))
	if carErr != nil || trackErr != nil || carID <= 0 || trackID <= 0 {
		s.fail(w, http.StatusBadRequest, errors.New("invalid Garage61 combination IDs"))
		return
	}
	s.deleteGarageCombinations(w, []store.GarageCombinationID{{CarID: carID, TrackID: trackID}})
}

func (s *Server) handleGarageCombinationsDelete(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Combinations []store.GarageCombinationID `json:"combinations"`
	}
	if !s.decodeBrakeRequest(w, r, &input) {
		return
	}
	s.deleteGarageCombinations(w, input.Combinations)
}

func (s *Server) deleteGarageCombinations(w http.ResponseWriter, combinations []store.GarageCombinationID) {
	if len(combinations) == 0 {
		s.fail(w, http.StatusBadRequest, errors.New("select at least one Garage61 combination"))
		return
	}
	seen := make(map[store.GarageCombinationID]bool, len(combinations))
	for _, pair := range combinations {
		if pair.CarID <= 0 || pair.TrackID <= 0 || seen[pair] {
			s.fail(w, http.StatusBadRequest, errors.New("invalid or duplicate Garage61 combination"))
			return
		}
		seen[pair] = true
	}
	s.garageState.mu.Lock()
	job := s.garageState.job
	if job.State == "running" {
		for _, pair := range combinations {
			if job.All || job.CarID == pair.CarID && job.TrackID == pair.TrackID {
				s.garageState.mu.Unlock()
				s.fail(w, http.StatusConflict, errors.New("wait for a selected combination to finish processing before deleting scenarios"))
				return
			}
		}
	}
	count, err := s.st.DeleteGarageCombinations(combinations)
	s.garageState.mu.Unlock()
	if err != nil {
		code := http.StatusInternalServerError
		if errors.Is(err, store.ErrNotFound) {
			code = http.StatusNotFound
		}
		s.fail(w, code, err)
		return
	}
	s.writeJSON(w, map[string]int64{"deleted": count})
}
