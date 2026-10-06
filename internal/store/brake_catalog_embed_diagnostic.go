//go:build diagnostic

package store

import "io/fs"

// Diagnostic binaries must be observational: an ignored developer catalog in
// the build tree must not be embedded and imported into the database under test.
type emptyBrakeCatalogFS struct{}

func (emptyBrakeCatalogFS) ReadFile(string) ([]byte, error) {
	return nil, fs.ErrNotExist
}

var packagedBrakeCatalogFS brakeCatalogFiles = emptyBrakeCatalogFS{}
