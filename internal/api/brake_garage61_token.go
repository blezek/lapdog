package api

import (
	"errors"
	"net/http"
)

// The token is write-only over HTTP. Even the status response omits its value.
func (s *Server) handleGarageToken(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	switch r.Method {
	case http.MethodGet:
		status, err := s.garageToken.Status()
		if err != nil {
			s.fail(w, http.StatusInternalServerError, err)
			return
		}
		s.writeJSON(w, status)
	case http.MethodPut:
		var input struct {
			Token string `json:"token"`
		}
		if !s.decodeJSONRequest(w, r, &input) {
			return
		}
		if err := s.garageToken.Save(input.Token); err != nil {
			s.fail(w, http.StatusBadRequest, err)
			return
		}
		s.garageTokenChanged()
		s.writeJSON(w, map[string]any{"configured": true, "source": "file"})
	case http.MethodDelete:
		if err := s.garageToken.Delete(); err != nil {
			s.fail(w, http.StatusInternalServerError, err)
			return
		}
		s.garageTokenChanged()
		status, err := s.garageToken.Status()
		if err != nil {
			s.fail(w, http.StatusInternalServerError, err)
			return
		}
		s.writeJSON(w, status)
	default:
		w.Header().Set("Allow", "GET, PUT, DELETE")
		s.fail(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
	}
}

func (s *Server) garageTokenChanged() {
	s.garageState.mu.Lock()
	s.garageState.loaded = false
	s.garageState.catalogEpoch++
	s.garageState.mu.Unlock()
	s.wakeGarageQueue()
}
