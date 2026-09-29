package render

import "math"

// ChunkPlanner converts a viewport into the spatial chunks needed for a
// frame. Margin is expressed in chunks and keeps a small off-screen buffer
// ready for smooth panning.
type ChunkPlanner struct {
	ChunkSize float64
	Margin    int
}

// NewChunkPlanner creates a planner with one world unit per chunk and one
// chunk of look-ahead around the visible extent.
func NewChunkPlanner() ChunkPlanner {
	return ChunkPlanner{ChunkSize: 1, Margin: 1}
}

// VisibleKeys returns stable, row-major chunk keys for the viewport. The
// viewport spans one world unit at zoom 1; increasing zoom narrows the visible
// world extent while changing the zoom bucket used for simplification.
func (p ChunkPlanner) VisibleKeys(viewport Viewport, layer string) []ChunkKey {
	chunkSize := p.ChunkSize
	if chunkSize <= 0 {
		chunkSize = 1
	}
	margin := p.Margin
	if margin < 0 {
		margin = 0
	}
	zoom := viewport.Zoom
	if zoom < 0.0001 {
		zoom = 0.0001
	}
	span := 1 / zoom
	minX := int(math.Floor((viewport.Center.X-span/2)/chunkSize)) - margin
	maxX := int(math.Floor((viewport.Center.X+span/2)/chunkSize)) + margin
	minY := int(math.Floor((viewport.Center.Y-span/2)/chunkSize)) - margin
	maxY := int(math.Floor((viewport.Center.Y+span/2)/chunkSize)) + margin

	zoomBucket := int(math.Floor(math.Log2(zoom)))
	keys := make([]ChunkKey, 0, (maxX-minX+1)*(maxY-minY+1))
	for y := minY; y <= maxY; y++ {
		for x := minX; x <= maxX; x++ {
			keys = append(keys, ChunkKey{
				Layer:      layer,
				ZoomBucket: zoomBucket,
				X:          x,
				Y:          y,
			})
		}
	}
	return keys
}
