package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/blezek/lapdog/internal/collector"
	"github.com/blezek/lapdog/internal/config"
	"github.com/blezek/lapdog/internal/store"
	"github.com/blezek/lapdog/internal/web"
)

// LoopbackHost is the only interface the server ever binds.
//
// This is the security model, not a default: the server exposes the user's entire
// racing history with no authentication, so it must be unreachable from any other
// machine. There is deliberately no way to change it.
const LoopbackHost = "127.0.0.1"

// StatusProvider supplies the collector's current state.
type StatusProvider interface {
	Status() collector.Status
}

// ConfigStore reads and persists user settings.
type ConfigStore interface {
	Get() config.Config
	Set(config.Config) error
}

// Server serves the JSON API and the embedded user interface.
type Server struct {
	st  *store.Store
	sp  StatusProvider
	cfg ConfigStore
	log *slog.Logger
}

// New returns a Server.
func New(st *store.Store, sp StatusProvider, cfg ConfigStore, log *slog.Logger) *Server {
	return &Server{st: st, sp: sp, cfg: cfg, log: log}
}

// listenAddr returns the address the server binds for a given port.
func listenAddr(port int) string {
	return net.JoinHostPort(LoopbackHost, strconv.Itoa(port))
}

// Handler returns the routed handler.
func (s *Server) Handler() (http.Handler, error) {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/status", s.handleStatus)
	mux.HandleFunc("GET /api/totals", s.handleTotals)
	mux.HandleFunc("GET /api/summary", s.handleSummary)
	mux.HandleFunc("GET /api/daily", s.handleDaily)
	mux.HandleFunc("GET /api/breakdown", s.handleBreakdown)
	mux.HandleFunc("GET /api/entities", s.handleEntities)
	mux.HandleFunc("GET /api/entity", s.handleEntity)
	mux.HandleFunc("GET /api/pace", s.handlePace)
	mux.HandleFunc("GET /api/progression", s.handleProgression)
	mux.HandleFunc("GET /api/rivals", s.handleRivals)
	mux.HandleFunc("GET /api/racecraft", s.handleRacecraft)
	mux.HandleFunc("GET /api/quali-pace", s.handleQualiPace)
	mux.HandleFunc("GET /api/combos", s.handleCombos)
	mux.HandleFunc("GET /api/ratings", s.handleRatings)
	mux.HandleFunc("GET /api/sessions", s.handleSessions)
	mux.HandleFunc("GET /api/sessions/{id}", s.handleSession)
	mux.HandleFunc("GET /api/sessions/{id}/laps", s.handleSessionLaps)
	mux.HandleFunc("GET /api/sessions/{id}/positions", s.handleSessionPositions)
	mux.HandleFunc("GET /api/laps", s.handleLaps)
	mux.HandleFunc("GET /api/facets", s.handleFacets)
	mux.HandleFunc("GET /api/export", s.handleExport)
	mux.HandleFunc("/api/settings", s.handleSettings)

	// Any other /api path is a 404 rather than falling through to the interface,
	// so a typo in an endpoint reads as a missing endpoint and not as a page of
	// HTML.
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		s.fail(w, http.StatusNotFound, fmt.Errorf("no such endpoint: %s", r.URL.Path))
	})

	// Everything else is the interface, served out of the executable.
	ui, err := web.Handler()
	if err != nil {
		return nil, err
	}
	mux.Handle("/", ui)
	return mux, nil
}

// InterfaceHandler returns the routed handler after confirming that the embedded
// interface is present and usable.
func (s *Server) InterfaceHandler() (http.Handler, error) {
	// Refuse to start without a usable interface.
	//
	// The bundle is generated rather than committed, and //go:embed always finds
	// something because a placeholder keeps it compiling on a clone that has not run
	// the frontend build. So the check has to be explicit: without it the server
	// started happily and served a blank page, which reads as a frontend fault
	// rather than as a build step that was skipped.
	//
	// This lives here, not in Handler, because Handler is also how tests assemble
	// the API. Requiring the bundle there would make every JSON endpoint test
	// depend on a Node toolchain to say anything about JSON.
	if err := web.Check(); err != nil {
		return nil, err
	}
	return s.Handler()
}

// Serve serves h on an already-bound listener.
func (s *Server) Serve(ln net.Listener, h http.Handler) error {
	if h == nil {
		ln.Close()
		return errors.New("api: nil handler")
	}
	addr := ln.Addr().String()
	srv := &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
	}
	s.log.Info("serving user interface", "url", "http://"+addr)
	return srv.Serve(ln)
}

// ListenAndServe binds the loopback interface only and serves until the listener
// fails.
func (s *Server) ListenAndServe(port int) error {
	h, err := s.InterfaceHandler()
	if err != nil {
		return err
	}
	addr := listenAddr(port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("api: cannot listen on %s: %w", addr, err)
	}
	return s.Serve(ln, h)
}

// writeJSON sends v as JSON with a 200 status.
func (s *Server) writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	// The interface is same-origin and the data is local, so caching would only
	// ever serve stale figures.
	w.Header().Set("Cache-Control", "no-store")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		s.log.Error("response encoding failed", "err", err)
	}
}

// fail sends a JSON error body with the given status.
func (s *Server) fail(w http.ResponseWriter, code int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

// filterOrFail parses the request filter, writing a 400 on failure.
func (s *Server) filterOrFail(w http.ResponseWriter, r *http.Request) (store.Filter, bool) {
	f, err := parseFilter(r.URL.Query())
	if err != nil {
		s.fail(w, http.StatusBadRequest, err)
		return store.Filter{}, false
	}
	return f, true
}

// idOrFail parses the {id} path value, writing a 400 on failure.
func (s *Server) idOrFail(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		s.fail(w, http.StatusBadRequest, fmt.Errorf("%w: id must be an integer", ErrBadRequest))
		return 0, false
	}
	return id, true
}

// notFoundOr500 maps a store error onto the right status.
func (s *Server) notFoundOr500(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrNotFound) {
		s.fail(w, http.StatusNotFound, err)
		return
	}
	s.fail(w, http.StatusInternalServerError, err)
}
