package render

import (
	"context"
	"fmt"
	"testing"

	"gogis/internal/core"
)

func benchmarkLineLayer(featureCount int) core.Layer {
	features := make([]core.Feature, featureCount)
	for index := range features {
		x := float64(index % 1000)
		y := float64(index / 1000)
		features[index] = core.Feature{
			ID:       uint64(index + 1),
			Geometry: core.WKTGeometry{WKT: fmt.Sprintf("LINESTRING (%g %g, %g %g)", x, y, x+0.75, y+0.5)},
		}
	}
	return core.Layer{Name: "benchmark", Features: features}
}

func BenchmarkLayerSource100KLines(b *testing.B) {
	layer := benchmarkLineLayer(100_000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := NewLayerSource(layer); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkLayerSourceScale(b *testing.B) {
	for _, featureCount := range []int{10_000, 100_000, 1_000_000} {
		featureCount := featureCount
		b.Run(fmt.Sprintf("%d_lines", featureCount), func(b *testing.B) {
			layer := benchmarkLineLayer(featureCount)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := NewLayerSource(layer); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkLayerSource100KChunkBuild(b *testing.B) {
	source, err := NewLayerSource(benchmarkLineLayer(100_000))
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	key := ChunkKey{Layer: "benchmark", ZoomBucket: 0}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := source.Builder(ctx, key); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMultiLayerChunkBuild(b *testing.B) {
	layers := make([]core.Layer, 4)
	for index := range layers {
		layers[index] = benchmarkLineLayer(25_000)
		layers[index].Name = fmt.Sprintf("layer-%d", index)
	}
	sources, err := NewLayerSources(layers)
	if err != nil {
		b.Fatal(err)
	}
	keys := make([]ChunkKey, 0, len(sources))
	for name := range sources {
		keys = append(keys, ChunkKey{Layer: name, X: 1, Y: 1})
	}
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, key := range keys {
			if _, err := sources[key.Layer].Builder(ctx, key); err != nil {
				b.Fatal(err)
			}
		}
	}
}

func BenchmarkHitTestLinear100K(b *testing.B) {
	source, err := NewLayerSource(benchmarkLineLayer(100_000))
	if err != nil {
		b.Fatal(err)
	}
	point := Point{X: 0.5, Y: 0.5}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = HitTest(source.Features, point, 0.01)
	}
}

func BenchmarkHitTestIndexed100K(b *testing.B) {
	source, err := NewLayerSource(benchmarkLineLayer(100_000))
	if err != nil {
		b.Fatal(err)
	}
	index := NewHitIndex(source.Features, 0.01)
	point := Point{X: 0.5, Y: 0.5}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = index.HitTest(point, 0.01)
	}
}
