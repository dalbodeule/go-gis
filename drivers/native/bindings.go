//go:build native

package native

import (
	"fmt"
	"sync"

	"github.com/airbusgeo/godal"
	"github.com/twpayne/go-geos"
	proj "github.com/twpayne/go-proj/v11"
)

var gdalRegisterOnce sync.Once

// RegisterGDAL initializes GDAL's driver registry for a native build.
func RegisterGDAL() {
	gdalRegisterOnce.Do(godal.RegisterAll)
}

// NewTransformer creates a PROJ CRS-to-CRS transformation.
func NewTransformer(sourceCRS, targetCRS string) (*proj.PJ, error) {
	if sourceCRS == "" || targetCRS == "" {
		return nil, fmt.Errorf("source and target CRS are required")
	}
	return proj.NewCRSToCRS(sourceCRS, targetCRS, nil)
}

// NewGEOSContext creates a thread-safe GEOS context for one worker.
func NewGEOSContext() *geos.Context {
	return geos.NewContext()
}
