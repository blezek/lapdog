//go:build !diagnostic

package store

import "embed"

// packagedBrakeCatalogFS contains the privacy-screened catalog copied into
// this package by `make brake-it`. The tracked placeholder keeps ordinary Go
// builds valid when no local Garage61 catalog has been generated.
//
//go:embed all:brake_catalog_data
var embeddedBrakeCatalogFS embed.FS

var packagedBrakeCatalogFS brakeCatalogFiles = embeddedBrakeCatalogFS
