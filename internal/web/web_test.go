package web

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The whole interface must live inside the binary. If this fails, the release is
// not a single self-contained executable.
func TestCheckFindsTheEmbeddedBundle(t *testing.T) {
	if err := Check(); err != nil {
		t.Fatalf("Check: %v", err)
	}
}

func TestServesIndexAtRoot(t *testing.T) {
	h, err := Handler()
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, "LapDog") {
		t.Errorf("root did not serve the interface:\n%s", body)
	}
}

// A page reload on a client-side route must return the app shell, not a 404, or
// refreshing any page but the root would break.
func TestUnknownRouteFallsBackToTheAppShell(t *testing.T) {
	h, err := Handler()
	if err != nil {
		t.Fatal(err)
	}
	for _, route := range []string{"/sessions", "/laps", "/settings", "/sessions/1042"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, route, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("%s: status = %d, want 200 so client-side routing survives a reload", route, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "LapDog") {
			t.Errorf("%s did not return the app shell", route)
		}
	}
}

// API paths must not fall through to index.html. A typo in an endpoint should
// read as a 404, not as a page of HTML.
func TestAPIPathsAreNotSwallowed(t *testing.T) {
	h, err := Handler()
	if err != nil {
		t.Fatal(err)
	}
	for _, route := range []string{"/api", "/api/", "/api/nope", "/api/status"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, route, nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404 from the web handler", route, rec.Code)
		}
		if strings.Contains(rec.Body.String(), "<html") {
			t.Errorf("%s returned HTML; API paths must not fall back to the app shell", route)
		}
	}
}

// Icons are served from the same executable, so the frontend needs no second
// copy and there is one licence to account for.
func TestServesEmbeddedIcons(t *testing.T) {
	h, err := Handler()
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/icons/racing-helmet.svg", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "<svg") || !strings.Contains(body, "mdi-racing-helmet") {
		t.Errorf("icon response is not the expected SVG:\n%s", body)
	}
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Errorf("Cache-Control = %q; icons ship with the binary and can be cached indefinitely", cc)
	}
}

func TestMissingIconIs404(t *testing.T) {
	h, err := Handler()
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/icons/no-such-icon.svg", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

// A directory traversal attempt must not escape the embedded filesystem. The
// server is loopback-only, but the check costs nothing.
func TestNoDirectoryTraversal(t *testing.T) {
	h, err := Handler()
	if err != nil {
		t.Fatal(err)
	}
	for _, route := range []string{
		"/icons/../../go.mod",
		"/../go.mod",
		"/icons/..%2f..%2fgo.mod",
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, route, nil))
		if strings.Contains(rec.Body.String(), "module github.com/blezek/lapdog") {
			t.Errorf("%s escaped the embedded filesystem and read go.mod", route)
		}
	}
}

// Regression: a missing asset returned the app shell with a 200, because the SPA
// fallback caught every unmatched path. After an upgrade replaced the hashed
// bundle, a browser holding a stale reference received index.html and then failed
// to parse HTML as JavaScript — an error that says nothing about the real cause.
//
// Only extensionless client-side routes get the shell; anything that looks like a
// file must 404.
func TestMissingAssetIs404NotTheAppShell(t *testing.T) {
	h, err := Handler()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{
		"/assets/index-DOESNOTEXIST.js",
		"/assets/index-DOESNOTEXIST.css",
		"/favicon.ico",
		"/nested/thing.png",
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404 — a missing asset must not return the app shell", p, rec.Code)
		}
		if strings.Contains(rec.Body.String(), "<div id=\"root\">") {
			t.Errorf("%s returned the app shell; a browser would try to parse HTML as its asset", p)
		}
	}
}

// Client-side routes still get the shell, including nested ones, so a reload on
// any page keeps working.
func TestExtensionlessRoutesStillGetTheShell(t *testing.T) {
	h, err := Handler()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"/sessions", "/sessions/1042", "/laps", "/deeply/nested/route"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("%s: status = %d, want 200", p, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "LapDog") {
			t.Errorf("%s did not return the app shell", p)
		}
	}
}

// The real bundle must still be served, so the fix cannot have broken asset
// delivery.
func TestRealAssetsAreServed(t *testing.T) {
	sub, err := FS()
	if err != nil {
		t.Fatal(err)
	}
	entries, err := fs.ReadDir(sub, "assets")
	if err != nil {
		t.Skip("no assets directory in this build")
	}
	h, err := Handler()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("assets directory is empty; the frontend bundle is missing")
	}
	for _, e := range entries {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/"+e.Name(), nil))
		if rec.Code != http.StatusOK {
			t.Errorf("/assets/%s: status = %d, want 200", e.Name(), rec.Code)
		}
	}
}
