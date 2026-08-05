package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/blezek/lapdog/internal/collector"
	"github.com/blezek/lapdog/internal/config"
	"github.com/blezek/lapdog/internal/store"
	"github.com/blezek/lapdog/internal/version"
)

// statusResponse is the collector status plus the process-level facts the settings
// screen shows.
type statusResponse struct {
	collector.Status
	Version      string `json:"version"`
	DatabasePath string `json:"databasePath"`
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	var st collector.Status
	if s.sp != nil {
		st = s.sp.Status()
	}
	s.writeJSON(w, statusResponse{
		Status:       st,
		Version:      version.Version,
		DatabasePath: s.st.Path(),
	})
}

func (s *Server) handleTotals(w http.ResponseWriter, r *http.Request) {
	f, ok := s.filterOrFail(w, r)
	if !ok {
		return
	}
	t, err := s.st.Totals(f)
	if err != nil {
		s.fail(w, http.StatusInternalServerError, err)
		return
	}
	s.writeJSON(w, t)
}

func (s *Server) handleSummary(w http.ResponseWriter, r *http.Request) {
	f, ok := s.filterOrFail(w, r)
	if !ok {
		return
	}
	groupBy := r.URL.Query().Get("group_by")
	if groupBy == "" {
		// The stacked bar is the primary consumer, so its grouping is the natural
		// default rather than an error.
		groupBy = "typecontext"
	}
	rows, err := s.st.Summary(f, groupBy)
	if errors.Is(err, store.ErrBadGroupBy) {
		// An unrecognised grouping is a client mistake, not a server fault.
		s.fail(w, http.StatusBadRequest, err)
		return
	}
	if err != nil {
		s.fail(w, http.StatusInternalServerError, err)
		return
	}
	s.writeJSON(w, rows)
}

// handleBreakdown serves a two-dimensional aggregate for the stacked bars: driving
// time per car or track, split by what the driver was doing.
func (s *Server) handleBreakdown(w http.ResponseWriter, r *http.Request) {
	f, ok := s.filterOrFail(w, r)
	if !ok {
		return
	}
	by := r.URL.Query().Get("by")
	if by == "" {
		by = "car"
	}
	rows, err := s.st.Breakdown(f, by)
	if errors.Is(err, store.ErrBadGroupBy) {
		// An unrecognised dimension is a client mistake, not a server fault.
		s.fail(w, http.StatusBadRequest, err)
		return
	}
	if err != nil {
		s.fail(w, http.StatusInternalServerError, err)
		return
	}
	s.writeJSON(w, rows)
}

func (s *Server) handleDaily(w http.ResponseWriter, r *http.Request) {
	f, ok := s.filterOrFail(w, r)
	if !ok {
		return
	}
	rows, err := s.st.Daily(f)
	if err != nil {
		s.fail(w, http.StatusInternalServerError, err)
		return
	}
	s.writeJSON(w, rows)
}

// listResponse wraps a page of rows with the total match count, so the interface
// can paginate and still say "86 sessions matched".
type listResponse struct {
	Items any `json:"items"`
	Total int `json:"total"`
}

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	f, ok := s.filterOrFail(w, r)
	if !ok {
		return
	}
	rows, total, err := s.st.ListSessions(f)
	if err != nil {
		s.fail(w, http.StatusInternalServerError, err)
		return
	}
	s.writeJSON(w, listResponse{Items: rows, Total: total})
}

func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	id, ok := s.idOrFail(w, r)
	if !ok {
		return
	}
	rec, err := s.st.SessionByID(id)
	if err != nil {
		s.notFoundOr500(w, err)
		return
	}
	s.writeJSON(w, rec)
}

func (s *Server) handleSessionLaps(w http.ResponseWriter, r *http.Request) {
	id, ok := s.idOrFail(w, r)
	if !ok {
		return
	}
	laps, err := s.st.LapsForSession(id)
	if err != nil {
		s.fail(w, http.StatusInternalServerError, err)
		return
	}
	s.writeJSON(w, laps)
}

func (s *Server) handleSessionPositions(w http.ResponseWriter, r *http.Request) {
	id, ok := s.idOrFail(w, r)
	if !ok {
		return
	}
	evs, err := s.st.PositionEventsForSession(id)
	if err != nil {
		s.fail(w, http.StatusInternalServerError, err)
		return
	}
	s.writeJSON(w, evs)
}

func (s *Server) handleLaps(w http.ResponseWriter, r *http.Request) {
	f, ok := s.filterOrFail(w, r)
	if !ok {
		return
	}
	rows, total, err := s.st.ListLaps(f)
	if err != nil {
		s.fail(w, http.StatusInternalServerError, err)
		return
	}
	s.writeJSON(w, listResponse{Items: rows, Total: total})
}

// facetsResponse adds the allowlisted grouping names, so the interface does not
// have to hard-code a list the server owns.
type facetsResponse struct {
	store.Facets
	GroupBy     []string `json:"groupBy"`
	BreakdownBy []string `json:"breakdownBy"`
}

func (s *Server) handleFacets(w http.ResponseWriter, r *http.Request) {
	f, err := s.st.Facets()
	if err != nil {
		s.fail(w, http.StatusInternalServerError, err)
		return
	}
	s.writeJSON(w, facetsResponse{
		Facets:      f,
		GroupBy:     store.GroupByNames(),
		BreakdownBy: store.BreakdownNames(),
	})
}

// settingsResponse echoes the saved config and names the fields whose change needs
// a restart, which the interface tells the user about.
type settingsResponse struct {
	Config          config.Config `json:"config"`
	RestartRequired []string      `json:"restartRequired"`
}

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	if s.cfg == nil {
		s.fail(w, http.StatusInternalServerError, errors.New("no configuration store"))
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.writeJSON(w, s.cfg.Get())

	case http.MethodPut:
		before := s.cfg.Get()
		// Start from the current values so a partial body updates only what it
		// names rather than silently zeroing everything it omits.
		next := before
		if err := json.NewDecoder(r.Body).Decode(&next); err != nil {
			s.fail(w, http.StatusBadRequest, err)
			return
		}
		// Validate rather than normalise: a value the user explicitly typed should
		// be reported as wrong, not silently changed underneath them.
		if err := next.Validate(); err != nil {
			s.fail(w, http.StatusBadRequest, err)
			return
		}
		if err := s.cfg.Set(next); err != nil {
			s.fail(w, http.StatusInternalServerError, err)
			return
		}
		var restart []string
		if next.Port != before.Port {
			restart = append(restart, "port")
		}
		s.writeJSON(w, settingsResponse{Config: next, RestartRequired: restart})

	default:
		w.Header().Set("Allow", "GET, PUT")
		s.fail(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
	}
}

// requiredInt reads a required integer query parameter.
//
// Absent and unparseable are both client mistakes, and both must be 400 rather than
// a zero that would silently query entity 0.
func (s *Server) requiredInt(w http.ResponseWriter, q url.Values, key string) (int, bool) {
	raw := q.Get(key)
	if raw == "" {
		s.fail(w, http.StatusBadRequest, fmt.Errorf("%w: %s is required", ErrBadRequest, key))
		return 0, false
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		s.fail(w, http.StatusBadRequest,
			fmt.Errorf("%w: %s must be an integer", ErrBadRequest, key))
		return 0, false
	}
	return v, true
}

// dimension reads the by parameter, defaulting to car as /api/breakdown does.
func dimension(q url.Values) string {
	if by := q.Get("by"); by != "" {
		return by
	}
	return "car"
}

// requiredDimension reads a required by parameter, writing a 400 when it is
// absent.
//
// /api/breakdown can default by to car because by only changes how the response
// is grouped: a caller who forgets it sees the wrong shape and can tell. On the
// id-scoped endpoints (/api/entity, /api/pace, /api/progression,
// /api/quali-pace), by also selects which id space id is looked up in. Car ids
// and track ids are independent iRacing integers with no guaranteed disjoint
// ranges, so defaulting by there would let an id meant for one dimension resolve,
// silently and with a 200, against the other. There the same default that is a
// convenience on /api/breakdown would hide a client mistake instead.
func (s *Server) requiredDimension(w http.ResponseWriter, q url.Values) (string, bool) {
	by := q.Get("by")
	if by == "" {
		s.fail(w, http.StatusBadRequest, fmt.Errorf("%w: by is required", ErrBadRequest))
		return "", false
	}
	return by, true
}

func (s *Server) handleEntities(w http.ResponseWriter, r *http.Request) {
	f, ok := s.filterOrFail(w, r)
	if !ok {
		return
	}
	rows, err := s.st.EntityList(f, dimension(r.URL.Query()))
	if errors.Is(err, store.ErrBadGroupBy) {
		s.fail(w, http.StatusBadRequest, err)
		return
	}
	if err != nil {
		s.fail(w, http.StatusInternalServerError, err)
		return
	}
	s.writeJSON(w, rows)
}

func (s *Server) handleEntity(w http.ResponseWriter, r *http.Request) {
	f, ok := s.filterOrFail(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	id, ok := s.requiredInt(w, q, "id")
	if !ok {
		return
	}
	by, ok := s.requiredDimension(w, q)
	if !ok {
		return
	}
	st, err := s.st.EntityStats(f, by, id)
	if errors.Is(err, store.ErrBadGroupBy) {
		s.fail(w, http.StatusBadRequest, err)
		return
	}
	if err != nil {
		s.notFoundOr500(w, err)
		return
	}
	s.writeJSON(w, st)
}

func (s *Server) handlePace(w http.ResponseWriter, r *http.Request) {
	f, ok := s.filterOrFail(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	id, ok := s.requiredInt(w, q, "id")
	if !ok {
		return
	}
	by, ok := s.requiredDimension(w, q)
	if !ok {
		return
	}
	rows, err := s.st.EntityPace(f, by, id)
	if errors.Is(err, store.ErrBadGroupBy) {
		s.fail(w, http.StatusBadRequest, err)
		return
	}
	if err != nil {
		s.fail(w, http.StatusInternalServerError, err)
		return
	}
	s.writeJSON(w, rows)
}

func (s *Server) handleProgression(w http.ResponseWriter, r *http.Request) {
	f, ok := s.filterOrFail(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	id, ok := s.requiredInt(w, q, "id")
	if !ok {
		return
	}
	other, ok := s.requiredInt(w, q, "other")
	if !ok {
		return
	}
	by, ok := s.requiredDimension(w, q)
	if !ok {
		return
	}
	rows, err := s.st.EntityProgression(f, by, id, other)
	if errors.Is(err, store.ErrBadGroupBy) {
		s.fail(w, http.StatusBadRequest, err)
		return
	}
	if err != nil {
		s.fail(w, http.StatusInternalServerError, err)
		return
	}
	s.writeJSON(w, rows)
}

func (s *Server) handleRivals(w http.ResponseWriter, r *http.Request) {
	f, ok := s.filterOrFail(w, r)
	if !ok {
		return
	}
	rows, err := s.st.Rivals(f)
	if err != nil {
		s.fail(w, http.StatusInternalServerError, err)
		return
	}
	s.writeJSON(w, rows)
}

func (s *Server) handleQualiPace(w http.ResponseWriter, r *http.Request) {
	f, ok := s.filterOrFail(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	id, ok := s.requiredInt(w, q, "id")
	if !ok {
		return
	}
	by, ok := s.requiredDimension(w, q)
	if !ok {
		return
	}
	got, err := s.st.QualifyingVsRace(f, by, id)
	if errors.Is(err, store.ErrBadGroupBy) {
		s.fail(w, http.StatusBadRequest, err)
		return
	}
	if err != nil {
		s.fail(w, http.StatusInternalServerError, err)
		return
	}
	s.writeJSON(w, got)
}

func (s *Server) handleCombos(w http.ResponseWriter, r *http.Request) {
	f, ok := s.filterOrFail(w, r)
	if !ok {
		return
	}
	// top is optional; the store clamps a missing or non-positive value. Named
	// "top" rather than "limit" on the wire: Filter already has its own "limit"
	// for pagination, and reusing the key would let the two silently collide.
	limit := 0
	if raw := r.URL.Query().Get("top"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil {
			s.fail(w, http.StatusBadRequest,
				fmt.Errorf("%w: top must be an integer", ErrBadRequest))
			return
		}
		limit = v
	}
	cells, err := s.st.TopCombos(f, limit)
	if err != nil {
		s.fail(w, http.StatusInternalServerError, err)
		return
	}
	s.writeJSON(w, cells)
}
