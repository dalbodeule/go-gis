package render

import (
	"context"
	"fmt"
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
	X      float32
	Y      float32
	Color  uint32
	SizeMM float32
	Kind   uint32
}

const (
	VertexLine uint32 = iota
	VertexPoint
	VertexFill
)

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
	mu           sync.RWMutex
	generation   uint64
	chunks       map[ChunkKey][]Vertex
	orderedKeys  []ChunkKey
	orderedVerts [][]Vertex
	orderedIndex map[ChunkKey]orderedPosition
	knownKeys    map[ChunkKey]uint64
	visibleEpoch uint64
	vertexCount  int
	revision     uint64
}

type orderedPosition struct {
	epoch uint64
	index int
}

// NewBatchStore creates an empty active batch at generation zero.
func NewBatchStore() *BatchStore {
	return &BatchStore{
		chunks:       make(map[ChunkKey][]Vertex),
		orderedIndex: make(map[ChunkKey]orderedPosition),
		knownKeys:    make(map[ChunkKey]uint64),
	}
}

// BeginGeneration switches the active viewport generation. Existing vertices
// remain available until a matching result is applied, avoiding a blank frame
// while newly visible chunks are being built. When visible is provided, chunks
// outside the new extent are dropped while overlapping chunks are retained.
func (s *BatchStore) BeginGeneration(generation uint64, visible ...ChunkKey) bool {
	return s.beginGeneration(generation, len(visible) > 0, visible)
}

// BeginGenerationWithVisible switches generations using an explicit viewport
// key list. Unlike BeginGeneration's omitted variadic list, an empty slice
// means that no chunks remain visible and must release all retained geometry.
func (s *BatchStore) BeginGenerationWithVisible(generation uint64, visible []ChunkKey) bool {
	return s.beginGeneration(generation, true, visible)
}

func (s *BatchStore) beginGeneration(generation uint64, hasVisible bool, visible []ChunkKey) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if generation < s.generation {
		return false
	}
	s.generation = generation
	if hasVisible {
		if sameChunkKeyOrder(s.orderedKeys, visible) {
			return true
		}
		s.revision++
		s.visibleEpoch++
		if s.visibleEpoch == 0 {
			// Epoch wraparound is practically unreachable, but resetting the
			// markers keeps the invariant explicit if it ever occurs.
			for key := range s.knownKeys {
				delete(s.knownKeys, key)
			}
			for key := range s.orderedIndex {
				delete(s.orderedIndex, key)
			}
			s.visibleEpoch = 1
		}
		epoch := s.visibleEpoch
		oldOrderedKeys := s.orderedKeys
		oldOrderedVerts := s.orderedVerts
		ordered := make([]ChunkKey, 0, len(visible))
		orderedVerts := make([][]Vertex, 0, len(visible))
		chunks := make(map[ChunkKey][]Vertex, min(len(visible), len(s.chunks)))
		orderedIndex := make(map[ChunkKey]orderedPosition, len(visible))
		knownKeys := make(map[ChunkKey]uint64, len(visible))
		for _, key := range visible {
			if knownKeys[key] == epoch {
				continue
			}
			knownKeys[key] = epoch
			ordered = append(ordered, key)
			vertices, exists := s.chunks[key]
			orderedVerts = append(orderedVerts, vertices)
			orderedIndex[key] = orderedPosition{epoch: epoch, index: len(ordered) - 1}
			if exists {
				chunks[key] = vertices
			}
		}
		if cap(oldOrderedKeys) > 0 {
			clear(oldOrderedKeys[:cap(oldOrderedKeys)])
		}
		if cap(oldOrderedVerts) > 0 {
			clear(oldOrderedVerts[:cap(oldOrderedVerts)])
		}
		s.chunks = chunks
		s.orderedIndex = orderedIndex
		s.knownKeys = knownKeys
		s.orderedKeys = ordered
		s.orderedVerts = orderedVerts
		s.vertexCount = 0
		for _, vertices := range chunks {
			s.vertexCount += len(vertices)
		}
	}
	return true
}

func sameChunkKeyOrder(left, right []ChunkKey) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
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

// Revision changes only when visible batch content or its draw order changes.
// A new viewport generation with identical cached geometry retains its revision.
func (s *BatchStore) Revision() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.revision
}

// Apply installs a result only when it belongs to the active generation.
func (s *BatchStore) Apply(result ChunkResult) bool {
	applied, _ := s.apply(result, true)
	return applied
}

// ApplyImmutable installs a result whose Vertices slice is immutable for the
// lifetime of the batch. Scheduler/cache-backed render paths can use this to
// avoid copying a chunk that is already owned by an immutable source.
func (s *BatchStore) ApplyImmutable(result ChunkResult) bool {
	applied, _ := s.apply(result, false)
	return applied
}

// ApplyImmutableChecked installs an immutable result and reports a viewport
// batch budget error separately from stale-generation rejection.
func (s *BatchStore) ApplyImmutableChecked(result ChunkResult) (bool, error) {
	return s.apply(result, false)
}

func batchVertexCountWithinLimit(current, previous, next, limit int) bool {
	if current < 0 || previous < 0 || next < 0 || limit < 0 || previous > current {
		return false
	}
	retained := current - previous
	return retained <= limit && next <= limit-retained
}

func (s *BatchStore) apply(result ChunkResult, cloneVertices bool) (bool, error) {
	return s.applyWithLimit(result, cloneVertices, MaxBatchVertices)
}

func (s *BatchStore) applyWithLimit(result ChunkResult, cloneVertices bool, limit int) (bool, error) {
	if result.Err != nil || result.Stale {
		return false, result.Err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if result.Generation != s.generation {
		return false, nil
	}
	previousCount := len(s.chunks[result.Key])
	vertices := result.Chunk.Vertices
	if !batchVertexCountWithinLimit(s.vertexCount, previousCount, len(vertices), limit) {
		return false, fmt.Errorf("viewport render batch exceeds the %d-vertex safety limit", limit)
	}
	if !cloneVertices {
		if previous, exists := s.chunks[result.Key]; exists && len(previous) == len(vertices) &&
			(len(vertices) == 0 || &previous[0] == &vertices[0]) {
			return true, nil
		}
	}
	if cloneVertices {
		vertices = append([]Vertex(nil), vertices...)
	}
	s.chunks[result.Key] = vertices
	s.revision++
	s.vertexCount += len(vertices) - previousCount
	if position, exists := s.orderedIndex[result.Key]; exists && position.epoch == s.visibleEpoch {
		s.orderedVerts[position.index] = vertices
	} else {
		s.knownKeys[result.Key] = 0
		s.orderedKeys = append(s.orderedKeys, result.Key)
		s.orderedVerts = append(s.orderedVerts, vertices)
		s.orderedIndex[result.Key] = orderedPosition{epoch: s.visibleEpoch, index: len(s.orderedKeys) - 1}
	}
	return true, nil
}

// Current returns a copy so a renderer adapter cannot mutate the store while
// another worker is preparing the next batch. Chunk order follows the visible
// request order, avoiding a map-key sort on every frame.
func (s *BatchStore) Current() (uint64, []Vertex) {
	return s.CurrentInto(nil)
}

// CurrentInto copies the current batch into dst when its capacity is enough,
// allowing a renderer adapter to reuse the flatten buffer across publishes.
// The returned slice remains caller-owned and is safe to pass to a synchronous
// adapter such as the Qt bridge.
func (s *BatchStore) CurrentInto(dst []Vertex) (uint64, []Vertex) {
	generation, _, vertices := s.CurrentIntoVersion(dst)
	return generation, vertices
}

// CurrentIntoVersion also returns the content revision of the copied batch.
func (s *BatchStore) CurrentIntoVersion(dst []Vertex) (uint64, uint64, []Vertex) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if cap(dst) < s.vertexCount {
		dst = make([]Vertex, 0, s.vertexCount)
	} else {
		dst = dst[:0]
	}
	for _, vertices := range s.orderedVerts {
		dst = append(dst, vertices...)
	}
	return s.generation, s.revision, dst
}

// Clear removes all currently retained chunks. The generation itself is kept
// so a hidden layer cannot be restored by an older worker result.
func (s *BatchStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.orderedKeys) > 0 || s.vertexCount > 0 {
		s.revision++
	}
	s.chunks = make(map[ChunkKey][]Vertex)
	s.orderedKeys = nil
	s.orderedVerts = nil
	s.orderedIndex = make(map[ChunkKey]orderedPosition)
	for key := range s.knownKeys {
		delete(s.knownKeys, key)
	}
	s.visibleEpoch = 0
	s.vertexCount = 0
}

// Scheduler coordinates asynchronous chunk creation with viewport generations.
// A pan or zoom should call AdvanceGeneration before requesting new chunks. Any
// work completing for an older generation is reported as stale and is never
// inserted into the cache.
type Scheduler struct {
	mu            sync.RWMutex
	generation    uint64
	cache         map[ChunkKey]Chunk
	cacheVertices int
	maxWorkers    int
	workerSlots   chan struct{}
	stats         schedulerCounters
	cachedPool    sync.Pool
	requestPool   sync.Pool
	keyPool       sync.Pool
}

type cachedChunkSnapshot struct {
	chunks []Chunk
}

// ChunkKeyBuffer owns reusable viewport keys until it is returned to a
// Scheduler. Callers may mutate Keys while they own the buffer.
type ChunkKeyBuffer struct {
	Keys []ChunkKey
}

type schedulerRequestBuffers struct {
	cachedKeys []ChunkKey
	missing    []ChunkKey
}

type schedulerCounters struct {
	requests      atomic.Uint64
	chunksBuilt   atomic.Uint64
	cacheHits     atomic.Uint64
	staleResults  atomic.Uint64
	canceledCalls atomic.Uint64
}

// NewScheduler creates an empty chunk scheduler with four build workers.
func NewScheduler() *Scheduler {
	return NewSchedulerWithMaxWorkers(4)
}

// NewSchedulerWithMaxWorkers creates a scheduler with an explicit upper bound
// on concurrent chunk builds. Values below one become one; values above 64
// are clamped to keep accidental configurations from spawning huge worker sets.
func NewSchedulerWithMaxWorkers(maxWorkers int) *Scheduler {
	if maxWorkers < 1 {
		maxWorkers = 1
	}
	if maxWorkers > 64 {
		maxWorkers = 64
	}
	return &Scheduler{
		cache: make(map[ChunkKey]Chunk), maxWorkers: maxWorkers,
		workerSlots: make(chan struct{}, maxWorkers),
	}
}

// MaxWorkers reports the configured upper bound for concurrent chunk builds.
func (s *Scheduler) MaxWorkers() int {
	if s == nil || s.maxWorkers < 1 {
		return 1
	}
	return s.maxWorkers
}

// AcquireChunkKeyBuffer returns a reusable scratch buffer for viewport
// requests. The caller owns it until ReleaseChunkKeyBuffer and must not mutate
// it while a scheduler request still references it.
func (s *Scheduler) AcquireChunkKeyBuffer(minCapacity int) *ChunkKeyBuffer {
	if minCapacity < 0 {
		minCapacity = 0
	}
	if minCapacity > MaxViewportChunkKeys {
		minCapacity = MaxViewportChunkKeys
	}
	value := s.keyPool.Get()
	if value == nil {
		return &ChunkKeyBuffer{Keys: make([]ChunkKey, 0, minCapacity)}
	}
	buffer := value.(*ChunkKeyBuffer)
	keys := buffer.Keys
	if cap(keys) < minCapacity {
		buffer.Keys = make([]ChunkKey, 0, minCapacity)
		return buffer
	}
	buffer.Keys = keys[:0]
	return buffer
}

// ReleaseChunkKeyBuffer returns a viewport key buffer after its request
// channel has closed. Keeping ownership explicit prevents a canceled request
// from racing with the next viewport refresh.
func (s *Scheduler) ReleaseChunkKeyBuffer(buffer *ChunkKeyBuffer) {
	if buffer == nil {
		return
	}
	clear(buffer.Keys)
	buffer.Keys = buffer.Keys[:0]
	s.keyPool.Put(buffer)
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
	for key, chunk := range s.cache {
		if key.Layer == layer {
			s.cacheVertices -= len(chunk.Vertices)
			delete(s.cache, key)
		}
	}
}

// RetainOnly drops cached chunks that are not in keys, bounding cache memory
// to the current viewport and planner margin instead of all regions visited.
func (s *Scheduler) RetainOnly(keys []ChunkKey) {
	if len(keys) > MaxViewportChunkKeys {
		s.mu.Lock()
		s.cache = make(map[ChunkKey]Chunk)
		s.cacheVertices = 0
		s.mu.Unlock()
		return
	}
	keep := make(map[ChunkKey]struct{}, len(keys))
	for _, key := range keys {
		keep[key] = struct{}{}
	}
	s.mu.Lock()
	retained := make(map[ChunkKey]Chunk, min(len(keep), len(s.cache)))
	retainedVertices := 0
	for key := range keep {
		if chunk, exists := s.cache[key]; exists {
			retained[key] = chunk
			retainedVertices += len(chunk.Vertices)
		}
	}
	s.cache = retained
	s.cacheVertices = retainedVertices
	s.mu.Unlock()
}

// RetainViewportChunks keeps every cached visible chunk and a larger-first,
// vertex-bounded subset of cached hidden chunks from the same viewport.
func (s *Scheduler) RetainViewportChunks(visibleKeys, hiddenKeys []ChunkKey, hiddenVertexLimit int) {
	if hiddenVertexLimit < 0 {
		hiddenVertexLimit = 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	retained := make(map[ChunkKey]Chunk, min(len(visibleKeys)+len(hiddenKeys), len(s.cache)))
	visibleSet := make(map[ChunkKey]struct{}, len(visibleKeys))
	retainedVertices := 0
	for _, key := range visibleKeys {
		if _, exists := visibleSet[key]; exists {
			continue
		}
		visibleSet[key] = struct{}{}
		if chunk, exists := s.cache[key]; exists {
			retained[key] = chunk
			retainedVertices += len(chunk.Vertices)
		}
	}
	hiddenLimit := min(hiddenVertexLimit, max(0, MaxBatchVertices-retainedVertices))
	type entry struct {
		key   ChunkKey
		chunk Chunk
	}
	candidates := make([]entry, 0, min(len(hiddenKeys), len(s.cache)))
	seenHidden := make(map[ChunkKey]struct{}, len(hiddenKeys))
	for _, key := range hiddenKeys {
		if _, visible := visibleSet[key]; visible {
			continue
		}
		if _, duplicate := seenHidden[key]; duplicate {
			continue
		}
		seenHidden[key] = struct{}{}
		if chunk, exists := s.cache[key]; exists {
			candidates = append(candidates, entry{key: key, chunk: chunk})
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		return len(candidates[i].chunk.Vertices) > len(candidates[j].chunk.Vertices)
	})
	hiddenVertices := 0
	for _, item := range candidates {
		count := len(item.chunk.Vertices)
		if count > hiddenLimit-hiddenVertices {
			continue
		}
		retained[item.key] = item.chunk
		hiddenVertices += count
	}
	s.cache = retained
	s.cacheVertices = retainedVertices + hiddenVertices
}

// cacheChunkLocked accounts immutable geometry payload by vertex count;
// callers hold s.mu. A rejected replacement removes the old same-key value.
func (s *Scheduler) cacheChunkLocked(key ChunkKey, chunk Chunk, limit int) bool {
	previousCount := 0
	previous, previousExists := s.cache[key]
	if previousExists {
		previousCount = len(previous.Vertices)
	}
	if !batchVertexCountWithinLimit(s.cacheVertices, previousCount, len(chunk.Vertices), limit) {
		if previousExists {
			delete(s.cache, key)
			s.cacheVertices -= previousCount
		}
		return false
	}
	chunk.Key = key
	s.cache[key] = chunk
	s.cacheVertices += len(chunk.Vertices) - previousCount
	return true
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
	if len(keys) > MaxViewportChunkKeys {
		return s.rejectOversizedRequest(len(keys))
	}
	return s.request(ctx, uniqueChunkKeys(keys), builder)
}

// RequestUnique is the allocation-friendly form of Request for callers that
// already guarantee each key appears once. The planner/visibility pipeline
// uses this path because it creates disjoint ordered keys per layer.
func (s *Scheduler) RequestUnique(ctx context.Context, keys []ChunkKey, builder ChunkBuilder) <-chan ChunkResult {
	if len(keys) > MaxViewportChunkKeys {
		return s.rejectOversizedRequest(len(keys))
	}
	return s.request(ctx, keys, builder)
}

func (s *Scheduler) rejectOversizedRequest(count int) <-chan ChunkResult {
	results := make(chan ChunkResult, 1)
	results <- ChunkResult{
		Generation: s.Generation(),
		Err:        fmt.Errorf("viewport request has %d chunk keys; safety limit is %d", count, MaxViewportChunkKeys),
	}
	close(results)
	return results
}

func (s *Scheduler) request(ctx context.Context, uniqueKeys []ChunkKey, builder ChunkBuilder) <-chan ChunkResult {
	resultBuffer := len(uniqueKeys)
	if resultBuffer > 32 {
		resultBuffer = 32
	}
	results := make(chan ChunkResult, resultBuffer)
	requestGeneration := s.Generation()
	s.stats.requests.Add(1)
	snapshot, cachedKeys, missing, buffers := s.splitCachedRequest(uniqueKeys)
	var cached []Chunk
	if snapshot != nil {
		cached = snapshot.chunks
	}
	if missing == nil {
		s.stats.cacheHits.Add(uint64(len(cached)))
		if len(cached) <= resultBuffer {
			for index, chunk := range cached {
				if ctx.Err() != nil {
					break
				}
				results <- ChunkResult{Key: uniqueKeys[index], Generation: requestGeneration, Chunk: chunk}
			}
			s.releaseCachedSnapshot(snapshot)
			if ctx.Err() != nil {
				s.stats.canceledCalls.Add(1)
			}
			close(results)
			return results
		}
		go func(snapshot *cachedChunkSnapshot, buffers *schedulerRequestBuffers) {
			defer close(results)
			defer s.releaseCachedSnapshot(snapshot)
			defer s.releaseRequestBuffers(buffers)
			defer func() {
				if ctx.Err() != nil {
					s.stats.canceledCalls.Add(1)
				}
			}()
			for index, chunk := range cached {
				select {
				case results <- ChunkResult{Key: uniqueKeys[index], Generation: requestGeneration, Chunk: chunk}:
				case <-ctx.Done():
					return
				}
			}
		}(snapshot, buffers)
		return results
	}
	if len(cached) > 0 {
		s.stats.cacheHits.Add(uint64(len(cached)))
		go func(snapshot *cachedChunkSnapshot, buffers *schedulerRequestBuffers) {
			defer close(results)
			defer s.releaseCachedSnapshot(snapshot)
			defer s.releaseRequestBuffers(buffers)
			defer func() {
				if ctx.Err() != nil {
					s.stats.canceledCalls.Add(1)
				}
			}()
			for index, chunk := range cached {
				select {
				case results <- ChunkResult{Key: cachedKeys[index], Generation: requestGeneration, Chunk: chunk}:
				case <-ctx.Done():
					return
				}
			}
			s.runMissing(ctx, requestGeneration, missing, builder, results)
		}(snapshot, buffers)
		return results
	}

	go func() {
		defer close(results)
		defer func() {
			if ctx.Err() != nil {
				s.stats.canceledCalls.Add(1)
			}
		}()
		s.runMissing(ctx, requestGeneration, missing, builder, results)
	}()

	return results
}

func (s *Scheduler) splitCachedRequest(keys []ChunkKey) (*cachedChunkSnapshot, []ChunkKey, []ChunkKey, *schedulerRequestBuffers) {
	if len(keys) == 0 {
		return nil, nil, keys, nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	var snapshot *cachedChunkSnapshot
	var cachedKeys []ChunkKey
	var missing []ChunkKey
	var buffers *schedulerRequestBuffers
	firstMissing := -1
	seenHit := false
	for index, key := range keys {
		chunk, ok := s.cache[key]
		if !ok {
			if !seenHit {
				if firstMissing < 0 {
					firstMissing = index
				}
				continue
			}
			if missing == nil {
				missing = make([]ChunkKey, 0, len(keys))
				cachedKeys = append([]ChunkKey(nil), keys[:len(snapshot.chunks)]...)
			}
			missing = append(missing, key)
			continue
		}
		if !seenHit {
			seenHit = true
			snapshot = s.acquireCachedSnapshot(len(keys))
			if firstMissing >= 0 {
				buffers = s.acquireRequestBuffers(len(keys))
				missing = append(buffers.missing, keys[:firstMissing]...)
				cachedKeys = buffers.cachedKeys
			}
		}
		snapshot.chunks = append(snapshot.chunks, chunk)
		if cachedKeys != nil {
			cachedKeys = append(cachedKeys, key)
		}
	}
	if !seenHit {
		return nil, nil, keys, nil
	}
	if len(missing) == 0 {
		return snapshot, nil, nil, nil
	}
	if buffers == nil {
		buffers = s.acquireRequestBuffers(len(keys))
		cachedKeys = append(buffers.cachedKeys, keys[:len(snapshot.chunks)]...)
		missing = buffers.missing
	}
	buffers.cachedKeys = cachedKeys
	buffers.missing = missing
	return snapshot, cachedKeys, missing, buffers
}

func (s *Scheduler) acquireCachedSnapshot(capacity int) *cachedChunkSnapshot {
	value := s.cachedPool.Get()
	if value == nil {
		return &cachedChunkSnapshot{chunks: make([]Chunk, 0, capacity)}
	}
	snapshot := value.(*cachedChunkSnapshot)
	if cap(snapshot.chunks) < capacity {
		snapshot.chunks = make([]Chunk, 0, capacity)
	} else {
		snapshot.chunks = snapshot.chunks[:0]
	}
	return snapshot
}

func (s *Scheduler) releaseCachedSnapshot(snapshot *cachedChunkSnapshot) {
	if snapshot == nil {
		return
	}
	clear(snapshot.chunks)
	snapshot.chunks = snapshot.chunks[:0]
	s.cachedPool.Put(snapshot)
}

func (s *Scheduler) acquireRequestBuffers(capacity int) *schedulerRequestBuffers {
	value := s.requestPool.Get()
	if value == nil {
		return &schedulerRequestBuffers{
			cachedKeys: make([]ChunkKey, 0, capacity),
			missing:    make([]ChunkKey, 0, capacity),
		}
	}
	buffers := value.(*schedulerRequestBuffers)
	if cap(buffers.cachedKeys) < capacity {
		buffers.cachedKeys = make([]ChunkKey, 0, capacity)
	} else {
		buffers.cachedKeys = buffers.cachedKeys[:0]
	}
	if cap(buffers.missing) < capacity {
		buffers.missing = make([]ChunkKey, 0, capacity)
	} else {
		buffers.missing = buffers.missing[:0]
	}
	return buffers
}

func (s *Scheduler) releaseRequestBuffers(buffers *schedulerRequestBuffers) {
	if buffers == nil {
		return
	}
	clear(buffers.cachedKeys)
	clear(buffers.missing)
	buffers.cachedKeys = buffers.cachedKeys[:0]
	buffers.missing = buffers.missing[:0]
	s.requestPool.Put(buffers)
}

func (s *Scheduler) runMissing(ctx context.Context, requestGeneration uint64, keys []ChunkKey, builder ChunkBuilder, results chan<- ChunkResult) {
	workers := len(keys)
	if workers > s.MaxWorkers() {
		workers = s.MaxWorkers()
	}
	if workers == 0 {
		return
	}
	var wait sync.WaitGroup
	wait.Add(workers)
	for worker := 0; worker < workers; worker++ {
		start := len(keys) * worker / workers
		end := len(keys) * (worker + 1) / workers
		go func(start, end int) {
			defer wait.Done()
			for index := start; index < end; index++ {
				if ctx.Err() != nil {
					return
				}
				select {
				case s.workerSlots <- struct{}{}:
				case <-ctx.Done():
					return
				}
				if ctx.Err() != nil {
					<-s.workerSlots
					return
				}
				s.buildChunk(ctx, requestGeneration, keys[index], builder, results)
				<-s.workerSlots
			}
		}(start, end)
	}
	wait.Wait()
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
		s.cacheChunkLocked(key, chunk, MaxBatchVertices)
	} else {
		s.stats.staleResults.Add(1)
	}
	s.mu.Unlock()

	select {
	case results <- ChunkResult{Key: key, Generation: requestGeneration, Chunk: chunk, Stale: stale}:
	case <-ctx.Done():
	}
}
