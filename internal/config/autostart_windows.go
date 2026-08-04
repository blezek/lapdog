//go:build windows

package config

import (
	"errors"
	"fmt"

	"golang.org/x/sys/windows/registry"
)

// runKeyPath is the per-user startup key.
//
// HKCU rather than HKLM: a per-user entry needs no elevation, and LapDog records
// one person's driving, so a machine-wide entry would be wrong even if it were
// easier.
const runKeyPath = `Software\Microsoft\Windows\CurrentVersion\Run`

// runValueName is the entry LapDog owns under the Run key.
const runValueName = "LapDog"

// SetAutostart adds or removes the per-user startup entry.
func SetAutostart(enabled bool, exePath string) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("config: open Run key: %w", err)
	}
	defer k.Close()

	if !enabled {
		// Absent is the desired end state, so a value that is already missing is
		// success rather than an error to report.
		if err := k.DeleteValue(runValueName); err != nil && !errors.Is(err, registry.ErrNotExist) {
			return fmt.Errorf("config: remove startup entry: %w", err)
		}
		return nil
	}
	// Quoted so a space in "Program Files" cannot split the command into a
	// program and a stray argument.
	if err := k.SetStringValue(runValueName, `"`+exePath+`"`); err != nil {
		return fmt.Errorf("config: write startup entry: %w", err)
	}
	return nil
}
