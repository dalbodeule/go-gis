//go:build native

package geos

import (
	"context"
	"fmt"
	"strings"

	"gogis/internal/core"
)

// PointOnSurface returns an XY coordinate inside a polygonal geometry. The
// point is suitable for placing labels on concave polygons and polygons with
// holes, unlike a vertex average or ordinary centroid.
func (o *Operator) PointOnSurface(ctx context.Context, geometry core.Geometry) ([2]float64, bool, error) {
	if err := ctx.Err(); err != nil {
		return [2]float64{}, false, err
	}
	input, err := o.read(geometry)
	if err != nil {
		return [2]float64{}, false, err
	}
	defer input.Destroy()
	if !strings.Contains(strings.ToUpper(input.Type()), "POLYGON") {
		return [2]float64{}, false, nil
	}
	point := input.PointOnSurface()
	if point == nil || point.IsEmpty() {
		if point != nil {
			point.Destroy()
		}
		return [2]float64{}, false, nil
	}
	defer point.Destroy()
	coordinates := point.CoordSeq().ToCoords()
	if len(coordinates) == 0 || len(coordinates[0]) < 2 {
		return [2]float64{}, false, fmt.Errorf("GEOS returned an interior label point without XY coordinates")
	}
	if err := ctx.Err(); err != nil {
		return [2]float64{}, false, err
	}
	return [2]float64{coordinates[0][0], coordinates[0][1]}, true, nil
}
