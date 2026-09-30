//go:build native

package geos

import (
	"context"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"gogis/internal/core"
)

func BenchmarkCloneLayerForOperation10KPoints(b *testing.B) {
	layer := benchmarkGEOSLayer(10_000)
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		benchmarkGEOSSink = cloneLayerForOperation(layer)
	}
}

func BenchmarkCloneLayerForOperationGeometryCloneBaseline10KPoints(b *testing.B) {
	layer := benchmarkGEOSLayer(10_000)
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		benchmarkGEOSSink = layer.Clone()
	}
}

var benchmarkGEOSSink any

func BenchmarkReadWKBPoint(b *testing.B) {
	data, err := hex.DecodeString("0101000000000000000000f03f0000000000000040")
	if err != nil {
		b.Fatal(err)
	}
	operator := NewOperator()
	geometry := core.WKBGeometry{WKB: data}
	b.ReportAllocs()
	for index := 0; index < b.N; index++ {
		result, err := operator.read(geometry)
		if err != nil {
			b.Fatal(err)
		}
		result.Destroy()
	}
}

func BenchmarkReadWKTPoint(b *testing.B) {
	operator := NewOperator()
	geometry := core.WKTGeometry{WKT: "POINT (1 2)"}
	b.ReportAllocs()
	for index := 0; index < b.N; index++ {
		result, err := operator.read(geometry)
		if err != nil {
			b.Fatal(err)
		}
		result.Destroy()
	}
}

func BenchmarkWriteGEOSWKBPoint(b *testing.B) {
	operator := NewOperator()
	geometry, err := operator.read(core.WKTGeometry{WKT: "POLYGON ((0 0, 10 0, 10 10, 0 10, 0 0))"})
	if err != nil {
		b.Fatal(err)
	}
	defer geometry.Destroy()
	b.ReportAllocs()
	for index := 0; index < b.N; index++ {
		benchmarkGEOSSink = geometry.ToWKB()
	}
}

func BenchmarkWriteGEOSWKTPoint(b *testing.B) {
	operator := NewOperator()
	geometry, err := operator.read(core.WKTGeometry{WKT: "POLYGON ((0 0, 10 0, 10 10, 0 10, 0 0))"})
	if err != nil {
		b.Fatal(err)
	}
	defer geometry.Destroy()
	b.ReportAllocs()
	for index := 0; index < b.N; index++ {
		benchmarkGEOSSink = geometry.ToWKT()
	}
}

func benchmarkGEOSLayer(count int) core.Layer {
	features := make([]core.Feature, count)
	for index := range features {
		features[index] = core.Feature{
			ID:         uint64(index + 1),
			Geometry:   core.WKTGeometry{WKT: fmt.Sprintf("POINT (%d %d)", index, index)},
			Properties: map[string]any{"name": fmt.Sprintf("feature-%d", index)},
			Label:      &core.Label{Text: "label"},
		}
	}
	return core.Layer{Fields: []core.Field{{Name: "name", Type: core.FieldTypeText}}, Features: features}
}

func TestOperatorBuffersWithGEOS(t *testing.T) {
	layer := core.Layer{Features: []core.Feature{{ID: 1, Geometry: core.WKTGeometry{WKT: "POINT (0 0)"}, Properties: map[string]any{"name": "source"}, Label: &core.Label{Text: "source"}}}}
	result, err := NewOperator().Buffer(context.Background(), layer, 10)
	if err != nil {
		t.Fatal(err)
	}
	geometry, err := core.ToWKT(result.Features[0].Geometry)
	if err != nil || !strings.HasPrefix(geometry.WKT, "POLYGON") {
		t.Fatalf("buffer WKT = %#v, err=%v", geometry, err)
	}
	result.Features[0].Properties["name"] = "changed"
	result.Features[0].Label.Text = "changed"
	if layer.Features[0].Properties["name"] != "source" || layer.Features[0].Label.Text != "source" {
		t.Fatal("buffer result shares mutable metadata with source")
	}
}

func TestOperatorReadsWKBDirectly(t *testing.T) {
	data, err := hex.DecodeString("0101000000000000000000f03f0000000000000040")
	if err != nil {
		t.Fatal(err)
	}
	result, err := NewOperator().Buffer(context.Background(), core.Layer{
		Features: []core.Feature{{ID: 1, Geometry: core.WKBGeometry{WKB: data}}},
	}, 1)
	if err != nil {
		t.Fatal(err)
	}
	geometry, err := core.ToWKT(result.Features[0].Geometry)
	if err != nil || !strings.HasPrefix(geometry.WKT, "POLYGON") {
		t.Fatalf("WKB buffer result = %#v, err=%v", result.Features[0].Geometry, err)
	}
}

func TestOperatorRunsBinaryOperationsWithGEOS(t *testing.T) {
	left := core.Layer{Features: []core.Feature{{ID: 1, Geometry: core.WKTGeometry{WKT: "POLYGON ((0 0, 10 0, 10 10, 0 10, 0 0))"}}}}
	right := core.Layer{Features: []core.Feature{{ID: 2, Geometry: core.WKTGeometry{WKT: "POLYGON ((5 5, 15 5, 15 15, 5 15, 5 5))"}}}}
	operator := NewOperator()
	for name, operation := range map[string]func(context.Context, core.Layer, core.Layer) (core.Layer, error){
		"intersection": operator.Intersect,
		"union":        operator.Union,
		"difference":   operator.Difference,
	} {
		result, err := operation(context.Background(), left, right)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		geometry, err := core.ToWKT(result.Features[0].Geometry)
		if err != nil || geometry.WKT == "" {
			t.Fatalf("%s returned empty geometry: %#v, err=%v", name, geometry, err)
		}
	}
}
