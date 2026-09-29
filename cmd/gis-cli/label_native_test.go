//go:build native

package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gogis/drivers/gdal"
	"gogis/internal/core"
)

func TestRunLabelGeoPackageToDXF(t *testing.T) {
	directory := t.TempDir()
	input := filepath.Join(directory, "roads.gpkg")
	output := filepath.Join(directory, "roads-labeled.dxf")
	layer := core.Layer{
		Name: "roads", CRS: core.CRS{AuthorityCode: "EPSG:5179"},
		Fields: []core.Field{{Name: "name", Type: core.FieldTypeText}},
		Features: []core.Feature{{
			ID: 1, Geometry: core.WKTGeometry{WKT: "LINESTRING (0 0, 2 0, 2 10)"},
			Properties: map[string]any{"name": "주요 도로"},
		}},
	}
	if err := (gdal.Writer{}).Write(context.Background(), input, layer); err != nil {
		t.Fatal(err)
	}
	if err := runLabel(context.Background(), labelOptions{
		input: input, layer: "roads", field: "name", output: output,
		height: 2, style: "Korean", profile: "ares-utf8",
	}); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	text := string(contents)
	for _, expected := range []string{"TEXT", "주요 도로", "2", "4", "Korean", "EOF"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("label DXF does not contain %q:\n%s", expected, text)
		}
	}
}
