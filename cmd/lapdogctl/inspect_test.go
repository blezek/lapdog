package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/blezek/lapdog/internal/capture"
)

// fixture returns a committed capture path, skipping if the fixtures are absent.
//
// The fixtures are committed precisely so this test needs no generated dataset; if
// someone removes them, skipping is better than failing on unrelated grounds.
// numberedMatch is the shape a real grep hit takes: leading spaces, a line number,
// a colon, then the matching text containing the search term.
var numberedMatch = regexp.MustCompile(`(?m)^\s*\d+:.*CarIsAI`)

func fixture(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join("..", "..", "testdata", "fixtures", name)
	if _, err := os.Stat(path); err != nil {
		t.Skipf("fixture %s is absent: %v", name, err)
	}
	return path
}

// capturingStdout runs fn with stdout redirected and returns what it wrote.
func capturingStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	runErr := fn()
	w.Close()
	os.Stdout = orig

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		t.Fatal(err)
	}
	return buf.String(), runErr
}

// The default output is the session YAML, because reading what the simulator
// actually reported is the reason to open a capture at all.
func TestInspectPrintsSessionYAML(t *testing.T) {
	out, err := capturingStdout(t, func() error {
		return inspectCapture([]string{fixture(t, "ai-race-field-present.lpd")})
	})
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if !strings.Contains(out, "WeekendInfo:") {
		t.Errorf("output does not look like session YAML:\n%s", out[:min(400, len(out))])
	}
	if !strings.Contains(out, "session record(s)") {
		t.Error("output does not report how many session records were found")
	}
}

// The grep mode is what answers a field-name question, and it reports line numbers
// because a match's position says which section of the document it belongs to.
func TestInspectGrepFindsAndReportsLineNumbers(t *testing.T) {
	out, err := capturingStdout(t, func() error {
		return inspectCapture([]string{"-grep", "CarIsAI", fixture(t, "ai-race-field-present.lpd")})
	})
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	// Assert a numbered match line, not merely that the term appears somewhere. The
	// no-match message echoes the search term, so a bare Contains check is satisfied
	// by the failure path — verified: making grep match nothing left this test green
	// until the assertion was tightened.
	if strings.Contains(out, "no line contains") {
		t.Fatalf("grep found nothing in a fixture built to contain the field:\n%s", out)
	}
	if !numberedMatch.MatchString(out) {
		t.Fatalf("no line matched the numbered form \"   588: ... CarIsAI\":\n%s", out)
	}
	// Every emitted line carries a number followed by a colon.
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !strings.Contains(line, ":") {
			t.Errorf("line has no line number: %q", line)
		}
	}
}

// An absent field must say so rather than printing nothing, which would be
// indistinguishable from a broken command.
func TestInspectGrepReportsNoMatchExplicitly(t *testing.T) {
	out, err := capturingStdout(t, func() error {
		return inspectCapture([]string{"-grep", "NoSuchFieldAnywhere", fixture(t, "ai-race-field-present.lpd")})
	})
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if !strings.Contains(out, "no line contains") {
		t.Errorf("a failed grep printed no explanation:\n%s", out)
	}
}

// The two fixtures differ in exactly this field, so grep must distinguish them.
// This is the check that matters: it is the procedure for confirming the AI field
// name on real data, exercised against data where the answer is known.
func TestInspectGrepDistinguishesTheFixtures(t *testing.T) {
	present, err := capturingStdout(t, func() error {
		return inspectCapture([]string{"-grep", "CarIsAI", fixture(t, "ai-race-field-present.lpd")})
	})
	if err != nil {
		t.Fatal(err)
	}
	absent, err := capturingStdout(t, func() error {
		return inspectCapture([]string{"-grep", "CarIsAI", fixture(t, "ai-race-field-absent.lpd")})
	})
	if err != nil {
		t.Fatal(err)
	}
	// Both checks avoid a bare Contains, because the no-match message repeats the
	// search term and would satisfy one.
	if !numberedMatch.MatchString(present) {
		t.Errorf("the field-present fixture produced no numbered match:\n%s", present)
	}
	if numberedMatch.MatchString(absent) {
		t.Errorf("the field-absent fixture produced a match:\n%s", absent)
	}
	if !strings.Contains(absent, "no line contains") {
		t.Errorf("the field-absent fixture did not report the absence:\n%s", absent)
	}
}

// NDJSON emits a header object then one object per record, so the output can be
// piped into jq rather than read.
func TestInspectNDJSONEmitsOneObjectPerLine(t *testing.T) {
	out, err := capturingStdout(t, func() error {
		return inspectCapture([]string{"-ndjson", "-limit", "4", fixture(t, "ai-race-field-present.lpd")})
	})
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	// One header plus at most the limit.
	if len(lines) < 2 || len(lines) > 5 {
		t.Fatalf("got %d lines, want a header plus up to 4 records", len(lines))
	}
	var header map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &header); err != nil {
		t.Fatalf("first line is not JSON: %v", err)
	}
	if header["kind"] != "header" {
		t.Errorf("first line kind = %v, want header", header["kind"])
	}
	if _, ok := header["varNames"]; !ok {
		t.Error("header carries no variable names, which is what makes the frames readable")
	}
	for _, l := range lines[1:] {
		var obj map[string]any
		if err := json.Unmarshal([]byte(l), &obj); err != nil {
			t.Errorf("record line is not JSON: %v", err)
		}
	}
}

// A capture with no session YAML must report that rather than printing an empty
// document, which would read as a session that recorded nothing.
func TestInspectReportsMissingYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "novars"+capture.Ext)
	w, err := capture.NewWriter(path, capture.Meta{TickRate: 60, NumVars: 0, BufLen: 0})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	_, err = capturingStdout(t, func() error { return inspectCapture([]string{path}) })
	if err == nil {
		t.Fatal("inspect on a capture with no session YAML returned no error")
	}
	if !strings.Contains(err.Error(), "no session YAML") {
		t.Errorf("error does not name the cause: %v", err)
	}
}

// Argument errors print the usage rather than a bare flag-package message.
func TestInspectRequiresExactlyOnePath(t *testing.T) {
	for _, args := range [][]string{{}, {"a.lpd", "b.lpd"}} {
		if err := inspectCapture(args); err == nil {
			t.Errorf("inspect(%v) returned no error", args)
		} else if !strings.Contains(err.Error(), "lapdogctl inspect") {
			t.Errorf("inspect(%v) error omits the usage: %v", args, err)
		}
	}
}

// The last session record wins, not the first.
//
// The simulator rewrites the whole document whenever anything changes, so an early
// copy can predate the driver list being populated — which is exactly the section a
// field-name question is about. The committed fixtures cannot check this because
// their session records do not differ, so this builds a capture where only the last
// one carries the marker.
func TestInspectUsesTheLastSessionRecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), "two"+capture.Ext)
	w, err := capture.NewWriter(path, capture.Meta{TickRate: 60, NumVars: 0, BufLen: 0})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.WriteSession(0, 1, []byte("WeekendInfo:\n EarlyOnly: 1\n")); err != nil {
		t.Fatal(err)
	}
	if err := w.WriteSession(1, 2, []byte("WeekendInfo:\n LateOnly: 1\n")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	out, err := capturingStdout(t, func() error { return inspectCapture([]string{path}) })
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if !strings.Contains(out, "LateOnly") {
		t.Errorf("output lacks the last record's content:\n%s", out)
	}
	if strings.Contains(out, "EarlyOnly") {
		t.Errorf("output carries the first record's content, so the wrong one won:\n%s", out)
	}
	if !strings.Contains(out, "2 session record(s)") {
		t.Errorf("output does not report both records were seen:\n%s", out)
	}
}
