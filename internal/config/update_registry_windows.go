//go:build windows

package config

import (
	"fmt"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

const uninstallKeyPath = `Software\Microsoft\Windows\CurrentVersion\Uninstall\LapDog`

// ReconcileInstalledVersion updates only an installer-created entry whose
// InstallLocation is the directory containing this executable. Portable copies
// therefore never create or borrow installer metadata.
func ReconcileInstalledVersion(exePath, version string) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, uninstallKeyPath, registry.QUERY_VALUE|registry.SET_VALUE)
	if err == registry.ErrNotExist {
		return nil
	}
	if err != nil {
		return fmt.Errorf("config: open uninstall entry: %w", err)
	}
	defer k.Close()
	installed, _, err := k.GetStringValue("InstallLocation")
	if err != nil {
		return nil
	}
	a, err := filepath.Abs(filepath.Dir(exePath))
	if err != nil {
		return err
	}
	b, err := filepath.Abs(installed)
	if err != nil {
		return err
	}
	if !strings.EqualFold(filepath.Clean(a), filepath.Clean(b)) {
		return nil
	}
	if err := k.SetStringValue("DisplayVersion", strings.TrimPrefix(version, "v")); err != nil {
		return fmt.Errorf("config: update installed version: %w", err)
	}
	return nil
}
