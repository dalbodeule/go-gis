//go:build native

package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"gogis/drivers/gdal"
	"gogis/internal/core"
)

func TestRunScriptFiltersLabelsAndWritesGeoPackage(t *testing.T) {
	directory := t.TempDir()
	input := filepath.Join(directory, "roads.gpkg")
	layer := core.Layer{
		Name: "roads",
		CRS:  core.CRS{AuthorityCode: "EPSG:4326"},
		Fields: []core.Field{
			{Name: "name", Type: core.FieldTypeText},
			{Name: "kind", Type: core.FieldTypeText},
			{Name: "lanes", Type: core.FieldTypeNumber},
		},
		Features: []core.Feature{
			{ID: 1, Geometry: core.WKTGeometry{WKT: "POINT (127.1 37.4)"}, Properties: map[string]any{"name": "Main", "kind": "primary", "lanes": 4}},
			{ID: 2, Geometry: core.WKTGeometry{WKT: "POINT (127.2 37.5)"}, Properties: map[string]any{"name": "Side", "kind": "local", "lanes": 1}},
		},
	}
	if err := (gdal.Writer{}).Write(context.Background(), input, layer); err != nil {
		t.Fatal(err)
	}

	script := filepath.Join(directory, "workflow.lua")
	source := `gogis.filter_lua("roads", "primary_roads", "return feature.kind == 'primary'")
gogis.filter_lua("primary_roads", "wide_roads", "return feature.lanes >= 2")
gogis.label_lua("roads", "road_labels", "return feature.name .. ' · ' .. feature.lanes .. '차선'", "return feature.lanes >= 2")
`
	if err := os.WriteFile(script, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(directory, "results.gpkg")
	if err := runScript(context.Background(), scriptOptions{
		inputs: []string{input}, layers: []string{"roads"}, script: script, output: output,
	}); err != nil {
		t.Fatal(err)
	}

	reader := gdal.Reader{}
	for name, wantCount := range map[string]int{"roads": 2, "primary_roads": 1, "wide_roads": 1, "road_labels": 2} {
		got, err := reader.Open(context.Background(), output, name)
		if err != nil {
			t.Fatalf("open output layer %q: %v", name, err)
		}
		if len(got.Features) != wantCount {
			t.Errorf("layer %q feature count = %d, want %d", name, len(got.Features), wantCount)
		}
	}

	shapefile := filepath.Join(directory, "wide_roads.shp")
	if err := runScript(context.Background(), scriptOptions{
		inputs: []string{input}, layers: []string{"roads"}, script: script, output: shapefile, outputLayer: "wide_roads",
	}); err != nil {
		t.Fatal(err)
	}
	selected, err := (gdal.Reader{}).Open(context.Background(), shapefile, "wide_roads")
	if err != nil {
		t.Fatal(err)
	}
	if len(selected.Features) != 1 {
		t.Fatalf("selected Shapefile output has %d features, want 1", len(selected.Features))
	}
}
