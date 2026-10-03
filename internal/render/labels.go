package render

import (
	"math"

	"gogis/internal/core"
)

// LongestSegmentPlacement returns the midpoint and readable angle of the
// longest non-degenerate segment. It is shared by screen labels and DXF export.
func LongestSegmentPlacement(geometry core.Geometry) (Point, float64, bool, error) {
	if geometry == nil {
		return Point{}, 0, false, nil
	}
	parsed, _, _, _, err := parseFeaturePointsInto(geometry, nil, nil)
	if err != nil {
		return Point{}, 0, false, err
	}
	anchor, angle, found := longestSegmentPlacementParsed(&parsed)
	return anchor, angle, found, nil
}

func longestSegmentPlacementParsed(parsed *parsedFeaturePoints) (Point, float64, bool) {
	if parsed == nil {
		return Point{}, 0, false
	}
	parts := parsed.parts
	if parts == nil {
		parts = [][]Point{parsed.points}
	}
	longestSquared := 0.0
	var anchor Point
	angle := 0.0
	for _, part := range parts {
		for index := 1; index < len(part); index++ {
			start, end := part[index-1], part[index]
			dx, dy := end.X-start.X, end.Y-start.Y
			lengthSquared := dx*dx + dy*dy
			if lengthSquared <= longestSquared || math.IsNaN(lengthSquared) || math.IsInf(lengthSquared, 0) {
				continue
			}
			longestSquared = lengthSquared
			anchor = Point{X: start.X + dx/2, Y: start.Y + dy/2}
			angle = math.Atan2(dy, dx) * 180 / math.Pi
			if angle > 90 {
				angle -= 180
			} else if angle < -90 {
				angle += 180
			}
		}
	}
	return anchor, angle, longestSquared > 0
}
