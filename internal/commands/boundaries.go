package commands

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"gogis/internal/core"
)

const BoundaryIdentifierField = "shape_id" // DBF-safe for SHP round trips.

type boundaryPoint struct{ x, y float64 }
type boundaryGroup struct {
	identifier string
	points     []boundaryPoint
	indices    []int
}

// BuildBoundaryHulls calculates a planar convex outline for each group. Input
// row order is irrelevant and the survey does not need a repeated start point.
// A group field may be empty to calculate one outline from all imported points.
func BuildBoundaryHulls(ctx context.Context, points core.Layer, xField, yField, groupField, boundaryName string) (core.Layer, core.Layer, error) {
	if xField == "" || yField == "" || boundaryName == "" || points.CRS.AuthorityCode == "" {
		return core.Layer{}, core.Layer{}, fmt.Errorf("boundary calculation requires X/Y fields, output name, and CRS")
	}
	fieldNames := make(map[string]bool, len(points.Fields))
	for _, field := range points.Fields {
		fieldNames[field.Name] = true
	}
	if !fieldNames[xField] || !fieldNames[yField] || groupField != "" && !fieldNames[groupField] {
		return core.Layer{}, core.Layer{}, fmt.Errorf("boundary coordinate or group field is missing")
	}
	for name := range fieldNames {
		if strings.EqualFold(name, BoundaryIdentifierField) { return core.Layer{}, core.Layer{}, fmt.Errorf("field %q already exists", name) }
	}
	groups := make([]boundaryGroup, 0)
	byKey := make(map[string]int)
	for index, feature := range points.Features {
		if err := ctx.Err(); err != nil {
			return core.Layer{}, core.Layer{}, err
		}
		x, ex := strconv.ParseFloat(strings.TrimSpace(fmt.Sprint(feature.Properties[xField])), 64)
		y, ey := strconv.ParseFloat(strings.TrimSpace(fmt.Sprint(feature.Properties[yField])), 64)
		if ex != nil || ey != nil || math.IsNaN(x) || math.IsNaN(y) || math.IsInf(x, 0) || math.IsInf(y, 0) {
			return core.Layer{}, core.Layer{}, fmt.Errorf("feature %d has invalid boundary coordinates", feature.ID)
		}
		key := ""
		if groupField != "" {
			if feature.Properties[groupField] == nil || strings.TrimSpace(fmt.Sprint(feature.Properties[groupField])) == "" {
				return core.Layer{}, core.Layer{}, fmt.Errorf("feature %d has empty boundary group", feature.ID)
			}
			key = fmt.Sprint(feature.Properties[groupField])
		}
		groupIndex, exists := byKey[key]
		if !exists {
			groupIndex = len(groups)
			byKey[key] = groupIndex
			groups = append(groups, boundaryGroup{identifier: fmt.Sprintf("B%06d", groupIndex+1)})
		}
		groups[groupIndex].points = append(groups[groupIndex].points, boundaryPoint{x, y})
		groups[groupIndex].indices = append(groups[groupIndex].indices, index)
	}
	if len(groups) == 0 {
		return core.Layer{}, core.Layer{}, fmt.Errorf("boundary calculation needs points")
	}
	boundaries := core.Layer{Name: boundaryName, DisplayName: boundaryName, CRS: points.CRS, GeometryType: "POLYGON", Editable: true, GeometryLocked: true,
		Fields: []core.Field{{Name: BoundaryIdentifierField, Type: core.FieldTypeText}}}
	if groupField != "" {
		boundaries.Fields = append(boundaries.Fields, core.Field{Name: groupField, Type: core.FieldTypeText})
	}
	// Clone only after all hulls have passed validation, so failures leave the
	// source untouched and do not produce a partially tagged point layer.
	hulls := make([][]boundaryPoint, len(groups))
	for index, group := range groups {
		hull := convexBoundary(group.points)
		if len(hull) < 3 {
			return core.Layer{}, core.Layer{}, fmt.Errorf("boundary group %q has fewer than three non-collinear points", group.identifier)
		}
		hulls[index] = hull
	}
	tagged := points.Clone()
	tagged.Fields = append(tagged.Fields, core.Field{Name: BoundaryIdentifierField, Type: core.FieldTypeText})
	for index, group := range groups {
		for _, pointIndex := range group.indices {
			if tagged.Features[pointIndex].Properties == nil {
				tagged.Features[pointIndex].Properties = make(map[string]any)
			}
			tagged.Features[pointIndex].Properties[BoundaryIdentifierField] = group.identifier
		}
		coords := make([]string, 0, len(hulls[index])+1)
		for _, point := range hulls[index] {
			coords = append(coords, strconv.FormatFloat(point.x, 'g', -1, 64)+" "+strconv.FormatFloat(point.y, 'g', -1, 64))
		}
		coords = append(coords, coords[0])
		props := map[string]any{BoundaryIdentifierField: group.identifier}
		if groupField != "" {
			first := group.indices[0]
			props[groupField] = fmt.Sprint(points.Features[first].Properties[groupField])
		}
		boundaries.Features = append(boundaries.Features, core.Feature{ID: uint64(index + 1), Geometry: core.WKTGeometry{WKT: "POLYGON ((" + strings.Join(coords, ", ") + "))"}, Properties: props})
	}
	return tagged, boundaries, nil
}

func convexBoundary(input []boundaryPoint) []boundaryPoint {
	points := append([]boundaryPoint(nil), input...)
	sort.Slice(points, func(i, j int) bool {
		if points[i].x == points[j].x {
			return points[i].y < points[j].y
		}
		return points[i].x < points[j].x
	})
	unique := points[:0]
	for _, point := range points {
		if len(unique) == 0 || unique[len(unique)-1] != point {
			unique = append(unique, point)
		}
	}
	if len(unique) < 3 {
		return nil
	}
	cross := func(a, b, c boundaryPoint) float64 { return (b.x-a.x)*(c.y-a.y) - (b.y-a.y)*(c.x-a.x) }
	lower := make([]boundaryPoint, 0, len(unique))
	for _, point := range unique {
		for len(lower) >= 2 && cross(lower[len(lower)-2], lower[len(lower)-1], point) <= 0 {
			lower = lower[:len(lower)-1]
		}
		lower = append(lower, point)
	}
	upper := make([]boundaryPoint, 0, len(unique))
	for i := len(unique) - 1; i >= 0; i-- {
		point := unique[i]
		for len(upper) >= 2 && cross(upper[len(upper)-2], upper[len(upper)-1], point) <= 0 {
			upper = upper[:len(upper)-1]
		}
		upper = append(upper, point)
	}
	return append(lower[:len(lower)-1], upper[:len(upper)-1]...)
}
