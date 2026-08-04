//go:build windows

package config

import "golang.org/x/sys/windows"

// driveRemote is DRIVE_REMOTE from the Win32 GetDriveType API.
const driveRemote = 4

// isRemoteDrive reports whether a drive letter such as "Z:" refers to a mapped
// network drive.
func isRemoteDrive(drive string) (bool, error) {
	root, err := windows.UTF16PtrFromString(drive + `\`)
	if err != nil {
		return false, err
	}
	return windows.GetDriveType(root) == driveRemote, nil
}
