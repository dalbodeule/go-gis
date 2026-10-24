//go:build native

package geos

import (
	"context"
	"fmt"
	"maps"

	"gogis/internal/core"

	geoslib "github.com/twpayne/go-geos"
)

// IntersectPolygonBoundaries reads each source geometry once, rejects pairs
// with disjoint envelopes, and materializes only polygonal intersections.
// The operator owns one GEOS context and must be used by one worker at a time.
func (o *Operator) IntersectPolygonBoundaries(ctx context.Context, source, boundaries core.Layer, maxCandidatePairs int) ([][]core.Feature, error) {
	results := make([][]core.Feature, len(boundaries.Features))
	geometries := make([]*geoslib.Geom, len(boundaries.Features))
	bounds := make([]*geoslib.Box2D, len(boundaries.Features))
	defer func() {
		for _, geometry := range geometries {
			if geometry != nil {
				geometry.Destroy()
			}
		}
	}()
	for index, boundary := range boundaries.Features {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		geometry, err := o.read(boundary.Geometry)
		if err != nil {
			return nil, fmt.Errorf("read boundary feature %d: %w", boundary.ID, err)
		}
		if geometry.IsEmpty() {
			geometry.Destroy()
			return nil, fmt.Errorf("boundary feature %d is empty", boundary.ID)
		}
		geometries[index] = geometry
		bounds[index] = geometry.Bounds()
	}
	candidatePairs := 0
	for _, feature := range source.Features {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		geometry, err := o.read(feature.Geometry)
		if err != nil {
			return nil, fmt.Errorf("read source feature %d: %w", feature.ID, err)
		}
		if geometry.IsEmpty() {
			geometry.Destroy()
			continue
		}
		featureBounds := geometry.Bounds()
		for index, boundary := range geometries {
			if !featureBounds.Intersects(bounds[index]) {
				continue
			}
			candidatePairs++
			if maxCandidatePairs > 0 && candidatePairs > maxCandidatePairs {
				geometry.Destroy()
				return nil, fmt.Errorf("boundary split exceeds %d candidate geometry comparisons; reduce the input or use CLI", maxCandidatePairs)
			}
			if err := ctx.Err(); err != nil {
				geometry.Destroy()
				return nil, err
			}
			if !geometry.Intersects(boundary) {
				continue
			}
			intersection := geometry.Intersection(boundary)
			if intersection == nil {
				geometry.Destroy()
				return nil, fmt.Errorf("intersect source feature %d with boundary %d failed", feature.ID, boundaries.Features[index].ID)
			}
			polygonWKB, hasArea := polygonalIntersectionWKB(o.context, intersection)
			intersection.Destroy()
			if !hasArea {
				continue
			}
			result := feature
			result.Geometry = core.WKBGeometry{WKB: polygonWKB}
			result.Properties = maps.Clone(feature.Properties)
			if feature.Label != nil {
				label := *feature.Label
				result.Label = &label
			}
			results[index] = append(results[index], result)
		}
		geometry.Destroy()
	}
	return results, nil
}

func polygonalIntersectionWKB(context *geoslib.Context, geometry *geoslib.Geom) ([]byte, bool) {
	if geometry == nil || geometry.IsEmpty() {
		return nil, false
	}
	switch geometry.TypeID() {
	case geoslib.TypeIDPolygon, geoslib.TypeIDMultiPolygon:
		return geometry.ToWKB(), true
	case geoslib.TypeIDGeometryCollection:
		parts := make([]*geoslib.Geom, 0)
		var collect func(*geoslib.Geom)
		collect = func(parent *geoslib.Geom) {
			switch parent.TypeID() {
			case geoslib.TypeIDPolygon:
				if !parent.IsEmpty() {
					parts = append(parts, parent)
				}
			case geoslib.TypeIDMultiPolygon, geoslib.TypeIDGeometryCollection:
				for index := 0; index < parent.NumGeometries(); index++ {
					collect(parent.Geometry(index))
				}
			}
		}
		collect(geometry)
		if len(parts) == 0 {
			return nil, false
		}
		if len(parts) == 1 {
			return parts[0].ToWKB(), true
		}
		multi := context.NewCollection(geoslib.TypeIDMultiPolygon, parts)
		wkb := multi.ToWKB()
		multi.Destroy()
		return wkb, true
	default:
		return nil, false
	}
}
