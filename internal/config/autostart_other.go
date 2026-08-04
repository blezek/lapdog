//go:build !windows

package config

// SetAutostart is a no-op off Windows, where there is no Run key.
//
// It returns nil rather than an error so the settings handler needs no platform
// branch: on a development machine the setting is simply inert.
func SetAutostart(bool, string) error { return nil }
