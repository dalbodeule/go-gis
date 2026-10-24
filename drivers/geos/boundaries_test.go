//go:build native

package geos_test

import (
	"context"
	"strings"
	"testing"

	geoslib "github.com/twpayne/go-geos"
	geosdriver "gogis/drivers/geos"
	"gogis/internal/commands"
	"gogis/internal/core"
)

func TestBoundaryIntersectionKeepsPolygonFromMixedCollection(t *testing.T) {
	source := core.Layer{Name: "parcel", CRS: core.CRS{AuthorityCode: "EPSG:5186"}, GeometryType: "MULTIPOLYGON", Features: []core.Feature{{ID: 7,
		Geometry: core.WKTGeometry{WKT: "MULTIPOLYGON (((0 0, 1 0, 1 1, 0 1, 0 0)), ((2 2, 3 2, 3 3, 2 3, 2 2)))"}, Properties: map[string]any{"owner": "a"}}}}
	boundaries := core.Layer{Name: "block", CRS: source.CRS, GeometryType: "POLYGON", Fields: []core.Field{{Name: commands.BoundaryIdentifierField, Type: core.FieldTypeText}}, Features: []core.Feature{{ID: 1,
		Geometry: core.WKTGeometry{WKT: "POLYGON ((-1 -1, 2 -1, 2 2, -1 2, -1 -1))"}, Properties: map[string]any{commands.BoundaryIdentifierField: "B000001"}}}}
	left, err := geoslib.NewGeomFromWKT(source.Features[0].Geometry.(core.WKTGeometry).WKT)
	if err != nil {
		t.Fatal(err)
	}
	right, err := geoslib.NewGeomFromWKT(boundaries.Features[0].Geometry.(core.WKTGeometry).WKT)
	if err != nil {
		t.Fatal(err)
	}
	mixed := left.Intersection(right)
	if mixed.TypeID() != geoslib.TypeIDGeometryCollection {
		t.Fatalf("test fixture should produce mixed geometry, got %s", mixed.Type())
	}
	mixed.Destroy()
	left.Destroy()
	right.Destroy()
	results, err := commands.SplitPolygonLayerByBoundaries(context.Background(), geosdriver.NewOperator(), source, boundaries, "cut", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || len(results[0].Features) != 1 || !strings.Contains(results[0].Features[0].Geometry.GeometryType(), "POLYGON") || results[0].Features[0].Properties["shape_id"] != "B000001" {
		t.Fatalf("mixed intersection discarded polygon: %+v", results)
	}
	if _, exists := source.Features[0].Properties["shape_id"]; exists {
		t.Fatal("source attributes were mutated")
	}
}

func TestBoundarySplitLimitsActualCandidates(t *testing.T) {
	source := core.Layer{Name: "parcels", CRS: core.CRS{AuthorityCode: "EPSG:5186"}, GeometryType: "POLYGON", Features: []core.Feature{
		{ID: 1, Geometry: core.WKTGeometry{WKT: "POLYGON ((0 0, 1 0, 1 1, 0 1, 0 0))"}},
		{ID: 2, Geometry: core.WKTGeometry{WKT: "POLYGON ((10 0, 11 0, 11 1, 10 1, 10 0))"}},
		{ID: 3, Geometry: core.WKTGeometry{WKT: "POLYGON ((20 0, 21 0, 21 1, 20 1, 20 0))"}},
	}}
	boundaries := core.Layer{Name: "blocks", CRS: source.CRS, GeometryType: "POLYGON", Fields: []core.Field{{Name: commands.BoundaryIdentifierField, Type: core.FieldTypeText}}, Features: []core.Feature{
		{ID: 1, Geometry: core.WKTGeometry{WKT: "POLYGON ((-1 -1, 2 -1, 2 2, -1 2, -1 -1))"}, Properties: map[string]any{"shape_id": "A"}},
		{ID: 2, Geometry: core.WKTGeometry{WKT: "POLYGON ((9 -1, 12 -1, 12 2, 9 2, 9 -1))"}, Properties: map[string]any{"shape_id": "B"}},
		{ID: 3, Geometry: core.WKTGeometry{WKT: "POLYGON ((19 -1, 22 -1, 22 2, 19 2, 19 -1))"}, Properties: map[string]any{"shape_id": "C"}},
	}}
	results, err := commands.SplitPolygonLayerByBoundaries(context.Background(), geosdriver.NewOperator(), source, boundaries, "part", 3)
	if err != nil || len(results) != 3 {
		t.Fatalf("sparse candidates should fit limit: %d results, %v", len(results), err)
	}
	if _, err := commands.SplitPolygonLayerByBoundaries(context.Background(), geosdriver.NewOperator(), source, boundaries, "part", 2); err == nil {
		t.Fatal("expected candidate limit")
	}
}
