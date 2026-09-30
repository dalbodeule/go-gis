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

// GeometryOnlyReader loads geometry without materializing feature property
// maps. UI renderers can use it when attributes are fetched on demand.
type GeometryOnlyReader interface {
	OpenGeometryOnly(ctx context.Context, source string, layerName string) (core.Layer, error)
}

// FeatureReader loads one feature's geometry and properties by the stable
// application feature ID assigned by the reader's sequential scan.
type FeatureReader interface {
	OpenFeature(ctx context.Context, source string, layerName string, featureID uint64) (core.Feature, error)
}

// AttributePageReader loads one page of properties without decoding geometry.
type AttributePageReader interface {
	OpenAttributePage(ctx context.Context, source string, layerName string, offset, limit int) (core.Layer, int, error)
}

// LayerCollectionReader loads every vector layer in one dataset. A caller can
// still use LayerReader when it wants one explicitly named layer.
type LayerCollectionReader interface {
	OpenAll(ctx context.Context, source string) ([]core.Layer, error)
}

// GeometryOnlyCollectionReader loads all layers without feature properties.
type GeometryOnlyCollectionReader interface {
	OpenAllGeometryOnly(ctx context.Context, source string) ([]core.Layer, error)
}

// LayerWindowReader loads only features intersecting an axis-aligned window.
// The window coordinates use the source layer's CRS. Implementations may
// decline this optimization when the backing format cannot push down a
// spatial filter.
type LayerWindowReader interface {
	OpenWindow(ctx context.Context, source string, layerName string, bounds [4]float64) (core.Layer, error)
}

// GeometryOnlyWindowReader exposes the spatially filtered geometry path for
// viewport renderers that fetch attributes separately.
type GeometryOnlyWindowReader interface {
	OpenWindowGeometryOnly(ctx context.Context, source string, layerName string, bounds [4]float64) (core.Layer, error)
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
