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

func TestRunConvertGeoPackageToDXFWithCRSTransform(t *testing.T) {
	directory := t.TempDir()
	input := filepath.Join(directory, "roads.gpkg")
	output := filepath.Join(directory, "roads.dxf")
	layer := core.Layer{
		Name: "roads",
		CRS:  core.CRS{AuthorityCode: "EPSG:4326"},
		Fields: []core.Field{
			{Name: "name", Type: core.FieldTypeText},
			{Name: "label", Type: core.FieldTypeText},
		},
		Features: []core.Feature{{
			ID:       1,
			Geometry: core.WKTGeometry{WKT: "POINT (127.0276 37.4979)"},
			Properties: map[string]any{
				"name":  "서울 도로",
				"label": "한글 레이블",
			},
		}},
		Editable: true,
	}
	if err := (gdal.Writer{}).Write(context.Background(), input, layer); err != nil {
		t.Fatalf("create input GeoPackage: %v", err)
	}

	if err := runConvert(context.Background(), convertOptions{
		input: input, output: output, layer: "roads", targetCRS: "EPSG:5179", profile: "ares-utf8",
	}); err != nil {
		t.Fatalf("run convert: %v", err)
	}

	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("read output DXF: %v", err)
	}
	text := string(data)
	for _, expected := range []string{"$DWGCODEPAGE", "UTF-8", "POINT", "한글 레이블"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("output DXF does not contain %q:\n%s", expected, text)
		}
	}
	if strings.Contains(text, "127.0276") || strings.Contains(text, "37.4979") {
		t.Fatal("output DXF still contains untransformed WGS84 coordinates")
	}
}
