// Package version reports the application version, set at build time.
package version

import "fmt"

// Version is the application version, overridden at build time with
// -ldflags "-X github.com/blezek/lapdog/internal/version.Version=x.y.z".
var Version = "dev"

// Revision is the source commit used for the build. It is empty when the build
// system cannot provide one; callers expose that absence as null rather than
// inventing a commit.
var Revision = ""

// String returns a human-readable version string.
func String() string {
	return fmt.Sprintf("lapdog %s", Version)
}

// RevisionPtr returns the stamped revision, or nil when it is unavailable.
func RevisionPtr() *string {
	if Revision == "" || Revision == "unknown" {
		return nil
	}
	r := Revision
	return &r
}
