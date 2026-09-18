package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"

	"github.com/blezek/lapdog/internal/store"
	"github.com/google/uuid"
)

const (
	brakeRequestLimit = 2 << 20
	maxBrakeSamples   = 5000
	maxBrakeRunMS     = 120000
)

func (s *Server) handleBrakeScenarios(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		rows, err := s.st.ListBrakeScenarios()
		if err != nil {
			s.fail(w, http.StatusInternalServerError, err)
			return
		}
		s.writeJSON(w, rows)
	case http.MethodPost:
		var rec store.BrakeScenario
		if !s.decodeBrakeRequest(w, r, &rec) {
			return
		}
		// The server owns custom identities. Accepting a caller-selected id would
		// let a custom record impersonate the stable built-in catalog namespace.
		rec.ID = uuid.NewString()
		if err := validateBrakeScenario(rec); err != nil {
			s.fail(w, http.StatusBadRequest, err)
			return
		}
		if err := s.st.CreateBrakeScenario(&rec); err != nil {
			s.fail(w, http.StatusInternalServerError, err)
			return
		}
		s.writeJSONStatus(w, http.StatusCreated, rec)
	default:
		w.Header().Set("Allow", "GET, POST")
		s.fail(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
	}
}

func (s *Server) handleBrakeScenario(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		s.fail(w, http.StatusBadRequest, errors.New("scenario id is required"))
		return
	}
	switch r.Method {
	case http.MethodGet:
		rec, err := s.st.BrakeScenarioByID(id)
		if err != nil {
			s.brakeStoreError(w, err)
			return
		}
		s.writeJSON(w, rec)
	case http.MethodPut:
		var rec store.BrakeScenario
		if !s.decodeBrakeRequest(w, r, &rec) {
			return
		}
		if rec.ID != "" && rec.ID != id {
			s.fail(w, http.StatusBadRequest, errors.New("scenario id does not match request path"))
			return
		}
		rec.ID = id
		if err := validateBrakeScenario(rec); err != nil {
			s.fail(w, http.StatusBadRequest, err)
			return
		}
		if err := s.st.UpdateBrakeScenario(&rec); err != nil {
			s.brakeStoreError(w, err)
			return
		}
		s.writeJSON(w, rec)
	case http.MethodDelete:
		if err := s.st.DeleteBrakeScenario(id); err != nil {
			s.brakeStoreError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.Header().Set("Allow", "GET, PUT, DELETE")
		s.fail(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
	}
}

func (s *Server) handleBrakeResults(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		rows, err := s.st.ListBrakeRuns()
		if err != nil {
			s.fail(w, http.StatusInternalServerError, err)
			return
		}
		s.writeJSON(w, rows)
	case http.MethodPost:
		var run store.BrakeRun
		if !s.decodeBrakeRequest(w, r, &run) {
			return
		}
		run.ID = uuid.NewString()
		scenario, err := s.st.BrakeScenarioByID(run.ScenarioID)
		if err != nil {
			s.brakeStoreError(w, err)
			return
		}
		if err := validateBrakeRun(run); err != nil {
			s.fail(w, http.StatusBadRequest, err)
			return
		}
		snapshot, err := json.Marshal(scenario)
		if err != nil {
			s.fail(w, http.StatusInternalServerError, err)
			return
		}
		run.ScenarioName = scenario.Name
		run.ScenarioSnapshot = string(snapshot)
		run.ScoringVersion = 2
		run.CreatedAt = store.Now()
		if err := s.st.InsertBrakeRun(&run); err != nil {
			s.fail(w, http.StatusInternalServerError, err)
			return
		}
		s.writeJSONStatus(w, http.StatusCreated, run)
	default:
		w.Header().Set("Allow", "GET, POST")
		s.fail(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
	}
}

func (s *Server) handleBrakeSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		settings, err := s.st.GetBrakeSettings()
		if err != nil {
			s.fail(w, http.StatusInternalServerError, err)
			return
		}
		s.writeJSON(w, settings)
	case http.MethodPut:
		var settings store.BrakeSettings
		if !s.decodeBrakeRequest(w, r, &settings) {
			return
		}
		if err := validateBrakeSettings(settings); err != nil {
			s.fail(w, http.StatusBadRequest, err)
			return
		}
		if settings.SelectedScenarioID != nil {
			if _, err := s.st.BrakeScenarioByID(*settings.SelectedScenarioID); err != nil {
				s.brakeStoreError(w, err)
				return
			}
		}
		if err := s.st.SetBrakeSettings(settings); err != nil {
			s.fail(w, http.StatusInternalServerError, err)
			return
		}
		s.writeJSON(w, settings)
	default:
		w.Header().Set("Allow", "GET, PUT")
		s.fail(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
	}
}

func decodeBrakeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, brakeRequestLimit)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("invalid JSON body: %w", err)
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("invalid JSON body: multiple values")
		}
		return fmt.Errorf("invalid JSON body: %w", err)
	}
	return nil
}

func (s *Server) decodeBrakeRequest(w http.ResponseWriter, r *http.Request, dst any) bool {
	err := decodeBrakeJSON(w, r, dst)
	if err == nil {
		return true
	}
	status := http.StatusBadRequest
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		status = http.StatusRequestEntityTooLarge
	}
	s.fail(w, status, err)
	return false
}

func validateBrakeScenario(rec store.BrakeScenario) error {
	if strings.TrimSpace(rec.ID) == "" || len(rec.ID) > 120 {
		return errors.New("scenario id must contain 1 to 120 characters")
	}
	if strings.TrimSpace(rec.Name) == "" || len(rec.Name) > 160 {
		return errors.New("scenario name must contain 1 to 160 characters")
	}
	if len(rec.Description) > 2000 {
		return errors.New("scenario description is longer than 2000 characters")
	}
	if rec.ApproachMS < 0 || rec.ApproachMS > 30000 ||
		rec.AcceleratorFallTargetMS < 1 || rec.AcceleratorFallTargetMS > 10000 ||
		rec.BrakeRiseTargetMS < 1 || rec.BrakeRiseTargetMS > 10000 ||
		rec.BrakeHoldMS < 0 || rec.BrakeHoldMS > 30000 ||
		rec.TrailBrakeReleaseMS < 1 || rec.TrailBrakeReleaseMS > 30000 ||
		rec.AcceleratorRampMS < 1 || rec.AcceleratorRampMS > 30000 {
		return errors.New("scenario duration is outside its allowed range")
	}
	if !percent(rec.TargetBrakePercent) || !percent(rec.BrakeTolerancePercent) {
		return errors.New("brake target and tolerance must be between 0 and 100")
	}
	if rec.TransitionMode != nil && *rec.TransitionMode != "coast" && *rec.TransitionMode != "low-brake" {
		return errors.New("transition mode must be coast or low-brake")
	}
	if rec.TransitionDurationMS != nil && (*rec.TransitionDurationMS < 1 || *rec.TransitionDurationMS > 10000) {
		return errors.New("transition duration must be between 1 and 10000 ms")
	}
	if rec.TransitionBrakePercent != nil && !percent(*rec.TransitionBrakePercent) {
		return errors.New("transition brake target must be between 0 and 100")
	}
	if rec.TransitionEnabled {
		if rec.TransitionMode == nil || (*rec.TransitionMode != "coast" && *rec.TransitionMode != "low-brake") {
			return errors.New("enabled transition requires mode coast or low-brake")
		}
		if rec.TransitionDurationMS == nil || *rec.TransitionDurationMS < 1 || *rec.TransitionDurationMS > 10000 {
			return errors.New("enabled transition requires a duration between 1 and 10000 ms")
		}
		if *rec.TransitionMode == "low-brake" && (rec.TransitionBrakePercent == nil || !percent(*rec.TransitionBrakePercent)) {
			return errors.New("low-brake transition requires a brake target between 0 and 100")
		}
	}
	return nil
}

func validateBrakeRun(run store.BrakeRun) error {
	if run.ScenarioID == "" {
		return errors.New("scenarioId is required")
	}
	if strings.TrimSpace(run.DeviceLabel) == "" || len(run.DeviceLabel) > 200 {
		return errors.New("deviceLabel must contain 1 to 200 characters")
	}
	if len(run.Samples) < 3 || len(run.Samples) > maxBrakeSamples {
		return fmt.Errorf("samples must contain between 3 and %d readings", maxBrakeSamples)
	}
	if !finiteRange(run.Metrics.Score, 0, 100) ||
		!nonNegative(run.Metrics.AverageBrakeDeviationPercent) ||
		!nonNegative(run.Metrics.HoldTimeInBandMS) ||
		!nonNegative(run.Metrics.TrailErrorPercent) {
		return errors.New("run metrics contain an invalid value")
	}
	if run.Metrics.AcceleratorFallMS != nil && !nonNegative(*run.Metrics.AcceleratorFallMS) ||
		run.Metrics.BrakeRiseMS != nil && !nonNegative(*run.Metrics.BrakeRiseMS) ||
		run.Metrics.TransitionErrorPercent != nil && !nonNegative(*run.Metrics.TransitionErrorPercent) ||
		run.Metrics.AcceleratorRampErrorPercent != nil && !nonNegative(*run.Metrics.AcceleratorRampErrorPercent) {
		return errors.New("run metrics contain an invalid optional value")
	}
	if run.AccelerationIncluded && run.Metrics.AcceleratorRampErrorPercent == nil {
		return errors.New("acceleration practice requires an accelerator ramp metric")
	}
	if !run.AccelerationIncluded && run.Metrics.AcceleratorRampErrorPercent != nil {
		return errors.New("braking-only practice cannot include an accelerator ramp metric")
	}
	previous := -1.0
	for i, sample := range run.Samples {
		if !finiteRange(sample.TimeMS, 0, maxBrakeRunMS) || sample.TimeMS < previous {
			return fmt.Errorf("sample %d has an invalid or out-of-order time", i)
		}
		if !percent(sample.Accelerator) || !percent(sample.Brake) {
			return fmt.Errorf("sample %d pedal value is outside 0 to 100", i)
		}
		if strings.TrimSpace(sample.Source) == "" || len(sample.Source) > 40 {
			return fmt.Errorf("sample %d source is invalid", i)
		}
		previous = sample.TimeMS
	}
	return nil
}

func validateBrakeSettings(settings store.BrakeSettings) error {
	if settings.BaudRate < 1200 || settings.BaudRate > 3000000 {
		return errors.New("baudRate must be between 1200 and 3000000")
	}
	for name, value := range map[string]*string{
		"usbVendorId": settings.USBVendorID, "usbProductId": settings.USBProductID,
	} {
		if value != nil && (len(*value) > 6 || !validHexID(*value)) {
			return fmt.Errorf("%s must be a hexadecimal USB id", name)
		}
	}
	return nil
}

func validHexID(value string) bool {
	v := strings.TrimSpace(strings.ToLower(value))
	v = strings.TrimPrefix(v, "0x")
	if v == "" || len(v) > 4 {
		return false
	}
	for _, r := range v {
		if !strings.ContainsRune("0123456789abcdef", r) {
			return false
		}
	}
	return true
}

func percent(value float64) bool { return finiteRange(value, 0, 100) }

func nonNegative(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0
}

func finiteRange(value, min, max float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= min && value <= max
}

func (s *Server) brakeStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		s.fail(w, http.StatusNotFound, err)
	case errors.Is(err, store.ErrReadOnly):
		s.fail(w, http.StatusConflict, err)
	case strings.Contains(strings.ToLower(err.Error()), "constraint"):
		s.fail(w, http.StatusConflict, err)
	default:
		s.fail(w, http.StatusInternalServerError, err)
	}
}
