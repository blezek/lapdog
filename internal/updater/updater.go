// Package updater coordinates discovery, consent, verified staging and safe
// replacement of LapDog releases.
package updater

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	selfupdate "github.com/creativeprojects/go-selfupdate"
)

const (
	CheckInterval = 24 * time.Hour
	DeferInterval = 24 * time.Hour
	StartupDelay  = 10 * time.Second
	assetName     = "lapdog-windows-amd64.zip"
	checksumName  = "SHA256SUMS"
)

// State names are intentionally API values as well as coordinator values.
const (
	Disabled        = "disabled"
	Checking        = "checking"
	Current         = "current"
	Available       = "available"
	Deferred        = "deferred"
	Skipped         = "skipped"
	Downloading     = "downloading"
	Waiting         = "waiting"
	Applying        = "applying"
	RestartRequired = "restart-required"
	Failed          = "failed"
)

// Release is the metadata retained across restarts. Every absent fact remains a
// nil pointer in Snapshot rather than becoming a plausible empty value.
type Release struct {
	Version     string     `json:"version"`
	URL         string     `json:"url"`
	Notes       string     `json:"notes"`
	PublishedAt *time.Time `json:"publishedAt"`
	AssetURL    string     `json:"assetUrl"`
	ChecksumURL string     `json:"checksumUrl"`
}

type persisted struct {
	LastCheck     *time.Time `json:"lastCheck"`
	DeferredUntil *time.Time `json:"deferredUntil"`
	Skipped       string     `json:"skippedVersion,omitempty"`
	Accepted      string     `json:"acceptedVersion,omitempty"`
	Release       *Release   `json:"release"`
	Staged        string     `json:"stagedArtifact,omitempty"`
	Pending       bool       `json:"pendingRestart"`
	Shown         string     `json:"shownVersion,omitempty"`
	Error         string     `json:"error,omitempty"`
}

// Snapshot is returned by GET /api/update.
type Snapshot struct {
	State           string     `json:"state"`
	CurrentVersion  string     `json:"currentVersion"`
	CurrentRevision *string    `json:"currentRevision"`
	Available       *Release   `json:"availableRelease"`
	LastCheck       *time.Time `json:"lastCheck"`
	DeferredUntil   *time.Time `json:"deferredUntil"`
	SkippedVersion  *string    `json:"skippedVersion"`
	AcceptedVersion *string    `json:"acceptedVersion"`
	PromptEligible  bool       `json:"promptEligible"`
	Recording       bool       `json:"recording"`
	Reindexing      bool       `json:"reindexing"`
	RestartSafe     bool       `json:"restartSafe"`
	PendingRestart  bool       `json:"pendingRestart"`
	Error           *string    `json:"error"`
}

// Detector is the narrow release-discovery seam used by tests.
type Detector interface {
	Latest(context.Context) (*Release, error)
}

// Gate avoids tying this package to collector.Status in its tests.
type Gate interface {
	Recording() bool
	TryQuiesce() bool
	ResumeRecording()
}

type Launcher func(path string, args ...string) error

type Options struct {
	Version, Revision   string
	GOOS, GOARCH        string
	DataDir, Executable string
	Detector            Detector
	HTTPClient          *http.Client
	Gate                Gate
	Reindexing          func() bool
	Launch              Launcher
	Shutdown            func()
	Now                 func() time.Time
	Log                 *slog.Logger
}

// Coordinator owns updater operations and their persisted state.
type Coordinator struct {
	mu                   sync.Mutex
	op                   sync.Mutex
	p                    persisted
	state, lastError     string
	opts                 Options
	statePath, updateDir string
	enabled              bool
	completedCleanup     bool
}

func New(opts Options) (*Coordinator, error) {
	if opts.GOOS == "" {
		opts.GOOS = runtime.GOOS
	}
	if opts.GOARCH == "" {
		opts.GOARCH = runtime.GOARCH
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.HTTPClient == nil {
		opts.HTTPClient = &http.Client{Timeout: 30 * time.Second}
	}
	if opts.Log == nil {
		opts.Log = slog.Default()
	}
	u := &Coordinator{opts: opts, updateDir: filepath.Join(opts.DataDir, "update")}
	u.statePath = filepath.Join(u.updateDir, "state.json")
	u.enabled = opts.Version != "dev" && opts.GOOS == "windows" && opts.GOARCH == "amd64"
	u.state = Disabled
	if u.enabled {
		if err := u.load(); err != nil {
			u.lastError = err.Error()
			u.state = Failed
		} else {
			u.lastError = u.p.Error
			u.state = Current
			// Pending only describes the previous process. If the old version reached
			// normal startup again, retry the durable acceptance.
			if u.p.Pending && !equalVersion(u.p.Accepted, opts.Version) {
				u.p.Pending = false
			}
			if u.p.Release != nil && newer(u.p.Release.Version, opts.Version) {
				u.state = u.releaseStateLocked(opts.Now())
			}
		}
	}
	// A new build reaching normal startup proves the handoff completed.
	if u.enabled && u.p.Accepted != "" && equalVersion(u.p.Accepted, opts.Version) {
		u.completedCleanup = true
		_ = os.Remove(u.p.Staged)
		_ = os.Remove(filepath.Join(u.updateDir, "lapdog.backup.exe"))
		u.p = persisted{LastCheck: u.p.LastCheck}
		u.lastError = ""
		u.state = Current
		if err := u.saveLocked(); err != nil {
			u.lastError = err.Error()
			u.state = Failed
		}
	}
	if u.enabled && opts.Detector == nil {
		d, err := newGitHubDetector()
		if err != nil {
			u.state = Failed
			u.lastError = err.Error()
		} else {
			u.opts.Detector = d
		}
	}
	if opts.Launch == nil {
		u.opts.Launch = launchProcess
	}
	return u, nil
}

// Start checks shortly after startup and every day thereafter. It also resumes
// accepted work after an application restart without asking again.
func (u *Coordinator) Start(ctx context.Context) {
	if !u.enabled {
		return
	}
	go func() {
		// The handoff helper may still have its own staged executable open when
		// the replacement reaches normal startup. Retry fixed, updater-owned files
		// after the helper has had time to exit.
		if u.completedCleanup {
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
				_ = os.Remove(filepath.Join(u.updateDir, "staged-lapdog.exe"))
				_ = os.Remove(filepath.Join(u.updateDir, "lapdog.backup.exe"))
			}
		}
		t := time.NewTimer(StartupDelay)
		defer t.Stop()
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		u.resume(ctx)
		u.mu.Lock()
		due := checkDue(u.p.LastCheck, u.opts.Now())
		u.mu.Unlock()
		if due {
			_ = u.check(ctx, false)
		}
		daily := time.NewTicker(CheckInterval)
		retry := time.NewTicker(2 * time.Second)
		defer daily.Stop()
		defer retry.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-daily.C:
				_ = u.check(ctx, false)
			case <-retry.C:
				u.mu.Lock()
				accepted := u.p.Accepted != "" && u.state == Waiting
				u.mu.Unlock()
				if accepted {
					u.resume(ctx)
				}
			}
		}
	}()
}

func checkDue(last *time.Time, now time.Time) bool {
	return last == nil || now.Sub(*last) >= CheckInterval
}

func (u *Coordinator) Snapshot() Snapshot {
	u.mu.Lock()
	defer u.mu.Unlock()
	now := u.opts.Now()
	recording := u.opts.Gate != nil && u.opts.Gate.Recording()
	reindexing := u.opts.Reindexing != nil && u.opts.Reindexing()
	s := Snapshot{State: u.state, CurrentVersion: u.opts.Version, Available: cloneRelease(u.p.Release),
		LastCheck: cloneTime(u.p.LastCheck), DeferredUntil: cloneTime(u.p.DeferredUntil),
		PromptEligible: u.promptEligibleLocked(now), Recording: recording, Reindexing: reindexing,
		RestartSafe: !recording && !reindexing, PendingRestart: u.p.Pending}
	if u.opts.Revision != "" && u.opts.Revision != "unknown" {
		r := u.opts.Revision
		s.CurrentRevision = &r
	}
	if u.p.Skipped != "" {
		v := u.p.Skipped
		s.SkippedVersion = &v
	}
	if u.p.Accepted != "" {
		v := u.p.Accepted
		s.AcceptedVersion = &v
	}
	if u.lastError != "" {
		e := u.lastError
		s.Error = &e
	}
	return s
}

// Check refreshes metadata in response to an explicit user action.
func (u *Coordinator) Check(ctx context.Context) error { return u.check(ctx, true) }

func (u *Coordinator) check(ctx context.Context, manual bool) error {
	if !u.enabled {
		return errors.New("updates are disabled for this build")
	}
	u.op.Lock()
	defer u.op.Unlock()
	u.mu.Lock()
	if u.opts.Detector == nil {
		u.state = Failed
		u.lastError = "release discovery is unavailable"
		u.mu.Unlock()
		return errors.New("release discovery is unavailable")
	}
	u.state = Checking
	u.lastError = ""
	u.mu.Unlock()
	checkCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	rel, err := u.opts.Detector.Latest(checkCtx)
	now := u.opts.Now().UTC()
	u.mu.Lock()
	defer u.mu.Unlock()
	u.p.LastCheck = &now
	if err != nil {
		message := actionable(err)
		if manual {
			u.state = Failed
			u.lastError = message
		} else {
			u.state = u.releaseStateLocked(now)
		}
		_ = u.saveLocked()
		return errors.New(message)
	}
	u.p.Release = cloneRelease(rel)
	if rel == nil || !newer(rel.Version, u.opts.Version) {
		u.state = Current
		u.p.Release = nil
	} else {
		if u.p.Skipped != "" && newer(rel.Version, u.p.Skipped) {
			u.p.Skipped = ""
		}
		u.state = u.releaseStateLocked(now)
	}
	return u.saveLocked()
}

// Action records a user decision. Install returns after consent is durable and
// performs the network work asynchronously.
func (u *Coordinator) Action(ctx context.Context, action string) error {
	u.mu.Lock()
	if !u.enabled {
		u.mu.Unlock()
		return errors.New("updates are disabled for this build")
	}
	rel := cloneRelease(u.p.Release)
	now := u.opts.Now().UTC()
	switch action {
	case "shown":
		if rel != nil {
			u.p.Shown = rel.Version
		}
		err := u.saveLocked()
		u.mu.Unlock()
		return err
	case "later":
		if rel == nil {
			u.mu.Unlock()
			return errors.New("no update is available")
		}
		until := now.Add(DeferInterval)
		u.p.DeferredUntil = &until
		u.state = Deferred
		err := u.saveLocked()
		u.mu.Unlock()
		return err
	case "skip":
		if rel == nil {
			u.mu.Unlock()
			return errors.New("no update is available")
		}
		u.p.Skipped = rel.Version
		u.p.Accepted = ""
		u.p.Staged = ""
		u.state = Skipped
		err := u.saveLocked()
		u.mu.Unlock()
		return err
	case "install":
		if rel == nil || !newer(rel.Version, u.opts.Version) {
			u.mu.Unlock()
			return errors.New("no update is available")
		}
		u.p.Accepted = rel.Version
		u.p.DeferredUntil = nil
		u.state = Downloading
		u.lastError = ""
		if err := u.saveLocked(); err != nil {
			u.mu.Unlock()
			return err
		}
		u.mu.Unlock()
		go u.resume(context.WithoutCancel(ctx))
		return nil
	default:
		u.mu.Unlock()
		return fmt.Errorf("unknown update action %q", action)
	}
}

func (u *Coordinator) resume(ctx context.Context) {
	u.op.Lock()
	defer u.op.Unlock()
	u.mu.Lock()
	if u.p.Accepted == "" || u.p.Release == nil || u.p.Pending {
		u.mu.Unlock()
		return
	}
	rel := *u.p.Release
	staged := u.p.Staged
	u.mu.Unlock()
	if staged == "" {
		u.setState(Downloading, "")
		path, err := stage(ctx, u.opts.HTTPClient, u.updateDir, rel)
		if err != nil {
			u.fail("download or verification failed: " + actionable(err))
			return
		}
		u.mu.Lock()
		u.p.Staged = path
		u.state = Waiting
		_ = u.saveLocked()
		u.mu.Unlock()
		staged = path
	}
	if u.opts.Reindexing != nil && u.opts.Reindexing() {
		u.setState(Waiting, "")
		return
	}
	if u.opts.Gate != nil && !u.opts.Gate.TryQuiesce() {
		u.setState(Waiting, "")
		return
	}
	u.setState(Applying, "")
	backup := filepath.Join(u.updateDir, "lapdog.backup.exe")
	args := []string{"--wait-for-pid", fmt.Sprint(os.Getpid()), "--replace", u.opts.Executable, "--backup", backup, "--update-state", u.statePath}
	if err := u.opts.Launch(staged, args...); err != nil {
		if u.opts.Gate != nil {
			u.opts.Gate.ResumeRecording()
		}
		u.mu.Lock()
		u.p.Pending = true
		u.state = RestartRequired
		u.lastError = "could not launch the replacement; restart LapDog to try again: " + err.Error()
		_ = u.saveLocked()
		u.mu.Unlock()
		return
	}
	u.mu.Lock()
	u.p.Pending = true
	_ = u.saveLocked()
	u.mu.Unlock()
	if u.opts.Shutdown != nil {
		u.opts.Shutdown()
	}
}

func (u *Coordinator) releaseStateLocked(now time.Time) string {
	if u.p.Release == nil {
		return Current
	}
	if u.p.Accepted == u.p.Release.Version {
		if u.p.Staged != "" {
			return Waiting
		}
		return Downloading
	}
	if u.p.Skipped == u.p.Release.Version {
		return Skipped
	}
	if u.p.DeferredUntil != nil && now.Before(*u.p.DeferredUntil) {
		return Deferred
	}
	return Available
}
func (u *Coordinator) promptEligibleLocked(now time.Time) bool {
	return u.state == Available && u.p.Release != nil && u.p.Shown != u.p.Release.Version &&
		(u.p.DeferredUntil == nil || !now.Before(*u.p.DeferredUntil)) && u.p.Skipped != u.p.Release.Version
}
func (u *Coordinator) setState(state, message string) {
	u.mu.Lock()
	u.state = state
	u.lastError = message
	_ = u.saveLocked()
	u.mu.Unlock()
}
func (u *Coordinator) fail(message string) { u.setState(Failed, message) }

func (u *Coordinator) load() error {
	b, err := os.ReadFile(u.statePath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("updater: read state: %w", err)
	}
	if err = json.Unmarshal(b, &u.p); err != nil {
		return fmt.Errorf("updater: decode state: %w", err)
	}
	return nil
}
func (u *Coordinator) saveLocked() error {
	u.p.Error = u.lastError
	return atomicJSON(u.statePath, u.p)
}

func atomicJSON(path string, value any) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("updater: create state directory: %w", err)
	}
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("updater: encode state: %w", err)
	}
	b = append(b, '\n')
	f, err := os.CreateTemp(dir, ".state-*.tmp")
	if err != nil {
		return fmt.Errorf("updater: create state temp file: %w", err)
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err = f.Chmod(0o600); err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("updater: write state: %w", err)
	}
	if err = os.Rename(tmp, path); err != nil {
		return fmt.Errorf("updater: replace state: %w", err)
	}
	return nil
}

func cloneRelease(r *Release) *Release {
	if r == nil {
		return nil
	}
	c := *r
	c.PublishedAt = cloneTime(r.PublishedAt)
	return &c
}
func cloneTime(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	c := *t
	return &c
}
func actionable(err error) string {
	s := err.Error()
	if strings.Contains(strings.ToLower(s), "rate limit") {
		return "GitHub rate limit reached; try again later"
	}
	return s
}

type githubDetector struct{ up *selfupdate.Updater }

func newGitHubDetector() (Detector, error) {
	up, err := selfupdate.NewUpdater(selfupdate.Config{OS: "windows", Arch: "amd64", Filters: []string{`^lapdog-windows-amd64\.zip$`}, Validator: &selfupdate.ChecksumValidator{UniqueFilename: checksumName}})
	if err != nil {
		return nil, err
	}
	return &githubDetector{up: up}, nil
}
func (d *githubDetector) Latest(ctx context.Context) (*Release, error) {
	r, found, err := d.up.DetectLatest(ctx, selfupdate.NewRepositorySlug("blezek", "lapdog"))
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}
	var published *time.Time
	if !r.PublishedAt.IsZero() {
		value := r.PublishedAt
		published = &value
	}
	return &Release{Version: "v" + r.Version(), URL: r.URL, Notes: r.ReleaseNotes, PublishedAt: published, AssetURL: r.AssetURL, ChecksumURL: r.ValidationAssetURL}, nil
}
