package config

import (
	"path/filepath"
	"sync"
	"testing"
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
}

// Autostart is a no-op off Windows and must not error, so the settings handler
// needs no platform branch.
func TestSetAutostartOffWindowsIsHarmless(t *testing.T) {
	if err := SetAutostart(true, "/nonexistent/lapdog.exe"); err != nil {
		t.Errorf("SetAutostart = %v, want nil on a non-Windows host", err)
	}
}
