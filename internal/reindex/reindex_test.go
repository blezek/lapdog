package reindex

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/blezek/lapdog/internal/store"
	"github.com/blezek/lapdog/internal/synth"
)

func TestRunReindexesWithoutDuplicatesAndKeepsCaptureName(t *testing.T) {
	dir := t.TempDir()
	name := "20260812T014837Z-public-practice.lpd"
	copyFile(t, generatedFixture(t, "public-practice.lpd"), filepath.Join(dir, name))
	paths, err := Discover(dir)
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(t.TempDir(), "lapdog.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	var last Progress
	result, err := Run(context.Background(), paths, st, Options{OnProgress: func(p Progress) { last = p }})
	if err != nil {
		t.Fatal(err)
	}
	if result.Replayed != 1 || result.Segments != 1 || result.Failed != 0 || last.Processed != 1 {
		t.Fatalf("result=%+v last=%+v", result, last)
	}
	rows, total, err := st.ListSessions(store.Filter{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(rows) != 1 {
		t.Fatalf("first replay produced %d sessions, want 1", total)
	}
	if rows[0].CaptureFile == nil || *rows[0].CaptureFile != name {
		t.Errorf("CaptureFile = %v, want %q", rows[0].CaptureFile, name)
	}

	if _, err := Run(context.Background(), paths, st, Options{}); err != nil {
		t.Fatal(err)
	}
	_, total, err = st.ListSessions(store.Filter{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 {
		t.Errorf("second replay produced %d sessions, want the same 1", total)
	}
}

func TestRunReportsOneBadCaptureAndContinues(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "000-broken.lpd"), []byte("not a capture"), 0o644); err != nil {
		t.Fatal(err)
	}
	copyFile(t, generatedFixture(t, "public-practice.lpd"), filepath.Join(dir, "100-good.lpd"))
	paths, err := Discover(dir)
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(t.TempDir(), "lapdog.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	result, err := Run(context.Background(), paths, st, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Processed != 2 || result.Replayed != 1 || result.Segments != 1 || result.Failed != 1 {
		t.Fatalf("result = %+v", result)
	}
	if len(result.Failures) != 1 || result.Failures[0].File != "000-broken.lpd" {
		t.Errorf("failures = %+v", result.Failures)
	}
}

func TestTimeFromNameUsesExplicitFallback(t *testing.T) {
	fallback := time.Date(2024, 3, 2, 1, 0, 0, 0, time.FixedZone("test", -6*60*60))
	if got := TimeFromName("no-time.lpd", fallback); !got.Equal(fallback.UTC()) || got.Location() != time.UTC {
		t.Errorf("TimeFromName fallback = %v, want %v", got, fallback.UTC())
	}
}

func copyFile(t *testing.T, from, to string) {
	t.Helper()
	body, err := os.ReadFile(from)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(to, body, 0o644); err != nil {
		t.Fatal(err)
	}
}

func generatedFixture(t *testing.T, name string) string {
	t.Helper()
	dir := t.TempDir()
	if _, err := synth.WriteFixtures(dir); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, name)
}
