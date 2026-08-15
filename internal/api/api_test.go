package api

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/blezek/lapdog/internal/collector"
	"github.com/blezek/lapdog/internal/config"
	"github.com/blezek/lapdog/internal/irsdk"
	"github.com/blezek/lapdog/internal/store"
	"github.com/blezek/lapdog/internal/synth"
	"github.com/blezek/lapdog/internal/web/webtest"
)

// ------------------------------------------------------------- test doubles

// fixtureIntervalSeconds is deliberately not 1.
//
// One is both the collector's default and the value the endpoint used to
// substitute when it saw a non-positive interval, so a fixture set to 1 could not
// tell a forwarded interval from a fabricated one.
const fixtureIntervalSeconds = 2

type fakeStatus struct {
	s collector.Status

	// frame is a field rather than baked in, so one double can stand in both for a
	// session that is reporting frames and for one that has not handled any yet.
	// Nil and a present zero are different fixtures on purpose, the same way they
	// are different facts on the wire.
	frame *collector.LiveFrame
}

func (f fakeStatus) Status() collector.Status { return f.s }

func (f fakeStatus) Live() collector.Live {
	return collector.Live{Status: f.s, Frame: f.frame}
}

type pausableStatus struct {
	mu    sync.Mutex
	s     collector.Status
	calls []bool
}

func (p *pausableStatus) Status() collector.Status {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.s
}

func (p *pausableStatus) Live() collector.Live { return collector.Live{Status: p.Status()} }

func (p *pausableStatus) SetPaused(paused bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.s.Paused = paused
	p.calls = append(p.calls, paused)
}

func (p *pausableStatus) pauseCalls() []bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]bool(nil), p.calls...)
}

// stationaryCarFrame is a frame with speed present and zero, gear absent
// entirely, so tests can tell a real zero from a missing value.
func stationaryCarFrame() *collector.LiveFrame {
	zero := 0.0
	return &collector.LiveFrame{
		At: time.Now(), InCar: true, Driving: false,
		Reason: collector.ReasonPitBox,
		Speed:  &zero,
	}
}

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
		irating               int
		sr                    float64
	}{
		{"900001/0", "Practice", "OfficialPractice", "2026-07-01T10:00:00Z", 3600, 2400, 2000, 20, 2, 102.5, false, 2431, 3.55},
		{"900002/2", "Race", "OfficialRace", "2026-07-08T18:45:00Z", 3000, 2900, 2800, 25, 6, 102.0, true, 2498, 3.71},
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
			DriverUserID: intp(271828), DriverIRating: intp(r.irating),
			DriverSafetyRating: f64p(r.sr), DriverLicString: strp("A 3.55"),
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
		Connected: true, IntervalSeconds: fixtureIntervalSeconds,
		SessionLabel: "Public Practice", TrackName: "Watkins Glen International",
	}, frame: stationaryCarFrame()}
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
			"&track_id=341,18&car_id=173&car_id=45&league_id=4242"+
			"&hour_from=18&hour_to=23&weekday=1,3,5"+
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
	if len(f.TrackIDs) != 2 || f.TrackIDs[0] != 341 || f.TrackIDs[1] != 18 ||
		len(f.CarIDs) != 2 || f.CarIDs[0] != 173 || f.CarIDs[1] != 45 || f.LeagueID == nil {
		t.Errorf("ids = %v %v %v", f.TrackIDs, f.CarIDs, f.LeagueID)
	}
	if f.HourFrom == nil || *f.HourFrom != 18 || f.HourTo == nil || *f.HourTo != 23 {
		t.Errorf("hours = %v / %v", f.HourFrom, f.HourTo)
	}
	if len(f.Weekdays) != 3 || f.Weekdays[0] != 1 || f.Weekdays[2] != 5 {
		t.Errorf("weekdays = %v, want [1 3 5]", f.Weekdays)
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
		"track_id=notanumber", "track_id=1,x", "car_id=-1", "limit=abc", "offset=-5", "limit=-1",
		"from=yesterday", "to=2026-13-45", "exclude_ai=maybe",
		"hour_from=24", "hour_to=-1", "hour_from=noon", "weekday=7", "weekday=1,x",
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

// The endpoint forwards the provider's own poll interval, so the interface can
// pace itself and judge staleness rather than being told the answer.
//
// Equality, not "greater than zero": the endpoint once floored the interval at 1,
// which no fixture set to 1 could have detected. The frame itself is covered by
// TestLiveEndpointWithNoFrame and TestLiveEndpointDistinguishesZeroFromAbsent.
func TestLiveEndpoint(t *testing.T) {
	h, _, _ := newTestServer(t)
	var got liveResponse
	rec := get(t, h, "/api/live", &got)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if got.IntervalSeconds != fixtureIntervalSeconds {
		t.Errorf("IntervalSeconds = %v, want the provider's %v",
			got.IntervalSeconds, fixtureIntervalSeconds)
	}
}

// With no frame handled, the response says so rather than inventing zeroes.
//
// This uses its own StatusProvider rather than newTestServer's, because that
// fixture's fakeStatus reports a stationary-car frame — the two fixtures
// exist precisely so "no frame yet" and "a frame with a real zero in it" stay
// distinguishable in the tests, the same way they must on the wire.
func TestLiveEndpointWithNoFrame(t *testing.T) {
	_, st, cfg := newTestServer(t)
	sp := fakeStatus{s: collector.Status{IntervalSeconds: 1}}
	srv := New(st, sp, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	h, err := srv.Handler()
	if err != nil {
		t.Fatal(err)
	}
	var got liveResponse
	get(t, h, "/api/live", &got)
	if got.Frame != nil {
		t.Errorf("a frame was reported when none had been handled: %+v", got.Frame)
	}
}

// A zero speed is a real reading and must not be confused with an absent one.
func TestLiveEndpointDistinguishesZeroFromAbsent(t *testing.T) {
	h, _, _ := newTestServer(t)
	// fakeStatus is configured with a stationary car: speed present and zero,
	// gear absent entirely.
	var got liveResponse
	get(t, h, "/api/live", &got)
	if got.Frame == nil {
		t.Fatal("the fixture provides no frame; fakeStatus.Live should have returned one")
	}
	if got.Frame.Speed == nil || *got.Frame.Speed != 0 {
		t.Errorf("Speed = %v, want a present zero", got.Frame.Speed)
	}
	if got.Frame.Gear != nil {
		t.Errorf("Gear = %v, want absent", got.Frame.Gear)
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
	if rec := get(t, h, "/api/laps?clean_laps=maybe", nil); rec.Code != http.StatusBadRequest {
		t.Errorf("bad lap filter: status = %d, want 400", rec.Code)
	}
}

func TestLapsAndFacetsEndpoints(t *testing.T) {
	h, st, _ := newTestServer(t)

	sessions, _, err := st.ListSessions(store.Filter{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("seed session lookup returned %d sessions, want 1", len(sessions))
	}
	timed := 103.0
	zero := 0.0
	for _, lap := range []store.Lap{
		{SessionID: sessions[0].ID, LapNumber: 97, LapTimeS: &timed, IsPitLap: true},
		{SessionID: sessions[0].ID, LapNumber: 98, LapTimeS: &timed, IncidentsOnLap: 1},
		{SessionID: sessions[0].ID, LapNumber: 99, LapTimeS: &zero},
	} {
		if _, err := st.InsertLap(&lap); err != nil {
			t.Fatal(err)
		}
	}

	var laps struct {
		Items []store.LapRow `json:"items"`
		Total int            `json:"total"`
	}
	get(t, h, "/api/laps", &laps)
	if laps.Total != 7 {
		t.Errorf("lap total = %d, want 7", laps.Total)
	}
	if laps.Items[0].TrackName == "" {
		t.Error("lap rows must carry the joined session context")
	}

	var clean struct {
		Items []store.LapRow `json:"items"`
		Total int            `json:"total"`
	}
	get(t, h, "/api/laps?clean_laps=true&limit=1", &clean)
	if clean.Total != 4 || len(clean.Items) != 1 {
		t.Errorf("clean laps total=%d len=%d, want total 4 and paged len 1", clean.Total, len(clean.Items))
	}
	for _, r := range clean.Items {
		if r.IsPitLap || r.IncidentsOnLap != 0 || r.LapTimeS == nil || *r.LapTimeS <= 0 {
			t.Errorf("dirty lap returned by clean endpoint: %+v", r)
		}
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

func TestCaptureReindexRefusesConnectedSimulator(t *testing.T) {
	h, _, _ := newTestServer(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/captures/reindex", nil))
	if rec.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409 while connected; body=%s", rec.Code, rec.Body.String())
	}
}

func TestCaptureReindexReportsNoSavedCaptures(t *testing.T) {
	_, st, cfg := newTestServer(t)
	srv := New(st, fakeStatus{s: collector.Status{Connected: false}}, cfg,
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	h, err := srv.Handler()
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/captures/reindex", nil))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 with no captures; body=%s", rec.Code, rec.Body.String())
	}
}

func TestCaptureReindexRunsInBackgroundAndReportsResult(t *testing.T) {
	_, st, cfg := newTestServer(t)
	if _, err := st.UpsertSession(&store.Session{
		SessionKey: "database-only/0", SessionNum: 0,
		SessionType: "Practice", EventContext: "OfficialPractice",
		StartedAt: "2026-08-01T00:00:00Z", ClassifySourceJSON: "{}",
	}); err != nil {
		t.Fatal(err)
	}
	capturesDir := config.CapturesDir(filepath.Dir(st.Path()))
	if err := os.MkdirAll(capturesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	name := "20260812T014837Z-public-practice.lpd"
	fixtureDir := t.TempDir()
	if _, err := synth.WriteFixtures(fixtureDir); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(fixtureDir, "public-practice.lpd"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(capturesDir, name), body, 0o644); err != nil {
		t.Fatal(err)
	}

	provider := &pausableStatus{s: collector.Status{Connected: false}}
	srv := New(st, provider, cfg,
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	h, err := srv.Handler()
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/captures/reindex", nil))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
	if calls := provider.pauseCalls(); len(calls) == 0 || !calls[0] {
		t.Fatalf("first SetPaused call after POST = %v, want true", calls)
	}

	waitForReindex := func() captureReindexStatus {
		t.Helper()
		var status captureReindexStatus
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			get(t, h, "/api/captures/reindex", &status)
			if status.State != "running" {
				return status
			}
			time.Sleep(10 * time.Millisecond)
		}
		return status
	}
	status := waitForReindex()
	if status.State != "complete" || status.Total != 1 || status.Replayed != 1 || status.Segments != 1 || status.Failed != 0 {
		t.Fatalf("status = %+v", status)
	}
	if calls := provider.pauseCalls(); len(calls) != 2 || !calls[0] || calls[1] {
		t.Errorf("SetPaused calls = %v, want [true false]", calls)
	}
	rows, total, err := st.ListSessions(store.Filter{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 {
		t.Fatalf("sessions after destructive rebuild = %d, want only 1 capture-derived row", total)
	}
	found := false
	for _, row := range rows {
		if row.CaptureFile != nil && *row.CaptureFile == name {
			found = true
		}
	}
	if !found {
		t.Errorf("no indexed session retained capture filename %q", name)
	}
	if _, err := st.SessionByKey("database-only/0"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("database-only session survived destructive rebuild: %v", err)
	}
	firstDriving := rows[0].DrivingSeconds

	second := httptest.NewRecorder()
	h.ServeHTTP(second, httptest.NewRequest(http.MethodPost, "/api/captures/reindex", nil))
	if second.Code != http.StatusAccepted {
		t.Fatalf("second rebuild status = %d, want 202; body=%s", second.Code, second.Body.String())
	}
	status = waitForReindex()
	if status.State != "complete" {
		t.Fatalf("second rebuild status = %+v", status)
	}
	rows, total, err = st.ListSessions(store.Filter{Limit: 100})
	if err != nil || total != 1 || len(rows) != 1 {
		t.Fatalf("sessions after second rebuild = %d/%d, %v; want one", len(rows), total, err)
	}
	if rows[0].DrivingSeconds != firstDriving {
		t.Errorf("driving after second rebuild = %.3f, want unchanged %.3f", rows[0].DrivingSeconds, firstDriving)
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

func TestCombosEndpoint(t *testing.T) {
	h, _, _ := newTestServer(t)
	rec := get(t, h, "/api/combos?range=all", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var cells []store.ComboCell
	if err := json.Unmarshal(rec.Body.Bytes(), &cells); err != nil {
		t.Fatalf("decode: %v", err)
	}
}

// The endpoint reports the identity and the movement across the range, and honours
// the filter while doing it.
func TestRatingsEndpoint(t *testing.T) {
	h, _, _ := newTestServer(t)
	var got store.Ratings
	rec := get(t, h, "/api/ratings?range=all", &got)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if got.UserID == nil || *got.UserID != 271828 {
		t.Errorf("userId = %v, want 271828", got.UserID)
	}
	if got.IRating == nil || *got.IRating != 2498 {
		t.Errorf("iRating = %v, want the newest, 2498", got.IRating)
	}
	if got.IRatingDelta == nil || *got.IRatingDelta != 67 {
		t.Errorf("iRatingDelta = %v, want 67", got.IRatingDelta)
	}
	if len(got.Points) != 2 {
		t.Errorf("points = %d, want 2", len(got.Points))
	}
}

// A range that admits one session reports no delta, which is what proves the
// endpoint passes the filter through rather than aggregating all history.
func TestRatingsEndpointHonoursTheRange(t *testing.T) {
	h, _, _ := newTestServer(t)
	var got store.Ratings
	get(t, h, "/api/ratings?from=2026-07-05&to=2026-07-10", &got)
	if len(got.Points) != 1 {
		t.Fatalf("points = %d, want 1; the range was not applied", len(got.Points))
	}
	if got.IRatingDelta != nil {
		t.Errorf("iRatingDelta = %v within a one-session range, want absent", got.IRatingDelta)
	}
}

func TestCombosRejectsNonNumericLimit(t *testing.T) {
	h, _, _ := newTestServer(t)
	rec := get(t, h, "/api/combos?top=ten", nil)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
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

// The racecraft endpoint carries the §5.6 figures. The seeded race has one
// on-track pass and one place inherited when an opponent pitted, so asserting the
// pass count also pins that the store's cause filter reached the response: without
// it the inherited place would be counted as an overtake too.
func TestRacecraftEndpoint(t *testing.T) {
	h, _, _ := newTestServer(t)
	rec := get(t, h, "/api/racecraft?by=car&id=173&range=all", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var got store.Racecraft
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.PassesMade != 1 {
		t.Errorf("passesMade = %d, want 1 — the OpponentPit event is an inherited "+
			"place, not an overtake", got.PassesMade)
	}
	// The seeded race records a finish but no grid position, so the averages have
	// nothing to average and must be null rather than zero.
	if got.Races != 0 || got.AvgStartPosition != nil {
		t.Errorf("got %+v, want no races counted: the fixture records no grid position", got)
	}
}

func TestRacecraftEndpointRequiresDimension(t *testing.T) {
	h, _, _ := newTestServer(t)
	rec := get(t, h, "/api/racecraft?id=173", nil)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 when by is absent", rec.Code)
	}
}

func TestRacecraftEndpointRequiresID(t *testing.T) {
	h, _, _ := newTestServer(t)
	rec := get(t, h, "/api/racecraft?by=car", nil)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 when id is absent", rec.Code)
	}
}

// The status response names where the readings come from.
//
// This exists because a Windows build recorded nothing while connected, and the first
// question was what the reader had been pointed at — which the interface could not
// answer. The source is also the most misunderstood thing about this application: it
// is live shared memory, not the .ibt files iRacing writes to disk.
func TestStatusReportsTheTelemetrySource(t *testing.T) {
	h, _, _ := newTestServer(t)
	rec := get(t, h, "/api/status", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var got struct {
		DatabasePath string `json:"databasePath"`
		Telemetry    struct {
			Source      string `json:"source"`
			SourceKind  string `json:"sourceKind"`
			Available   bool   `json:"available"`
			Platform    string `json:"platform"`
			DataDir     string `json:"dataDir"`
			CapturesDir string `json:"capturesDir"`
			LogPath     string `json:"logPath"`
		} `json:"telemetry"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// The mapping name comes from the SDK constant rather than being retyped, so a
	// change there cannot leave the interface reporting a stale name.
	if got.Telemetry.Source != irsdk.MemMapFileName {
		t.Errorf("source = %q, want %q", got.Telemetry.Source, irsdk.MemMapFileName)
	}
	// The kind must say it is shared memory, because a reader who assumes it is the
	// .ibt file will conclude the wrong thing about why nothing recorded.
	if !strings.Contains(strings.ToLower(got.Telemetry.SourceKind), "shared memory") {
		t.Errorf("sourceKind = %q, want it to say shared memory", got.Telemetry.SourceKind)
	}
	if got.Telemetry.Platform != runtime.GOOS {
		t.Errorf("platform = %q, want %q", got.Telemetry.Platform, runtime.GOOS)
	}
	// Live telemetry exists only on Windows, and the interface must say so rather than
	// leaving a Mac user wondering why no session appears.
	if want := runtime.GOOS == "windows"; got.Telemetry.Available != want {
		t.Errorf("available = %v on %s, want %v", got.Telemetry.Available, runtime.GOOS, want)
	}

	// The sibling paths are derived from the database's directory, so they must agree
	// with it rather than being independently guessed.
	wantDir := filepath.Dir(got.DatabasePath)
	if got.Telemetry.DataDir != wantDir {
		t.Errorf("dataDir = %q, want %q, the database's directory", got.Telemetry.DataDir, wantDir)
	}
	for name, path := range map[string]string{
		"capturesDir": got.Telemetry.CapturesDir,
		"logPath":     got.Telemetry.LogPath,
	} {
		if !strings.HasPrefix(path, wantDir) {
			t.Errorf("%s = %q, which is not inside the data directory %q", name, path, wantDir)
		}
	}
}
