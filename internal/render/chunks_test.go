package render

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestViewportBatchBudgetFitsQtSceneGraphExpansionLimit(t *testing.T) {
	const maxQtSceneGraphVertices = 8 * 1024 * 1024
	const maxExpandedVerticesPerSourceVertex = 3
	if expanded := MaxBatchVertices * maxExpandedVerticesPerSourceVertex; expanded > maxQtSceneGraphVertices {
		t.Fatalf("viewport source budget %d may expand to %d Qt vertices, exceeding %d",
			MaxBatchVertices, expanded, maxQtSceneGraphVertices)
	}
}

func TestSchedulerConfiguresBoundedWorkerCount(t *testing.T) {
	for _, test := range []struct{ configured, want int }{{-1, 1}, {0, 1}, {1, 1}, {3, 3}, {100, 64}} {
		scheduler := NewSchedulerWithMaxWorkers(test.configured)
		if got := scheduler.MaxWorkers(); got != test.want {
			t.Errorf("worker limit for %d = %d, want %d", test.configured, got, test.want)
		}
	}
	if got := NewScheduler().MaxWorkers(); got != 4 {
		t.Fatalf("default worker limit = %d, want 4", got)
	}
}

func TestSchedulerWorkerLimitAppliesAcrossOverlappingRequests(t *testing.T) {
	const workerLimit = 2
	scheduler := NewSchedulerWithMaxWorkers(workerLimit)
	active := atomic.Int32{}
	maximumActive := atomic.Int32{}
	started := make(chan struct{}, 8)
	release := make(chan struct{})
	builder := func(_ context.Context, key ChunkKey) (Chunk, error) {
		current := active.Add(1)
		for previous := maximumActive.Load(); current > previous && !maximumActive.CompareAndSwap(previous, current); previous = maximumActive.Load() {
		}
		started <- struct{}{}
		<-release
		active.Add(-1)
		return Chunk{Key: key, Vertices: []Vertex{{X: float32(key.X)}}}, nil
	}
	first := scheduler.Request(context.Background(), []ChunkKey{{Layer: "roads", X: 1}, {Layer: "roads", X: 2}}, builder)
	for range workerLimit {
		select {
		case <-started:
		case <-time.After(time.Second):
			close(release)
			t.Fatal("first request did not occupy both worker slots")
		}
	}
	second := scheduler.Request(context.Background(), []ChunkKey{{Layer: "roads", X: 3}, {Layer: "roads", X: 4}}, builder)
	overlapped := false
	select {
	case <-started:
		overlapped = true
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	firstResults, secondResults := collect(first), collect(second)
	if overlapped || maximumActive.Load() > workerLimit {
		t.Fatalf("overlapping requests ran %d builders concurrently (worker limit %d)", maximumActive.Load(), workerLimit)
	}
	if len(firstResults) != 2 || len(secondResults) != 2 {
		t.Fatalf("request results: first=%d second=%d; want 2 each", len(firstResults), len(secondResults))
	}
}

func TestSchedulerCachesChunksAndDeduplicatesKeys(t *testing.T) {
	scheduler := NewScheduler()
	key := ChunkKey{Layer: "roads", ZoomBucket: 4, X: 2, Y: 3}
	var builds atomic.Int32
	builder := func(context.Context, ChunkKey) (Chunk, error) {
		builds.Add(1)
		return Chunk{Vertices: []Vertex{{X: 1, Y: 2}}}, nil
	}

	results := collect(scheduler.Request(context.Background(), []ChunkKey{key, key}, builder))
	if len(results) != 1 || results[0].Err != nil {
		t.Fatalf("first request = %#v", results)
	}
	if results[0].Chunk.Key != key || results[0].Chunk.Generation != 0 {
		t.Fatalf("chunk metadata = %#v", results[0].Chunk)
	}
	if results[0].Generation != 0 {
		t.Fatalf("result generation = %d, want 0", results[0].Generation)
	}

	results = collect(scheduler.Request(context.Background(), []ChunkKey{key}, builder))
	if len(results) != 1 || builds.Load() != 1 {
		t.Fatalf("cached request = %#v, builds = %d", results, builds.Load())
	}
}

func TestSchedulerRetainOnlyBoundsViewportCache(t *testing.T) {
	scheduler := NewScheduler()
	first := ChunkKey{Layer: "roads", ZoomBucket: 3, X: 1, Y: 2}
	second := ChunkKey{Layer: "roads", ZoomBucket: 3, X: 2, Y: 2}
	third := ChunkKey{Layer: "roads", ZoomBucket: 3, X: 3, Y: 2}
	builder := func(_ context.Context, key ChunkKey) (Chunk, error) {
		return Chunk{Key: key, Vertices: []Vertex{{X: float32(key.X)}}}, nil
	}
	collect(scheduler.Request(context.Background(), []ChunkKey{first, second, third}, builder))

	scheduler.RetainOnly([]ChunkKey{second})
	if _, ok := scheduler.Cached(second); !ok {
		t.Fatal("visible chunk was evicted")
	}
	if _, ok := scheduler.Cached(first); ok {
		t.Fatal("chunk outside the retained viewport was kept")
	}
	if _, ok := scheduler.Cached(third); ok {
		t.Fatal("chunk outside the retained viewport was kept")
	}
}

func TestSchedulerRetainOnlyRebuildsCacheAcrossVisitedViewports(t *testing.T) {
	scheduler := NewScheduler()
	for x := 0; x < 100; x++ {
		key := ChunkKey{Layer: "roads", X: x}
		results := collect(scheduler.Request(context.Background(), []ChunkKey{key}, func(_ context.Context, key ChunkKey) (Chunk, error) {
			return Chunk{Key: key, Vertices: []Vertex{{X: float32(key.X)}}}, nil
		}))
		if len(results) != 1 || results[0].Err != nil {
			t.Fatalf("request for viewport %d = %#v", x, results)
		}
		scheduler.RetainOnly([]ChunkKey{key})
		if len(scheduler.cache) != 1 {
			t.Fatalf("viewport %d retained %d cache entries, want 1", x, len(scheduler.cache))
		}
	}
	scheduler.RetainOnly(nil)
	if len(scheduler.cache) != 0 {
		t.Fatalf("empty viewport retained %d cache entries", len(scheduler.cache))
	}
}

func TestSchedulerRetainViewportChunksReservesVisibleAndBoundsHiddenCache(t *testing.T) {
	scheduler := NewScheduler()
	visible := ChunkKey{Layer: "buildings", X: 1}
	hiddenLarge := ChunkKey{Layer: "parcels", X: 1}
	hiddenMedium := ChunkKey{Layer: "parcels", X: 2}
	hiddenSmall := ChunkKey{Layer: "points", X: 1}
	scheduler.mu.Lock()
	scheduler.cacheChunkLocked(visible, Chunk{Vertices: make([]Vertex, 4)}, MaxBatchVertices)
	scheduler.cacheChunkLocked(hiddenLarge, Chunk{Vertices: make([]Vertex, 8)}, MaxBatchVertices)
	scheduler.cacheChunkLocked(hiddenMedium, Chunk{Vertices: make([]Vertex, 4)}, MaxBatchVertices)
	scheduler.cacheChunkLocked(hiddenSmall, Chunk{Vertices: make([]Vertex, 1)}, MaxBatchVertices)
	scheduler.mu.Unlock()

	scheduler.RetainViewportChunks([]ChunkKey{visible}, []ChunkKey{hiddenLarge, hiddenMedium, hiddenSmall}, 9)
	for _, key := range []ChunkKey{visible, hiddenLarge, hiddenSmall} {
		if _, ok := scheduler.Cached(key); !ok {
			t.Fatalf("visible/largest-within-budget chunk %v was evicted", key)
		}
	}
	if _, ok := scheduler.Cached(hiddenMedium); ok {
		t.Fatal("hidden chunk beyond the visibility cache budget was retained")
	}
	if scheduler.cacheVertices != 13 {
		t.Fatalf("retained vertex count = %d, want visible 4 + hidden 9", scheduler.cacheVertices)
	}
}

func TestSchedulerCacheEnforcesAggregateVertexBudget(t *testing.T) {
	scheduler := NewScheduler()
	first := ChunkKey{Layer: "roads", X: 0}
	second := ChunkKey{Layer: "roads", X: 1}
	third := ChunkKey{Layer: "roads", X: 2}
	scheduler.mu.Lock()
	if !scheduler.cacheChunkLocked(first, Chunk{Vertices: make([]Vertex, 2)}, 3) ||
		!scheduler.cacheChunkLocked(second, Chunk{Vertices: make([]Vertex, 1)}, 3) {
		scheduler.mu.Unlock()
		t.Fatal("cache rejected chunks within the budget")
	}
	if scheduler.cacheChunkLocked(third, Chunk{Vertices: make([]Vertex, 1)}, 3) {
		scheduler.mu.Unlock()
		t.Fatal("cache accepted aggregate vertex overflow")
	}
	if scheduler.cacheVertices != 3 || len(scheduler.cache) != 2 {
		scheduler.mu.Unlock()
		t.Fatalf("cache after overflow: vertices=%d entries=%d", scheduler.cacheVertices, len(scheduler.cache))
	}
	if scheduler.cacheChunkLocked(first, Chunk{Vertices: make([]Vertex, 4)}, 3) {
		scheduler.mu.Unlock()
		t.Fatal("cache accepted oversized same-key replacement")
	}
	if scheduler.cacheVertices != 1 || len(scheduler.cache) != 1 {
		scheduler.mu.Unlock()
		t.Fatalf("cache after rejected replacement: vertices=%d entries=%d", scheduler.cacheVertices, len(scheduler.cache))
	}
	scheduler.mu.Unlock()

	scheduler.RetainOnly([]ChunkKey{second})
	if scheduler.cacheVertices != 1 || len(scheduler.cache) != 1 {
		t.Fatalf("cache after retain-only: vertices=%d entries=%d", scheduler.cacheVertices, len(scheduler.cache))
	}
	scheduler.InvalidateLayer("roads")
	if scheduler.cacheVertices != 0 || len(scheduler.cache) != 0 {
		t.Fatalf("cache after invalidation: vertices=%d entries=%d", scheduler.cacheVertices, len(scheduler.cache))
	}
}

func TestSchedulerRejectsOversizedViewportKeyRequests(t *testing.T) {
	scheduler := NewScheduler()
	keys := make([]ChunkKey, MaxViewportChunkKeys+1)
	built := false
	builder := func(context.Context, ChunkKey) (Chunk, error) {
		built = true
		return Chunk{}, nil
	}
	for name, results := range map[string]<-chan ChunkResult{
		"deduplicating": scheduler.Request(context.Background(), keys, builder),
		"unique":        scheduler.RequestUnique(context.Background(), keys, builder),
	} {
		result, ok := <-results
		if !ok || result.Err == nil || !strings.Contains(result.Err.Error(), "safety limit") {
			t.Errorf("%s request result = %#v, want a safety-limit error", name, result)
		}
		if _, open := <-results; open {
			t.Errorf("%s oversized request emitted extra results", name)
		}
	}
	if built {
		t.Fatal("oversized request called the chunk builder")
	}
}

func TestChunkKeyBufferBoundsCapacityAndClearsReferences(t *testing.T) {
	scheduler := NewScheduler()
	buffer := scheduler.AcquireChunkKeyBuffer(MaxViewportChunkKeys + 10)
	if cap(buffer.Keys) > MaxViewportChunkKeys {
		t.Fatalf("key buffer capacity = %d, exceeds %d", cap(buffer.Keys), MaxViewportChunkKeys)
	}
	buffer.Keys = append(buffer.Keys, ChunkKey{Layer: strings.Repeat("layer", 32)})
	backing := buffer.Keys
	scheduler.ReleaseChunkKeyBuffer(buffer)
	if len(buffer.Keys) != 0 || backing[0] != (ChunkKey{}) {
		t.Fatal("released key buffer retained a layer-key reference")
	}
}

func TestBatchStoreRejectsLateResultsAndKeepsCurrentBatch(t *testing.T) {
	store := NewBatchStore()
	first := ChunkResult{
		Generation: 0,
		Chunk:      Chunk{Vertices: []Vertex{{X: 1, Y: 1}}},
	}
	if !store.Apply(first) {
		t.Fatal("first batch was rejected")
	}
	if !store.BeginGeneration(1) {
		t.Fatal("generation transition was rejected")
	}

	late := ChunkResult{
		Generation: 0,
		Chunk:      Chunk{Vertices: []Vertex{{X: 9, Y: 9}}},
	}
	if store.Apply(late) {
		t.Fatal("late result was applied")
	}
	if generation, vertices := store.Current(); generation != 1 || len(vertices) != 1 || vertices[0].X != 1 {
		t.Fatalf("store after late result = generation %d, vertices %#v", generation, vertices)
	}

	current := ChunkResult{
		Generation: 1,
		Chunk:      Chunk{Vertices: []Vertex{{X: 2, Y: 2}}},
	}
	if !store.Apply(current) {
		t.Fatal("current result was rejected")
	}
	if _, vertices := store.Current(); len(vertices) != 1 || vertices[0].X != 2 {
		t.Fatalf("current batch = %#v", vertices)
	}
}

func TestBatchStoreAcceptsCachedChunkForNewGeneration(t *testing.T) {
	scheduler := NewScheduler()
	key := ChunkKey{Layer: "roads"}
	builder := func(context.Context, ChunkKey) (Chunk, error) {
		return Chunk{Vertices: []Vertex{{X: 4, Y: 5}}}, nil
	}
	collect(scheduler.Request(context.Background(), []ChunkKey{key}, builder))
	scheduler.AdvanceGeneration()
	store := NewBatchStore()
	store.BeginGeneration(scheduler.Generation())
	results := collect(scheduler.Request(context.Background(), []ChunkKey{key}, builder))
	if len(results) != 1 || results[0].Chunk.Generation != 0 || results[0].Generation != 1 {
		t.Fatalf("cached result = %#v", results)
	}
	if !store.Apply(results[0]) {
		t.Fatal("cached chunk was rejected for current generation")
	}
}

func TestBatchStoreMergesChunksAndDropsOutOfExtentData(t *testing.T) {
	store := NewBatchStore()
	first := ChunkKey{Layer: "roads", X: 0, Y: 0}
	second := ChunkKey{Layer: "roads", X: 1, Y: 0}
	if !store.Apply(ChunkResult{Key: first, Chunk: Chunk{Key: first, Vertices: []Vertex{{X: 1}}}}) {
		t.Fatal("first chunk was rejected")
	}
	if !store.Apply(ChunkResult{Key: second, Chunk: Chunk{Key: second, Vertices: []Vertex{{X: 2}}}}) {
		t.Fatal("second chunk was rejected")
	}
	if _, vertices := store.Current(); len(vertices) != 2 {
		t.Fatalf("merged vertices = %#v", vertices)
	}

	if !store.BeginGeneration(1, second) {
		t.Fatal("generation transition was rejected")
	}
	if _, vertices := store.Current(); len(vertices) != 1 || vertices[0].X != 2 {
		t.Fatalf("retained vertices = %#v", vertices)
	}
}

func TestBatchStoreViewportRetentionDropsVisitedIndexesAndBackingReferences(t *testing.T) {
	store := NewBatchStore()
	first := ChunkKey{Layer: "roads", X: 0}
	second := ChunkKey{Layer: "roads", X: 1}
	if !store.BeginGenerationWithVisible(1, []ChunkKey{first, second}) {
		t.Fatal("initial visible generation was rejected")
	}
	firstVertices := []Vertex{{X: 1}, {X: 2}}
	secondVertices := []Vertex{{X: 3}, {X: 4}}
	store.ApplyImmutable(ChunkResult{Key: first, Generation: 1, Chunk: Chunk{Vertices: firstVertices}})
	store.ApplyImmutable(ChunkResult{Key: second, Generation: 1, Chunk: Chunk{Vertices: secondVertices}})
	oldOrderedVerts := store.orderedVerts

	if !store.BeginGenerationWithVisible(2, []ChunkKey{second}) {
		t.Fatal("narrowed visible generation was rejected")
	}
	if len(store.chunks) != 1 || len(store.orderedIndex) != 1 || len(store.knownKeys) != 1 {
		t.Fatalf("retained maps: chunks=%d indexes=%d known=%d", len(store.chunks), len(store.orderedIndex), len(store.knownKeys))
	}
	if _, ok := store.orderedIndex[first]; ok {
		t.Fatal("out-of-viewport ordered index entry was retained")
	}
	if cap(oldOrderedVerts) > 1 && oldOrderedVerts[:cap(oldOrderedVerts)][1] != nil {
		t.Fatal("truncated ordered vertex backing array retains an old chunk slice")
	}

	for generation := uint64(3); generation < 103; generation++ {
		key := ChunkKey{Layer: "roads", X: int(generation)}
		if !store.BeginGenerationWithVisible(generation, []ChunkKey{key}) {
			t.Fatalf("generation %d was rejected", generation)
		}
		if !store.ApplyImmutable(ChunkResult{Key: key, Generation: generation, Chunk: Chunk{Vertices: []Vertex{{X: float32(generation)}}}}) {
			t.Fatalf("chunk for generation %d was rejected", generation)
		}
		if len(store.chunks) != 1 || len(store.orderedIndex) != 1 || len(store.knownKeys) != 1 {
			t.Fatalf("generation %d retained visited keys: chunks=%d indexes=%d known=%d", generation, len(store.chunks), len(store.orderedIndex), len(store.knownKeys))
		}
	}
	if !store.BeginGenerationWithVisible(103, nil) {
		t.Fatal("empty viewport generation was rejected")
	}
	if len(store.chunks) != 0 || len(store.orderedIndex) != 0 || len(store.knownKeys) != 0 || store.vertexCount != 0 {
		t.Fatalf("empty viewport retained state: chunks=%d indexes=%d known=%d vertices=%d", len(store.chunks), len(store.orderedIndex), len(store.knownKeys), store.vertexCount)
	}
	if generation, vertices := store.Current(); generation != 103 || len(vertices) != 0 {
		t.Fatalf("empty viewport batch: generation=%d vertices=%d", generation, len(vertices))
	}
}

func TestBatchStoreDeduplicatesVisibleKeysAcrossSameGeneration(t *testing.T) {
	store := NewBatchStore()
	first := ChunkKey{Layer: "roads", X: 0, Y: 0}
	second := ChunkKey{Layer: "roads", X: 1, Y: 0}
	if !store.Apply(ChunkResult{Key: first, Chunk: Chunk{Vertices: []Vertex{{X: 1}}}}) {
		t.Fatal("first chunk was rejected")
	}
	if !store.Apply(ChunkResult{Key: second, Chunk: Chunk{Vertices: []Vertex{{X: 2}}}}) {
		t.Fatal("second chunk was rejected")
	}
	if !store.BeginGeneration(1, first, first, second) {
		t.Fatal("first visible generation was rejected")
	}
	if _, vertices := store.Current(); len(vertices) != 2 {
		t.Fatalf("deduplicated vertices = %#v", vertices)
	}
	if !store.BeginGeneration(1, second) {
		t.Fatal("same-numbered generation refresh was rejected")
	}
	if _, vertices := store.Current(); len(vertices) != 1 || vertices[0].X != 2 {
		t.Fatalf("same-generation extent = %#v", vertices)
	}
}

func TestBatchStoreSameVisibleKeysOnlyUpdatesGeneration(t *testing.T) {
	store := NewBatchStore()
	keys := []ChunkKey{{Layer: "roads", X: 0}, {Layer: "roads", X: 1}}
	if !store.BeginGeneration(1, keys...) {
		t.Fatal("initial generation was rejected")
	}
	if !store.ApplyImmutable(ChunkResult{Key: keys[0], Generation: 1, Chunk: Chunk{Vertices: []Vertex{{X: 1}}}}) {
		t.Fatal("initial chunk was rejected")
	}
	if !store.BeginGeneration(2, keys...) {
		t.Fatal("same visible generation was rejected")
	}
	if got := store.Generation(); got != 2 {
		t.Fatalf("generation = %d, want 2", got)
	}
	if _, vertices := store.Current(); len(vertices) != 1 || vertices[0].X != 1 {
		t.Fatalf("same visible keys dropped current vertices: %#v", vertices)
	}
}

func TestBatchStoreRejectsAggregateVertexOverflowBeforeFlattening(t *testing.T) {
	store := NewBatchStore()
	first := ChunkKey{Layer: "roads", X: 0}
	second := ChunkKey{Layer: "roads", X: 1}
	if applied, err := store.applyWithLimit(ChunkResult{Key: first, Chunk: Chunk{Vertices: []Vertex{{X: 1}, {X: 2}}}}, false, 3); err != nil || !applied {
		t.Fatalf("first chunk apply = (%t, %v), want success", applied, err)
	}
	if applied, err := store.applyWithLimit(ChunkResult{Key: second, Chunk: Chunk{Vertices: []Vertex{{X: 3}, {X: 4}}}}, false, 3); err == nil || applied {
		t.Fatalf("aggregate overflow apply = (%t, %v), want bounded error", applied, err)
	}
	if _, vertices := store.Current(); len(vertices) != 2 || vertices[0].X != 1 || vertices[1].X != 2 {
		t.Fatalf("overflow partially changed batch: %#v", vertices)
	}
	if applied, err := store.applyWithLimit(ChunkResult{Key: first, Chunk: Chunk{Vertices: []Vertex{{X: 5}, {X: 6}, {X: 7}}}}, false, 3); err != nil || !applied {
		t.Fatalf("same-key replacement apply = (%t, %v), want success", applied, err)
	}
}

func TestBatchVertexLimitArithmetic(t *testing.T) {
	for _, test := range []struct {
		current, previous, next, limit int
		want                           bool
	}{
		{0, 0, 0, 3, true},
		{2, 0, 1, 3, true},
		{2, 0, 2, 3, false},
		{2, 2, 3, 3, true},
		{3, 4, 0, 3, false},
		{0, 0, 1, -1, false},
	} {
		if got := batchVertexCountWithinLimit(test.current, test.previous, test.next, test.limit); got != test.want {
			t.Errorf("batchVertexCountWithinLimit(%d, %d, %d, %d) = %t, want %t", test.current, test.previous, test.next, test.limit, got, test.want)
		}
	}
}

func TestBatchStoreRevisionSkipsIdenticalImmutableBatch(t *testing.T) {
	store := NewBatchStore()
	first := ChunkKey{Layer: "roads", X: 0}
	second := ChunkKey{Layer: "roads", X: 1}
	vertices := []Vertex{{X: 1, Y: 2}}
	store.BeginGeneration(1, first, second)
	initialRevision := store.Revision()
	if !store.ApplyImmutable(ChunkResult{Key: first, Generation: 1, Chunk: Chunk{Vertices: vertices}}) {
		t.Fatal("first immutable chunk was rejected")
	}
	loadedRevision := store.Revision()
	if loadedRevision <= initialRevision {
		t.Fatal("new chunk did not change the batch revision")
	}
	store.BeginGeneration(2, first, second)
	if store.Revision() != loadedRevision {
		t.Fatal("identical visible order changed the batch revision")
	}
	if !store.ApplyImmutable(ChunkResult{Key: first, Generation: 2, Chunk: Chunk{Vertices: vertices}}) {
		t.Fatal("cached immutable chunk was rejected")
	}
	if store.Revision() != loadedRevision {
		t.Fatal("identical cached backing changed the batch revision")
	}
	_, copiedRevision, copied := store.CurrentIntoVersion(nil)
	if copiedRevision != loadedRevision || len(copied) != 1 || copied[0] != vertices[0] {
		t.Fatalf("copied batch revision=%d vertices=%v", copiedRevision, copied)
	}
	store.BeginGeneration(3, second, first)
	if store.Revision() <= loadedRevision {
		t.Fatal("changed draw order did not change the batch revision")
	}
}

func TestSchedulerDoesNotCacheStaleWork(t *testing.T) {
	scheduler := NewScheduler()
	key := ChunkKey{Layer: "buildings", ZoomBucket: 8, X: 1, Y: 1}
	started := make(chan struct{})
	release := make(chan struct{})
	builder := func(context.Context, ChunkKey) (Chunk, error) {
		close(started)
		<-release
		return Chunk{Vertices: []Vertex{{X: 3, Y: 4}}}, nil
	}

	resultCh := scheduler.Request(context.Background(), []ChunkKey{key}, builder)
	<-started
	if got := scheduler.AdvanceGeneration(); got != 1 {
		t.Fatalf("generation = %d, want 1", got)
	}
	close(release)
	results := collect(resultCh)
	if len(results) != 1 || !results[0].Stale {
		t.Fatalf("stale result = %#v", results)
	}
	if _, ok := scheduler.Cached(key); ok {
		t.Fatal("stale chunk was cached")
	}
}

func TestSchedulerPropagatesBuilderError(t *testing.T) {
	scheduler := NewScheduler()
	want := errors.New("decode failed")
	key := ChunkKey{Layer: "labels"}
	results := collect(scheduler.Request(context.Background(), []ChunkKey{key}, func(context.Context, ChunkKey) (Chunk, error) {
		return Chunk{}, want
	}))
	if len(results) != 1 || !errors.Is(results[0].Err, want) {
		t.Fatalf("result = %#v", results)
	}
}

func TestSchedulerInvalidatesOneLayer(t *testing.T) {
	scheduler := NewScheduler()
	roads := ChunkKey{Layer: "roads", X: 1}
	buildings := ChunkKey{Layer: "buildings", X: 1}
	builder := func(context.Context, ChunkKey) (Chunk, error) { return Chunk{}, nil }
	collect(scheduler.Request(context.Background(), []ChunkKey{roads, buildings}, builder))

	scheduler.InvalidateLayer("roads")
	if _, ok := scheduler.Cached(roads); ok {
		t.Fatal("roads chunk was not invalidated")
	}
	if _, ok := scheduler.Cached(buildings); !ok {
		t.Fatal("unrelated buildings chunk was invalidated")
	}
}

func TestSchedulerHonorsCancellation(t *testing.T) {
	scheduler := NewScheduler()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	results := collect(scheduler.Request(ctx, []ChunkKey{{Layer: "roads"}}, func(context.Context, ChunkKey) (Chunk, error) {
		t.Fatal("builder ran after cancellation")
		return Chunk{}, nil
	}))
	if len(results) != 0 {
		t.Fatalf("results after cancellation = %#v", results)
	}
}

func TestSchedulerCancelsInFlightBuilder(t *testing.T) {
	scheduler := NewScheduler()
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	results := scheduler.Request(ctx, []ChunkKey{{Layer: "roads"}}, func(ctx context.Context, _ ChunkKey) (Chunk, error) {
		close(started)
		<-ctx.Done()
		return Chunk{}, ctx.Err()
	})
	<-started
	cancel()
	if got := collect(results); len(got) != 0 {
		t.Fatalf("results after in-flight cancellation = %#v", got)
	}
}

func TestSchedulerBuildsVisibleChunksConcurrently(t *testing.T) {
	scheduler := NewScheduler()
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	keys := []ChunkKey{{Layer: "roads", X: 0}, {Layer: "roads", X: 1}}
	results := scheduler.Request(context.Background(), keys, func(context.Context, ChunkKey) (Chunk, error) {
		started <- struct{}{}
		<-release
		return Chunk{}, nil
	})
	<-started
	<-started
	close(release)
	if got := collect(results); len(got) != len(keys) {
		t.Fatalf("results = %#v", got)
	}
}

func TestSchedulerStatsTrackCacheStaleAndCancellation(t *testing.T) {
	scheduler := NewScheduler()
	key := ChunkKey{Layer: "roads"}
	builder := func(context.Context, ChunkKey) (Chunk, error) {
		return Chunk{Vertices: []Vertex{{X: 1}}}, nil
	}
	collect(scheduler.Request(context.Background(), []ChunkKey{key}, builder))
	collect(scheduler.Request(context.Background(), []ChunkKey{key}, builder))
	if got := scheduler.Stats(); got.Requests != 2 || got.ChunksBuilt != 1 || got.CacheHits != 1 {
		t.Fatalf("cache stats = %#v", got)
	}

	started := make(chan struct{})
	release := make(chan struct{})
	staleResults := scheduler.Request(context.Background(), []ChunkKey{{Layer: "buildings"}}, func(context.Context, ChunkKey) (Chunk, error) {
		close(started)
		<-release
		return Chunk{}, nil
	})
	<-started
	scheduler.AdvanceGeneration()
	close(release)
	collect(staleResults)
	if got := scheduler.Stats(); got.StaleResults != 1 {
		t.Fatalf("stale stats = %#v", got)
	}

	ctx, cancel := context.WithCancel(context.Background())
	canceled := scheduler.Request(ctx, []ChunkKey{{Layer: "labels"}}, func(ctx context.Context, _ ChunkKey) (Chunk, error) {
		<-ctx.Done()
		return Chunk{}, ctx.Err()
	})
	cancel()
	collect(canceled)
	if got := scheduler.Stats(); got.CanceledCalls != 1 {
		t.Fatalf("cancellation stats = %#v", got)
	}
}

func collect(results <-chan ChunkResult) []ChunkResult {
	var collected []ChunkResult
	for result := range results {
		collected = append(collected, result)
	}
	return collected
}
