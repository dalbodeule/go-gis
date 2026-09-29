//go:build native

package main

import (
	"context"
	"path/filepath"
	"testing"

	"gogis/drivers/gdal"
	"gogis/internal/core"
)

func TestRunMergeGeoPackagesToGeoPackage(t *testing.T) {
	directory := t.TempDir()
	fields := []core.Field{{Name: "name", Type: core.FieldTypeText}}
	for _, input := range []struct {
		name string
		id   uint64
		text string
	}{
		{name: "a", id: 1, text: "첫 번째"},
		{name: "b", id: 2, text: "두 번째"},
	} {
		path := filepath.Join(directory, input.name+".gpkg")
		layer := core.Layer{
			Name: "roads", CRS: core.CRS{AuthorityCode: "EPSG:5179"}, Fields: fields,
			Features: []core.Feature{{ID: input.id, Geometry: core.WKTGeometry{WKT: "POINT (1 2)"}, Properties: map[string]any{"name": input.text}}},
		}
		if err := (gdal.Writer{}).Write(context.Background(), path, layer); err != nil {
			t.Fatalf("create %s: %v", input.name, err)
		}
	}
	output := filepath.Join(directory, "merged.gpkg")
	if err := runMerge(context.Background(), mergeOptions{
		inputs: []string{filepath.Join(directory, "a.gpkg"), filepath.Join(directory, "b.gpkg")},
		layers: []string{"roads", "roads"}, output: output,
	}); err != nil {
		t.Fatalf("run merge: %v", err)
	}
	result, err := (gdal.Reader{}).Open(context.Background(), output, "merged")
	if err != nil {
		t.Fatalf("read merged output: %v", err)
	}
	if len(result.Features) != 2 || result.CRS.AuthorityCode != "EPSG:5179" {
		t.Fatalf("unexpected merged layer: %#v", result)
	}
}
