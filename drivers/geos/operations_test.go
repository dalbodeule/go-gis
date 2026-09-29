//go:build native

package geos

import (
	"context"
	"strings"
	"testing"

	"gogis/internal/core"
)

func TestOperatorBuffersWithGEOS(t *testing.T) {
	layer := core.Layer{Features: []core.Feature{{ID: 1, Geometry: core.WKTGeometry{WKT: "POINT (0 0)"}}}}
	result, err := NewOperator().Buffer(context.Background(), layer, 10)
	if err != nil {
		t.Fatal(err)
	}
	geometry := result.Features[0].Geometry.(core.WKTGeometry)
	if !strings.HasPrefix(geometry.WKT, "POLYGON") {
		t.Fatalf("buffer WKT = %s", geometry.WKT)
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
		geometry := result.Features[0].Geometry.(core.WKTGeometry)
		if geometry.WKT == "" {
			t.Fatalf("%s returned empty WKT", name)
		}
	}
}
