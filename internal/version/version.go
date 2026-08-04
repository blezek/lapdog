// Package version reports the application version, set at build time.
package version

import "fmt"

// Version is the application version, overridden at build time with
// -ldflags "-X github.com/blezek/lapdog/internal/version.Version=x.y.z".
var Version = "dev"

// String returns a human-readable version string.
func String() string {
	return fmt.Sprintf("lapdog %s", Version)
}
