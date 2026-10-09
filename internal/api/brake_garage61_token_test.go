package api

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blezek/lapdog/internal/config"
)

func TestGarageTokenAPIStoresSecretOnlyInDataDirectory(t *testing.T) {
	t.Setenv("GARAGE61_TOKEN", "")
	_, st, cfg := newTestServer(t)
	srv := New(st, fakeStatus{}, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	h, err := srv.Handler()
	if err != nil {
		t.Fatal(err)
	}
	request := func(method, body string) *httptest.ResponseRecorder {
		t.Helper()
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, jsonRequest(method, "/api/brake-it/garage61/token", strings.NewReader(body)))
		return rec
	}
	if rec := request("GET", ""); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"source":"none"`) {
		t.Fatalf("missing token status: %d %s", rec.Code, rec.Body.String())
	}
	blocked := jsonRequest("PUT", "/api/brake-it/garage61/token", strings.NewReader(`{"token":"cross-site-secret"}`))
	blocked.Header.Set("Origin", "https://other.example")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, blocked)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("cross-site token write: %d", rec.Code)
	}
	if rec := request("PUT", `{"token":"two words"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid token write: %d", rec.Code)
	}
	srv.garageState.loaded = true
	if rec := request("PUT", `{"token":"test-secret-value"}`); rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "test-secret-value") {
		t.Fatalf("token write response: %d %s", rec.Code, rec.Body.String())
	}
	if srv.garageState.loaded {
		t.Fatal("token change did not invalidate the cached catalog")
	}
	select {
	case <-srv.garageWake:
	default:
		t.Fatal("saving a token did not wake the Garage61 queue")
	}
	path := config.Garage61TokenPath(filepath.Dir(st.Path()))
	data, err := os.ReadFile(path)
	if err != nil || strings.TrimSpace(string(data)) != "test-secret-value" {
		t.Fatalf("token file missing or incorrect: %v", err)
	}
	if !srv.garage.Configured(context.Background()) {
		t.Fatal("client did not pick up the new file token")
	}
	if rec := request("GET", ""); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"source":"file"`) || strings.Contains(rec.Body.String(), "test-secret-value") || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("token status exposed secret or was cacheable: %d %s", rec.Code, rec.Body.String())
	}
	if rec := request("DELETE", `{}`); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"source":"none"`) {
		t.Fatalf("token delete: %d %s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("token file remains after removal: %v", err)
	}
	if srv.garage.Configured(context.Background()) {
		t.Fatal("client remained configured after token removal")
	}
}
