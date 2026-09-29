package render

import (
	"context"
	"testing"
)

func BenchmarkChunkPlannerVisibleKeys(b *testing.B) {
	planner := ChunkPlanner{ChunkSize: 0.01, Margin: 1}
	viewport := Viewport{Center: Point{X: 0.5, Y: 0.5}, Zoom: 2}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = planner.VisibleKeys(viewport, "roads")
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
