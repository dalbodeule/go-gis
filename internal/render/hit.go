package render

import (
	"math"
	"sort"
)

// HitFeature is the render-side selectable representation of a line feature.
// Coordinates are in the same world space as the active viewport.
type HitFeature struct {
	Layer     string
	FeatureID uint64
	Vertices  []Point
	// Parts preserves disjoint geometry components. Vertices remains populated
	// for compatibility with simple single-part callers.
	Parts [][]Point
}

// HitResult identifies the closest feature within a tolerance.
type HitResult struct {
	Layer     string
	FeatureID uint64
	Distance  float64
}

// HitIndex is an immutable uniform-grid index for selectable features. The
// index is deliberately UI-neutral and can be rebuilt when a layer changes.
// Feature geometry remains owned by the caller; callers must not mutate it
// after constructing the index.
type HitIndex struct {
	cellSize float64
	features []HitFeature
	cells    map[[2]int][]int
}

// NewHitIndex builds a uniform-grid index. A non-positive cell size uses a
// conservative default suitable for normalized layer coordinates.
func NewHitIndex(features []HitFeature, cellSize float64) HitIndex {
	if cellSize <= 0 {
		cellSize = 0.25
	}
	index := HitIndex{
		cellSize: cellSize,
		features: append([]HitFeature(nil), features...),
		cells:    make(map[[2]int][]int),
	}
	for featureIndex, feature := range index.features {
		if len(featureParts(feature)) == 0 {
			continue
		}
		minX, minY := math.Inf(1), math.Inf(1)
		maxX, maxY := math.Inf(-1), math.Inf(-1)
		for _, part := range featureParts(feature) {
			for _, point := range part {
				minX, maxX = math.Min(minX, point.X), math.Max(maxX, point.X)
				minY, maxY = math.Min(minY, point.Y), math.Max(maxY, point.Y)
			}
		}
		minCellX := int(math.Floor(minX / index.cellSize))
		maxCellX := int(math.Floor(maxX / index.cellSize))
		minCellY := int(math.Floor(minY / index.cellSize))
		maxCellY := int(math.Floor(maxY / index.cellSize))
		for cellY := minCellY; cellY <= maxCellY; cellY++ {
			for cellX := minCellX; cellX <= maxCellX; cellX++ {
				key := [2]int{cellX, cellY}
				index.cells[key] = append(index.cells[key], featureIndex)
			}
		}
	}
	return index
}

// HitTest performs a tolerance-aware lookup using only nearby grid cells.
func (i HitIndex) HitTest(point Point, tolerance float64) (HitResult, bool) {
	return i.hitTest(point, tolerance, nil)
}

// HitTestVisible is the visibility-aware form used by layer tree adapters.
// The map is read-only for the duration of the call.
func (i HitIndex) HitTestVisible(point Point, tolerance float64, visible map[string]bool) (HitResult, bool) {
	return i.hitTest(point, tolerance, func(layer string) bool { return visible[layer] })
}

func (i HitIndex) hitTest(point Point, tolerance float64, visible func(string) bool) (HitResult, bool) {
	if tolerance < 0 || i.cellSize <= 0 || len(i.features) == 0 {
		return HitResult{}, false
	}
	minCellX := int(math.Floor((point.X - tolerance) / i.cellSize))
	maxCellX := int(math.Floor((point.X + tolerance) / i.cellSize))
	minCellY := int(math.Floor((point.Y - tolerance) / i.cellSize))
	maxCellY := int(math.Floor((point.Y + tolerance) / i.cellSize))
	candidates := make([]int, 0)
	for cellY := minCellY; cellY <= maxCellY; cellY++ {
		for cellX := minCellX; cellX <= maxCellX; cellX++ {
			candidates = append(candidates, i.cells[[2]int{cellX, cellY}]...)
		}
	}
	sort.Ints(candidates)
	unique := candidates[:0]
	for _, featureIndex := range candidates {
		if len(unique) == 0 || unique[len(unique)-1] != featureIndex {
			unique = append(unique, featureIndex)
		}
	}
	best := HitResult{Distance: math.Inf(1)}
	found := false
	for _, featureIndex := range unique {
		if visible != nil && !visible(i.features[featureIndex].Layer) {
			continue
		}
		result, ok := hitFeature(i.features[featureIndex], point, tolerance, best)
		if ok && (!found || result.Distance < best.Distance) {
			best, found = result, true
		}
	}
	return best, found
}

// HitTestScreen performs the indexed equivalent of HitTestScreen.
func (i HitIndex) HitTestScreen(screen Point, viewport Viewport, width, height, tolerancePixels float64) (HitResult, bool) {
	world, ok := ScreenPointToWorld(screen, viewport, width, height)
	if !ok || tolerancePixels < 0 {
		return HitResult{}, false
	}
	minimumDimension := math.Min(width, height)
	return i.HitTest(world, tolerancePixels/(minimumDimension*viewport.Zoom))
}

// HitTestScreenVisible combines screen conversion with layer visibility.
func (i HitIndex) HitTestScreenVisible(screen Point, viewport Viewport, width, height, tolerancePixels float64, visible map[string]bool) (HitResult, bool) {
	world, ok := ScreenPointToWorld(screen, viewport, width, height)
	if !ok || tolerancePixels < 0 {
		return HitResult{}, false
	}
	minimumDimension := math.Min(width, height)
	return i.HitTestVisible(world, tolerancePixels/(minimumDimension*viewport.Zoom), visible)
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
		result, ok := hitFeature(feature, point, tolerance, best)
		if ok && result.Distance*result.Distance <= maxDistance && result.Distance < best.Distance {
			best, found = result, true
		}
	}
	return best, found
}

func hitFeature(feature HitFeature, point Point, tolerance float64, best HitResult) (HitResult, bool) {
	parts := featureParts(feature)
	if len(parts) == 0 {
		return HitResult{}, false
	}
	maxDistance := tolerance * tolerance
	closest := math.Inf(1)
	for _, part := range parts {
		if len(part) == 1 {
			closest = math.Min(closest, squaredDistance(point, part[0]))
			continue
		}
		for i := 1; i < len(part); i++ {
			closest = math.Min(closest, squaredSegmentDistance(point, part[i-1], part[i]))
		}
	}
	if closest <= maxDistance && math.Sqrt(closest) < best.Distance {
		return HitResult{Layer: feature.Layer, FeatureID: feature.FeatureID, Distance: math.Sqrt(closest)}, true
	}
	return HitResult{}, false
}

func featureParts(feature HitFeature) [][]Point {
	if len(feature.Parts) > 0 {
		return feature.Parts
	}
	if len(feature.Vertices) > 0 {
		return [][]Point{feature.Vertices}
	}
	return nil
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
