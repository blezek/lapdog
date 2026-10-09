package api

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHostGuardRejectsReboundHostsForReadsAndWrites(t *testing.T) {
	h, st, cfg := newTestServer(t)
	guarded := New(st, nil, cfg, slog.Default()).rejectUntrustedHost(h, "47047")
	for _, method := range []string{http.MethodGet, http.MethodPut} {
		r := httptest.NewRequest(method, "http://127.0.0.1:47047/api/status", nil)
		r.Host = "attacker.example:47047"
		rec := httptest.NewRecorder()
		guarded.ServeHTTP(rec, r)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s with rebound Host returned %d, want 403", method, rec.Code)
		}
	}
	for _, host := range []string{"127.0.0.1:47047", "localhost:47047"} {
		r := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:47047/api/status", nil)
		r.Host = host
		rec := httptest.NewRecorder()
		guarded.ServeHTTP(rec, r)
		if rec.Code != http.StatusOK {
			t.Errorf("GET with Host %q returned %d, want 200", host, rec.Code)
		}
	}
}

func TestSettingsRejectsTrailingJSONAndOversizedBody(t *testing.T) {
	h, _, _ := newTestServer(t)
	for _, tc := range []struct {
		name, body string
		want       int
	}{
		{name: "trailing value", body: `{} {}`, want: http.StatusBadRequest},
		{name: "oversized", body: `{"theme":"` + strings.Repeat("x", brakeRequestLimit) + `"}`, want: http.StatusRequestEntityTooLarge},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := jsonRequest(http.MethodPut, "/api/settings", strings.NewReader(tc.body))
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, r)
			if rec.Code != tc.want {
				t.Errorf("settings status = %d, want %d", rec.Code, tc.want)
			}
		})
	}
}

func TestMutationsRequireJSONAndRejectCrossSite(t *testing.T) {
	h, _, _ := newTestServer(t)
	missing := httptest.NewRecorder()
	h.ServeHTTP(missing, httptest.NewRequest(http.MethodPut, "/api/settings", strings.NewReader(`{}`)))
	if missing.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("missing content type status=%d, want 415", missing.Code)
	}
	cross := jsonRequest(http.MethodPut, "/api/settings", strings.NewReader(`{}`))
	cross.Header.Set("Sec-Fetch-Site", "cross-site")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, cross)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("cross-site status=%d, want 403", rec.Code)
	}
	wrongScheme := jsonRequest(http.MethodPut, "/api/settings", strings.NewReader(`{}`))
	wrongScheme.Header.Set("Origin", "https://"+wrongScheme.Host)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, wrongScheme)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("wrong-scheme origin status=%d, want 403", rec.Code)
	}
}

func TestBrakeMutationsUseTheSameOriginGuard(t *testing.T) {
	h, _, _ := newTestServer(t)
	r := jsonRequest(http.MethodPost, "/api/brake-it/scenarios", strings.NewReader(`{}`))
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("cross-site Brake-it mutation status=%d, want 403", rec.Code)
	}
}
