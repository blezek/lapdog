package api

import (
	"errors"
	"net/http"

	"github.com/blezek/lapdog/internal/updater"
	"github.com/blezek/lapdog/internal/version"
)

func (s *Server) handleUpdate(w http.ResponseWriter, _ *http.Request) {
	if s.updates == nil {
		s.writeJSON(w, updater.Snapshot{State: updater.Disabled, CurrentVersion: version.Version, CurrentRevision: version.RevisionPtr(), RestartSafe: true})
		return
	}
	s.writeJSON(w, s.updates.Snapshot())
}

func (s *Server) handleUpdateCheck(w http.ResponseWriter, r *http.Request) {
	if s.updates == nil {
		s.fail(w, http.StatusServiceUnavailable, errors.New("update coordinator unavailable"))
		return
	}
	if err := s.updates.Check(r.Context()); err != nil {
		s.fail(w, http.StatusBadGateway, err)
		return
	}
	s.writeJSON(w, s.updates.Snapshot())
}

func (s *Server) handleUpdateAction(w http.ResponseWriter, r *http.Request) {
	if s.updates == nil {
		s.fail(w, http.StatusServiceUnavailable, errors.New("update coordinator unavailable"))
		return
	}
	var body struct {
		Action string `json:"action"`
	}
	if !s.decodeJSONRequest(w, r, &body) {
		return
	}
	if err := s.updates.Action(r.Context(), body.Action); err != nil {
		s.fail(w, http.StatusConflict, err)
		return
	}
	s.writeJSON(w, s.updates.Snapshot())
}
