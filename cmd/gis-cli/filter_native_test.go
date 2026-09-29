//go:build native

package main

import (
	"context"
	"path/filepath"
	"testing"

	"gogis/drivers/gdal"
	"gogis/internal/core"
)

func TestRunFilterGeoPackageToGeoPackage(t *testing.T) {
	directory := t.TempDir()
	input := filepath.Join(directory, "roads.gpkg")
	output := filepath.Join(directory, "roads-only.gpkg")
	if err := (gdal.Writer{}).Write(context.Background(), input, core.Layer{
		Name: "roads", CRS: core.CRS{AuthorityCode: "EPSG:5179"},
		Fields: []core.Field{{Name: "kind", Type: core.FieldTypeText}},
		Features: []core.Feature{
			{ID: 1, Geometry: core.WKTGeometry{WKT: "POINT (1 2)"}, Properties: map[string]any{"kind": "road"}},
			{ID: 2, Geometry: core.WKTGeometry{WKT: "POINT (3 4)"}, Properties: map[string]any{"kind": "building"}},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := runFilter(context.Background(), filterOptions{input: input, layer: "roads", field: "kind", value: "road", output: output}); err != nil {
		t.Fatal(err)
	}
	result, err := (gdal.Reader{}).Open(context.Background(), output, "roads_filtered")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Features) != 1 || result.Features[0].ID != 1 {
		t.Fatalf("unexpected filter output: %#v", result)
	}
}
