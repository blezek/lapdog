// Package webtest gates tests that need the built frontend bundle.
//
// The bundle is generated rather than committed, so a clone without a Node
// toolchain has no interface embedded. Tests that serve the interface would fail
// there over something the developer never broke, so they skip instead — and
// anything that needs the bundle should say so through RequireBundle rather than
// by asserting on a blank response.
//
// It is a separate package because internal/web's own tests are in package web
// and importing this from there would be an import cycle.
package webtest

import (
	"os"
	"testing"

	"github.com/blezek/lapdog/internal/web"
)

// RequireBundle skips the test unless a usable frontend bundle is embedded.
//
// A skip is only safe if something guarantees it does not apply where it matters.
// CI builds the bundle first and sets web.RequireBundleEnv, which turns the skip into
// a failure; otherwise the tests that prove the interface ships inside the binary
// would quietly disable themselves exactly when the bundle went missing, which is
// the failure they exist to catch. `make test-ci` sets it too.
func RequireBundle(t testing.TB) {
	t.Helper()
	err := web.Check()
	if err == nil {
		return
	}
	if os.Getenv(web.RequireBundleEnv) != "" {
		t.Fatalf("%s is set, so the bundle must be usable, but: %v", web.RequireBundleEnv, err)
	}
	t.Skipf("no frontend bundle (run `make ui`): %v", err)
}
