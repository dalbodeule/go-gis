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
	cells    map[[2]int]cellChain
	spans    []hitSpan
	indices  []int32
	next     []int32
}

type cellChain struct {
	head int32
	tail int32
}

type hitSpan struct {
	featureIndex int32
	fixedCell    int
	minCell      int
	maxCell      int
	horizontal   bool
}

// NewHitIndex builds a uniform-grid index. A non-positive cell size uses a
// conservative default suitable for normalized layer coordinates.
func NewHitIndex(features []HitFeature, cellSize float64) HitIndex {
	if cellSize <= 0 {
		cellSize = 0.25
	}
	// Normalized vector layers usually distribute several features per cell.
	// Reserve a modest fraction up front to avoid repeated map growth without
	// over-reserving one bucket per feature for sparse or long geometries.
	index := HitIndex{
		cellSize: cellSize,
		// HitIndex is immutable after construction. The caller-owned feature
		// slice is therefore safe to retain under the documented no-mutation
		// contract and avoids copying every feature during lazy index build.
		features: features,
		cells:    make(map[[2]int]cellChain, len(features)/8+1),
	}
	// A typical normalized line touches one or two cells. Reserve that common
	// membership count up front; unusual long segments can still grow these
	// slices safely through append.
	initialCapacity := len(features)
	if initialCapacity <= int(^uint(0)>>1)/2 {
		initialCapacity *= 2
	}
	index.indices = make([]int32, 0, initialCapacity)
	index.next = make([]int32, 0, initialCapacity)
	// Append memberships to one flat array and link them per cell. Index the
	// cells actually crossed by each part instead of the feature bounding box;
	// a long diagonal would otherwise register every cell in its rectangle.
	for featureIndex, feature := range index.features {
		addCell := func(key [2]int) {
			chain, exists := index.cells[key]
			if !exists {
				chain.head = -1
				chain.tail = -1
			}
			entry := int32(len(index.indices))
			index.indices = append(index.indices, int32(featureIndex))
			index.next = append(index.next, -1)
			if chain.head < 0 {
				chain.head = entry
			} else {
				index.next[int(chain.tail)] = entry
			}
			chain.tail = entry
			index.cells[key] = chain
		}
		addSpan := func(horizontal bool, fixedCell, minCell, maxCell int) {
			index.spans = append(index.spans, hitSpan{
				featureIndex: int32(featureIndex),
				fixedCell:    fixedCell,
				minCell:      minCell,
				maxCell:      maxCell,
				horizontal:   horizontal,
			})
		}
		if len(feature.Parts) > 0 {
			for _, part := range feature.Parts {
				addHitPartCells(part, index.cellSize, addCell, addSpan)
			}
		} else {
			addHitPartCells(feature.Vertices, index.cellSize, addCell, addSpan)
		}
	}
	return index
}

func addHitPartCells(part []Point, cellSize float64, addCell func([2]int), addSpan func(bool, int, int, int)) {
	if len(part) == 0 {
		return
	}
	if len(part) == 1 {
		addCell([2]int{
			int(math.Floor(part[0].X / cellSize)),
			int(math.Floor(part[0].Y / cellSize)),
		})
		return
	}
	for index := 1; index < len(part); index++ {
		addHitSegmentCells(part[index-1], part[index], cellSize, addCell, addSpan)
	}
}

func addHitSegmentCells(start, end Point, cellSize float64, addCell func([2]int), addSpan func(bool, int, int, int)) {
	startCellX := int(math.Floor(start.X / cellSize))
	endCellX := int(math.Floor(end.X / cellSize))
	startCellY := int(math.Floor(start.Y / cellSize))
	endCellY := int(math.Floor(end.Y / cellSize))
	if startCellX == endCellX && startCellY == endCellY {
		addCell([2]int{startCellX, startCellY})
		return
	}
	minCellX, maxCellX := startCellX, endCellX
	if minCellX > maxCellX {
		minCellX, maxCellX = maxCellX, minCellX
	}
	minCellY, maxCellY := startCellY, endCellY
	if minCellY > maxCellY {
		minCellY, maxCellY = maxCellY, minCellY
	}
	if start.Y == end.Y {
		if maxCellX-minCellX > 32 {
			addSpan(true, startCellY, minCellX, maxCellX)
			return
		}
		for cellX := minCellX; cellX <= maxCellX; cellX++ {
			addCell([2]int{cellX, startCellY})
		}
		return
	}
	if start.X == end.X {
		if maxCellY-minCellY > 32 {
			addSpan(false, startCellX, minCellY, maxCellY)
			return
		}
		for cellY := minCellY; cellY <= maxCellY; cellY++ {
			addCell([2]int{startCellX, cellY})
		}
		return
	}
	if maxCellX-minCellX > 32 && maxCellY-minCellY > 32 {
		addHitSegmentCellsByTraversal(start, end, cellSize, addCell)
		return
	}
	for cellY := minCellY; cellY <= maxCellY; cellY++ {
		for cellX := minCellX; cellX <= maxCellX; cellX++ {
			_, _, visible := clipSegmentToRect(start, end,
				float64(cellX)*cellSize, float64(cellY)*cellSize,
				float64(cellX+1)*cellSize, float64(cellY+1)*cellSize)
			if visible {
				addCell([2]int{cellX, cellY})
			}
		}
	}
}

func addHitSegmentCellsByTraversal(start, end Point, cellSize float64, addCell func([2]int)) {
	deltaX, deltaY := end.X-start.X, end.Y-start.Y
	cellX := int(math.Floor(start.X / cellSize))
	cellY := int(math.Floor(start.Y / cellSize))
	endCellX := int(math.Floor(end.X / cellSize))
	endCellY := int(math.Floor(end.Y / cellSize))
	stepX, stepY := 0, 0
	tMaxX, tMaxY := math.Inf(1), math.Inf(1)
	tDeltaX, tDeltaY := math.Inf(1), math.Inf(1)
	if deltaX > 0 {
		stepX = 1
		tDeltaX = cellSize / deltaX
		tMaxX = (float64(cellX+1)*cellSize - start.X) / deltaX
	} else if deltaX < 0 {
		stepX = -1
		tDeltaX = -cellSize / deltaX
		tMaxX = (float64(cellX)*cellSize - start.X) / deltaX
	}
	if deltaY > 0 {
		stepY = 1
		tDeltaY = cellSize / deltaY
		tMaxY = (float64(cellY+1)*cellSize - start.Y) / deltaY
	} else if deltaY < 0 {
		stepY = -1
		tDeltaY = -cellSize / deltaY
		tMaxY = (float64(cellY)*cellSize - start.Y) / deltaY
	}
	for {
		addCell([2]int{cellX, cellY})
		if cellX == endCellX && cellY == endCellY {
			return
		}
		if tMaxX < tMaxY {
			cellX += stepX
			tMaxX += tDeltaX
			continue
		}
		if tMaxY < tMaxX {
			cellY += stepY
			tMaxY += tDeltaY
			continue
		}
		// A corner crossing touches both side cells. Include them before
		// advancing diagonally so boundary hits retain the old supercover
		// behavior of the rectangle clipping path.
		addCell([2]int{cellX + stepX, cellY})
		addCell([2]int{cellX, cellY + stepY})
		cellX += stepX
		cellY += stepY
		tMaxX += tDeltaX
		tMaxY += tDeltaY
	}
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

// HitTestVisibleFunc is the allocation-free visibility variant for adapters
// that already own a visibility predicate. It avoids constructing a map for
// each pointer event.
func (i HitIndex) HitTestVisibleFunc(point Point, tolerance float64, visible func(string) bool) (HitResult, bool) {
	return i.hitTest(point, tolerance, visible)
}

func (i HitIndex) hitTest(point Point, tolerance float64, visible func(string) bool) (HitResult, bool) {
	if tolerance < 0 || i.cellSize <= 0 || len(i.features) == 0 {
		return HitResult{}, false
	}
	minCellX := int(math.Floor((point.X - tolerance) / i.cellSize))
	maxCellX := int(math.Floor((point.X + tolerance) / i.cellSize))
	minCellY := int(math.Floor((point.Y - tolerance) / i.cellSize))
	maxCellY := int(math.Floor((point.Y + tolerance) / i.cellSize))
	cellSpanX := maxCellX - minCellX + 1
	cellSpanY := maxCellY - minCellY + 1
	denseCells := len(i.cells) == 0 && len(i.spans) == 0
	if cellSpanX > 0 && cellSpanY > 0 && len(i.cells) > 0 {
		denseCells = cellSpanX >= len(i.cells) || cellSpanY >= len(i.cells) || cellSpanX >= len(i.cells)/cellSpanY
	}
	if denseCells {
		return i.hitAllFeatures(point, tolerance, visible)
	}
	// Most clicks touch only a few cells. Keep the candidate list on the
	// stack for that common case and spill to the heap only for very dense
	// cells or unusually large tolerances.
	var candidateStorage [256]int
	candidates := candidateStorage[:0]
	for cellY := minCellY; cellY <= maxCellY; cellY++ {
		for cellX := minCellX; cellX <= maxCellX; cellX++ {
			chain, exists := i.cells[[2]int{cellX, cellY}]
			if exists {
				for entry := chain.head; entry >= 0; entry = i.next[entry] {
					candidates = append(candidates, int(i.indices[entry]))
				}
			}
		}
	}
	for _, span := range i.spans {
		if span.horizontal {
			if span.fixedCell >= minCellY && span.fixedCell <= maxCellY &&
				span.maxCell >= minCellX && span.minCell <= maxCellX {
				candidates = append(candidates, int(span.featureIndex))
			}
			continue
		}
		if span.fixedCell >= minCellX && span.fixedCell <= maxCellX &&
			span.maxCell >= minCellY && span.minCell <= maxCellY {
			candidates = append(candidates, int(span.featureIndex))
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

func (i HitIndex) hitAllFeatures(point Point, tolerance float64, visible func(string) bool) (HitResult, bool) {
	best := HitResult{Distance: math.Inf(1)}
	found := false
	for _, feature := range i.features {
		if visible != nil && !visible(feature.Layer) {
			continue
		}
		result, ok := hitFeature(feature, point, tolerance, best)
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

// HitTestScreenVisibleFunc is the screen-space form of HitTestVisibleFunc.
func (i HitIndex) HitTestScreenVisibleFunc(screen Point, viewport Viewport, width, height, tolerancePixels float64, visible func(string) bool) (HitResult, bool) {
	world, ok := ScreenPointToWorld(screen, viewport, width, height)
	if !ok || tolerancePixels < 0 {
		return HitResult{}, false
	}
	minimumDimension := math.Min(width, height)
	return i.HitTestVisibleFunc(world, tolerancePixels/(minimumDimension*viewport.Zoom), visible)
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
	if len(feature.Parts) == 0 && len(feature.Vertices) == 0 {
		return HitResult{}, false
	}
	maxDistance := tolerance * tolerance
	closest := math.Inf(1)
	if len(feature.Parts) == 0 {
		closest = squaredPartDistance(feature.Vertices, point, closest)
	} else {
		for _, part := range feature.Parts {
			closest = squaredPartDistance(part, point, closest)
		}
	}
	if closest <= maxDistance {
		distance := math.Sqrt(closest)
		if distance < best.Distance {
			return HitResult{Layer: feature.Layer, FeatureID: feature.FeatureID, Distance: distance}, true
		}
	}
	return HitResult{}, false
}

func squaredPartDistance(part []Point, point Point, closest float64) float64 {
	if len(part) == 1 {
		return math.Min(closest, squaredDistance(point, part[0]))
	}
	for i := 1; i < len(part); i++ {
		closest = math.Min(closest, squaredSegmentDistance(point, part[i-1], part[i]))
	}
	return closest
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
