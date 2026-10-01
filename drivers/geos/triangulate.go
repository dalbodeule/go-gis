//go:build native

package geos

import (
	"context"
	"fmt"
	"strings"

	"gogis/internal/core"

	geoslib "github.com/twpayne/go-geos"
)

// Triangle contains the three XY vertices of a constrained polygon triangle.
type Triangle [3][2]float64

const maxConstrainedTriangulationCoordinates = 250_000
const maxConstrainedTriangulationWKBBytes = 8 << 20
const maxConstrainedTriangulationWKTBytes = 1_000_000
const maxConstrainedTriangles = 250_000

// ConstrainedTriangles tessellates polygonal input while respecting interior
// rings. Non-polygon geometry components in collections are ignored.
func (o *Operator) ConstrainedTriangles(ctx context.Context, geometry core.Geometry) ([]Triangle, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if geometry == nil {
		return nil, fmt.Errorf("cannot triangulate a nil geometry")
	}
	geometryType := strings.ToUpper(geometry.GeometryType())
	switch geometryType {
	case "POINT", "MULTIPOINT", "LINESTRING", "MULTILINESTRING":
		return nil, nil
	}
	if err := validateConstrainedTriangulationInput(geometry); err != nil {
		return nil, err
	}
	input, err := o.read(geometry)
	if err != nil {
		return nil, err
	}
	defer input.Destroy()
	if !strings.Contains(strings.ToUpper(input.Type()), "POLYGON") && input.Type() != "GeometryCollection" {
		return nil, nil
	}
	triangulated := input.ConstrainedDelaunayTriangulation()
	if triangulated == nil || triangulated.IsEmpty() {
		if triangulated != nil {
			triangulated.Destroy()
		}
		return nil, nil
	}
	defer triangulated.Destroy()
	capacity := min(triangulated.NumGeometries(), maxConstrainedTriangles)
	triangles := make([]Triangle, 0, capacity)
	var appendGeometry func(*geoslib.Geom) error
	appendGeometry = func(candidate *geoslib.Geom) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		switch candidate.Type() {
		case "Polygon":
			ring := candidate.ExteriorRing()
			if ring == nil || ring.CoordSeq() == nil {
				return fmt.Errorf("GEOS returned a polygon without an exterior ring")
			}
			coordinates := ring.CoordSeq().ToCoords()
			if len(coordinates) < 4 {
				return nil
			}
			// A constrained Delaunay result is triangular; the repeated closing
			// coordinate follows the first three unique vertices.
			if len(coordinates) != 4 {
				return fmt.Errorf("GEOS returned a non-triangle polygon with %d ring coordinates", len(coordinates))
			}
			if len(triangles) >= maxConstrainedTriangles {
				return fmt.Errorf("GEOS triangulation exceeds the %d-triangle safety limit", maxConstrainedTriangles)
			}
			triangles = append(triangles, Triangle{
				{coordinates[0][0], coordinates[0][1]},
				{coordinates[1][0], coordinates[1][1]},
				{coordinates[2][0], coordinates[2][1]},
			})
		case "MultiPolygon", "GeometryCollection":
			for index := 0; index < candidate.NumGeometries(); index++ {
				if err := appendGeometry(candidate.Geometry(index)); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := appendGeometry(triangulated); err != nil {
		return nil, err
	}
	return triangles, nil
}

func validateConstrainedTriangulationInput(geometry core.Geometry) error {
	switch geometry := geometry.(type) {
	case core.WKBGeometry:
		if len(geometry.WKB) > maxConstrainedTriangulationWKBBytes {
			return fmt.Errorf("triangulation WKB exceeds the %d MiB safety limit", maxConstrainedTriangulationWKBBytes>>20)
		}
		count, err := geometry.PointCount()
		if err != nil {
			return fmt.Errorf("validate triangulation WKB: %w", err)
		}
		if count > maxConstrainedTriangulationCoordinates {
			return fmt.Errorf("triangulation input exceeds the %d-coordinate safety limit", maxConstrainedTriangulationCoordinates)
		}
	case core.WKTGeometry:
		if len(geometry.WKT) > maxConstrainedTriangulationWKTBytes {
			return fmt.Errorf("triangulation WKT exceeds the %d-byte safety limit", maxConstrainedTriangulationWKTBytes)
		}
	default:
		return fmt.Errorf("unsupported triangulation geometry %T", geometry)
	}
	return nil
}
