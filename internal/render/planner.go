package render

import "math"

// ChunkPlanner converts a viewport into the spatial chunks needed for a
// frame. Margin is expressed in chunks and keeps a small off-screen buffer
// ready for smooth panning.
type ChunkPlanner struct {
	ChunkSize float64
	Margin    int
	// Domain optionally bounds chunk enumeration to the coordinate range that
	// can contain source geometry. Data-backed render sources normalize their
	// coordinates to a compact domain, so enumerating tiles outside it only
	// creates empty work (and can become enormous at low zoom).
	Domain    [4]float64
	HasDomain bool
}

// MaxViewportChunkKeys caps an aggregate render plan across all visible layers.
const MaxViewportChunkKeys = 1 << 16

// NewChunkPlanner creates a planner with one world unit per chunk and one
// chunk of look-ahead around the visible extent.
func NewChunkPlanner() ChunkPlanner {
	return ChunkPlanner{ChunkSize: 1, Margin: 1}
}

// VisibleKeys returns stable, row-major chunk keys for the viewport. The
// viewport spans one world unit at zoom 1; increasing zoom narrows the visible
// world extent while changing the zoom bucket used for simplification.
func (p ChunkPlanner) VisibleKeys(viewport Viewport, layer string) []ChunkKey {
	return p.VisibleKeysInto(nil, viewport, layer)
}

// VisibleKeysInto appends the viewport's row-major keys to dst, reusing its
// backing array when possible. Render loops can retain this buffer across
// refreshes while VisibleKeys keeps the independent-slice convenience API.
func (p ChunkPlanner) VisibleKeysInto(dst []ChunkKey, viewport Viewport, layer string) []ChunkKey {
	keys, _ := p.VisibleKeysIntoLimit(dst, viewport, layer, len(dst)+MaxViewportChunkKeys)
	return keys
}

// VisibleKeysIntoLimit appends the viewport's keys only when doing so keeps
// dst at or below limit. On overflow, dst is returned unchanged with ok=false.
func (p ChunkPlanner) VisibleKeysIntoLimit(dst []ChunkKey, viewport Viewport, layer string, limit int) ([]ChunkKey, bool) {
	chunkSize := p.ChunkSize
	if math.IsNaN(chunkSize) || math.IsInf(chunkSize, 0) || chunkSize <= 0 {
		chunkSize = 1
	}
	margin := p.Margin
	if margin < 0 {
		margin = 0
	}
	zoom := viewport.Zoom
	if math.IsNaN(zoom) || math.IsInf(zoom, 0) || math.IsNaN(viewport.Center.X) ||
		math.IsInf(viewport.Center.X, 0) || math.IsNaN(viewport.Center.Y) || math.IsInf(viewport.Center.Y, 0) {
		return dst, true
	}
	if zoom < 0.0001 {
		zoom = 0.0001
	}
	spanX, spanY := 1/zoom, 1/zoom
	if viewport.ScreenWidth > 0 && viewport.CanvasWidth > 0 {
		spanX *= viewport.ScreenWidth / viewport.CanvasWidth
	}
	if viewport.ScreenHeight > 0 && viewport.CanvasHeight > 0 {
		spanY *= viewport.ScreenHeight / viewport.CanvasHeight
	}
	minXValue := math.Floor((viewport.Center.X-spanX/2)/chunkSize) - float64(margin)
	maxXValue := math.Floor((viewport.Center.X+spanX/2)/chunkSize) + float64(margin)
	minYValue := math.Floor((viewport.Center.Y-spanY/2)/chunkSize) - float64(margin)
	maxYValue := math.Floor((viewport.Center.Y+spanY/2)/chunkSize) + float64(margin)
	// Reject values outside a conservative integer range before converting;
	// this also keeps subtraction and loop increments overflow-safe.
	const maxChunkCoordinate = float64(1 << 30)
	if minXValue < -maxChunkCoordinate || maxXValue > maxChunkCoordinate ||
		minYValue < -maxChunkCoordinate || maxYValue > maxChunkCoordinate {
		return dst, true
	}
	minX, maxX := int(minXValue), int(maxXValue)
	minY, maxY := int(minYValue), int(maxYValue)
	if p.HasDomain {
		domain := p.Domain
		for _, value := range domain {
			if math.IsNaN(value) || math.IsInf(value, 0) {
				return dst, true
			}
		}
		if domain[0] > domain[2] || domain[1] > domain[3] {
			return dst, true
		}
		domainMinX, domainMaxX := math.Floor(domain[0]/chunkSize), math.Floor(domain[2]/chunkSize)
		domainMinY, domainMaxY := math.Floor(domain[1]/chunkSize), math.Floor(domain[3]/chunkSize)
		if math.IsInf(domainMinX, 0) || math.IsInf(domainMaxX, 0) ||
			math.IsInf(domainMinY, 0) || math.IsInf(domainMaxY, 0) ||
			domainMinX < -maxChunkCoordinate || domainMaxX > maxChunkCoordinate ||
			domainMinY < -maxChunkCoordinate || domainMaxY > maxChunkCoordinate {
			return dst, true
		}
		minX = max(minX, int(domainMinX)-margin)
		maxX = min(maxX, int(domainMaxX)+margin)
		minY = max(minY, int(domainMinY)-margin)
		maxY = min(maxY, int(domainMaxY)+margin)
		if minX > maxX || minY > maxY {
			return dst, true
		}
	}
	width, height := int64(maxX)-int64(minX)+1, int64(maxY)-int64(minY)+1
	if width <= 0 || height <= 0 {
		return dst, true
	}
	if width > MaxViewportChunkKeys/height || width*height > MaxViewportChunkKeys {
		return dst, false
	}
	count := int(width * height)
	if limit < len(dst) || count > limit-len(dst) {
		return dst, false
	}

	zoomBucket := int(math.Floor(math.Log2(zoom)))
	start := len(dst)
	if cap(dst)-start < count {
		grown := make([]ChunkKey, start, start+count)
		copy(grown, dst)
		dst = grown
	}
	dst = dst[:start+count]
	index := start
	for y := minY; y <= maxY; y++ {
		for x := minX; x <= maxX; x++ {
			dst[index] = ChunkKey{
				Layer:      layer,
				ZoomBucket: zoomBucket,
				X:          x,
				Y:          y,
			}
			index++
		}
	}
	return dst, true
}
