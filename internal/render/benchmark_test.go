package render

import (
	"context"
	"sort"
	"sync"
	"testing"
)

var benchmarkRenderChunkSink ChunkResult

func BenchmarkChunkPlannerVisibleKeys(b *testing.B) {
	planner := ChunkPlanner{ChunkSize: 0.01, Margin: 1}
	viewport := Viewport{Center: Point{X: 0.5, Y: 0.5}, Zoom: 2}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = planner.VisibleKeys(viewport, "roads")
	}
}

func BenchmarkChunkPlannerVisibleKeysInto(b *testing.B) {
	planner := ChunkPlanner{ChunkSize: 0.01, Margin: 1}
	viewport := Viewport{Center: Point{X: 0.5, Y: 0.5}, Zoom: 2}
	keys := make([]ChunkKey, 0, 3000)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		keys = planner.VisibleKeysInto(keys[:0], viewport, "roads")
	}
}

func BenchmarkChunkPlannerVisibleKeysPooled(b *testing.B) {
	planner := ChunkPlanner{ChunkSize: 0.01, Margin: 1}
	viewport := Viewport{Center: Point{X: 0.5, Y: 0.5}, Zoom: 2}
	scheduler := NewScheduler()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		buffer := scheduler.AcquireChunkKeyBuffer(0)
		buffer.Keys = planner.VisibleKeysInto(buffer.Keys, viewport, "roads")
		keys := buffer.Keys
		if len(keys) == 0 {
			b.Fatal("planner returned no keys")
		}
		scheduler.ReleaseChunkKeyBuffer(buffer)
	}
}

func BenchmarkLayerVisibilityFilterChunkKeys(b *testing.B) {
	state := NewLayerVisibility("roads", "buildings")
	state.Set("buildings", false)
	keys := make([]ChunkKey, 0, 10_000)
	for index := 0; index < cap(keys); index++ {
		layer := "roads"
		if index%2 == 1 {
			layer = "buildings"
		}
		keys = append(keys, ChunkKey{Layer: layer, X: index})
	}
	original := append([]ChunkKey(nil), keys...)
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		copy(keys, original)
		filtered := state.FilterChunkKeys(keys)
		if len(filtered) != 5_000 {
			b.Fatal("unexpected filtered key count")
		}
	}
}

func BenchmarkLayerVisibilityFilterChunkKeysInPlace(b *testing.B) {
	state := NewLayerVisibility("roads", "buildings")
	state.Set("buildings", false)
	keys := make([]ChunkKey, 0, 10_000)
	for index := 0; index < cap(keys); index++ {
		layer := "roads"
		if index%2 == 1 {
			layer = "buildings"
		}
		keys = append(keys, ChunkKey{Layer: layer, X: index})
	}
	original := append([]ChunkKey(nil), keys...)
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		copy(keys, original)
		filtered := state.FilterChunkKeysInPlace(keys)
		if len(filtered) != 5_000 {
			b.Fatal("unexpected filtered key count")
		}
	}
}

func BenchmarkLayerVisibilityFilterChunkKeysInPlaceAllVisible(b *testing.B) {
	state := NewLayerVisibility("roads", "buildings")
	keys := make([]ChunkKey, 0, 10_000)
	for index := 0; index < cap(keys); index++ {
		layer := "roads"
		if index%2 == 1 {
			layer = "buildings"
		}
		keys = append(keys, ChunkKey{Layer: layer, X: index})
	}
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		filtered := state.FilterChunkKeysInPlace(keys)
		if len(filtered) != 10_000 {
			b.Fatal("unexpected filtered key count")
		}
	}
}

func BenchmarkSchedulerCachedVisibleRequest(b *testing.B) {
	scheduler := NewScheduler()
	planner := ChunkPlanner{ChunkSize: 0.05, Margin: 1}
	keys := planner.VisibleKeys(Viewport{Zoom: 2}, "roads")
	builder := func(context.Context, ChunkKey) (Chunk, error) {
		return Chunk{Vertices: []Vertex{{X: 0, Y: 0}, {X: 1, Y: 1}}}, nil
	}
	collect(scheduler.Request(context.Background(), keys, builder))

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		collect(scheduler.Request(context.Background(), keys, builder))
	}
}

func BenchmarkSchedulerCachedLargeVisibleRequest(b *testing.B) {
	scheduler := NewScheduler()
	planner := ChunkPlanner{ChunkSize: 0.01, Margin: 1}
	keys := planner.VisibleKeys(Viewport{Center: Point{X: 0.5, Y: 0.5}, Zoom: 2}, "roads")
	builder := func(context.Context, ChunkKey) (Chunk, error) {
		return Chunk{Vertices: []Vertex{{X: 0, Y: 0}, {X: 1, Y: 1}}}, nil
	}
	collect(scheduler.Request(context.Background(), keys, builder))
	b.ReportMetric(float64(len(keys)), "chunks/request")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		collect(scheduler.Request(context.Background(), keys, builder))
	}
}

func BenchmarkSchedulerCachedLargeVisibleRequestUnique(b *testing.B) {
	scheduler := NewScheduler()
	planner := ChunkPlanner{ChunkSize: 0.01, Margin: 1}
	keys := planner.VisibleKeys(Viewport{Center: Point{X: 0.5, Y: 0.5}, Zoom: 2}, "roads")
	builder := func(context.Context, ChunkKey) (Chunk, error) {
		return Chunk{Vertices: []Vertex{{X: 0, Y: 0}, {X: 1, Y: 1}}}, nil
	}
	collect(scheduler.RequestUnique(context.Background(), keys, builder))
	b.ReportMetric(float64(len(keys)), "chunks/request")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		collect(scheduler.RequestUnique(context.Background(), keys, builder))
	}
}

func BenchmarkSchedulerCachedLargeVisibleRequestUniqueDrain(b *testing.B) {
	scheduler := NewScheduler()
	planner := ChunkPlanner{ChunkSize: 0.01, Margin: 1}
	keys := planner.VisibleKeys(Viewport{Center: Point{X: 0.5, Y: 0.5}, Zoom: 2}, "roads")
	builder := func(context.Context, ChunkKey) (Chunk, error) {
		return Chunk{Vertices: []Vertex{{X: 0, Y: 0}, {X: 1, Y: 1}}}, nil
	}
	for result := range scheduler.RequestUnique(context.Background(), keys, builder) {
		benchmarkRenderChunkSink = result
	}
	b.ReportMetric(float64(len(keys)), "chunks/request")
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		for result := range scheduler.RequestUnique(context.Background(), keys, builder) {
			benchmarkRenderChunkSink = result
		}
	}
}

func BenchmarkSchedulerMixedVisibleRequestUnique(b *testing.B) {
	scheduler := NewScheduler()
	planner := ChunkPlanner{ChunkSize: 0.01, Margin: 1}
	baseKeys := planner.VisibleKeys(Viewport{Center: Point{X: 0.5, Y: 0.5}, Zoom: 2}, "roads")
	keys := make([]ChunkKey, len(baseKeys))
	for index, key := range baseKeys {
		key.Layer = "roads"
		if index%2 == 1 {
			key.Layer = "buildings"
		}
		keys[index] = key
	}
	builder := func(context.Context, ChunkKey) (Chunk, error) {
		return Chunk{Vertices: []Vertex{{X: 0, Y: 0}, {X: 1, Y: 1}}}, nil
	}
	collect(scheduler.RequestUnique(context.Background(), keys, builder))
	scheduler.InvalidateLayer("buildings")
	b.ReportMetric(float64(len(keys)), "chunks/request")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		collect(scheduler.RequestUnique(context.Background(), keys, builder))
		scheduler.InvalidateLayer("buildings")
	}
}

func BenchmarkSchedulerRunMissing1000(b *testing.B) {
	keys := make([]ChunkKey, 1000)
	for index := range keys {
		keys[index] = ChunkKey{Layer: "roads", ZoomBucket: 2, X: index, Y: index % 17}
	}
	builder := func(context.Context, ChunkKey) (Chunk, error) {
		return Chunk{Vertices: []Vertex{{X: 0, Y: 0}, {X: 1, Y: 1}}}, nil
	}
	b.ReportMetric(float64(len(keys)), "chunks/request")
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		scheduler := NewScheduler()
		results := make(chan ChunkResult, len(keys))
		scheduler.runMissing(context.Background(), 1, keys, builder, results)
		for range len(keys) {
			benchmarkRenderChunkSink = <-results
		}
	}
}

func BenchmarkSchedulerCachedLargeVisibleRequestLegacyGoroutines(b *testing.B) {
	planner := ChunkPlanner{ChunkSize: 0.01, Margin: 1}
	keys := planner.VisibleKeys(Viewport{Center: Point{X: 0.5, Y: 0.5}, Zoom: 2}, "roads")
	builder := func(context.Context, ChunkKey) (Chunk, error) {
		return Chunk{Vertices: []Vertex{{X: 0, Y: 0}, {X: 1, Y: 1}}}, nil
	}
	b.ReportMetric(float64(len(keys)), "chunks/request")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		legacySchedulerRequest(context.Background(), keys, builder)
	}
}

func legacySchedulerRequest(ctx context.Context, keys []ChunkKey, builder ChunkBuilder) {
	results := make(chan ChunkResult, len(keys))
	workers := len(keys)
	if workers > 4 {
		workers = 4
	}
	semaphore := make(chan struct{}, workers)
	var wait sync.WaitGroup
	for _, key := range keys {
		key := key
		wait.Add(1)
		go func() {
			defer wait.Done()
			select {
			case semaphore <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-semaphore }()
			chunk, err := builder(ctx, key)
			results <- ChunkResult{Key: key, Chunk: chunk, Err: err}
		}()
	}
	wait.Wait()
	close(results)
	collect(results)
}

func BenchmarkBatchStoreCurrent10KChunks(b *testing.B) {
	store := NewBatchStore()
	keys := make([]ChunkKey, 10_000)
	for index := range keys {
		keys[index] = ChunkKey{Layer: "roads", X: index % 100, Y: index / 100}
		store.Apply(ChunkResult{
			Key: keys[index],
			Chunk: Chunk{Vertices: []Vertex{
				{X: 0, Y: 0}, {X: 1, Y: 0}, {X: 1, Y: 1}, {X: 0, Y: 1},
			}},
		})
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, vertices := store.Current()
		if len(vertices) != len(keys)*4 {
			b.Fatalf("vertex count = %d, want %d", len(vertices), len(keys)*4)
		}
	}
}

func BenchmarkBatchStoreCurrentInto10KChunks(b *testing.B) {
	store := NewBatchStore()
	keys := make([]ChunkKey, 10_000)
	for index := range keys {
		keys[index] = ChunkKey{Layer: "roads", X: index % 100, Y: index / 100}
		store.Apply(ChunkResult{
			Key: keys[index],
			Chunk: Chunk{Vertices: []Vertex{
				{X: 0, Y: 0}, {X: 1, Y: 0}, {X: 1, Y: 1}, {X: 0, Y: 1},
			}},
		})
	}
	var scratch []Vertex
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, scratch = store.CurrentInto(scratch)
		if len(scratch) != len(keys)*4 {
			b.Fatalf("vertex count = %d, want %d", len(scratch), len(keys)*4)
		}
	}
}

func BenchmarkBatchStoreApplyCopy(b *testing.B) {
	store := NewBatchStore()
	vertices := make([]Vertex, 4_096)
	result := ChunkResult{Key: ChunkKey{Layer: "roads"}, Chunk: Chunk{Vertices: vertices}}
	if !store.Apply(result) {
		b.Fatal("initial apply failed")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if !store.Apply(result) {
			b.Fatal("apply failed")
		}
	}
}

func BenchmarkBatchStoreApplyImmutable(b *testing.B) {
	store := NewBatchStore()
	vertices := make([]Vertex, 4_096)
	result := ChunkResult{Key: ChunkKey{Layer: "roads"}, Chunk: Chunk{Vertices: vertices}}
	if !store.ApplyImmutable(result) {
		b.Fatal("initial apply failed")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if !store.ApplyImmutable(result) {
			b.Fatal("apply failed")
		}
	}
}

func BenchmarkBatchStoreBeginGenerationReusesVisibilityMap(b *testing.B) {
	store := NewBatchStore()
	keys := make([]ChunkKey, 0, 2704)
	for index := 0; index < cap(keys); index++ {
		keys = append(keys, ChunkKey{Layer: "roads", X: index % 52, Y: index / 52})
	}
	store.BeginGeneration(1, keys...)
	b.ReportMetric(float64(len(keys)), "chunks/generation")
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		store.BeginGeneration(uint64(index+2), keys...)
	}
}

func BenchmarkBatchStoreRepeatedCachedBatch100K(b *testing.B) {
	for _, skipUnchanged := range []bool{false, true} {
		name := "always-flatten"
		if skipUnchanged {
			name = "revision-skip"
		}
		b.Run(name, func(b *testing.B) {
			store := NewBatchStore()
			key := ChunkKey{Layer: "roads"}
			vertices := make([]Vertex, 100_000)
			store.BeginGeneration(1, key)
			result := ChunkResult{Key: key, Generation: 1, Chunk: Chunk{Vertices: vertices}}
			store.ApplyImmutable(result)
			publishedRevision := store.Revision()
			var scratch []Vertex
			b.ReportAllocs()
			b.ResetTimer()
			for index := 0; index < b.N; index++ {
				generation := uint64(index + 2)
				store.BeginGeneration(generation, key)
				result.Generation = generation
				store.ApplyImmutable(result)
				if !skipUnchanged || store.Revision() != publishedRevision {
					_, scratch = store.CurrentInto(scratch)
					publishedRevision = store.Revision()
				}
			}
		})
	}
}

func BenchmarkBatchStoreSortedBaseline10KChunks(b *testing.B) {
	chunks := make(map[ChunkKey][]Vertex, 10_000)
	for index := 0; index < 10_000; index++ {
		key := ChunkKey{Layer: "roads", X: index % 100, Y: index / 100}
		chunks[key] = []Vertex{{X: 0, Y: 0}, {X: 1, Y: 0}, {X: 1, Y: 1}, {X: 0, Y: 1}}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		keys := make([]ChunkKey, 0, len(chunks))
		for key := range chunks {
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
		vertices := make([]Vertex, 0, len(chunks)*4)
		for _, key := range keys {
			vertices = append(vertices, chunks[key]...)
		}
		if len(vertices) != len(chunks)*4 {
			b.Fatalf("vertex count = %d, want %d", len(vertices), len(chunks)*4)
		}
	}
}
