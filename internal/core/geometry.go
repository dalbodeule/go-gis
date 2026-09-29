package core

import "strings"

// WKTGeometry is the interchange representation used at native driver
// boundaries. Keeping WKT here avoids exposing GDAL or GEOS C handles to the
// application core and makes snapshots safe to clone.
type WKTGeometry struct {
	WKT string
}

// GeometryType returns the WKT type token, for example POINT or POLYGON.
func (g WKTGeometry) GeometryType() string {
	typeName := g.WKT
	if index := strings.IndexAny(typeName, " (\t\r\n"); index >= 0 {
		typeName = typeName[:index]
	}
	return strings.ToUpper(typeName)
}

// Clone returns a detached WKT geometry.
func (g WKTGeometry) Clone() Geometry {
	return g
}
