//go:build windows

package main

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

// GUI builds have no console by default. Preserve redirected streams and bind
// missing Go streams to the standard handles supplied by AttachConsole.
func attachConsole() error {
	kernel := windows.NewLazySystemDLL("kernel32.dll")
	result, _, err := kernel.NewProc("AttachConsole").Call(uintptr(uint32(0xffffffff)))
	if result == 0 && err != windows.ERROR_ACCESS_DENIED {
		if err != windows.ERROR_INVALID_HANDLE {
			return fmt.Errorf("attach console: %w", err)
		}
		result, _, err = kernel.NewProc("AllocConsole").Call()
		if result == 0 {
			return fmt.Errorf("allocate console: %w", err)
		}
	}
	for _, stream := range []struct {
		target   **os.File
		handle   uint32
		name     string
		required bool
	}{
		{&os.Stdin, windows.STD_INPUT_HANDLE, "stdin", false},
		{&os.Stdout, windows.STD_OUTPUT_HANDLE, "stdout", false},
		{&os.Stderr, windows.STD_ERROR_HANDLE, "stderr", true},
	} {
		if _, err := (*stream.target).Stat(); err == nil {
			continue
		}
		handle, err := windows.GetStdHandle(stream.handle)
		if err != nil {
			if !stream.required {
				continue
			}
			return fmt.Errorf("console %s: %w", stream.name, err)
		}
		if handle == 0 || handle == windows.InvalidHandle {
			if !stream.required {
				continue
			}
			return fmt.Errorf("console %s: invalid handle", stream.name)
		}
		*stream.target = os.NewFile(uintptr(handle), stream.name)
	}
	return nil
}
