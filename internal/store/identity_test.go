package store

import (
	"database/sql"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// nowRFC is the timestamp format the store keeps its columns in.
func nowRFC() string { return time.Now().UTC().Format(time.RFC3339Nano) }

// showI and showF render a nullable value for a failure message. Printing the
// pointer itself reports an address, which says nothing about what went wrong.
func showI(p *int) string {
	if p == nil {
		return "<nil>"
	}
	return strconv.Itoa(*p)
}

func showF(p *float64) string {
	if p == nil {
		return "<nil>"
	}
	return strconv.FormatFloat(*p, 'f', -1, 64)
}

// The identity and both ratings survive a write and read back.
//
// Six columns were added to the INSERT, the ON CONFLICT clause, the two column
// lists and the scan. A mismatch between any two of those is a runtime error or,
// worse, a silent shift that reads the licence level into the iRating — so the
// values here are deliberately distinct rather than all set to 1.
func TestSessionIdentityRoundTrips(t *testing.T) {
	s := openTemp(t)
	rec := &Session{
		UUID:        "u-identity",
		SessionKey:  "k-identity",
		SessionType: "Race",
		StartedAt:   nowRFC(),

		DriverUserID:       intp(271828),
		DriverIRating:      intp(2431),
		DriverLicString:    strp("A 3.55"),
		DriverLicLevel:     intp(13),
		DriverLicSubLevel:  intp(355),
		DriverSafetyRating: f64p(3.55),
	}
	if _, err := s.UpsertSession(rec); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}

	got, err := s.SessionByKey("k-identity")
	if err != nil {
		t.Fatalf("SessionByKey: %v", err)
	}
	for _, c := range []struct {
		name string
		got  *int
		want int
	}{
		{"DriverUserID", got.DriverUserID, 271828},
		{"DriverIRating", got.DriverIRating, 2431},
		{"DriverLicLevel", got.DriverLicLevel, 13},
		{"DriverLicSubLevel", got.DriverLicSubLevel, 355},
	} {
		if c.got == nil {
			t.Errorf("%s is nil after round trip", c.name)
			continue
		}
		if *c.got != c.want {
			t.Errorf("%s = %d, want %d", c.name, *c.got, c.want)
		}
	}
	if got.DriverLicString == nil || *got.DriverLicString != "A 3.55" {
		want := "A 3.55"
		if got.DriverLicString != nil {
			want = *got.DriverLicString
		}
		t.Errorf("DriverLicString = %q, want \"A 3.55\"", want)
	}
	if got.DriverSafetyRating == nil || *got.DriverSafetyRating != 3.55 {
		t.Errorf("DriverSafetyRating = %s, want 3.55", showF(got.DriverSafetyRating))
	}
}

// A session recorded with no identity reads back absent, not zero.
//
// Absent and zero are different facts: an iRating of 0 is a real value for an
// unrated licence, so a NULL that scanned as 0 would invent a rating.
func TestSessionWithoutIdentityReadsBackNull(t *testing.T) {
	s := openTemp(t)
	if _, err := s.UpsertSession(&Session{
		UUID: "u-none", SessionKey: "k-none", SessionType: "Practice",
		StartedAt: nowRFC(),
	}); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}
	got, err := s.SessionByKey("k-none")
	if err != nil {
		t.Fatalf("SessionByKey: %v", err)
	}
	if got.DriverUserID != nil || got.DriverIRating != nil || got.DriverSafetyRating != nil {
		t.Errorf("absent identity read back as %s/%s/%s, want all nil",
			showI(got.DriverUserID), showI(got.DriverIRating), showF(got.DriverSafetyRating))
	}
}

// A later session updates the ratings rather than keeping the first values.
//
// The same session is upserted repeatedly as it runs, and iRating is only final
// once the result is posted. If the ON CONFLICT clause omitted these columns the
// stored rating would be whichever one happened to be read first.
func TestUpsertUpdatesRatings(t *testing.T) {
	s := openTemp(t)
	rec := &Session{
		UUID: "u-upd", SessionKey: "k-upd", SessionType: "Race",
		StartedAt:     nowRFC(),
		DriverIRating: intp(2431), DriverSafetyRating: f64p(3.55),
	}
	if _, err := s.UpsertSession(rec); err != nil {
		t.Fatal(err)
	}
	rec.DriverIRating = intp(2498)
	rec.DriverSafetyRating = f64p(3.71)
	if _, err := s.UpsertSession(rec); err != nil {
		t.Fatal(err)
	}

	got, err := s.SessionByKey("k-upd")
	if err != nil {
		t.Fatal(err)
	}
	if got.DriverIRating == nil || *got.DriverIRating != 2498 {
		t.Errorf("DriverIRating = %s after re-upsert, want 2498", showI(got.DriverIRating))
	}
	if got.DriverSafetyRating == nil || *got.DriverSafetyRating != 3.71 {
		t.Errorf("DriverSafetyRating = %s after re-upsert, want 3.71", showF(got.DriverSafetyRating))
	}
}

// A database created before this migration upgrades in place, keeping its rows.
//
// This is the path every existing install takes. It is exercised by building a
// version-1 database from the 0001 migration alone — not by opening a current
// one — because opening a current database would apply every migration and prove
// nothing about the upgrade.
func TestUpgradeFromVersionOne(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")

	// A version-1 database, built from the shipped 0001 migration so it matches
	// what an existing install actually holds.
	body, err := migrationFS.ReadFile("migrations/0001_init.sql")
	if err != nil {
		t.Fatalf("read 0001: %v", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(string(body)); err != nil {
		t.Fatalf("apply 0001: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO sessions (uuid, session_key, session_num, session_type, event_context,
		                       started_at, classify_source_json, created_at, updated_at)
		 VALUES ('u-old', 'k-old', 0, 'Race', 'hosted', ?, '{}', ?, ?)`,
		nowRFC(), nowRFC(), nowRFC(),
	); err != nil {
		t.Fatalf("seed a pre-migration row: %v", err)
	}
	var v int
	if err := db.QueryRow(`SELECT version FROM schema_version`).Scan(&v); err != nil {
		t.Fatal(err)
	}
	if v != 1 {
		t.Fatalf("fixture is version %d, want 1; this test no longer exercises an upgrade", v)
	}
	db.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open on a version-1 database: %v", err)
	}
	defer s.Close()

	if v, err := s.SchemaVersion(); err != nil || v != CurrentSchemaVersion {
		t.Errorf("SchemaVersion after upgrade = %d (err %v), want %d", v, err, CurrentSchemaVersion)
	}
	// The pre-existing row is still readable, which is what the added scan targets
	// have to tolerate: its identity columns are NULL.
	got, err := s.SessionByKey("k-old")
	if err != nil {
		t.Fatalf("reading a pre-migration row: %v", err)
	}
	if got.DriverUserID != nil {
		t.Errorf("pre-migration row has DriverUserID %s, want nil", showI(got.DriverUserID))
	}
	// And the upgraded database accepts identity on new rows.
	if _, err := s.UpsertSession(&Session{
		UUID: "u-new", SessionKey: "k-new", SessionType: "Race",
		StartedAt: nowRFC(), DriverUserID: intp(271828),
	}); err != nil {
		t.Fatalf("writing identity to an upgraded database: %v", err)
	}
}
