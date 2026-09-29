package render

import (
	"context"
	"sort"
	"sync"
	"sync/atomic"
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
	Key        ChunkKey
	Generation uint64
	Chunk      Chunk
	Err        error
	Stale      bool
}

// SchedulerStats is a point-in-time snapshot of render scheduling activity.
// These counters are intentionally UI-neutral so CLI benchmarks and the Qt
// adapter can use the same observability contract.
type SchedulerStats struct {
	Requests      uint64
	ChunksBuilt   uint64
	CacheHits     uint64
	StaleResults  uint64
	CanceledCalls uint64
}

// BatchStore holds the vertex batch currently presented by a renderer. It
// accepts only results for its active viewport generation, preventing late
// worker results from replacing a newer frame.
type BatchStore struct {
	mu         sync.RWMutex
	generation uint64
	chunks     map[ChunkKey][]Vertex
}

// NewBatchStore creates an empty active batch at generation zero.
func NewBatchStore() *BatchStore {
	return &BatchStore{chunks: make(map[ChunkKey][]Vertex)}
}

// BeginGeneration switches the active viewport generation. Existing vertices
// remain available until a matching result is applied, avoiding a blank frame
// while newly visible chunks are being built. When visible is provided, chunks
// outside the new extent are dropped while overlapping chunks are retained.
func (s *BatchStore) BeginGeneration(generation uint64, visible ...ChunkKey) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if generation < s.generation {
		return false
	}
	s.generation = generation
	if len(visible) > 0 {
		keep := make(map[ChunkKey]struct{}, len(visible))
		for _, key := range visible {
			keep[key] = struct{}{}
		}
		for key := range s.chunks {
			if _, ok := keep[key]; !ok {
				delete(s.chunks, key)
			}
		}
	}
	return true
}

// Generation returns the generation currently presented by the store.
func (s *BatchStore) Generation() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.generation
}

// Apply installs a result only when it belongs to the active generation.
func (s *BatchStore) Apply(result ChunkResult) bool {
	if result.Err != nil || result.Stale {
		return false
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if result.Generation != s.generation {
		return false
	}
	s.chunks[result.Key] = append([]Vertex(nil), result.Chunk.Vertices...)
	return true
}

// Current returns a copy so a renderer adapter cannot mutate the store while
// another worker is preparing the next batch.
func (s *BatchStore) Current() (uint64, []Vertex) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	keys := make([]ChunkKey, 0, len(s.chunks))
	for key := range s.chunks {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		left, right := keys[i], keys[j]
		if left.Layer != right.Layer {
			return left.Layer < right.Layer
		}
		if left.ZoomBucket != right.ZoomBucket {
			return left.ZoomBucket < right.ZoomBucket
		}
		if left.Y != right.Y {
			return left.Y < right.Y
		}
		return left.X < right.X
	})
	var vertices []Vertex
	for _, key := range keys {
		vertices = append(vertices, s.chunks[key]...)
	}
	return s.generation, vertices
}

// Clear removes all currently retained chunks. The generation itself is kept
// so a hidden layer cannot be restored by an older worker result.
func (s *BatchStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.chunks = make(map[ChunkKey][]Vertex)
}

// Scheduler coordinates asynchronous chunk creation with viewport generations.
// A pan or zoom should call AdvanceGeneration before requesting new chunks. Any
// work completing for an older generation is reported as stale and is never
// inserted into the cache.
type Scheduler struct {
	mu         sync.RWMutex
	generation uint64
	cache      map[ChunkKey]Chunk
	stats      schedulerCounters
}

type schedulerCounters struct {
	requests      atomic.Uint64
	chunksBuilt   atomic.Uint64
	cacheHits     atomic.Uint64
	staleResults  atomic.Uint64
	canceledCalls atomic.Uint64
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

// Stats returns a diagnostic snapshot without stopping in-flight workers.
func (s *Scheduler) Stats() SchedulerStats {
	return SchedulerStats{
		Requests:      s.stats.requests.Load(),
		ChunksBuilt:   s.stats.chunksBuilt.Load(),
		CacheHits:     s.stats.cacheHits.Load(),
		StaleResults:  s.stats.staleResults.Load(),
		CanceledCalls: s.stats.canceledCalls.Load(),
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
	uniqueKeys := uniqueChunkKeys(keys)
	s.stats.requests.Add(1)

	go func() {
		defer close(results)
		defer func() {
			if ctx.Err() != nil {
				s.stats.canceledCalls.Add(1)
			}
		}()
		workers := len(uniqueKeys)
		if workers > 4 {
			workers = 4
		}
		if workers == 0 {
			return
		}
		semaphore := make(chan struct{}, workers)
		var wait sync.WaitGroup
		for _, key := range uniqueKeys {
			key := key
			wait.Add(1)
			go func() {
				defer wait.Done()
				if ctx.Err() != nil {
					return
				}
				select {
				case semaphore <- struct{}{}:
				case <-ctx.Done():
					return
				}
				defer func() { <-semaphore }()
				if ctx.Err() != nil {
					return
				}
				s.buildChunk(ctx, requestGeneration, key, builder, results)
			}()
		}
		wait.Wait()
	}()

	return results
}

func uniqueChunkKeys(keys []ChunkKey) []ChunkKey {
	seen := make(map[ChunkKey]struct{}, len(keys))
	unique := make([]ChunkKey, 0, len(keys))
	for _, key := range keys {
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		unique = append(unique, key)
	}
	return unique
}

func (s *Scheduler) buildChunk(ctx context.Context, requestGeneration uint64, key ChunkKey, builder ChunkBuilder, results chan<- ChunkResult) {
	if chunk, ok := s.Cached(key); ok {
		s.stats.cacheHits.Add(1)
		if ctx.Err() != nil {
			return
		}
		select {
		case results <- ChunkResult{Key: key, Generation: requestGeneration, Chunk: chunk}:
		case <-ctx.Done():
		}
		return
	}

	s.stats.chunksBuilt.Add(1)
	chunk, err := builder(ctx, key)
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		select {
		case results <- ChunkResult{Key: key, Generation: requestGeneration, Err: err}:
		case <-ctx.Done():
		}
		return
	}
	if ctx.Err() != nil {
		return
	}

	s.mu.Lock()
	stale := requestGeneration != s.generation
	if !stale {
		chunk.Key = key
		chunk.Generation = requestGeneration
		s.cache[key] = chunk
	} else {
		s.stats.staleResults.Add(1)
	}
	s.mu.Unlock()

	select {
	case results <- ChunkResult{Key: key, Generation: requestGeneration, Chunk: chunk, Stale: stale}:
	case <-ctx.Done():
	}
}
