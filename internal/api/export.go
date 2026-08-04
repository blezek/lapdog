package api

import (
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/blezek/lapdog/internal/store"
)

// exportScopes is the allowlist of exportable row sets.
//
// Scope selects a fixed SQL statement; it is never interpolated, because it
// arrives from a query parameter.
var exportScopes = map[string]bool{
	"sessions":  true,
	"laps":      true,
	"positions": true,
}

// ExportScopes returns the allowlisted scope names.
func ExportScopes() []string {
	out := make([]string, 0, len(exportScopes))
	for k := range exportScopes {
		out = append(out, k)
	}
	return out
}

// exportQuery returns the column names and the SQL for a scope.
//
// Each statement selects explicit columns rather than star, so the export format
// stays stable even if the schema later gains columns.
func exportQuery(scope, predicate string) ([]string, string) {
	switch scope {
	case "sessions":
		cols := []string{
			"id", "session_key", "subsession_id", "session_num",
			"session_type", "event_context", "league_id", "series_id", "official",
			"track_id", "track_name", "track_config", "track_length_km",
			"car_id", "car_name", "car_class_name",
			"started_at", "ended_at",
			"connected_seconds", "in_car_seconds", "driving_seconds",
			"laps_completed", "incidents", "best_lap_time_s",
			"starting_position", "finish_position", "finish_class_position",
			"qualify_position", "qualify_class_position", "qualify_best_time_s",
			"field_size", "ai_opponent_count", "ai_detection", "incident_source",
		}
		q := `SELECT ` + prefixed("s", cols) +
			` FROM sessions s WHERE ` + predicate +
			` ORDER BY s.started_at, s.session_num`
		return cols, q

	case "laps":
		cols := []string{
			"session_id", "lap_number", "lap_time_s", "delta_to_best_s",
			"fuel_used_l", "fuel_level_end_l", "incidents_on_lap", "is_pit_lap",
			"position", "class_position", "recorded_at",
			"started_at", "track_name", "car_name", "session_type", "event_context",
		}
		q := `SELECT l.session_id, l.lap_number, l.lap_time_s, l.delta_to_best_s,
		             l.fuel_used_l, l.fuel_level_end_l, l.incidents_on_lap, l.is_pit_lap,
		             l.position, l.class_position, l.recorded_at,
		             s.started_at, s.track_name, s.car_name, s.session_type, s.event_context
		      FROM laps l JOIN sessions s ON s.id = l.session_id
		      WHERE ` + predicate + `
		      ORDER BY s.started_at, l.lap_number`
		return cols, q

	default: // positions
		cols := []string{
			"session_id", "lap_number", "session_time_s",
			"from_position", "to_position", "is_class",
			"opponent_car_idx", "opponent_name", "cause", "recorded_at",
			"started_at", "track_name", "car_name", "session_type", "event_context",
		}
		q := `SELECT pe.session_id, pe.lap_number, pe.session_time_s,
		             pe.from_position, pe.to_position, pe.is_class,
		             pe.opponent_car_idx, pe.opponent_name, pe.cause, pe.recorded_at,
		             s.started_at, s.track_name, s.car_name, s.session_type, s.event_context
		      FROM position_events pe JOIN sessions s ON s.id = pe.session_id
		      WHERE ` + predicate + `
		      ORDER BY s.started_at, pe.session_time_s`
		return cols, q
	}
}

// prefixed qualifies each column with a table alias.
func prefixed(alias string, cols []string) string {
	out := make([]string, len(cols))
	for i, c := range cols {
		out[i] = alias + "." + c
	}
	return strings.Join(out, ", ")
}

// handleExport streams a filtered row set as CSV or JSON.
//
// Rows are written to the response as they are scanned rather than collected
// first, so exporting several years of history does not balloon memory.
func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	scope := q.Get("scope")
	if !exportScopes[scope] {
		s.fail(w, http.StatusBadRequest,
			fmt.Errorf("%w: scope must be one of %s", ErrBadRequest, strings.Join(ExportScopes(), ", ")))
		return
	}
	format := q.Get("format")
	if format == "" {
		format = "csv"
	}
	if format != "csv" && format != "json" {
		s.fail(w, http.StatusBadRequest,
			fmt.Errorf("%w: format must be csv or json", ErrBadRequest))
		return
	}

	f, ok := s.filterOrFail(w, r)
	if !ok {
		return
	}
	predicate, args := store.FilterPredicate(f)
	cols, sqlText := exportQuery(scope, predicate)

	rows, err := s.st.Reader().Query(sqlText, args...)
	if err != nil {
		s.fail(w, http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()

	// A date stamp keeps repeated exports from overwriting one another in the
	// browser's download folder.
	name := fmt.Sprintf("lapdog-%s-%s.%s", scope, time.Now().UTC().Format("20060102"), format)
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("Cache-Control", "no-store")

	if format == "csv" {
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		if err := streamCSV(w, rows, cols); err != nil {
			// Headers are already sent, so the response cannot become a 500. Log it
			// and let the truncated download surface the problem.
			s.log.Error("CSV export failed mid-stream", "scope", scope, "err", err)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := streamJSON(w, rows, cols); err != nil {
		s.log.Error("JSON export failed mid-stream", "scope", scope, "err", err)
	}
}

// scanTargets allocates one *any per column for a generic scan.
func scanTargets(n int) ([]any, []any) {
	vals := make([]any, n)
	ptrs := make([]any, n)
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	return vals, ptrs
}

// streamCSV writes the result set as CSV, header first.
//
// The header is always written, even for an empty result, so the file remains
// readable and self-describing rather than being a zero-byte download.
func streamCSV(w http.ResponseWriter, rows *sql.Rows, cols []string) error {
	cw := csv.NewWriter(w)
	defer cw.Flush()

	if err := cw.Write(cols); err != nil {
		return err
	}
	vals, ptrs := scanTargets(len(cols))
	line := make([]string, len(cols))

	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			return err
		}
		for i, v := range vals {
			line[i] = csvValue(v)
		}
		if err := cw.Write(line); err != nil {
			return err
		}
		// Flush periodically so a large export starts downloading promptly rather
		// than buffering in full.
		cw.Flush()
		if err := cw.Error(); err != nil {
			return err
		}
	}
	return rows.Err()
}

// csvValue renders a scanned value as a CSV field.
//
// NULL becomes an empty field, which is the convention spreadsheets expect.
func csvValue(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case []byte:
		return string(x)
	case string:
		return x
	case bool:
		if x {
			return "true"
		}
		return "false"
	case int64:
		return strconv.FormatInt(x, 10)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case time.Time:
		return x.UTC().Format(time.RFC3339)
	default:
		return fmt.Sprint(x)
	}
}

// streamJSON writes the result set as a JSON array of objects.
//
// The array brackets are written by hand so rows can be encoded one at a time
// instead of being gathered into a slice first.
func streamJSON(w http.ResponseWriter, rows *sql.Rows, cols []string) error {
	if _, err := w.Write([]byte("[")); err != nil {
		return err
	}
	enc := json.NewEncoder(w)
	vals, ptrs := scanTargets(len(cols))
	first := true

	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			return err
		}
		if !first {
			if _, err := w.Write([]byte(",")); err != nil {
				return err
			}
		}
		first = false

		obj := make(map[string]any, len(cols))
		for i, c := range cols {
			if b, ok := vals[i].([]byte); ok {
				obj[c] = string(b)
				continue
			}
			obj[c] = vals[i]
		}
		if err := enc.Encode(obj); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err := w.Write([]byte("]"))
	return err
}
