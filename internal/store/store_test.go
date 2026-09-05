package store

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

const testLocation = "America/Chicago"

// SQLite fixes the process timezone when its localtime support is first used.
// Establish a non-UTC test zone before any Store is opened so the calendar
// tests do not depend on the host running the suite.
func TestMain(m *testing.M) {
	loc, err := time.LoadLocation(testLocation)
	if err != nil {
		panic(err)
	}
	if err := os.Setenv("TZ", testLocation); err != nil {
		panic(err)
	}
	time.Local = loc
	os.Exit(m.Run())
}

func openTemp(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "lapdog.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func intp(v int) *int         { return &v }
func f64p(v float64) *float64 { return &v }
func strp(v string) *string   { return &v }

func TestOpenAppliesMigrationsAndCreatesTables(t *testing.T) {
	s := openTemp(t)
	v, err := s.SchemaVersion()
	if err != nil {
		t.Fatalf("SchemaVersion: %v", err)
	}
	if v != CurrentSchemaVersion {
		t.Errorf("SchemaVersion = %d, want %d", v, CurrentSchemaVersion)
	}
	for _, table := range []string{"schema_version", "sessions", "laps", "position_events"} {
		var name string
		if err := s.Reader().QueryRow(
			`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table,
		).Scan(&name); err != nil {
			t.Errorf("table %s missing: %v", table, err)
		}
	}
}

func TestWALIsEnabled(t *testing.T) {
	var mode string
	if err := openTemp(t).Reader().QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil {
		t.Fatalf("PRAGMA journal_mode: %v", err)
	}
	if mode != "wal" {
		t.Errorf("journal_mode = %q, want wal", mode)
	}
}

// ON DELETE CASCADE is decorative unless foreign keys are actually enforced.
func TestForeignKeysAreEnforced(t *testing.T) {
	s := openTemp(t)
	_, err := s.Writer().Exec(
		`INSERT INTO laps (uuid, session_id, lap_number, recorded_at)
		 VALUES ('u1', 9999, 1, '2026-01-01T00:00:00Z')`)
	if err == nil {
		t.Fatal("insert with a dangling session_id succeeded; foreign keys are not enforced")
	}
}

// A single writer connection is what makes SQLITE_BUSY impossible rather than
// merely unlikely.
func TestWriterIsSingleConnection(t *testing.T) {
	if got := openTemp(t).Writer().Stats().MaxOpenConnections; got != 1 {
		t.Errorf("writer MaxOpenConnections = %d, want 1", got)
	}
}

func TestReaderIsPooled(t *testing.T) {
	if got := openTemp(t).Reader().Stats().MaxOpenConnections; got <= 1 {
		t.Errorf("reader MaxOpenConnections = %d, want more than 1", got)
	}
}

// Readers must not block while a write is in flight; in WAL mode they read the
// last committed snapshot instead. If this hangs rather than fails, WAL is off.
func TestConcurrentWriteAndReadDoNotDeadlock(t *testing.T) {
	s := openTemp(t)
	var wg sync.WaitGroup

	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 40; i++ {
			rec := minimalSession("k" + strconv.Itoa(i))
			if _, err := s.UpsertSession(rec); err != nil {
				t.Errorf("write %d: %v", i, err)
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 40; i++ {
			var n int
			if err := s.Reader().QueryRow(`SELECT COUNT(*) FROM sessions`).Scan(&n); err != nil {
				t.Errorf("read %d: %v", i, err)
				return
			}
		}
	}()
	wg.Wait()

	var n int
	if err := s.Reader().QueryRow(`SELECT COUNT(*) FROM sessions`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 40 {
		t.Errorf("session count = %d, want 40", n)
	}
}

func TestOpenIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lapdog.db")
	s1, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s1.Close()
	s2, err := Open(path)
	if err != nil {
		t.Fatalf("second Open on an existing database: %v", err)
	}
	defer s2.Close()
	if v, _ := s2.SchemaVersion(); v != CurrentSchemaVersion {
		t.Errorf("SchemaVersion after reopen = %d", v)
	}
}

// A database written by a newer build must be refused, not silently used with
// columns reinterpreted.
func TestOpenRefusesNewerSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lapdog.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Writer().Exec(`UPDATE schema_version SET version = ?`, CurrentSchemaVersion+1); err != nil {
		t.Fatal(err)
	}
	s.Close()
	if _, err := Open(path); !errors.Is(err, ErrSchemaTooNew) {
		t.Fatalf("Open on a newer schema = %v, want ErrSchemaTooNew", err)
	}
}

// ---------------------------------------------------------------- sessions

func minimalSession(key string) *Session {
	return &Session{
		SessionKey:         key,
		SubsessionID:       55667788,
		SessionNum:         2,
		SessionType:        "Race",
		EventContext:       "OfficialRace",
		StartedAt:          "2026-08-04T19:30:00Z",
		ClassifySourceJSON: `{"WeekendInfo":{"LeagueID":0}}`,
		IncidentSource:     "yaml",
	}
}

func TestSessionKeyOnlineAndOffline(t *testing.T) {
	at := time.Date(2026, 8, 4, 19, 30, 0, 0, time.UTC)
	if got := SessionKey(55667788, 2, at); got != "55667788/2" {
		t.Errorf("SessionKey = %q", got)
	}
	// Offline sessions all report SubSessionID 0, so the start time must be in
	// the key or two offline tests would collide.
	a := SessionKey(0, 0, at)
	b := SessionKey(0, 0, at.Add(2*time.Hour))
	if a == b {
		t.Fatalf("two offline sessions produced the same key %q", a)
	}
	if a != "offline/0/2026-08-04T19:30:00Z" {
		t.Errorf("offline key = %q", a)
	}
	// Local time in must give UTC out, or the same session keyed from two
	// timezones would produce two rows.
	loc := time.FixedZone("UTC-5", -5*3600)
	if SessionKey(0, 0, time.Date(2026, 8, 4, 14, 30, 0, 0, loc)) != a {
		t.Error("SessionKey depends on the input timezone; it must normalise to UTC")
	}
}

func TestUpsertSessionPreservesIdentityAcrossFlushes(t *testing.T) {
	s := openTemp(t)
	rec := minimalSession("55667788/2")
	if _, err := s.UpsertSession(rec); err != nil {
		t.Fatal(err)
	}
	id, uid, created := rec.ID, rec.UUID, rec.CreatedAt
	if id == 0 || uid == "" || created == "" {
		t.Fatalf("identity not assigned: %+v", rec)
	}

	rec.DrivingSeconds = 840
	rec.LapsCompleted = 24
	rec.FinishPosition = intp(4)
	rec.EndedAt = strp("2026-08-04T20:18:00Z")
	if _, err := s.UpsertSession(rec); err != nil {
		t.Fatal(err)
	}
	if rec.ID != id || rec.UUID != uid || rec.CreatedAt != created {
		t.Errorf("identity churned on update: id %d->%d uuid %q->%q created %q->%q",
			id, rec.ID, uid, rec.UUID, created, rec.CreatedAt)
	}

	got, err := s.SessionByKey("55667788/2")
	if err != nil {
		t.Fatal(err)
	}
	if got.DrivingSeconds != 840 || got.LapsCompleted != 24 {
		t.Errorf("counters not updated: %+v", got)
	}
	if got.FinishPosition == nil || *got.FinishPosition != 4 {
		t.Errorf("FinishPosition = %v", got.FinishPosition)
	}
	var n int
	s.Reader().QueryRow(`SELECT COUNT(*) FROM sessions`).Scan(&n)
	if n != 1 {
		t.Errorf("session count = %d, want 1 — the upsert duplicated", n)
	}
}

func TestUpsertSessionPreservesEarliestStartAcrossReconnects(t *testing.T) {
	s := openTemp(t)
	rec := minimalSession("55667788/2")
	rec.StartedAt = "2026-08-12T02:00:00Z"
	if _, err := s.UpsertSession(rec); err != nil {
		t.Fatal(err)
	}

	rec.StartedAt = "2026-08-12T01:00:00Z"
	if _, err := s.UpsertSession(rec); err != nil {
		t.Fatal(err)
	}
	rec.StartedAt = "2026-08-12T03:00:00Z"
	if _, err := s.UpsertSession(rec); err != nil {
		t.Fatal(err)
	}

	got, err := s.SessionByKey(rec.SessionKey)
	if err != nil {
		t.Fatal(err)
	}
	if got.StartedAt != "2026-08-12T01:00:00Z" {
		t.Errorf("StartedAt = %q, want earliest reconnect time", got.StartedAt)
	}
}

// Nullable columns must come back nil, not zero, when never set.
func TestUpsertSessionNullsStayNull(t *testing.T) {
	s := openTemp(t)
	rec := minimalSession("55667788/2")
	if _, err := s.UpsertSession(rec); err != nil {
		t.Fatal(err)
	}
	got, err := s.SessionByID(rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.FinishPosition != nil || got.EndedAt != nil || got.BestLapTimeS != nil {
		t.Errorf("expected nils, got finish=%v ended=%v best=%v",
			got.FinishPosition, got.EndedAt, got.BestLapTimeS)
	}
	if got.UploadedAt != nil {
		t.Error("UploadedAt is set; nothing writes it in this version")
	}
}

func TestUpsertSessionOfflineSessionsDoNotCollide(t *testing.T) {
	s := openTemp(t)
	for _, at := range []time.Time{
		time.Date(2026, 8, 4, 19, 0, 0, 0, time.UTC),
		time.Date(2026, 8, 4, 21, 0, 0, 0, time.UTC),
	} {
		rec := minimalSession(SessionKey(0, 0, at))
		rec.SubsessionID, rec.SessionNum = 0, 0
		rec.SessionType, rec.EventContext = "OfflineTest", "Offline"
		rec.StartedAt = FormatTime(at)
		if _, err := s.UpsertSession(rec); err != nil {
			t.Fatalf("UpsertSession(%v): %v", at, err)
		}
	}
	var n int
	s.Reader().QueryRow(`SELECT COUNT(*) FROM sessions`).Scan(&n)
	if n != 2 {
		t.Errorf("session count = %d, want 2 — offline sessions collided", n)
	}
}

func TestNotFoundErrors(t *testing.T) {
	s := openTemp(t)
	if _, err := s.SessionByKey("nope/0"); !errors.Is(err, ErrNotFound) {
		t.Errorf("SessionByKey = %v, want ErrNotFound", err)
	}
	if _, err := s.SessionByID(9999); !errors.Is(err, ErrNotFound) {
		t.Errorf("SessionByID = %v, want ErrNotFound", err)
	}
	if err := s.DeleteSession(9999); !errors.Is(err, ErrNotFound) {
		t.Errorf("DeleteSession = %v, want ErrNotFound", err)
	}
}

func TestDeleteSessionCascades(t *testing.T) {
	s := openTemp(t)
	rec := minimalSession("55667788/2")
	id, _ := s.UpsertSession(rec)
	if _, err := s.InsertLap(&Lap{SessionID: id, LapNumber: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.InsertPositionEvent(&PositionEvent{
		SessionID: id, LapNumber: 1, FromPosition: 5, ToPosition: 4, Cause: CauseOnTrack,
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteSession(id); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"laps", "position_events"} {
		var n int
		s.Reader().QueryRow(`SELECT COUNT(*) FROM `+table+` WHERE session_id = ?`, id).Scan(&n)
		if n != 0 {
			t.Errorf("%s count after delete = %d, want 0 — cascade did not fire", table, n)
		}
	}
}

func TestClearHistoryRemovesAllRecordedDataButKeepsSchema(t *testing.T) {
	s := openTemp(t)
	id := seedSession(t, s)
	if _, err := s.InsertLap(&Lap{SessionID: id, LapNumber: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.InsertPositionEvent(&PositionEvent{
		SessionID: id, LapNumber: 1, FromPosition: 5, ToPosition: 4, Cause: CauseOnTrack,
	}); err != nil {
		t.Fatal(err)
	}

	if err := s.ClearHistory(); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"sessions", "laps", "position_events"} {
		var n int
		if err := s.Reader().QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Errorf("%s count after ClearHistory = %d, want 0", table, n)
		}
	}
	if version, err := s.SchemaVersion(); err != nil || version != CurrentSchemaVersion {
		t.Errorf("schema after ClearHistory = %d, %v; want version %d", version, err, CurrentSchemaVersion)
	}
}

// The two column lists must stay in the same order, since scanSession reads
// positionally and a mismatch would be a silent wrong-field bug.
func TestColumnListsAgree(t *testing.T) {
	split := func(s string) []string {
		var out []string
		for _, part := range strings.Split(s, ",") {
			out = append(out, strings.TrimSpace(part))
		}
		return out
	}
	plain, aliased := split(sessionColumns), split(sessionColumnsAliased)
	if len(plain) != len(aliased) {
		t.Fatalf("column counts differ: %d plain, %d aliased", len(plain), len(aliased))
	}
	for i := range plain {
		if want := "s." + plain[i]; aliased[i] != want {
			t.Errorf("column %d: aliased is %q, want %q", i, aliased[i], want)
		}
	}
}

// ------------------------------------------------------------- laps, events

func seedSession(t *testing.T, s *Store) int64 {
	t.Helper()
	id, err := s.UpsertSession(minimalSession("55667788/2"))
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestInsertLapAndRead(t *testing.T) {
	s := openTemp(t)
	sid := seedSession(t, s)
	rec := &Lap{
		SessionID: sid, LapNumber: 11,
		LapTimeS: f64p(102.312), DeltaToBestS: f64p(0.686),
		FuelUsedL: f64p(2.41), IncidentsOnLap: 1, IsPitLap: true,
		Position: intp(5), ClassPosition: intp(3),
	}
	if _, err := s.InsertLap(rec); err != nil {
		t.Fatal(err)
	}
	laps, err := s.LapsForSession(sid)
	if err != nil {
		t.Fatal(err)
	}
	if len(laps) != 1 {
		t.Fatalf("len = %d, want 1", len(laps))
	}
	g := laps[0]
	if g.LapNumber != 11 || !g.IsPitLap || g.IncidentsOnLap != 1 {
		t.Errorf("lap = %+v", g)
	}
	if g.LapTimeS == nil || *g.LapTimeS != 102.312 {
		t.Errorf("LapTimeS = %v", g.LapTimeS)
	}
}

// A collector restart can re-observe a lap it already wrote; that must not error
// or it would crash the poll loop. The first write wins.
func TestInsertLapIsIdempotent(t *testing.T) {
	s := openTemp(t)
	sid := seedSession(t, s)
	if _, err := s.InsertLap(&Lap{SessionID: sid, LapNumber: 3, LapTimeS: f64p(100)}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.InsertLap(&Lap{SessionID: sid, LapNumber: 3, LapTimeS: f64p(999)}); err != nil {
		t.Fatalf("re-inserting the same lap errored: %v", err)
	}
	laps, _ := s.LapsForSession(sid)
	if len(laps) != 1 {
		t.Fatalf("len = %d, want 1", len(laps))
	}
	if *laps[0].LapTimeS != 100 {
		t.Errorf("LapTimeS = %v, want the first write preserved", *laps[0].LapTimeS)
	}
	if n, err := s.LapCount(sid); err != nil || n != 1 {
		t.Errorf("LapCount = %d, %v; want 1, nil", n, err)
	}
}

// Repeated swaps between the same positions are distinct events; collapsing them
// would undercount a battle.
func TestInsertPositionEventAllowsRepeats(t *testing.T) {
	s := openTemp(t)
	sid := seedSession(t, s)
	for i := 0; i < 3; i++ {
		if _, err := s.InsertPositionEvent(&PositionEvent{
			SessionID: sid, LapNumber: 5, SessionTimeS: float64(300 + i),
			FromPosition: 4, ToPosition: 5, Cause: CauseOnTrack,
		}); err != nil {
			t.Fatal(err)
		}
	}
	evs, _ := s.PositionEventsForSession(sid)
	if len(evs) != 3 {
		t.Errorf("len = %d, want 3", len(evs))
	}
}

func TestPositionEventStoresOpponentName(t *testing.T) {
	s := openTemp(t)
	sid := seedSession(t, s)
	if _, err := s.InsertPositionEvent(&PositionEvent{
		SessionID: sid, LapNumber: 7, SessionTimeS: 412,
		FromPosition: 6, ToPosition: 5,
		OpponentCarIdx: intp(14), OpponentName: strp("Other Driver"),
		Cause: CauseOnTrack,
	}); err != nil {
		t.Fatal(err)
	}
	evs, _ := s.PositionEventsForSession(sid)
	if evs[0].OpponentName == nil || *evs[0].OpponentName != "Other Driver" {
		t.Errorf("OpponentName = %v — names are stored, never anonymised", evs[0].OpponentName)
	}
}

// ---------------------------------------------------------------- queries

// seed builds a small realistic dataset spanning several event contexts.
func seed(t *testing.T, s *Store) {
	t.Helper()
	rows := []struct {
		key, st, ctx, started string
		conn, car, drive      float64
		laps, inc             int
		trackID               int
		trackName             string
		carID                 int
		carName               string
		leagueID              int
		best                  float64
	}{
		{"1001/0", "Practice", "OfficialPractice", "2026-07-01T10:00:00Z", 3600, 2400, 2000, 20, 2, 18, "Watkins Glen", 173, "Porsche 911 GT3 R", 0, 102.5},
		{"1002/0", "Practice", "OfficialRace", "2026-07-08T18:00:00Z", 1800, 1500, 1400, 15, 1, 18, "Watkins Glen", 173, "Porsche 911 GT3 R", 0, 102.1},
		{"1002/1", "Qualify", "OfficialRace", "2026-07-08T18:30:00Z", 600, 500, 450, 3, 0, 18, "Watkins Glen", 173, "Porsche 911 GT3 R", 0, 101.8},
		{"1002/2", "Race", "OfficialRace", "2026-07-08T18:45:00Z", 3000, 2900, 2800, 25, 6, 18, "Watkins Glen", 173, "Porsche 911 GT3 R", 0, 102.0},
		{"2001/2", "Race", "League", "2026-07-15T19:00:00Z", 3600, 3500, 3400, 30, 4, 341, "Spa", 173, "Porsche 911 GT3 R", 4242, 141.9},
		{"3001/0", "Race", "AI", "2026-07-20T12:00:00Z", 1200, 1100, 1000, 10, 0, 341, "Spa", 45, "Mazda MX-5", 0, 150.2},
	}
	for _, r := range rows {
		rec := &Session{
			SessionKey: r.key, SubsessionID: 1, SessionNum: 0,
			SessionType: r.st, EventContext: r.ctx, LeagueID: r.leagueID,
			StartedAt:        r.started,
			ConnectedSeconds: r.conn, InCarSeconds: r.car, DrivingSeconds: r.drive,
			LapsCompleted: r.laps, Incidents: r.inc, BestLapTimeS: f64p(r.best),
			TrackID: intp(r.trackID), TrackName: strp(r.trackName),
			CarID: intp(r.carID), CarName: strp(r.carName),
			ClassifySourceJSON: "{}", IncidentSource: "yaml",
		}
		id, err := s.UpsertSession(rec)
		if err != nil {
			t.Fatal(err)
		}
		for n := 1; n <= 2; n++ {
			if _, err := s.InsertLap(&Lap{
				SessionID: id, LapNumber: n, LapTimeS: f64p(r.best + float64(n)*0.5),
			}); err != nil {
				t.Fatal(err)
			}
		}
		if r.st == "Race" {
			for _, ev := range []PositionEvent{
				{LapNumber: 2, SessionTimeS: 100, FromPosition: 6, ToPosition: 5, Cause: CauseOnTrack},
				{LapNumber: 4, SessionTimeS: 200, FromPosition: 5, ToPosition: 6, Cause: CauseOnTrack},
				{LapNumber: 6, SessionTimeS: 300, FromPosition: 6, ToPosition: 5, Cause: CauseOpponentPit},
			} {
				ev.SessionID = id
				if _, err := s.InsertPositionEvent(&ev); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
}

func TestListSessionsFilters(t *testing.T) {
	s := openTemp(t)
	seed(t, s)
	cases := []struct {
		name string
		f    Filter
		want int
	}{
		{"no filter", Filter{}, 6},
		{"by type", Filter{SessionType: []string{"Race"}}, 3},
		{"by two types", Filter{SessionType: []string{"Practice", "Qualify"}}, 3},
		{"by context", Filter{EventContext: []string{"League"}}, 1},
		{"by track", Filter{TrackID: intp(341)}, 2},
		{"by car", Filter{CarID: intp(45)}, 1},
		{"by league", Filter{LeagueID: intp(4242)}, 1},
		{"date range", Filter{From: "2026-07-08T00:00:00Z", To: "2026-07-15T23:59:59Z"}, 4},
		{"exclude AI", Filter{ExcludeAI: true}, 5},
		{"races excluding AI", Filter{SessionType: []string{"Race"}, ExcludeAI: true}, 2},
		{"no matches", Filter{TrackID: intp(9999)}, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rows, total, err := s.ListSessions(c.f)
			if err != nil {
				t.Fatal(err)
			}
			if total != c.want || len(rows) != c.want {
				t.Errorf("total=%d len=%d, want %d", total, len(rows), c.want)
			}
		})
	}
}

// Total must count every match, not just the returned page.
func TestListSessionsPaginationTotalIgnoresLimit(t *testing.T) {
	s := openTemp(t)
	seed(t, s)
	rows, total, err := s.ListSessions(Filter{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || total != 6 {
		t.Errorf("len=%d total=%d, want 2 and 6", len(rows), total)
	}
	page2, _, _ := s.ListSessions(Filter{Limit: 2, Offset: 2})
	if len(page2) != 2 || page2[0].SessionKey == rows[0].SessionKey {
		t.Error("Offset did not advance the page")
	}
}

func TestSummaryGroupings(t *testing.T) {
	s := openTemp(t)
	seed(t, s)
	for _, g := range GroupByNames() {
		if rows, err := s.Summary(Filter{}, g); err != nil || len(rows) == 0 {
			t.Errorf("Summary(%q): rows=%d err=%v", g, len(rows), err)
		}
	}

	byKey := map[string]SummaryRow{}
	rows, _ := s.Summary(Filter{}, "type")
	for _, r := range rows {
		byKey[r.Key] = r
	}
	// Practice: 2000 + 1400 = 3400 driving seconds.
	if r := byKey["Practice"]; math.Abs(r.DrivingHours-3400.0/3600.0) > 1e-9 || r.Sessions != 2 {
		t.Errorf("Practice = %+v", r)
	}
	// Race: 2800 + 3400 + 1000 = 7200 = exactly 2 hours.
	if r := byKey["Race"]; math.Abs(r.DrivingHours-2.0) > 1e-9 {
		t.Errorf("Race DrivingHours = %v, want 2.0", r.DrivingHours)
	}
}

// The typecontext grouping drives the stacked bar, so both practice flavours must
// be distinguishable.
func TestSummaryTypeContextSeparatesPracticeFlavours(t *testing.T) {
	s := openTemp(t)
	seed(t, s)
	rows, err := s.Summary(Filter{}, "typecontext")
	if err != nil {
		t.Fatal(err)
	}
	keys := map[string]bool{}
	for _, r := range rows {
		keys[r.Key] = true
	}
	for _, want := range []string{
		"Practice/OfficialPractice", "Practice/OfficialRace", "Race/League", "Race/AI",
	} {
		if !keys[want] {
			t.Errorf("missing group %q; got %v", want, keys)
		}
	}
}

// group_by is an allowlist. Interpolating it would turn a query parameter into
// SQL injection.
func TestSummaryRejectsUnknownGroupBy(t *testing.T) {
	s := openTemp(t)
	seed(t, s)
	for _, bad := range []string{"", "nonsense", "track; DROP TABLE sessions", "1=1"} {
		if _, err := s.Summary(Filter{}, bad); !errors.Is(err, ErrBadGroupBy) {
			t.Errorf("Summary(%q) = %v, want ErrBadGroupBy", bad, err)
		}
	}
	if _, total, err := s.ListSessions(Filter{}); err != nil || total != 6 {
		t.Errorf("after injection attempts: total=%d err=%v, want 6 and nil", total, err)
	}
}

func TestDaily(t *testing.T) {
	s := openTemp(t)
	seed(t, s)
	rows, err := s.Daily(Filter{})
	if err != nil {
		t.Fatal(err)
	}
	byDay := map[string]float64{}
	for _, r := range rows {
		byDay[r.Day] = r.DrivingHours
	}
	// 2026-07-08: practice 1400 + qualify 450 + race 2800 = 4650 seconds.
	if got := byDay["2026-07-08"]; math.Abs(got-4650.0/3600.0) > 1e-9 {
		t.Errorf("2026-07-08 = %v hours, want %v", got, 4650.0/3600.0)
	}
	// The seed spans four dates: 07-01, 07-08 (three sessions), 07-15 and 07-20.
	// Daily groups by calendar day, so the three sessions on 07-08 collapse.
	if len(byDay) != 4 {
		t.Errorf("distinct days = %d, want 4", len(byDay))
	}
}

// The dashboard's calendar heatmap must label an evening session with the local
// day the driver experienced, even when UTC has already advanced to tomorrow.
func TestDailyUsesLocalCalendarDay(t *testing.T) {
	s := openTemp(t)
	seedAt(t, s, "evening", "2026-08-16T02:00:00Z")

	rows, err := s.Daily(Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Day != "2026-08-15" {
		t.Fatalf("daily rows = %+v, want the session under local day 2026-08-15", rows)
	}
}

// Week and month charts use the same local calendar contract as the daily
// heatmap. Each timestamp is just after a UTC boundary but still in the prior
// local period.
func TestCalendarSummariesUseLocalPeriods(t *testing.T) {
	for _, tc := range []struct {
		name, group, started, want string
	}{
		{"week", "week", "2026-08-03T02:00:00Z", "2026-W30"},
		{"month", "month", "2026-09-01T02:00:00Z", "2026-08"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := openTemp(t)
			seedAt(t, s, tc.name, tc.started)
			rows, err := s.Summary(Filter{}, tc.group)
			if err != nil {
				t.Fatal(err)
			}
			if len(rows) != 1 || rows[0].Key != tc.want {
				t.Fatalf("%s summary = %+v, want local period %q", tc.group, rows, tc.want)
			}
		})
	}
}

func TestTotals(t *testing.T) {
	s := openTemp(t)
	seed(t, s)
	got, err := s.Totals(Filter{})
	if err != nil {
		t.Fatal(err)
	}
	// driving 11050 s, connected 13800 s.
	if math.Abs(got.DrivingHours-11050.0/3600.0) > 1e-9 {
		t.Errorf("DrivingHours = %v", got.DrivingHours)
	}
	if math.Abs(got.Utilisation-11050.0/13800.0) > 1e-9 {
		t.Errorf("Utilisation = %v", got.Utilisation)
	}
	// Laps sums sessions.laps_completed rather than counting lap rows. Those are
	// different numbers on purpose: the session counter is what the sim reported,
	// and a lap row only exists for laps the collector observed being crossed. A
	// session joined part-way through has more completed laps than lap rows.
	if got.Sessions != 6 {
		t.Errorf("Sessions = %d, want 6", got.Sessions)
	}
	if got.ActiveDays != 4 {
		t.Errorf("ActiveDays = %d, want 4 distinct local dates", got.ActiveDays)
	}
	if got.AverageDrivingHoursPerActiveDay == nil ||
		math.Abs(*got.AverageDrivingHoursPerActiveDay-got.DrivingHours/4) > 1e-9 {
		t.Errorf("AverageDrivingHoursPerActiveDay = %v, want DrivingHours / 4 active days",
			got.AverageDrivingHoursPerActiveDay)
	}
	if want := 20 + 15 + 3 + 25 + 30 + 10; got.Laps != want {
		t.Errorf("Laps = %d, want %d from summing laps_completed", got.Laps, want)
	}
	if got.CleanLaps != 12 {
		t.Errorf("CleanLaps = %d, want 12 timed non-pit laps without incidents", got.CleanLaps)
	}
	if got.UniqueCars != 2 || got.UniqueTracks != 2 || got.UniqueCarTrackCombos != 3 {
		t.Errorf("unique cars/tracks/combos = %d/%d/%d, want 2/2/3",
			got.UniqueCars, got.UniqueTracks, got.UniqueCarTrackCombos)
	}
	// Three races, each with one OnTrack pass and one OnTrack loss. The pit-caused
	// gain must be excluded.
	if got.PassesMade != 3 || got.TimesPassed != 3 {
		t.Errorf("passes=%d passed=%d, want 3 and 3 — attrition must be excluded",
			got.PassesMade, got.TimesPassed)
	}
}

// Clean laps are observed lap records, not the simulator's session-level lap
// counter. Pit, incident-bearing, and untimed records must not enter the total.
func TestTotalsCleanLapsUsesCleanTimedNonPitDefinition(t *testing.T) {
	s := openTemp(t)
	sid := seedSession(t, s)
	for _, lap := range []*Lap{
		{SessionID: sid, LapNumber: 1, LapTimeS: f64p(100)},
		{SessionID: sid, LapNumber: 2, LapTimeS: f64p(101), IsPitLap: true},
		{SessionID: sid, LapNumber: 3, LapTimeS: f64p(102), IncidentsOnLap: 1},
		{SessionID: sid, LapNumber: 4},
	} {
		if _, err := s.InsertLap(lap); err != nil {
			t.Fatal(err)
		}
	}

	got, err := s.Totals(Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if got.CleanLaps != 1 {
		t.Errorf("CleanLaps = %d, want only the timed non-pit lap without incidents", got.CleanLaps)
	}
}

// The dashboard says "raced", so practice-only equipment and venues must not
// inflate these counts. Stable ids, rather than names, define uniqueness.
func TestTotalsUniqueRaceEntitiesExcludePracticeOnly(t *testing.T) {
	s := openTemp(t)
	for i, rec := range []*Session{
		{SessionType: "Practice", CarID: intp(10), TrackID: intp(20)},
		{SessionType: "Race", CarID: intp(30), TrackID: intp(40)},
		{SessionType: "Race", CarID: intp(30), TrackID: intp(50)},
	} {
		rec.SessionKey = fmt.Sprintf("unique-race/%d", i)
		rec.StartedAt = fmt.Sprintf("2026-08-%02dT12:00:00Z", i+1)
		rec.ClassifySourceJSON = "{}"
		if _, err := s.UpsertSession(rec); err != nil {
			t.Fatal(err)
		}
	}

	got, err := s.Totals(Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if got.UniqueCars != 1 || got.UniqueTracks != 2 || got.UniqueCarTrackCombos != 2 {
		t.Errorf("unique raced cars/tracks/combos = %d/%d/%d, want 1/2/2",
			got.UniqueCars, got.UniqueTracks, got.UniqueCarTrackCombos)
	}
}

func TestTotalsExcludeAI(t *testing.T) {
	s := openTemp(t)
	seed(t, s)
	got, err := s.Totals(Filter{ExcludeAI: true})
	if err != nil {
		t.Fatal(err)
	}
	if got.Sessions != 5 {
		t.Errorf("Sessions = %d, want 5 with AI excluded", got.Sessions)
	}
	if got.PassesMade != 2 {
		t.Errorf("PassesMade = %d, want 2 with the AI race excluded", got.PassesMade)
	}
}

// Utilisation must not divide by zero on an empty set.
func TestTotalsEmptySet(t *testing.T) {
	got, err := openTemp(t).Totals(Filter{})
	if err != nil {
		t.Fatalf("Totals on an empty database = %v", err)
	}
	if got.Sessions != 0 || got.Utilisation != 0 || got.IncidentsPerHour != 0 {
		t.Errorf("Totals = %+v, want zeroes", got)
	}
	if got.ActiveDays != 0 || got.AverageDrivingHoursPerActiveDay != nil {
		t.Errorf("empty active-day totals = %d, %v; want 0 days and no average",
			got.ActiveDays, got.AverageDrivingHoursPerActiveDay)
	}
	if got.CleanLaps != 0 || got.UniqueCars != 0 || got.UniqueTracks != 0 ||
		got.UniqueCarTrackCombos != 0 {
		t.Errorf("empty lifetime counters = %+v, want zeroes", got)
	}
}

func TestListLapsJoinsSessionContext(t *testing.T) {
	s := openTemp(t)
	seed(t, s)
	rows, total, err := s.ListLaps(LapFilter{Filter: Filter{TrackID: intp(341)}})
	if err != nil {
		t.Fatal(err)
	}
	if total != 4 || len(rows) != 4 {
		t.Fatalf("total=%d len=%d, want 4", total, len(rows))
	}
	for _, r := range rows {
		if r.TrackName != "Spa" || r.SessionID == 0 || r.StartedAt == "" || r.SessionType == "" {
			t.Errorf("session context not joined: %+v", r)
		}
	}
}

func TestListLapsCleanOnlyAppliesBeforePaging(t *testing.T) {
	s := openTemp(t)
	seed(t, s)

	sessions, _, err := s.ListSessions(Filter{TrackID: intp(341), Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("seed session lookup returned %d sessions, want 1", len(sessions))
	}

	timed := 142.5
	zero := 0.0
	for _, lap := range []Lap{
		{SessionID: sessions[0].ID, LapNumber: 3, LapTimeS: &timed, IsPitLap: true},
		{SessionID: sessions[0].ID, LapNumber: 4, LapTimeS: &timed, IncidentsOnLap: 1},
		{SessionID: sessions[0].ID, LapNumber: 5, LapTimeS: &zero},
		{SessionID: sessions[0].ID, LapNumber: 6},
	} {
		if _, err := s.InsertLap(&lap); err != nil {
			t.Fatal(err)
		}
	}

	_, allTotal, err := s.ListLaps(LapFilter{Filter: Filter{TrackID: intp(341)}})
	if err != nil {
		t.Fatal(err)
	}
	if allTotal != 8 {
		t.Fatalf("all lap total = %d, want 8", allTotal)
	}

	rows, cleanTotal, err := s.ListLaps(LapFilter{
		Filter:    Filter{TrackID: intp(341), Limit: 2},
		CleanOnly: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if cleanTotal != 4 || len(rows) != 2 {
		t.Fatalf("clean total=%d len=%d, want total 4 and paged len 2", cleanTotal, len(rows))
	}
	for _, r := range rows {
		if r.IsPitLap || r.IncidentsOnLap != 0 || r.LapTimeS == nil || *r.LapTimeS <= 0 {
			t.Errorf("dirty lap returned by clean filter: %+v", r)
		}
	}
}

func TestFacets(t *testing.T) {
	s := openTemp(t)
	seed(t, s)
	got, err := s.Facets()
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Tracks) != 2 || len(got.Cars) != 2 {
		t.Errorf("tracks=%v cars=%v", got.Tracks, got.Cars)
	}
	// League 0 means "not a league", so it must not be offered as a filter.
	if len(got.Leagues) != 1 {
		t.Errorf("Leagues = %v, want only the real league", got.Leagues)
	}
	if len(got.SessionTypes) != 3 {
		t.Errorf("SessionTypes = %v, want 3", got.SessionTypes)
	}
	for _, tr := range got.Tracks {
		if tr.Name == "Watkins Glen" && tr.Sessions != 4 {
			t.Errorf("Watkins Glen sessions = %d, want 4", tr.Sessions)
		}
	}
}

// Breakdown is the two-dimensional aggregate the stacked bars need. Summary cannot
// express it: it groups by one dimension, so asking it for "hours per car, split by
// category" would mean encoding two fields into one key and splitting the string back
// apart — which breaks the moment a name contains the separator.
func TestBreakdownByCar(t *testing.T) {
	s := openTemp(t)
	seed(t, s)
	// Give every seeded session a known five-kilometre lap so the distance split
	// has an independently computable expected value.
	if _, err := s.Writer().Exec(`UPDATE sessions SET track_length_km = 5`); err != nil {
		t.Fatal(err)
	}
	// Of the twelve inserted lap rows, make one dirty, one a pit lap, and one
	// untimed. Clean laps use the same exclusions as EntityStats.
	if _, err := s.Writer().Exec(`
UPDATE laps SET incidents_on_lap = 1 WHERE id = (SELECT MIN(id) FROM laps);
UPDATE laps SET is_pit_lap = 1 WHERE id = (SELECT MIN(id) + 1 FROM laps);
UPDATE laps SET lap_time_s = NULL WHERE id = (SELECT MIN(id) + 2 FROM laps)`); err != nil {
		t.Fatal(err)
	}

	rows, err := s.Breakdown(Filter{}, "car")
	if err != nil {
		t.Fatalf("Breakdown: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("no breakdown rows")
	}

	// The seed has two cars; the Porsche appears in several categories, the MX-5 in
	// exactly one (the AI race).
	byCar := map[string]float64{}
	var laps int
	var cleanLaps int
	var distanceKm float64
	cells := map[string]int{}
	for _, r := range rows {
		byCar[r.Group] += r.DrivingHours
		laps += r.Laps
		cleanLaps += r.CleanLaps
		distanceKm += r.DistanceKm
		cells[r.Group]++
		if r.Group == "" || r.Stack == "" {
			t.Errorf("row has an empty dimension: %+v", r)
		}
		if !strings.Contains(r.Stack, "/") {
			t.Errorf("stack %q is not a type/context pair", r.Stack)
		}
	}
	if len(byCar) != 2 {
		t.Errorf("cars = %v, want 2", byCar)
	}
	if cells["Mazda MX-5"] != 1 {
		t.Errorf("MX-5 cells = %d, want 1 — it appears in only one category", cells["Mazda MX-5"])
	}
	if cells["Porsche 911 GT3 R"] < 3 {
		t.Errorf("Porsche cells = %d, want at least 3 categories", cells["Porsche 911 GT3 R"])
	}
	if laps != 103 {
		t.Errorf("breakdown laps = %d, want 103 completed laps", laps)
	}
	if cleanLaps != 9 {
		t.Errorf("breakdown clean laps = %d, want 9 timed non-pit laps without incidents", cleanLaps)
	}
	if distanceKm != 515 {
		t.Errorf("breakdown distance = %v km, want 515 (103 laps x 5 km)", distanceKm)
	}

	// The per-car totals must reconcile with the overall driving time, or the stacked
	// bars would not add up to what the KPI row reports.
	total, err := s.Totals(Filter{})
	if err != nil {
		t.Fatal(err)
	}
	var sum float64
	for _, v := range byCar {
		sum += v
	}
	if math.Abs(sum-total.DrivingHours) > 1e-9 {
		t.Errorf("breakdown sums to %v driving hours but Totals reports %v", sum, total.DrivingHours)
	}
}

func TestBreakdownByTrackHonoursFilter(t *testing.T) {
	s := openTemp(t)
	seed(t, s)

	rows, err := s.Breakdown(Filter{TrackID: intp(341)}, "track")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.Group != "Spa" {
			t.Errorf("group = %q, want only Spa when filtered to that track", r.Group)
		}
	}
	if len(rows) == 0 {
		t.Error("filtering to a track produced no rows")
	}
}

// The outer dimension is an allowlist for the same reason group_by is: it arrives
// from a query parameter.
func TestBreakdownRejectsUnknownDimension(t *testing.T) {
	s := openTemp(t)
	seed(t, s)
	for _, bad := range []string{"", "nonsense", "car; DROP TABLE sessions", "1=1"} {
		if _, err := s.Breakdown(Filter{}, bad); !errors.Is(err, ErrBadGroupBy) {
			t.Errorf("Breakdown(%q) = %v, want ErrBadGroupBy", bad, err)
		}
	}
	if _, total, err := s.ListSessions(Filter{}); err != nil || total != 6 {
		t.Errorf("after injection attempts: total=%d err=%v, want 6 and nil", total, err)
	}
}

func TestBreakdownDimensionsAreAdvertised(t *testing.T) {
	names := BreakdownNames()
	want := map[string]bool{"car": false, "track": false, "carclass": false, "league": false}
	for _, n := range names {
		if _, ok := want[n]; !ok {
			t.Errorf("unexpected dimension %q", n)
		}
		want[n] = true
	}
	for n, seen := range want {
		if !seen {
			t.Errorf("dimension %q is not advertised", n)
		}
	}
}
