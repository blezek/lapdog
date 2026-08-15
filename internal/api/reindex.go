package api

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"time"

	"github.com/blezek/lapdog/internal/config"
	"github.com/blezek/lapdog/internal/reindex"
)

type captureReindexStatus struct {
	State string `json:"state"`
	reindex.Progress
	Failures   []reindex.Failure `json:"failures,omitempty"`
	StartedAt  *time.Time        `json:"startedAt,omitempty"`
	FinishedAt *time.Time        `json:"finishedAt,omitempty"`
	Error      string            `json:"error,omitempty"`
}

type pauseSetter interface {
	SetPaused(bool)
}

func (s *Server) handleCaptureReindex(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.writeJSON(w, s.captureReindexSnapshot())
	case http.MethodPost:
		if s.sp != nil && s.sp.Status().Connected {
			s.fail(w, http.StatusConflict, errors.New("disconnect from iRacing before re-indexing saved captures"))
			return
		}
		captureDir := config.CapturesDir(filepath.Dir(s.st.Path()))
		paths, err := reindex.Discover(captureDir)
		if err != nil {
			if errors.Is(err, reindex.ErrNoCaptures) {
				s.fail(w, http.StatusBadRequest, err)
			} else {
				s.fail(w, http.StatusInternalServerError, err)
			}
			return
		}
		status, ok := s.startCaptureReindex(paths)
		if !ok {
			s.fail(w, http.StatusConflict, errors.New("capture re-index is already running"))
			return
		}
		s.writeJSONStatus(w, http.StatusAccepted, status)
	default:
		w.Header().Set("Allow", "GET, POST")
		s.fail(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
	}
}

func (s *Server) captureReindexSnapshot() captureReindexStatus {
	s.reindexMu.Lock()
	defer s.reindexMu.Unlock()
	status := s.reindexStatus
	if status.State == "" {
		status.State = "idle"
	}
	status.Failures = append([]reindex.Failure(nil), status.Failures...)
	return status
}

func (s *Server) startCaptureReindex(paths []string) (captureReindexStatus, bool) {
	s.reindexMu.Lock()
	if s.reindexStatus.State == "running" {
		status := s.reindexStatus
		s.reindexMu.Unlock()
		return status, false
	}
	started := time.Now().UTC()
	s.reindexStatus = captureReindexStatus{
		State:     "running",
		Progress:  reindex.Progress{Total: len(paths)},
		StartedAt: &started,
	}
	status := s.reindexStatus
	s.reindexMu.Unlock()

	// Pause before the goroutine exists. If this happened inside runCaptureReindex,
	// a simulator reconnect could deliver a live frame in the scheduling window
	// between returning 202 and the goroutine first running.
	wasPaused := false
	if s.sp != nil {
		wasPaused = s.sp.Status().Paused
	}
	var pauser pauseSetter
	if candidate, ok := s.sp.(pauseSetter); ok {
		pauser = candidate
		pauser.SetPaused(true)
	}

	go s.runCaptureReindex(paths, pauser, wasPaused)
	return status, true
}

func (s *Server) runCaptureReindex(paths []string, pauser pauseSetter, wasPaused bool) {
	var result reindex.Result
	err := s.st.ClearHistory()
	if err == nil {
		result, err = reindex.Run(context.Background(), paths, s.st, reindex.Options{
			Logger: s.log,
			OnProgress: func(progress reindex.Progress) {
				s.reindexMu.Lock()
				s.reindexStatus.Progress = progress
				s.reindexMu.Unlock()
			},
		})
	}
	if pauser != nil {
		pauser.SetPaused(wasPaused)
	}
	finished := time.Now().UTC()
	s.reindexMu.Lock()
	s.reindexStatus.Progress = result.Progress
	s.reindexStatus.Failures = append([]reindex.Failure(nil), result.Failures...)
	s.reindexStatus.FinishedAt = &finished
	if err != nil {
		s.reindexStatus.State = "failed"
		s.reindexStatus.Error = err.Error()
	} else {
		s.reindexStatus.State = "complete"
	}
	s.reindexMu.Unlock()
}
