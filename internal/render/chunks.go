package render

import (
	"context"
	"sync"
)

// ChunkKey identifies a spatial tile at a discrete zoom bucket.
//
// Keeping the key independent of a UI toolkit lets the same cache feed Qt
// Quick, a test renderer, or a future headless export path.
type ChunkKey struct {
	Layer      string
	ZoomBucket int
	X          int
	Y          int
}

// Vertex is the compact representation consumed by a renderer adapter.
type Vertex struct {
	X     float32
	Y     float32
	Color uint32
}

// Chunk is an immutable render payload once returned by a ChunkBuilder.
type Chunk struct {
	Key        ChunkKey
	Generation uint64
	Vertices   []Vertex
}

// ChunkBuilder creates the GPU-ready payload for a chunk.
type ChunkBuilder func(context.Context, ChunkKey) (Chunk, error)

// ChunkResult is emitted for each requested key.
type ChunkResult struct {
	Key   ChunkKey
	Chunk Chunk
	Err   error
	Stale bool
}

// Scheduler coordinates asynchronous chunk creation with viewport generations.
// A pan or zoom should call AdvanceGeneration before requesting new chunks. Any
// work completing for an older generation is reported as stale and is never
// inserted into the cache.
type Scheduler struct {
	mu         sync.RWMutex
	generation uint64
	cache      map[ChunkKey]Chunk
}

// NewScheduler creates an empty chunk scheduler.
func NewScheduler() *Scheduler {
	return &Scheduler{cache: make(map[ChunkKey]Chunk)}
}

// Generation returns the current viewport generation.
func (s *Scheduler) Generation() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.generation
}

// AdvanceGeneration invalidates in-flight work logically and returns the new
// generation. Existing cache entries remain reusable when their keys are still
// visible after a pan or zoom.
func (s *Scheduler) AdvanceGeneration() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.generation++
	return s.generation
}

// InvalidateLayer removes cached chunks belonging to layer.
func (s *Scheduler) InvalidateLayer(layer string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key := range s.cache {
		if key.Layer == layer {
			delete(s.cache, key)
		}
	}
}

// Cached returns a cached chunk without exposing the scheduler's internal map.
func (s *Scheduler) Cached(key ChunkKey) (Chunk, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	chunk, ok := s.cache[key]
	return chunk, ok
}

// Request asynchronously builds missing chunks. Results are emitted in build
// completion order, which allows a UI adapter to present low-latency chunks
// before slower work finishes. The channel is always closed when all work ends.
func (s *Scheduler) Request(ctx context.Context, keys []ChunkKey, builder ChunkBuilder) <-chan ChunkResult {
	results := make(chan ChunkResult, len(keys))
	requestGeneration := s.Generation()

	go func() {
		defer close(results)
		seen := make(map[ChunkKey]struct{}, len(keys))
		for _, key := range keys {
			select {
			case <-ctx.Done():
				return
			default:
			}
			if _, duplicate := seen[key]; duplicate {
				continue
			}
			seen[key] = struct{}{}

			if chunk, ok := s.Cached(key); ok {
				select {
				case results <- ChunkResult{Key: key, Chunk: chunk}:
				case <-ctx.Done():
					return
				}
				continue
			}

			chunk, err := builder(ctx, key)
			if err != nil {
				select {
				case results <- ChunkResult{Key: key, Err: err}:
				case <-ctx.Done():
					return
				}
				continue
			}

			s.mu.Lock()
			stale := requestGeneration != s.generation
			if !stale {
				chunk.Key = key
				chunk.Generation = requestGeneration
				s.cache[key] = chunk
			}
			s.mu.Unlock()

			select {
			case results <- ChunkResult{Key: key, Chunk: chunk, Stale: stale}:
			case <-ctx.Done():
				return
			}
		}
	}()

	return results
}
