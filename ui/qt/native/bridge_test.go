//go:build qt

package native

import (
	"testing"

	"gogis/internal/render"
)

func BenchmarkSetVertices100K(b *testing.B) {
	vertices := make([]render.Vertex, 100_000)
	for index := range vertices {
		vertices[index] = render.Vertex{X: float32(index) / 100_000, Y: 0.5}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		SetVertices(vertices)
	}
}
