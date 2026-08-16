//go:build !windows

package config

func ReconcileInstalledVersion(string, string) error { return nil }
