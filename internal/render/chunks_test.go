package render

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
)

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
