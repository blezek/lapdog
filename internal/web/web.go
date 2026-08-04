// Package web serves LapDog's user interface out of the executable.
//
// Everything the browser needs — HTML, JavaScript, CSS and icons — is compiled
// into the binary with embed. A release is therefore a single .exe with no
// sidecar files, no install-time asset extraction and nothing to go missing on a
// user's machine. The build fails rather than shipping a blank interface if the
// frontend bundle is absent; see Check.
package web

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"path"
	"regexp"
	"strings"

	"github.com/blezek/lapdog/internal/ui/icons"
)

//go:embed all:dist
var distFS embed.FS

// ErrNoBundle indicates the frontend bundle is missing from the binary.
var ErrNoBundle = errors.New("web: no frontend bundle embedded")

// RequireBundleEnv names the variable that makes a missing bundle a test failure
// rather than a skipped test.
//
// The bundle is generated rather than committed, so tests that serve the interface
// skip on a clone that has not run the frontend build. CI and `make test-ci` set
// this so that the skip cannot be reached where it would hide a real breakage. It
// is declared here, beside Check, because it is part of the same contract; the
// helpers that read it are in webtest and in this package's own tests.
const RequireBundleEnv = "LAPDOG_REQUIRE_BUNDLE"

// FS returns the built frontend rooted at dist, so paths are served without the
// dist prefix.
func FS() (fs.FS, error) {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNoBundle, err)
	}
	return sub, nil
}

// Check reports whether a usable interface is embedded.
//
// It is called at start-up so a build that forgot to run the frontend bundler
// fails loudly instead of serving an empty page that looks like a runtime fault.
func Check() error {
	sub, err := FS()
	if err != nil {
		return err
	}
	return checkBundle(sub)
}

// assetRef matches the hashed bundle files index.html loads.
//
// The bundler rewrites these names on every build, so they cannot be checked
// against a fixed list — they have to be read back out of the HTML that
// references them.
var assetRef = regexp.MustCompile(`(?:src|href)="(/assets/[^"]+)"`)

// checkBundle verifies the interface is present and complete.
//
// Checking that index.html exists is not enough, and its absence is not the
// failure that actually happened. A stray "dist/" line in .gitignore matched
// internal/web/dist as well as the intended build directory, so a clone held
// index.html — added before that rule — without the hashed assets it loads. The
// binary then built and started cleanly and served a blank page, because the
// only thing verified was the one file that happened to be tracked. So every
// asset the HTML references is now confirmed to be embedded too.
func checkBundle(sub fs.FS) error {
	html, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		return fmt.Errorf("%w: index.html is not present", ErrNoBundle)
	}
	if len(html) == 0 {
		return fmt.Errorf("%w: index.html is empty", ErrNoBundle)
	}

	refs := assetRef.FindAllStringSubmatch(string(html), -1)
	if len(refs) == 0 {
		return fmt.Errorf("%w: index.html loads no bundle assets", ErrNoBundle)
	}
	for _, m := range refs {
		f, err := sub.Open(strings.TrimPrefix(m[1], "/"))
		if err != nil {
			return fmt.Errorf(
				"%w: index.html references %s, which is not embedded "+
					"(run `make ui`, and check it is not being ignored by git)",
				ErrNoBundle, m[1],
			)
		}
		f.Close()
	}
	return nil
}

// Handler serves the interface and the icon set.
//
// Unknown paths fall back to index.html so the frontend's client-side routing
// works on a page reload. Paths under /api are explicitly not handled here: the
// API mux owns those, and letting them fall through to index.html would turn a
// typo in an endpoint into a confusing block of HTML.
func Handler() (http.Handler, error) {
	sub, err := FS()
	if err != nil {
		return nil, err
	}

	ui := http.FileServer(http.FS(sub))
	iconSrv := http.StripPrefix("/icons/", http.FileServer(http.FS(icons.FS())))

	mux := http.NewServeMux()

	// Icons are served from the same binary rather than bundled a second time
	// into the frontend, so there is one copy of each and one licence to track.
	mux.Handle("/icons/", cacheImmutable(iconSrv))

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		clean := path.Clean("/" + r.URL.Path)
		if strings.HasPrefix(clean, "/api") {
			http.NotFound(w, r)
			return
		}
		// Serve the file when it exists.
		if clean != "/" {
			if f, err := sub.Open(strings.TrimPrefix(clean, "/")); err == nil {
				f.Close()
				ui.ServeHTTP(w, r)
				return
			}
		}
		// A missing asset must be a 404, not the app shell.
		//
		// Falling back for everything meant a browser holding a stale hashed asset
		// reference — after an upgrade replaced the bundle — received index.html
		// with a 200 and then failed to parse HTML as JavaScript. The resulting
		// console error says nothing about the real cause. Only client-side routes,
		// which carry no file extension, get the shell.
		if looksLikeFile(clean) {
			http.NotFound(w, r)
			return
		}
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/"
		ui.ServeHTTP(w, r2)
	})
	return mux, nil
}

// looksLikeFile reports whether a path is asking for an asset rather than a
// client-side route.
//
// Routes are path segments without an extension ("/sessions", "/sessions/1042");
// assets carry one ("/assets/index-abc123.js", "/favicon.ico").
func looksLikeFile(clean string) bool {
	return path.Ext(clean) != ""
}

// cacheImmutable marks a response as safe to cache indefinitely.
//
// Icons are versioned by the binary they ship in, so they can never change
// without the executable changing. Telling the browser that avoids re-fetching
// them on every navigation.
func cacheImmutable(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		h.ServeHTTP(w, r)
	})
}
