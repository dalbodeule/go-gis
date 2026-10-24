//go:build native

package geos_test

import (
	"context"
	"fmt"
	"testing"

	geosdriver "gogis/drivers/geos"
	"gogis/internal/commands"
	"gogis/internal/core"
)

func BenchmarkSplitPolygonBoundaries1000x32(b *testing.B) {
	benchmarkSplitPolygonBoundaries(b, 1000, 32)
}

func BenchmarkSplitPolygonBoundaries10000x32(b *testing.B) {
	benchmarkSplitPolygonBoundaries(b, 10000, 32)
}

func BenchmarkSplitPolygonBoundaries10000x1024(b *testing.B) {
	benchmarkSplitPolygonBoundaries(b, 10000, 1024)
}

func benchmarkSplitPolygonBoundaries(b *testing.B, featureCount, boundaryCount int) {
	source := core.Layer{Name: "parcels", CRS: core.CRS{AuthorityCode: "EPSG:5186"}, GeometryType: "POLYGON", Features: make([]core.Feature, featureCount)}
	for index := range source.Features {
		x := float64(index * 10)
		source.Features[index] = core.Feature{ID: uint64(index + 1), Geometry: core.WKTGeometry{WKT: fmt.Sprintf("POLYGON ((%g 0, %g 0, %g 1, %g 1, %g 0))", x, x+1, x+1, x, x)}}
	}
	boundaries := core.Layer{Name: "blocks", CRS: source.CRS, GeometryType: "POLYGON", Fields: []core.Field{{Name: commands.BoundaryIdentifierField, Type: core.FieldTypeText}}, Features: make([]core.Feature, boundaryCount)}
	blockSpan := featureCount * 10 / boundaryCount
	for index := range boundaries.Features {
		x := float64(index * blockSpan)
		end := x + float64(blockSpan-10)
		boundaries.Features[index] = core.Feature{ID: uint64(index + 1), Geometry: core.WKTGeometry{WKT: fmt.Sprintf("POLYGON ((%g -1, %g -1, %g 2, %g 2, %g -1))", x, end, end, x, x)}, Properties: map[string]any{commands.BoundaryIdentifierField: fmt.Sprintf("B%06d", index+1)}}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		results, err := commands.SplitPolygonLayerByBoundaries(context.Background(), geosdriver.NewOperator(), source, boundaries, "cut", 0)
		if err != nil || len(results) == 0 {
			b.Fatalf("split: %d results, %v", len(results), err)
		}
	}
}
