package updater

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	selfupdate "github.com/creativeprojects/go-selfupdate"
	selfreplace "github.com/creativeprojects/go-selfupdate/update"
)

type fakeDetector struct {
	release *Release
	err     error
}

func (f fakeDetector) Latest(context.Context) (*Release, error) {
	return cloneRelease(f.release), f.err
}

type fakeGate struct{ recording, quiesced, resumed bool }

func (g *fakeGate) Recording() bool { return g.recording }
func (g *fakeGate) TryQuiesce() bool {
	if g.recording {
		return false
	}
	g.quiesced = true
	return true
}
func (g *fakeGate) ResumeRecording() { g.resumed = true; g.quiesced = false }

func TestSemanticComparison(t *testing.T) {
	if !newer("v1.10.0", "v1.9.9") {
		t.Error("semantic 1.10.0 was not newer than 1.9.9")
	}
	if newer("v1.0.0-beta.1", "v1.0.0") {
		t.Error("prerelease was newer than its stable release")
	}
	if newer("not-a-version", "v1.0.0") {
		t.Error("invalid version was accepted")
	}
}

func TestDailyScheduling(t *testing.T) {
	now := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	if !checkDue(nil, now) {
		t.Fatal("never-checked build was not due")
	}
	recent := now.Add(-23*time.Hour - 59*time.Minute)
	if checkDue(&recent, now) {
		t.Fatal("check became due before 24 hours")
	}
	day := now.Add(-24 * time.Hour)
	if !checkDue(&day, now) {
		t.Fatal("check was not due at 24 hours")
	}
}

func TestDisabledBuilds(t *testing.T) {
	for _, tc := range []Options{{Version: "dev", GOOS: "windows", GOARCH: "amd64"}, {Version: "v1.0.0", GOOS: "darwin", GOARCH: "amd64"}, {Version: "v1.0.0", GOOS: "windows", GOARCH: "arm64"}} {
		t.Run(tc.Version+tc.GOOS+tc.GOARCH, func(t *testing.T) {
			tc.DataDir = t.TempDir()
			u, err := New(tc)
			if err != nil {
				t.Fatal(err)
			}
			if got := u.Snapshot().State; got != Disabled {
				t.Fatalf("state=%q, want disabled", got)
			}
		})
	}
}

func TestCorruptStateDoesNotPreventStartup(t *testing.T) {
	dir := t.TempDir()
	updateDir := filepath.Join(dir, "update")
	if err := os.MkdirAll(updateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(updateDir, "state.json"), []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	u, err := New(Options{Version: "v1.0.0", GOOS: "windows", GOARCH: "amd64", DataDir: dir, Detector: fakeDetector{}})
	if err != nil {
		t.Fatalf("corrupt updater state stopped startup: %v", err)
	}
	s := u.Snapshot()
	if s.State != Failed || s.Error == nil {
		t.Fatalf("snapshot=%+v, want visible failure", s)
	}
}

func TestSuccessfulRestartClearsStagingAndConsent(t *testing.T) {
	dir := t.TempDir()
	updateDir := filepath.Join(dir, "update")
	if err := os.MkdirAll(updateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	staged := filepath.Join(updateDir, "staged-lapdog.exe")
	backup := filepath.Join(updateDir, "lapdog.backup.exe")
	if err := os.WriteFile(staged, []byte("new"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(backup, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	p := persisted{Accepted: "v1.1.0", Staged: staged, Pending: true, Release: &Release{Version: "v1.1.0"}}
	if err := atomicJSON(filepath.Join(updateDir, "state.json"), p); err != nil {
		t.Fatal(err)
	}
	u, err := New(Options{Version: "1.1.0", GOOS: "windows", GOARCH: "amd64", DataDir: dir, Detector: fakeDetector{}})
	if err != nil {
		t.Fatal(err)
	}
	s := u.Snapshot()
	if s.State != Current || s.AcceptedVersion != nil || s.PendingRestart {
		t.Fatalf("snapshot=%+v, want completed state", s)
	}
	for _, path := range []string{staged, backup} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s still exists: %v", path, err)
		}
	}
}

func TestLaterSkipAndNewerReleasePersistence(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	rel := &Release{Version: "v1.1.0", URL: "https://example/release"}
	opts := Options{Version: "v1.0.0", GOOS: "windows", GOARCH: "amd64", DataDir: dir, Detector: fakeDetector{release: rel}, Now: func() time.Time { return now }}
	u, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	if err = u.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !u.Snapshot().PromptEligible {
		t.Fatal("new release was not prompt eligible")
	}
	if err = u.Action(context.Background(), "later"); err != nil {
		t.Fatal(err)
	}
	s := u.Snapshot()
	if s.State != Deferred || s.DeferredUntil == nil || !s.DeferredUntil.Equal(now.Add(24*time.Hour)) {
		t.Fatalf("deferred snapshot=%+v", s)
	}

	u2, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	if u2.Snapshot().State != Deferred {
		t.Fatal("deferral did not survive restart")
	}
	if err = u2.Action(context.Background(), "skip"); err != nil {
		t.Fatal(err)
	}
	if u2.Snapshot().State != Skipped {
		t.Fatal("exact release was not skipped")
	}
	now = now.Add(25 * time.Hour)
	u2.opts.Detector = fakeDetector{release: &Release{Version: "v1.2.0"}}
	if err = u2.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	s = u2.Snapshot()
	if s.State != Available || s.SkippedVersion != nil {
		t.Fatalf("newer release did not clear skip: %+v", s)
	}
}

func TestManualNetworkErrorIsActionable(t *testing.T) {
	u, err := New(Options{Version: "v1.0.0", GOOS: "windows", GOARCH: "amd64", DataDir: t.TempDir(), Detector: fakeDetector{err: fmt.Errorf("API rate limit exceeded")}})
	if err != nil {
		t.Fatal(err)
	}
	if err = u.Check(context.Background()); err == nil {
		t.Fatal("check succeeded, want error")
	}
	s := u.Snapshot()
	if s.State != Failed || s.Error == nil || *s.Error != "GitHub rate limit reached; try again later" {
		t.Fatalf("snapshot=%+v", s)
	}
}

func TestDiscoveryIgnoresDraftsAndPrereleases(t *testing.T) {
	source := fakeSource{releases: []selfupdate.SourceRelease{
		fakeSourceRelease{tag: "v3.0.0", draft: true, assets: []selfupdate.SourceAsset{fakeAsset{name: assetName}}},
		fakeSourceRelease{tag: "v2.0.0", prerelease: true, assets: []selfupdate.SourceAsset{fakeAsset{name: assetName}}},
		fakeSourceRelease{tag: "v1.2.0", assets: []selfupdate.SourceAsset{fakeAsset{name: assetName}}},
	}}
	up, err := selfupdate.NewUpdater(selfupdate.Config{Source: source, OS: "windows", Arch: "amd64", Filters: []string{`^lapdog-windows-amd64\.zip$`}})
	if err != nil {
		t.Fatal(err)
	}
	rel, err := (&githubDetector{up: up}).Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if rel == nil || rel.Version != "v1.2.0" {
		t.Fatalf("release=%+v, want stable v1.2.0", rel)
	}
}

func TestAcceptedUpdateWaitsAndLaunchFailureResumesRecording(t *testing.T) {
	dir := t.TempDir()
	gate := &fakeGate{recording: true}
	reindexing := false
	launches := 0
	u, err := New(Options{Version: "v1.0.0", GOOS: "windows", GOARCH: "amd64", DataDir: dir, Executable: filepath.Join(dir, "lapdog.exe"), Detector: fakeDetector{}, Gate: gate, Reindexing: func() bool { return reindexing }, Launch: func(string, ...string) error { launches++; return fmt.Errorf("launch denied") }})
	if err != nil {
		t.Fatal(err)
	}
	staged := filepath.Join(dir, "staged.exe")
	if err := os.WriteFile(staged, []byte("exe"), 0o700); err != nil {
		t.Fatal(err)
	}
	u.mu.Lock()
	u.p.Release = &Release{Version: "v1.1.0"}
	u.p.Accepted = "v1.1.0"
	u.p.Staged = staged
	u.mu.Unlock()
	u.resume(context.Background())
	if u.Snapshot().State != Waiting || launches != 0 {
		t.Fatalf("active recording did not wait: %+v launches=%d", u.Snapshot(), launches)
	}
	gate.recording = false
	reindexing = true
	u.resume(context.Background())
	if gate.quiesced || launches != 0 {
		t.Fatal("re-indexing did not block quiesce and launch")
	}
	reindexing = false
	u.resume(context.Background())
	s := u.Snapshot()
	if s.State != RestartRequired || launches != 1 || !gate.resumed {
		t.Fatalf("launch failure=%+v launches=%d resumed=%v", s, launches, gate.resumed)
	}
}

func TestStageValidatesChecksumAndZipContract(t *testing.T) {
	goodZip := zipBytes(t, map[string][]byte{"lapdog.exe": []byte("PE executable")})
	sum := sha256.Sum256(goodZip)
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body := goodZip
		if r.URL.Path == "/sums" {
			body = []byte(fmt.Sprintf("%x  %s\n", sum, assetName))
		}
		return response(r, body), nil
	})}
	dir := t.TempDir()
	path, err := stage(context.Background(), client, dir, Release{AssetURL: "https://example.test/archive", ChecksumURL: "https://example.test/sums"})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != "PE executable" {
		t.Fatalf("staged=%q err=%v", got, err)
	}

	for name, archive := range map[string][]byte{
		"missing":   zipBytes(t, map[string][]byte{"readme.txt": []byte("x")}),
		"duplicate": zipEntries(t, [][2]string{{"lapdog.exe", "a"}, {"lapdog.exe", "b"}}),
		"unsafe":    zipBytes(t, map[string][]byte{"../lapdog.exe": []byte("x")}),
	} {
		t.Run(name, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "bad.zip")
			if err := os.WriteFile(p, archive, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := extractExecutable(p, p+".exe"); err == nil {
				t.Fatal("invalid ZIP was accepted")
			}
		})
	}
	if _, err := checksumFor(assetName, []byte("aa  "+assetName+"\naa  "+assetName+"\n")); err == nil {
		t.Fatal("duplicate checksum was accepted")
	}
	badClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body := goodZip
		if r.URL.Path == "/sums" {
			body = []byte(fmt.Sprintf("%064x  %s\n", 0, assetName))
		}
		return response(r, body), nil
	})}
	if _, err := stage(context.Background(), badClient, t.TempDir(), Release{AssetURL: "https://example.test/archive", ChecksumURL: "https://example.test/sums"}); err == nil {
		t.Fatal("checksum mismatch was accepted")
	}
}

func TestDownloadLimitAndCancellation(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if err := r.Context().Err(); err != nil {
			return nil, err
		}
		resp := response(r, bytes.Repeat([]byte("x"), 20))
		resp.ContentLength = 20
		return resp, nil
	})}
	if _, err := downloadLimited(context.Background(), client, "https://example.test/file", 10); err == nil {
		t.Fatal("oversized response was accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := downloadLimited(ctx, client, "https://example.test/file", 100); err == nil {
		t.Fatal("cancelled download succeeded")
	}
}

func TestHandoffRestartsRolledBackExecutable(t *testing.T) {
	originalApply, originalRollback, originalStart := applyReplacement, rollbackError, startInstalled
	defer func() {
		applyReplacement = originalApply
		rollbackError = originalRollback
		startInstalled = originalStart
	}()
	applyErr := fmt.Errorf("permission denied")
	applyReplacement = func(io.Reader, selfreplace.Options) error { return applyErr }
	rollbackError = func(error) error { return nil }
	started := ""
	startInstalled = func(path string) error { started = path; return nil }
	target := filepath.Join(t.TempDir(), "lapdog.exe")
	err := RunHandoff(HandoffArgs{PID: 999999, Target: target, Backup: target + ".backup"})
	if err == nil || !strings.Contains(err.Error(), "rollback succeeded") {
		t.Fatalf("error=%v, want rollback result", err)
	}
	if started != target {
		t.Fatalf("restarted=%q, want %q", started, target)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func response(r *http.Request, body []byte) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Body: io.NopCloser(bytes.NewReader(body)), ContentLength: int64(len(body)), Header: make(http.Header), Request: r}
}

type fakeSource struct{ releases []selfupdate.SourceRelease }

func (f fakeSource) ListReleases(context.Context, selfupdate.Repository) ([]selfupdate.SourceRelease, error) {
	return f.releases, nil
}
func (fakeSource) DownloadReleaseAsset(context.Context, *selfupdate.Release, int64) (io.ReadCloser, error) {
	return nil, fmt.Errorf("unused")
}

type fakeSourceRelease struct {
	tag               string
	draft, prerelease bool
	assets            []selfupdate.SourceAsset
}

func (f fakeSourceRelease) GetID() int64                        { return 1 }
func (f fakeSourceRelease) GetTagName() string                  { return f.tag }
func (f fakeSourceRelease) GetDraft() bool                      { return f.draft }
func (f fakeSourceRelease) GetPrerelease() bool                 { return f.prerelease }
func (f fakeSourceRelease) GetPublishedAt() time.Time           { return time.Time{} }
func (f fakeSourceRelease) GetReleaseNotes() string             { return "" }
func (f fakeSourceRelease) GetName() string                     { return f.tag }
func (f fakeSourceRelease) GetURL() string                      { return "https://example.test/" + f.tag }
func (f fakeSourceRelease) GetAssets() []selfupdate.SourceAsset { return f.assets }

type fakeAsset struct{ name string }

func (fakeAsset) GetID() int64                    { return 1 }
func (f fakeAsset) GetName() string               { return f.name }
func (fakeAsset) GetSize() int                    { return 1 }
func (f fakeAsset) GetBrowserDownloadURL() string { return "https://example.test/" + f.name }

func zipBytes(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	for name, data := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = w.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func zipEntries(t *testing.T, files [][2]string) []byte {
	t.Helper()
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	for _, file := range files {
		w, err := zw.Create(file[0])
		if err != nil {
			t.Fatal(err)
		}
		if _, err = io.WriteString(w, file[1]); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
