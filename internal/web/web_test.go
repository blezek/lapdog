package web

import (
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

// The whole interface must live inside the binary. If this fails, the release is
// not a single self-contained executable.
func TestCheckFindsTheEmbeddedBundle(t *testing.T) {
	if err := Check(); err != nil {
		t.Fatalf("Check: %v", err)
	}
}

// The bundle really is complete, not merely present.
//
// This is the check that was missing. index.html was tracked in git while the
// hashed assets it loads were ignored, so the binary built, started, passed a
// bundle check that only looked for index.html, and served a blank page. Reading
// the asset names back out of the HTML is what makes the gap visible.
func TestEveryAssetIndexReferencesIsEmbedded(t *testing.T) {
	sub, err := FS()
	if err != nil {
		t.Fatalf("FS: %v", err)
	}
	html, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		t.Fatalf("read index.html: %v", err)
	}

	refs := assetRef.FindAllStringSubmatch(string(html), -1)
	if len(refs) == 0 {
		t.Fatal("index.html references no /assets/ files; expected the bundled script and stylesheet")
	}
	for _, m := range refs {
		name := strings.TrimPrefix(m[1], "/")
		data, err := fs.ReadFile(sub, name)
		if err != nil {
			t.Errorf("index.html references %s, which is not embedded: %v", m[1], err)
			continue
		}
		if len(data) == 0 {
			t.Errorf("embedded asset %s is empty", m[1])
		}
	}
}

func TestCheckBundleRejectsAnIncompleteBundle(t *testing.T) {
	const html = `<!DOCTYPE html><html><head>` +
		`<script type="module" src="/assets/index-abc123.js"></script>` +
		`<link rel="stylesheet" href="/assets/index-def456.css">` +
		`</head><body><div id="root"></div></body></html>`

	cases := []struct {
		name string
		fsys fs.FS
		want string
	}{
		{
			name: "index.html without its assets",
			// Exactly the state a bad .gitignore produced.
			fsys: fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte(html)}},
			want: "index-abc123.js",
		},
		{
			name: "stylesheet missing",
			fsys: fstest.MapFS{
				"index.html":             &fstest.MapFile{Data: []byte(html)},
				"assets/index-abc123.js": &fstest.MapFile{Data: []byte("console.log(1)")},
			},
			want: "index-def456.css",
		},
		{
			name: "no index.html at all",
			fsys: fstest.MapFS{},
			want: "index.html is not present",
		},
		{
			name: "empty index.html",
			fsys: fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte{}}},
			want: "index.html is empty",
		},
		{
			name: "index.html loading nothing",
			fsys: fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<html></html>")}},
			want: "loads no bundle assets",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := checkBundle(c.fsys)
			if err == nil {
				t.Fatal("checkBundle accepted an unusable bundle")
			}
			if !errors.Is(err, ErrNoBundle) {
				t.Errorf("error is not ErrNoBundle: %v", err)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error %q does not mention %q", err, c.want)
			}
		})
	}
}

func TestCheckBundleAcceptsACompleteBundle(t *testing.T) {
	fsys := fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte(
			`<html><head><script src="/assets/app.js"></script>` +
				`<link href="/assets/app.css"></head><body></body></html>`)},
		"assets/app.js":  &fstest.MapFile{Data: []byte("console.log(1)")},
		"assets/app.css": &fstest.MapFile{Data: []byte("body{}")},
	}
	if err := checkBundle(fsys); err != nil {
		t.Fatalf("checkBundle rejected a complete bundle: %v", err)
	}
}

// The icon link in index.html is served by the icon handler, not from dist, so it
// must not be mistaken for a missing bundle asset.
func TestCheckBundleIgnoresNonAssetLinks(t *testing.T) {
	fsys := fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte(
			`<html><head><link rel="icon" href="/icons/racing-helmet.svg">` +
				`<script src="/assets/app.js"></script></head><body></body></html>`)},
		"assets/app.js": &fstest.MapFile{Data: []byte("console.log(1)")},
	}
	if err := checkBundle(fsys); err != nil {
		t.Fatalf("checkBundle rejected a bundle over a non-asset link: %v", err)
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
