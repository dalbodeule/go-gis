//go:build native

package geos

import (
	"context"
	"fmt"
	"maps"
	"strconv"
	"strings"

	"gogis/internal/core"

	geoslib "github.com/twpayne/go-geos"
)

// Operator uses one GEOS context per operator instance. It is safe to use
// from one worker; create one operator per concurrent worker.
type Operator struct {
	context *geoslib.Context
}

const minDisplaySimplificationPoints = 32

// overviewUnionGrid suppresses sub-decimeter seams between separately digitized
// projected polygons in an approximate, read-only display overview.
const overviewUnionGrid = 0.1

// NewOperator creates a GEOS operator with a dedicated context.
func NewOperator() *Operator {
	return &Operator{context: geoslib.NewContext()}
}

func (o *Operator) Intersect(ctx context.Context, left, right core.Layer) (core.Layer, error) {
	return o.binary(ctx, left, right, func(a, b *geoslib.Geom) *geoslib.Geom { return a.Intersection(b) })
}

func (o *Operator) Union(ctx context.Context, left, right core.Layer) (core.Layer, error) {
	return o.binary(ctx, left, right, func(a, b *geoslib.Geom) *geoslib.Geom { return a.Union(b) })
}

func (o *Operator) Difference(ctx context.Context, left, right core.Layer) (core.Layer, error) {
	return o.binary(ctx, left, right, func(a, b *geoslib.Geom) *geoslib.Geom { return a.Difference(b) })
}

func (o *Operator) Buffer(ctx context.Context, layer core.Layer, distance float64) (core.Layer, error) {
	result := cloneLayerForOperation(layer)
	for i := range result.Features {
		if err := ctx.Err(); err != nil {
			return core.Layer{}, err
		}
		input, err := o.read(result.Features[i].Geometry)
		if err != nil {
			return core.Layer{}, err
		}
		output := input.Buffer(distance, 8)
		input.Destroy()
		result.Features[i].Geometry = core.WKBGeometry{WKB: output.ToWKB()}
		output.Destroy()
	}
	return result, nil
}

// SimplifyForDisplay returns a detached layer with topology-preserving
// simplification applied to line and polygon geometries. It is intended only
// for coarse-scale, read-only rendering; callers must retain the source layer
// for editing and export.
func (o *Operator) SimplifyForDisplay(ctx context.Context, layer core.Layer, tolerance float64) (core.Layer, error) {
	if tolerance <= 0 {
		return layer, nil
	}
	// This is an ephemeral read-only display copy. Shallow-copy feature headers
	// and replace only the geometries that benefit from simplification; cloning
	// every properties map here dominates the cost for cadastral windows.
	result := layer
	result.Features = append([]core.Feature(nil), layer.Features...)
	for index := range result.Features {
		if err := ctx.Err(); err != nil {
			return core.Layer{}, err
		}
		geometry := result.Features[index].Geometry
		if geometry == nil {
			continue
		}
		geometryType := strings.ToUpper(geometry.GeometryType())
		if !strings.Contains(geometryType, "LINE") && !strings.Contains(geometryType, "POLYGON") {
			continue
		}
		wkb, ok := geometry.(core.WKBGeometry)
		if !ok {
			continue
		}
		pointCount, err := wkb.PointCount()
		if err != nil || pointCount < minDisplaySimplificationPoints {
			continue
		}
		input, err := o.read(geometry)
		if err != nil {
			return core.Layer{}, fmt.Errorf("read feature %d for display simplification: %w", result.Features[index].ID, err)
		}
		output := input.TopologyPreserveSimplify(tolerance)
		input.Destroy()
		if output == nil {
			continue
		}
		if !output.IsEmpty() {
			result.Features[index].Geometry = core.WKBGeometry{WKB: output.ToWKB()}
		}
		output.Destroy()
	}
	return result, nil
}

// DissolvePolygonBoundariesForDisplay returns the coverage boundary of the
// polygons in a bounded read-only window. It is intended for coarse overview
// rendering only: parcel-level borders and feature interaction are omitted,
// while the source layer remains unchanged for detailed zooms and queries.
func (o *Operator) DissolvePolygonBoundariesForDisplay(ctx context.Context, layer core.Layer) (output core.Layer, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			output = core.Layer{}
			err = fmt.Errorf("GEOS failed to dissolve polygon overview: %v", recovered)
		}
	}()
	geometries := make([]*geoslib.Geom, 0, len(layer.Features))
	for index := range layer.Features {
		if err := ctx.Err(); err != nil {
			for _, geometry := range geometries {
				geometry.Destroy()
			}
			return core.Layer{}, err
		}
		geometry := layer.Features[index].Geometry
		if geometry == nil || !strings.Contains(strings.ToUpper(geometry.GeometryType()), "POLYGON") {
			continue
		}
		wkb, ok := geometry.(core.WKBGeometry)
		if !ok {
			continue
		}
		parsed, err := o.context.NewGeomFromWKB(wkb.WKB)
		if err != nil {
			for _, current := range geometries {
				current.Destroy()
			}
			return core.Layer{}, fmt.Errorf("read polygon feature %d for overview dissolve: %w", layer.Features[index].ID, err)
		}
		geometries = append(geometries, parsed)
	}
	if len(geometries) == 0 {
		return layer, nil
	}
	collection := o.context.NewCollection(geoslib.TypeIDGeometryCollection, geometries)
	// A fixed precision model prevents tiny coordinate discrepancies from
	// surviving as sliver gaps and spikes in the coarse coverage outline. Geographic
	// coordinates need a degree-sized grid; unknown CRS units disable snapping.
	// The input layer is untouched; exact source geometries are used at detail zooms.
	gridSize := overviewUnionGridSize(layer.CRS.AuthorityCode)
	var union *geoslib.Geom
	if gridSize > 0 {
		union = collection.UnaryUnionPrec(gridSize)
	} else {
		union = collection.UnaryUnion()
	}
	collection.Destroy()
	if union == nil {
		return core.Layer{}, fmt.Errorf("dissolve polygon coverage for overview")
	}
	boundary := union.Boundary()
	union.Destroy()
	if boundary == nil {
		return core.Layer{}, fmt.Errorf("extract dissolved polygon boundary for overview")
	}
	result := layer
	result.Features = []core.Feature{{Geometry: core.WKBGeometry{WKB: boundary.ToWKB()}}}
	boundary.Destroy()
	return result, nil
}

func overviewUnionGridSize(authorityCode string) float64 {
	parts := strings.Split(strings.ToUpper(strings.TrimSpace(authorityCode)), ":")
	if len(parts) != 2 || parts[0] != "EPSG" {
		return 0
	}
	code, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0
	}
	// Most EPSG geographic 2D/3D CRSs are in the 4xxx range. Include common
	// modern geographic codes outside that range; 1e-6 degree is about 0.1 m.
	if (code >= 4000 && code < 5000) || code == 4326 || code == 4979 || code == 6318 || code == 6668 || code == 7844 {
		return 1e-6
	}
	return overviewUnionGrid
}

func (o *Operator) binary(ctx context.Context, left, right core.Layer, operation func(*geoslib.Geom, *geoslib.Geom) *geoslib.Geom) (core.Layer, error) {
	if len(left.Features) != len(right.Features) {
		return core.Layer{}, fmt.Errorf("binary GEOS operation requires equal feature counts")
	}
	result := cloneLayerForOperation(left)
	for i := range result.Features {
		if err := ctx.Err(); err != nil {
			return core.Layer{}, err
		}
		a, err := o.read(left.Features[i].Geometry)
		if err != nil {
			return core.Layer{}, err
		}
		b, err := o.read(right.Features[i].Geometry)
		if err != nil {
			a.Destroy()
			return core.Layer{}, err
		}
		output := operation(a, b)
		a.Destroy()
		b.Destroy()
		result.Features[i].Geometry = core.WKBGeometry{WKB: output.ToWKB()}
		output.Destroy()
	}
	return result, nil
}

// cloneLayerForOperation detaches mutable metadata without cloning input
// geometries. Every successful operation replaces the corresponding geometry
// with a newly serialized GEOS result, so cloning the old geometry first only
// adds memory traffic. The input layers remain untouched while properties and
// labels stay detached for callers of the operator.
func cloneLayerForOperation(layer core.Layer) core.Layer {
	result := layer
	result.Fields = append([]core.Field(nil), layer.Fields...)
	result.Features = make([]core.Feature, len(layer.Features))
	var labelArena []core.Label
	for index, feature := range layer.Features {
		clone := feature
		clone.Properties = maps.Clone(feature.Properties)
		if feature.Label != nil {
			if labelArena == nil {
				labelArena = make([]core.Label, len(layer.Features))
			}
			labelArena[index] = *feature.Label
			clone.Label = &labelArena[index]
		}
		result.Features[index] = clone
	}
	return result
}

func (o *Operator) read(geometry core.Geometry) (*geoslib.Geom, error) {
	if wkbGeometry, ok := geometry.(core.WKBGeometry); ok {
		return o.context.NewGeomFromWKB(wkbGeometry.WKB)
	}
	wkt, err := core.ToWKT(geometry)
	if err != nil {
		return nil, fmt.Errorf("convert geometry to WKT: %w", err)
	}
	return o.context.NewGeomFromWKT(wkt.WKT)
}
