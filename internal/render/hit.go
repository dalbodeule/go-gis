package render

import "math"

// HitFeature is the render-side selectable representation of a line feature.
// Coordinates are in the same world space as the active viewport.
type HitFeature struct {
	Layer     string
	FeatureID uint64
	Vertices  []Point
}

// HitResult identifies the closest feature within a tolerance.
type HitResult struct {
	Layer     string
	FeatureID uint64
	Distance  float64
}

// ScreenPointToWorld converts a top-left-origin screen coordinate into the
// world coordinate represented by a centered viewport. The Y axis is inverted
// because screen coordinates grow down while map coordinates grow up.
func ScreenPointToWorld(screen Point, viewport Viewport, width, height float64) (Point, bool) {
	if width <= 0 || height <= 0 || viewport.Zoom <= 0 {
		return Point{}, false
	}
	return Point{
		X: viewport.Center.X + (screen.X-width/2)/(width*viewport.Zoom),
		Y: viewport.Center.Y - (screen.Y-height/2)/(height*viewport.Zoom),
	}, true
}

// HitTestScreen performs a pixel-tolerance hit test against a viewport. It is
// the UI-neutral boundary used by desktop adapters before they submit a
// selected core feature ID.
func HitTestScreen(features []HitFeature, screen Point, viewport Viewport, width, height, tolerancePixels float64) (HitResult, bool) {
	world, ok := ScreenPointToWorld(screen, viewport, width, height)
	if !ok || tolerancePixels < 0 {
		return HitResult{}, false
	}
	minimumDimension := math.Min(width, height)
	toleranceWorld := tolerancePixels / (minimumDimension * viewport.Zoom)
	return HitTest(features, world, toleranceWorld)
}

// HitTest finds the closest polyline feature to point. It returns false when
// no segment is within tolerance or when tolerance is negative.
func HitTest(features []HitFeature, point Point, tolerance float64) (HitResult, bool) {
	if tolerance < 0 {
		return HitResult{}, false
	}
	maxDistance := tolerance * tolerance
	best := HitResult{Distance: math.Inf(1)}
	found := false
	for _, feature := range features {
		if len(feature.Vertices) == 0 {
			continue
		}
		if len(feature.Vertices) == 1 {
			distance := squaredDistance(point, feature.Vertices[0])
			if distance <= maxDistance && distance < best.Distance {
				best = HitResult{Layer: feature.Layer, FeatureID: feature.FeatureID, Distance: math.Sqrt(distance)}
				found = true
			}
			continue
		}
		for i := 1; i < len(feature.Vertices); i++ {
			distance := squaredSegmentDistance(point, feature.Vertices[i-1], feature.Vertices[i])
			if distance <= maxDistance && distance < best.Distance {
				best = HitResult{Layer: feature.Layer, FeatureID: feature.FeatureID, Distance: math.Sqrt(distance)}
				found = true
			}
		}
	}
	return best, found
}

func squaredDistance(left, right Point) float64 {
	dx := left.X - right.X
	dy := left.Y - right.Y
	return dx*dx + dy*dy
}

func squaredSegmentDistance(point, start, end Point) float64 {
	segmentX := end.X - start.X
	segmentY := end.Y - start.Y
	segmentLengthSquared := segmentX*segmentX + segmentY*segmentY
	if segmentLengthSquared == 0 {
		return squaredDistance(point, start)
	}
	t := ((point.X-start.X)*segmentX + (point.Y-start.Y)*segmentY) / segmentLengthSquared
	if t < 0 {
		t = 0
	} else if t > 1 {
		t = 1
	}
	closest := Point{X: start.X + t*segmentX, Y: start.Y + t*segmentY}
	return squaredDistance(point, closest)
}
