package api

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/blezek/lapdog/internal/config"
	"github.com/blezek/lapdog/internal/store"
	"github.com/blezek/lapdog/internal/updater"
)

type fakeUpdates struct {
	mu      sync.Mutex
	s       updater.Snapshot
	checked int
	actions []string
}

func TestUpdateEndpointIsExplicitlyDisabledWithoutCoordinator(t *testing.T) {
	h, _, _ := newTestServer(t)
	rec := get(t, h, "/api/update", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"state":"disabled"`) {
		t.Fatalf("body=%s", rec.Body.String())
	}
}

func (f *fakeUpdates) Snapshot() updater.Snapshot { f.mu.Lock(); defer f.mu.Unlock(); return f.s }
func (f *fakeUpdates) Check(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.checked++
	f.s.State = updater.Current
	return nil
}
func (f *fakeUpdates) Action(_ context.Context, action string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.actions = append(f.actions, action)
	return nil
}

func TestUpdateEndpointsExposeNullableFactsAndActions(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "lapdog.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	updates := &fakeUpdates{s: updater.Snapshot{State: updater.Available, CurrentVersion: "v1.0.0", CurrentRevision: nil, Available: &updater.Release{Version: "v1.1.0"}}}
	srv := New(st, nil, &fakeConfig{c: config.Default()}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	srv.SetUpdater(updates)
	h, err := srv.Handler()
	if err != nil {
		t.Fatal(err)
	}
	rec := get(t, h, "/api/update", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status=%d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"currentRevision":null`) || !strings.Contains(body, `"lastCheck":null`) || !strings.Contains(body, `"publishedAt":null`) {
		t.Fatalf("nullable fields missing: %s", body)
	}
	check := httptest.NewRecorder()
	h.ServeHTTP(check, jsonRequest(http.MethodPost, "/api/update/check", strings.NewReader(`{}`)))
	if check.Code != http.StatusOK || updates.checked != 1 {
		t.Fatalf("check status=%d calls=%d", check.Code, updates.checked)
	}
	action := httptest.NewRecorder()
	h.ServeHTTP(action, jsonRequest(http.MethodPost, "/api/update/action", strings.NewReader(`{"action":"later"}`)))
	if action.Code != http.StatusOK || len(updates.actions) != 1 || updates.actions[0] != "later" {
		t.Fatalf("action status=%d actions=%v", action.Code, updates.actions)
	}
}
