//go:build native

package gdal

import (
	"sync"

	"github.com/airbusgeo/godal"
)

var registerOnce sync.Once

// registerDrivers initializes GDAL's process-wide driver registry once. GDAL
// keeps the registry globally, so repeating GDALAllRegister for every reader
// or writer call only adds startup/plugin-discovery work and does not provide
// dataset isolation.
func registerDrivers() {
	registerOnce.Do(godal.RegisterAll)
}
