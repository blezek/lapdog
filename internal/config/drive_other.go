//go:build !windows

package config

// isRemoteDrive is a no-op off Windows, where drive letters do not exist.
// Development machines are local, so there is nothing to probe.
func isRemoteDrive(string) (bool, error) { return false, nil }
