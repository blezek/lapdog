package config

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestStoreRoundTripsThroughDisk(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	s, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if s.Get() != Default() {
		t.Errorf("a fresh store = %+v, want Default()", s.Get())
	}

	next := Default()
	next.PollIntervalSeconds = 2.5
	next.Theme = "dark"
	if err := s.Set(next); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// Reopening must observe the persisted values, or a setting would survive only
	// until the process restarted.
	s2, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := s2.Get(); got.PollIntervalSeconds != 2.5 || got.Theme != "dark" {
		t.Errorf("reopened store = %+v", got)
	}
}

func TestInstallerStartupChoiceOverridesOnlyThatSetting(t *testing.T) {
	for _, tc := range []struct {
		name, marker  string
		initial, want bool
	}{
		{"declined", "0\r\n", true, false},
		{"selected", "1\r\n", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			cfg := Default()
			cfg.StartWithWindows = tc.initial
			cfg.Theme = "dark"
			if err := Save(path, cfg); err != nil {
				t.Fatal(err)
			}
			marker := filepath.Join(filepath.Dir(path), installerStartupChoice)
			if err := os.WriteFile(marker, []byte(tc.marker), 0o600); err != nil {
				t.Fatal(err)
			}
			store, err := NewStore(path)
			if err != nil {
				t.Fatal(err)
			}
			if got := store.Get(); got.StartWithWindows != tc.want || got.Theme != "dark" {
				t.Fatalf("after installer choice: %+v", got)
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatalf("marker was not consumed: %v", err)
			}
			reloaded, err := Load(path)
			if err != nil || reloaded.StartWithWindows != tc.want || reloaded.Theme != "dark" {
				t.Fatalf("saved choice=%+v err=%v", reloaded, err)
			}
		})
	}
}

func TestInstallerStartupChoiceOnFirstRun(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	marker := filepath.Join(filepath.Dir(path), installerStartupChoice)
	if err := os.WriteFile(marker, []byte("0\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if store.Get().StartWithWindows {
		t.Fatal("first-run installer choice was ignored")
	}
	saved, err := Load(path)
	if err != nil || saved.StartWithWindows {
		t.Fatalf("first-run choice was not saved: %+v err=%v", saved, err)
	}
}

func TestLoadIgnoresRemovedBrakeItSetting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"brakeItEnabled":true}`), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load legacy config: %v", err)
	}
	if err := Save(path, got); err != nil {
		t.Fatalf("Save config: %v", err)
	}
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(saved), "brakeItEnabled") {
		t.Errorf("saved config retained removed brakeItEnabled setting:\n%s", saved)
	}
}

func TestStoreRejectsInvalid(t *testing.T) {
	s, err := NewStore(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	before := s.Get()
	bad := Default()
	bad.PollIntervalSeconds = 999
	if err := s.Set(bad); err == nil {
		t.Error("Set with an out-of-range interval = nil, want an error")
	}
	if s.Get() != before {
		t.Error("a rejected Set mutated the in-memory config")
	}
}

// Subscribers are what let a poll-interval change take effect without a restart.
func TestStoreNotifiesSubscribers(t *testing.T) {
	s, err := NewStore(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var seen []float64
	s.OnChange(func(c Config) {
		mu.Lock()
		seen = append(seen, c.PollIntervalSeconds)
		mu.Unlock()
	})

	next := Default()
	next.PollIntervalSeconds = 5
	if err := s.Set(next); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 1 || seen[0] != 5 {
		t.Errorf("subscriber saw %v, want [5]", seen)
	}
}

func TestStoreDoesNotNotifyOnRejectedSet(t *testing.T) {
	s, err := NewStore(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	called := false
	s.OnChange(func(Config) { called = true })

	bad := Default()
	bad.Port = 0
	if err := s.Set(bad); err == nil {
		t.Fatal("Set with port 0 = nil, want an error")
	}
	if called {
		t.Error("a rejected Set notified subscribers")
	}
}

// A subscriber must be able to read the store without deadlocking, which means
// notification cannot happen while the write lock is held.
func TestStoreSubscriberMayReadDuringNotification(t *testing.T) {
	s, err := NewStore(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got float64
	s.OnChange(func(Config) { got = s.Get().PollIntervalSeconds })

	next := Default()
	next.PollIntervalSeconds = 3
	if err := s.Set(next); err != nil {
		t.Fatal(err)
	}
	if got != 3 {
		t.Errorf("subscriber read %v during notification, want 3", got)
	}
}

func TestStoreConcurrentAccess(t *testing.T) {
	s, err := NewStore(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			c := Default()
			c.PollIntervalSeconds = 1 + float64(n%5)
			_ = s.Set(c)
			_ = s.Get()
		}(i)
	}
	wg.Wait()
	if err := s.Get().Validate(); err != nil {
		t.Errorf("config is invalid after concurrent writes: %v", err)
	}
	// The file and live setting must describe the same last accepted change.
	// Saving before acquiring the store lock lets slower writes land after a
	// newer in-memory assignment.
	onDisk, err := Load(s.Path())
	if err != nil {
		t.Fatal(err)
	}
	if onDisk != s.Get() {
		t.Errorf("disk config %+v differs from live config %+v", onDisk, s.Get())
	}
}

func TestStoreUpdateKeepsConcurrentPartialChanges(t *testing.T) {
	s, err := NewStore(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for _, change := range []func(Config) (Config, error){
		func(c Config) (Config, error) { c.Theme = "dark"; return c, nil },
		func(c Config) (Config, error) { c.Units = "imperial"; return c, nil },
	} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, _, err := s.Update(change); err != nil {
				t.Errorf("Update: %v", err)
			}
		}()
	}
	wg.Wait()
	got := s.Get()
	if got.Theme != "dark" || got.Units != "imperial" {
		t.Fatalf("partial updates lost each other: %+v", got)
	}
	onDisk, err := Load(s.Path())
	if err != nil || onDisk != got {
		t.Fatalf("saved config=%+v live=%+v err=%v", onDisk, got, err)
	}
}

func TestStoreUpdateNotifiesInCommitOrder(t *testing.T) {
	s, err := NewStore(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	firstNotification := make(chan struct{})
	releaseFirst := make(chan struct{})
	var mu sync.Mutex
	seen := []string{}
	s.OnChange(func(c Config) {
		if c.Theme == "dark" && c.Units == "metric" {
			close(firstNotification)
			<-releaseFirst
		}
		mu.Lock()
		seen = append(seen, c.Theme+"/"+c.Units)
		mu.Unlock()
	})
	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		_, _, _ = s.Update(func(c Config) (Config, error) {
			c.Theme = "dark"
			return c, nil
		})
	}()
	select {
	case <-firstNotification:
	case <-time.After(2 * time.Second):
		t.Fatal("first notification did not start")
	}
	secondStarted := make(chan struct{})
	secondDone := make(chan struct{})
	go func() {
		close(secondStarted)
		defer close(secondDone)
		_, _, _ = s.Update(func(c Config) (Config, error) {
			c.Units = "imperial"
			return c, nil
		})
	}()
	<-secondStarted
	select {
	case <-secondDone:
		t.Fatal("later update notified before the earlier callback finished")
	case <-time.After(100 * time.Millisecond):
	}
	close(releaseFirst)
	<-firstDone
	<-secondDone
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 2 || seen[0] != "dark/metric" || seen[1] != "dark/imperial" {
		t.Fatalf("notifications out of commit order: %v", seen)
	}
}

// Autostart is a no-op off Windows and must not error, so the settings handler
// needs no platform branch.
func TestSetAutostartOffWindowsIsHarmless(t *testing.T) {
	if err := SetAutostart(true, "/nonexistent/lapdog.exe"); err != nil {
		t.Errorf("SetAutostart = %v, want nil on a non-Windows host", err)
	}
}
