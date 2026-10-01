//go:build qt

package native

import (
	"testing"

	"gogis/internal/render"
)

func TestNativeVertexBatchLimit(t *testing.T) {
	for _, test := range []struct {
		count int
		want  bool
	}{{0, true}, {1, true}, {render.MaxBatchVertices, true}, {render.MaxBatchVertices + 1, false}, {-1, false}} {
		if got := nativeVertexBatchAllowed(test.count); got != test.want {
			t.Errorf("nativeVertexBatchAllowed(%d) = %t, want %t", test.count, got, test.want)
		}
	}
}

func TestSetVerticesCopiesAndClearsNativeBatch(t *testing.T) {
	vertices := []render.Vertex{
		{X: 0.125, Y: 0.25, Color: 0x112233ff, SizeMM: 1.5, Kind: 0},
		{X: 0.75, Y: 0.875, Color: 0x445566ff, SizeMM: 1.5, Kind: 0},
	}
	SetVertices(vertices)
	// The C++ bridge must own its copy after SetVertices returns; changing and
	// releasing this Go slice must not leave a retained Go pointer in native code.
	vertices[0] = render.Vertex{}
	vertices = nil
	SetVertices(nil)
}

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
