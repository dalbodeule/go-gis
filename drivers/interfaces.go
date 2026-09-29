// Package drivers defines boundaries for native GIS and storage backends.
package drivers

import (
	"context"

	"gogis/internal/core"
)

// LayerReader reads one vector layer from a source such as SHP or GeoPackage.
type LayerReader interface {
	Open(ctx context.Context, source string, layerName string) (core.Layer, error)
}

// LayerWriter persists a vector layer to a file or database.
type LayerWriter interface {
	Write(ctx context.Context, destination string, layer core.Layer) error
}

// Transformer converts coordinates between CRS definitions.
type Transformer interface {
	Transform(ctx context.Context, source core.CRS, target core.CRS, layer core.Layer) (core.Layer, error)
}

// SpatialOperator performs a single geometry operation.
type SpatialOperator interface {
	Intersect(ctx context.Context, left core.Layer, right core.Layer) (core.Layer, error)
	Union(ctx context.Context, left core.Layer, right core.Layer) (core.Layer, error)
	Difference(ctx context.Context, left core.Layer, right core.Layer) (core.Layer, error)
	Buffer(ctx context.Context, layer core.Layer, distance float64) (core.Layer, error)
}

// TransactionalStore is the database boundary for PostGIS-like backends.
type TransactionalStore interface {
	Begin(ctx context.Context) (Transaction, error)
}

// Transaction provides all-or-nothing database writes.
type Transaction interface {
	WriteLayer(ctx context.Context, layer core.Layer) error
	Commit(ctx context.Context) error
	Rollback(ctx context.Context) error
}

// DXFExporter writes geometry and labels for a target CAD compatibility profile.
type DXFExporter interface {
	Export(ctx context.Context, destination string, layer core.Layer, profile string) error
}
