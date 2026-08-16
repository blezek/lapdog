package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

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
