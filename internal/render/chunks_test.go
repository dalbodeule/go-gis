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

	results = collect(scheduler.Request(context.Background(), []ChunkKey{key}, builder))
	if len(results) != 1 || builds.Load() != 1 {
		t.Fatalf("cached request = %#v, builds = %d", results, builds.Load())
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

func collect(results <-chan ChunkResult) []ChunkResult {
	var collected []ChunkResult
	for result := range results {
		collected = append(collected, result)
	}
	return collected
}
