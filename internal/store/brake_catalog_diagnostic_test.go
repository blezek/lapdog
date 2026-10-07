//go:build diagnostic

package store

import (
	"path/filepath"
	"testing"
)

func TestDiagnosticBuildOmitsPackagedBrakeCatalog(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "lapdog.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	rows, err := st.ListBrakeScenarios()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("diagnostic build has %d scenarios, want only the synthetic baseline", len(rows))
	}
	if rows[0].ID != "builtin-threshold-to-trail-baseline" {
		t.Fatalf("diagnostic build baseline id = %q", rows[0].ID)
	}
}
