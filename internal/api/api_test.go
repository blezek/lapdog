package api

import (
	"encoding/csv"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/blezek/lapdog/internal/collector"
	"github.com/blezek/lapdog/internal/config"
	"github.com/blezek/lapdog/internal/store"
	"github.com/blezek/lapdog/internal/web/webtest"
)

// ------------------------------------------------------------- test doubles

type fakeStatus struct{ s collector.Status }

func (f fakeStatus) Status() collector.Status { return f.s }

type fakeConfig struct {
	mu sync.Mutex
	c  config.Config
}

func (f *fakeConfig) Get() config.Config {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.c
}

func (f *fakeConfig) Set(c config.Config) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.c = c
	return nil
}

func intp(v int) *int         { return &v }
func f64p(v float64) *float64 { return &v }
func strp(v string) *string   { return &v }

// newTestServer seeds a store with a practice session and a race, so filters,
// aggregates and position events all have something to act on.
func newTestServer(t *testing.T) (http.Handler, *store.Store, *fakeConfig) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "lapdog.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	seed := []struct {
		key, st, ctx, started string
		conn, car, drive      float64
		laps, inc             int
		best                  float64
		isRace                bool
	}{
		{"900001/0", "Practice", "OfficialPractice", "2026-07-01T10:00:00Z", 3600, 2400, 2000, 20, 2, 102.5, false},
		{"900002/2", "Race", "OfficialRace", "2026-07-08T18:45:00Z", 3000, 2900, 2800, 25, 6, 102.0, true},
	}
	for _, r := range seed {
		rec := &store.Session{
			SessionKey: r.key, SessionType: r.st, EventContext: r.ctx,
			StartedAt:        r.started,
			ConnectedSeconds: r.conn, InCarSeconds: r.car, DrivingSeconds: r.drive,
			LapsCompleted: r.laps, Incidents: r.inc, BestLapTimeS: f64p(r.best),
			TrackID: intp(18), TrackName: strp("Watkins Glen International"),
			CarID: intp(173), CarName: strp("Porsche 911 GT3 R"),
			ClassifySourceJSON: "{}", IncidentSource: "yaml",
		}
		if r.isRace {
			rec.FinishPosition = intp(4)
			rec.QualifyPosition = intp(6)
			rec.FieldSize = intp(24)
		}
		id, err := st.UpsertSession(rec)
		if err != nil {
			t.Fatal(err)
		}
		for n := 1; n <= 2; n++ {
			lt := r.best + float64(n)*0.1
			if _, err := st.InsertLap(&store.Lap{
				SessionID: id, LapNumber: n, LapTimeS: &lt,
			}); err != nil {
				t.Fatal(err)
			}
		}
		if r.isRace {
			for _, ev := range []store.PositionEvent{
				{LapNumber: 2, SessionTimeS: 100, FromPosition: 6, ToPosition: 5,
					Cause: store.CauseOnTrack, OpponentCarIdx: intp(14), OpponentName: strp("Rival Driver")},
				{LapNumber: 9, SessionTimeS: 400, FromPosition: 5, ToPosition: 4,
					Cause: store.CauseOpponentPit, OpponentCarIdx: intp(22)},
			} {
				ev.SessionID = id
				if _, err := st.InsertPositionEvent(&ev); err != nil {
					t.Fatal(err)
				}
			}
		}
	}

	cfg := &fakeConfig{c: config.Default()}
	sp := fakeStatus{s: collector.Status{
		Connected: true, IntervalSeconds: 1,
		SessionLabel: "Public Practice", TrackName: "Watkins Glen International",
	}}
	srv := New(st, sp, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	h, err := srv.Handler()
	if err != nil {
		t.Fatal(err)
	}
	return h, st, cfg
}

// get performs a GET and decodes the JSON body into v when the status is 200.
func get(t *testing.T, h http.Handler, path string, v any) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	if v != nil && rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), v); err != nil {
			t.Fatalf("%s: body is not JSON: %v\n%s", path, err, rec.Body.String())
		}
	}
	return rec
}

// ------------------------------------------------------------------ filter

func mustValues(t *testing.T, raw string) url.Values {
	t.Helper()
	v, err := url.ParseQuery(raw)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestParseFilterAllFields(t *testing.T) {
	f, err := parseFilter(mustValues(t,
		"from=2026-07-01T00:00:00Z&to=2026-08-01T00:00:00Z"+
			"&session_type=Race&session_type=Qualify&event_context=League"+
			"&track_id=341&car_id=173&league_id=4242"+
			"&exclude_ai=true&limit=50&offset=100"))
	if err != nil {
		t.Fatalf("parseFilter: %v", err)
	}
	if f.From != "2026-07-01T00:00:00Z" || f.To != "2026-08-01T00:00:00Z" {
		t.Errorf("dates = %q / %q", f.From, f.To)
	}
	if len(f.SessionType) != 2 || len(f.EventContext) != 1 {
		t.Errorf("types=%v contexts=%v", f.SessionType, f.EventContext)
	}
	if f.TrackID == nil || *f.TrackID != 341 || f.CarID == nil || f.LeagueID == nil {
		t.Errorf("ids = %v %v %v", f.TrackID, f.CarID, f.LeagueID)
	}
	if !f.ExcludeAI || f.Limit != 50 || f.Offset != 100 {
		t.Errorf("excludeAI=%v limit=%d offset=%d", f.ExcludeAI, f.Limit, f.Offset)
	}
}

// Both repeated keys and a comma-separated value are natural from a query string.
func TestParseFilterCommaSeparatedList(t *testing.T) {
	f, err := parseFilter(mustValues(t, "session_type=Race,Qualify,Practice"))
	if err != nil {
		t.Fatal(err)
	}
	if len(f.SessionType) != 3 {
		t.Errorf("SessionType = %v, want three values", f.SessionType)
	}
}

// A bare date as the upper bound must cover the whole day. Treating it as midnight
// would silently exclude everything that happened on the day the user selected.
func TestParseFilterBareDateCoversWholeDay(t *testing.T) {
	f, err := parseFilter(mustValues(t, "from=2026-07-01&to=2026-08-04"))
	if err != nil {
		t.Fatalf("parseFilter: %v", err)
	}
	if f.From != "2026-07-01T00:00:00Z" {
		t.Errorf("From = %q, want the start of the day", f.From)
	}
	if !strings.HasPrefix(f.To, "2026-08-04T23:59:") {
		t.Errorf("To = %q, want the end of the day so that day's sessions are included", f.To)
	}
}

func TestParseFilterRejectsBadValues(t *testing.T) {
	for _, raw := range []string{
		"track_id=notanumber", "limit=abc", "offset=-5", "limit=-1",
		"from=yesterday", "to=2026-13-45", "exclude_ai=maybe",
	} {
		if _, err := parseFilter(mustValues(t, raw)); err == nil {
			t.Errorf("parseFilter(%q) = nil error, want a rejection", raw)
		}
	}
}

// A limit above the cap is clamped rather than rejected, so a bug in the interface
// degrades instead of failing.
func TestParseFilterClampsLimit(t *testing.T) {
	f, err := parseFilter(mustValues(t, "limit=999999"))
	if err != nil {
		t.Fatal(err)
	}
	if f.Limit != MaxLimit {
		t.Errorf("Limit = %d, want it clamped to %d", f.Limit, MaxLimit)
	}
}

// ---------------------------------------------------------------- endpoints

func TestStatusEndpoint(t *testing.T) {
	h, _, _ := newTestServer(t)
	var body struct {
		Connected    bool   `json:"connected"`
		SessionLabel string `json:"sessionLabel"`
		Version      string `json:"version"`
		DatabasePath string `json:"databasePath"`
	}
	rec := get(t, h, "/api/status", &body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !body.Connected || body.SessionLabel != "Public Practice" {
		t.Errorf("body = %+v", body)
	}
	if body.Version == "" || body.DatabasePath == "" {
		t.Errorf("version=%q dbPath=%q, both must be reported", body.Version, body.DatabasePath)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q", ct)
	}
	// Local, live data must never be cached or the figures go stale.
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", cc)
	}
}

func TestTotalsEndpoint(t *testing.T) {
	h, _, _ := newTestServer(t)
	var body store.Totals
	if rec := get(t, h, "/api/totals", &body); rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if body.Sessions != 2 {
		t.Errorf("Sessions = %d, want 2", body.Sessions)
	}
	if body.DrivingHours == 0 || body.Utilisation == 0 {
		t.Errorf("DrivingHours=%v Utilisation=%v", body.DrivingHours, body.Utilisation)
	}
	// One OnTrack pass; the pit-caused gain must not be counted.
	if body.PassesMade != 1 {
		t.Errorf("PassesMade = %d, want 1 — attrition must be excluded", body.PassesMade)
	}
}

func TestSummaryEndpointAndDefault(t *testing.T) {
	h, _, _ := newTestServer(t)

	var byType []store.SummaryRow
	get(t, h, "/api/summary?group_by=type", &byType)
	if len(byType) != 2 {
		t.Errorf("group_by=type returned %d rows, want 2", len(byType))
	}

	// No group_by must pick a sensible default rather than erroring.
	var dflt []store.SummaryRow
	if rec := get(t, h, "/api/summary", &dflt); rec.Code != http.StatusOK {
		t.Fatalf("default grouping: status = %d", rec.Code)
	}
	if len(dflt) == 0 {
		t.Error("default grouping returned no rows")
	}
}

// An unrecognised grouping is a client mistake, so 400 not 500.
func TestSummaryRejectsUnknownGroupBy(t *testing.T) {
	h, _, _ := newTestServer(t)
	if rec := get(t, h, "/api/summary?group_by=nonsense", nil); rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestSessionsEndpointAndChildren(t *testing.T) {
	h, st, _ := newTestServer(t)

	var body struct {
		Items []store.Session `json:"items"`
		Total int             `json:"total"`
	}
	get(t, h, "/api/sessions", &body)
	if body.Total != 2 || len(body.Items) != 2 {
		t.Fatalf("body = %+v", body)
	}

	rows, _, _ := st.ListSessions(store.Filter{SessionType: []string{"Race"}})
	id := strconv.FormatInt(rows[0].ID, 10)

	var sess store.Session
	if rec := get(t, h, "/api/sessions/"+id, &sess); rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if sess.FinishPosition == nil || *sess.FinishPosition != 4 {
		t.Errorf("FinishPosition = %v", sess.FinishPosition)
	}

	var laps []store.Lap
	get(t, h, "/api/sessions/"+id+"/laps", &laps)
	if len(laps) != 2 {
		t.Errorf("laps = %d, want 2", len(laps))
	}

	var evs []store.PositionEvent
	get(t, h, "/api/sessions/"+id+"/positions", &evs)
	if len(evs) != 2 {
		t.Errorf("position events = %d, want 2", len(evs))
	}
	// Opponent names are stored and served as-is; there is no anonymisation.
	found := false
	for _, ev := range evs {
		if ev.OpponentName != nil && *ev.OpponentName == "Rival Driver" {
			found = true
		}
	}
	if !found {
		t.Error("opponent name absent from the response")
	}
}

// The large provenance blob must not be shipped to the browser on every session.
func TestSessionResponseOmitsProvenance(t *testing.T) {
	h, _, _ := newTestServer(t)
	rec := get(t, h, "/api/sessions", nil)
	if strings.Contains(rec.Body.String(), "classifySourceJson") ||
		strings.Contains(rec.Body.String(), "classify_source_json") {
		t.Error("session responses include classify_source_json; it is large and only reclassify reads it")
	}
}

// An empty result must be [] not null, or the interface has to special-case it.
func TestEmptyResultsAreArrays(t *testing.T) {
	h, _, _ := newTestServer(t)
	rec := get(t, h, "/api/sessions?track_id=9999", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var body map[string]json.RawMessage
	json.Unmarshal(rec.Body.Bytes(), &body)
	if string(body["items"]) != "[]" {
		t.Errorf("items = %s, want []", body["items"])
	}
}

func TestSessionErrors(t *testing.T) {
	h, _, _ := newTestServer(t)
	if rec := get(t, h, "/api/sessions/999999", nil); rec.Code != http.StatusNotFound {
		t.Errorf("missing id: status = %d, want 404", rec.Code)
	}
	if rec := get(t, h, "/api/sessions/notanumber", nil); rec.Code != http.StatusBadRequest {
		t.Errorf("bad id: status = %d, want 400", rec.Code)
	}
	if rec := get(t, h, "/api/sessions?track_id=abc", nil); rec.Code != http.StatusBadRequest {
		t.Errorf("bad filter: status = %d, want 400", rec.Code)
	}
}

func TestLapsAndFacetsEndpoints(t *testing.T) {
	h, _, _ := newTestServer(t)

	var laps struct {
		Items []store.LapRow `json:"items"`
		Total int            `json:"total"`
	}
	get(t, h, "/api/laps", &laps)
	if laps.Total != 4 {
		t.Errorf("lap total = %d, want 4", laps.Total)
	}
	if laps.Items[0].TrackName == "" {
		t.Error("lap rows must carry the joined session context")
	}

	var facets facetsResponse
	get(t, h, "/api/facets", &facets)
	if len(facets.Tracks) != 1 || len(facets.Cars) != 1 {
		t.Errorf("facets = %+v", facets)
	}
	// The server owns the grouping allowlist, so it advertises it.
	if len(facets.GroupBy) == 0 {
		t.Error("facets response does not advertise the grouping names")
	}
}

// ----------------------------------------------------------------- settings

func TestGetAndPutSettings(t *testing.T) {
	h, _, cfg := newTestServer(t)

	var got config.Config
	get(t, h, "/api/settings", &got)
	if got.Port != config.DefaultPort {
		t.Errorf("Port = %d", got.Port)
	}

	body := `{"pollIntervalSeconds":2.5,"captureEnabled":false,"theme":"dark"}`
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/settings", strings.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d\n%s", rec.Code, rec.Body.String())
	}

	saved := cfg.Get()
	if saved.PollIntervalSeconds != 2.5 || saved.Theme != "dark" || saved.CaptureEnabled {
		t.Errorf("stored config = %+v", saved)
	}
	// A partial body must leave unnamed fields alone rather than zeroing them.
	if saved.Port != config.DefaultPort {
		t.Errorf("Port = %d after a partial update, want it preserved", saved.Port)
	}
	if saved.MinSessionSeconds != config.Default().MinSessionSeconds {
		t.Errorf("MinSessionSeconds = %v after a partial update, want it preserved",
			saved.MinSessionSeconds)
	}
}

// A port change needs a restart and the response must say so, since the interface
// tells the user.
func TestPutSettingsReportsRestartRequired(t *testing.T) {
	h, _, _ := newTestServer(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/settings",
		strings.NewReader(`{"port":48000}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var out settingsResponse
	json.Unmarshal(rec.Body.Bytes(), &out)
	found := false
	for _, f := range out.RestartRequired {
		if f == "port" {
			found = true
		}
	}
	if !found {
		t.Errorf("restartRequired = %v, want it to name port", out.RestartRequired)
	}
}

// An invalid value the user explicitly typed must be reported, not silently
// clamped underneath them, and must not mutate the stored config.
func TestPutSettingsRejectsInvalid(t *testing.T) {
	h, _, cfg := newTestServer(t)
	before := cfg.Get()
	for _, body := range []string{
		`{"pollIntervalSeconds":999}`,
		`{"port":0}`,
		`{"units":"furlongs"}`,
		`{not json`,
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/settings", strings.NewReader(body)))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("PUT %s: status = %d, want 400", body, rec.Code)
		}
	}
	if cfg.Get() != before {
		t.Error("a rejected update mutated the stored config")
	}
}

func TestSettingsRejectsWrongMethod(t *testing.T) {
	h, _, _ := newTestServer(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/api/settings", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.Code)
	}
	if allow := rec.Header().Get("Allow"); allow == "" {
		t.Error("405 response has no Allow header")
	}
}

// ------------------------------------------------------------------ routing

// A typo in an endpoint must read as a missing endpoint, not a page of HTML.
func TestUnknownAPIPathIs404JSON(t *testing.T) {
	h, _, _ := newTestServer(t)
	for _, p := range []string{"/api/nope", "/api/", "/api/sessions/1/nope"} {
		rec := get(t, h, p, nil)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404", p, rec.Code)
		}
		if strings.Contains(rec.Body.String(), "<html") {
			t.Errorf("%s returned HTML instead of a JSON error", p)
		}
	}
}

// Non-API paths serve the embedded interface, and client-side routes survive a
// reload.
func TestNonAPIPathsServeTheInterface(t *testing.T) {
	webtest.RequireBundle(t)
	h, _, _ := newTestServer(t)
	for _, p := range []string{"/", "/sessions", "/settings"} {
		rec := get(t, h, p, nil)
		if rec.Code != http.StatusOK {
			t.Errorf("%s: status = %d, want 200", p, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "LapDog") {
			t.Errorf("%s did not serve the interface", p)
		}
	}
	// Icons come out of the same executable.
	if rec := get(t, h, "/icons/racing-helmet.svg", nil); rec.Code != http.StatusOK {
		t.Errorf("icon: status = %d", rec.Code)
	}
}

// The server must only ever bind loopback. There is no authentication because the
// data is unreachable off the machine, so this is load-bearing.
func TestListenAddrIsLoopbackOnly(t *testing.T) {
	if LoopbackHost != "127.0.0.1" {
		t.Fatalf("LoopbackHost = %q, want 127.0.0.1", LoopbackHost)
	}
	got := listenAddr(config.DefaultPort)
	if got != "127.0.0.1:47047" {
		t.Errorf("listenAddr(47047) = %q, want 127.0.0.1:47047", got)
	}
	for _, bad := range []string{"0.0.0.0", "::", "[::]"} {
		if strings.Contains(got, bad) {
			t.Errorf("listenAddr produced %q, which contains the routable host %q", got, bad)
		}
	}
}

// ------------------------------------------------------------------- export

func exportCSV(t *testing.T, h http.Handler, query string) ([][]string, *httptest.ResponseRecorder) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/export?"+query, nil))
	if rec.Code != http.StatusOK {
		return nil, rec
	}
	rows, err := csv.NewReader(strings.NewReader(rec.Body.String())).ReadAll()
	if err != nil {
		t.Fatalf("response is not valid CSV: %v\n%s", err, rec.Body.String())
	}
	return rows, rec
}

func TestExportScopesCSV(t *testing.T) {
	h, _, _ := newTestServer(t)
	cases := []struct {
		scope    string
		wantRows int
	}{
		{"sessions", 3},  // header plus two sessions
		{"laps", 5},      // header plus four laps
		{"positions", 3}, // header plus two events
	}
	for _, c := range cases {
		t.Run(c.scope, func(t *testing.T) {
			rows, rec := exportCSV(t, h, "scope="+c.scope+"&format=csv")
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d\n%s", rec.Code, rec.Body.String())
			}
			if len(rows) != c.wantRows {
				t.Errorf("rows = %d, want %d", len(rows), c.wantRows)
			}
			if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/csv") {
				t.Errorf("Content-Type = %q", ct)
			}
			cd := rec.Header().Get("Content-Disposition")
			if !strings.Contains(cd, "attachment") || !strings.Contains(cd, c.scope) {
				t.Errorf("Content-Disposition = %q", cd)
			}
		})
	}
}

// A lap or position export is meaningless without its session context, so the
// join must be present in the header.
func TestExportJoinsSessionContext(t *testing.T) {
	h, _, _ := newTestServer(t)
	for scope, cols := range map[string][]string{
		"laps":      {"track_name", "car_name", "session_type", "lap_time_s"},
		"positions": {"track_name", "cause", "from_position", "to_position", "opponent_name"},
	} {
		rows, rec := exportCSV(t, h, "scope="+scope+"&format=csv")
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d", scope, rec.Code)
		}
		header := strings.Join(rows[0], ",")
		for _, c := range cols {
			if !strings.Contains(header, c) {
				t.Errorf("%s header is missing %q; header = %v", scope, c, rows[0])
			}
		}
	}
}

// An export must return exactly what the equivalent list endpoint would, since
// both go through parseFilter and the same predicate.
func TestExportHonoursFilter(t *testing.T) {
	h, _, _ := newTestServer(t)

	rows, _ := exportCSV(t, h, "scope=sessions&format=csv&session_type=Race")
	if len(rows) != 2 {
		t.Errorf("rows = %d, want a header plus the one race", len(rows))
	}
	rows, _ = exportCSV(t, h, "scope=sessions&format=csv&track_id=9999")
	if len(rows) != 1 {
		t.Errorf("rows = %d, want only the header for a non-matching track", len(rows))
	}
}

// An empty export must still emit the header, so the file is self-describing
// rather than a zero-byte download.
func TestExportEmptyStillHasHeader(t *testing.T) {
	h, _, _ := newTestServer(t)
	rows, rec := exportCSV(t, h, "scope=sessions&format=csv&track_id=9999")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if len(rows) != 1 || len(rows[0]) == 0 {
		t.Errorf("empty export = %v, want a single header row", rows)
	}
}

func TestExportJSON(t *testing.T) {
	h, _, _ := newTestServer(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/export?scope=sessions&format=json", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d\n%s", rec.Code, rec.Body.String())
	}
	var out []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("not a JSON array: %v\n%s", err, rec.Body.String())
	}
	if len(out) != 2 {
		t.Fatalf("rows = %d, want 2", len(out))
	}
	if out[0]["session_key"] != "900001/0" {
		t.Errorf("session_key = %v", out[0]["session_key"])
	}
}

// An empty JSON export must be [] rather than null.
func TestExportEmptyJSONIsArray(t *testing.T) {
	h, _, _ := newTestServer(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/api/export?scope=sessions&format=json&track_id=9999", nil))
	if got := strings.TrimSpace(rec.Body.String()); got != "[]" {
		t.Errorf("body = %q, want []", got)
	}
}

// Scope selects a fixed statement from an allowlist and is never interpolated.
func TestExportRejectsBadScopeAndFormat(t *testing.T) {
	h, _, _ := newTestServer(t)
	for _, q := range []string{
		"scope=nonsense&format=csv",
		"format=csv",
		// Percent-encoded, because a raw space is not a legal request target.
		"scope=sessions%3B+DROP+TABLE+sessions&format=csv",
		"scope=sessions&format=parquet",
		"scope=sessions&format=csv&track_id=abc",
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/export?"+q, nil))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("export?%s: status = %d, want 400", q, rec.Code)
		}
	}
}

// After the injection attempts the tables must still be intact.
func TestExportInjectionLeavesTablesIntact(t *testing.T) {
	h, store0, _ := newTestServer(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/api/export?scope=sessions%3B+DROP+TABLE+sessions&format=csv", nil))
	if _, total, err := store0.ListSessions(storeFilterAll()); err != nil || total != 2 {
		t.Errorf("after an injection attempt: total=%d err=%v, want 2 and nil", total, err)
	}
}

func TestExportDefaultsToCSV(t *testing.T) {
	h, _, _ := newTestServer(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/export?scope=sessions", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/csv") {
		t.Errorf("Content-Type = %q, want CSV by default", ct)
	}
}

func storeFilterAll() store.Filter { return store.Filter{} }

func TestBreakdownEndpoint(t *testing.T) {
	h, _, _ := newTestServer(t)

	var rows []store.BreakdownRow
	rec := get(t, h, "/api/breakdown?by=car", &rows)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d\n%s", rec.Code, rec.Body.String())
	}
	if len(rows) == 0 {
		t.Fatal("no breakdown rows")
	}
	for _, r := range rows {
		if r.Group == "" || r.Stack == "" {
			t.Errorf("row has an empty dimension: %+v", r)
		}
		if !strings.Contains(r.Stack, "/") {
			t.Errorf("stack %q is not a type/context pair", r.Stack)
		}
	}

	// The stacked bars must add up to what the KPI row shows, or the dashboard
	// contradicts itself.
	var totals store.Totals
	get(t, h, "/api/totals", &totals)
	var sum float64
	for _, r := range rows {
		sum += r.DrivingHours
	}
	if diff := sum - totals.DrivingHours; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("breakdown sums to %v driving hours but totals reports %v", sum, totals.DrivingHours)
	}
}

// The dimension defaults rather than erroring, since the stacked bar is the primary
// consumer and "by car" is its natural default.
func TestBreakdownDefaultsToCar(t *testing.T) {
	h, _, _ := newTestServer(t)
	var withDefault, explicit []store.BreakdownRow
	get(t, h, "/api/breakdown", &withDefault)
	get(t, h, "/api/breakdown?by=car", &explicit)
	if len(withDefault) != len(explicit) || len(withDefault) == 0 {
		t.Errorf("default gave %d rows, by=car gave %d", len(withDefault), len(explicit))
	}
}

// An unrecognised dimension is a client mistake, so 400 not 500, and the allowlist
// means it can never reach SQL.
func TestBreakdownRejectsUnknownDimension(t *testing.T) {
	h, store0, _ := newTestServer(t)
	for _, q := range []string{
		"by=nonsense",
		"by=car%3B+DROP+TABLE+sessions",
		"by=1%3D1",
	} {
		if rec := get(t, h, "/api/breakdown?"+q, nil); rec.Code != http.StatusBadRequest {
			t.Errorf("breakdown?%s: status = %d, want 400", q, rec.Code)
		}
	}
	if _, total, err := store0.ListSessions(storeFilterAll()); err != nil || total != 2 {
		t.Errorf("after injection attempts: total=%d err=%v, want 2 and nil", total, err)
	}
}

// The breakdown must honour the shared filter like every other endpoint.
func TestBreakdownHonoursFilter(t *testing.T) {
	h, _, _ := newTestServer(t)
	var rows []store.BreakdownRow
	get(t, h, "/api/breakdown?by=car&session_type=Race", &rows)
	for _, r := range rows {
		if !strings.HasPrefix(r.Stack, "Race/") {
			t.Errorf("stack %q leaked past a session_type=Race filter", r.Stack)
		}
	}
	if len(rows) == 0 {
		t.Error("filtering to races produced no rows")
	}
}

// The server owns the dimension allowlist, so it advertises it.
func TestFacetsAdvertisesBreakdownDimensions(t *testing.T) {
	h, _, _ := newTestServer(t)
	var f facetsResponse
	get(t, h, "/api/facets", &f)
	if len(f.BreakdownBy) == 0 {
		t.Error("facets does not advertise the breakdown dimensions")
	}
	found := false
	for _, d := range f.BreakdownBy {
		if d == "car" {
			found = true
		}
	}
	if !found {
		t.Errorf("breakdownBy = %v, want it to include car", f.BreakdownBy)
	}
}

func TestEntitiesEndpoint(t *testing.T) {
	h, _, _ := newTestServer(t)
	rec := get(t, h, "/api/entities?by=car", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var rows []store.EntityRow
	if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
		t.Fatalf("decode: %v", err)
	}
}

func TestEntitiesRejectsUnknownDimension(t *testing.T) {
	h, _, _ := newTestServer(t)
	rec := get(t, h, "/api/entities?by=driver", nil)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for an unknown dimension", rec.Code)
	}
}

func TestEntityEndpointRequiresID(t *testing.T) {
	h, _, _ := newTestServer(t)
	rec := get(t, h, "/api/entity?by=car", nil)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 when id is absent", rec.Code)
	}
}

func TestEntityEndpointUnknownIDIs404(t *testing.T) {
	h, _, _ := newTestServer(t)
	rec := get(t, h, "/api/entity?by=car&id=999999", nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404, body = %s", rec.Code, rec.Body.String())
	}
}

func TestPaceEndpoint(t *testing.T) {
	h, _, _ := newTestServer(t)
	rec := get(t, h, "/api/pace?by=car&id=173", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var rows []store.PaceRow
	if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
		t.Fatalf("decode: %v", err)
	}
}

func TestProgressionEndpointRequiresBothIDs(t *testing.T) {
	h, _, _ := newTestServer(t)
	rec := get(t, h, "/api/progression?by=car&id=173", nil)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 without the other id — a line mixing tracks "+
			"would be meaningless", rec.Code)
	}
}

func TestQualiPaceEndpoint(t *testing.T) {
	h, _, _ := newTestServer(t)
	rec := get(t, h, "/api/quali-pace?by=car&id=173&range=all", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var got store.QualiPace
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
}

func TestRivalsEndpoint(t *testing.T) {
	h, _, _ := newTestServer(t)
	rec := get(t, h, "/api/rivals?range=all", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var rows []store.RivalRow
	if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
		t.Fatalf("decode: %v", err)
	}
}

// The four id-scoped endpoints require by explicitly: car ids and track ids are
// independent iRacing integers with no guaranteed disjoint ranges, so defaulting
// by would let an id meant for one dimension silently resolve against the other.

func TestEntityEndpointRequiresDimension(t *testing.T) {
	h, _, _ := newTestServer(t)
	rec := get(t, h, "/api/entity?id=173", nil)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 when by is absent", rec.Code)
	}
}

func TestPaceEndpointRequiresDimension(t *testing.T) {
	h, _, _ := newTestServer(t)
	rec := get(t, h, "/api/pace?id=173", nil)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 when by is absent", rec.Code)
	}
}

func TestProgressionEndpointRequiresDimension(t *testing.T) {
	h, _, _ := newTestServer(t)
	rec := get(t, h, "/api/progression?id=173&other=18", nil)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 when by is absent", rec.Code)
	}
}

func TestQualiPaceEndpointRequiresDimension(t *testing.T) {
	h, _, _ := newTestServer(t)
	rec := get(t, h, "/api/quali-pace?id=173&range=all", nil)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 when by is absent", rec.Code)
	}
}

// /api/entities takes no id, so a defaulted dimension is a sensible answer
// rather than a wrong one — this pins that asymmetry deliberately.
func TestEntitiesDefaultsDimensionWhenAbsent(t *testing.T) {
	h, _, _ := newTestServer(t)
	rec := get(t, h, "/api/entities", nil)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 when by is absent on /api/entities", rec.Code)
	}
}
