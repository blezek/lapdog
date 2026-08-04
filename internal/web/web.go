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
	"strings"

	"github.com/blezek/lapdog/internal/ui/icons"
)

//go:embed all:dist
var distFS embed.FS

// ErrNoBundle indicates the frontend bundle is missing from the binary.
var ErrNoBundle = errors.New("web: no frontend bundle embedded")

// FS returns the built frontend rooted at dist, so paths are served without the
// dist prefix.
func FS() (fs.FS, error) {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		return nil, fmt.Errorf("web: %w: %v", ErrNoBundle, err)
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
	f, err := sub.Open("index.html")
	if err != nil {
		return fmt.Errorf("web: %w: index.html is not present", ErrNoBundle)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("web: cannot stat index.html: %w", err)
	}
	if info.Size() == 0 {
		return fmt.Errorf("web: %w: index.html is empty", ErrNoBundle)
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
		// Serve the file when it exists, otherwise hand the route to the SPA.
		if clean != "/" {
			if f, err := sub.Open(strings.TrimPrefix(clean, "/")); err == nil {
				f.Close()
				ui.ServeHTTP(w, r)
				return
			}
		}
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/"
		ui.ServeHTTP(w, r2)
	})
	return mux, nil
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
