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

func TestRunSpatialBufferGeoPackageToDXF(t *testing.T) {
	directory := t.TempDir()
	input := filepath.Join(directory, "points.gpkg")
	output := filepath.Join(directory, "buffer.dxf")
	layer := core.Layer{
		Name: "points",
		CRS:  core.CRS{AuthorityCode: "EPSG:5179"},
		Features: []core.Feature{{
			ID:       1,
			Geometry: core.WKTGeometry{WKT: "POINT (958000 1940000)"},
		}},
	}
	if err := (gdal.Writer{}).Write(context.Background(), input, layer); err != nil {
		t.Fatalf("create input GeoPackage: %v", err)
	}

	if err := runSpatial(context.Background(), spatialOptions{
		operation: "buffer",
		input:     input,
		layer:     "points",
		output:    output,
		distance:  10,
		profile:   "ares-utf8",
	}); err != nil {
		t.Fatalf("run spatial buffer: %v", err)
	}
	contents, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("read output DXF: %v", err)
	}
	text := string(contents)
	if !strings.Contains(text, "LWPOLYLINE") || !strings.Contains(text, "EOF") {
		t.Fatalf("buffer output is not a complete DXF: %s", text)
	}
}

func TestRunSpatialUnionDisjointPolygonsToDXF(t *testing.T) {
	directory := t.TempDir()
	leftPath := filepath.Join(directory, "left.gpkg")
	rightPath := filepath.Join(directory, "right.gpkg")
	for path, wkt := range map[string]string{
		leftPath:  "POLYGON ((0 0, 1 0, 1 1, 0 0))",
		rightPath: "POLYGON ((3 3, 4 3, 4 4, 3 3))",
	} {
		if err := (gdal.Writer{}).Write(context.Background(), path, core.Layer{
			Name: "areas", CRS: core.CRS{AuthorityCode: "EPSG:5179"},
			Features: []core.Feature{{ID: 1, Geometry: core.WKTGeometry{WKT: wkt}}},
		}); err != nil {
			t.Fatal(err)
		}
	}
	output := filepath.Join(directory, "union.dxf")
	if err := runSpatial(context.Background(), spatialOptions{
		operation: "union", input: leftPath, layer: "areas", rightInput: rightPath,
		rightLayer: "areas", output: output, profile: "ares-utf8",
	}); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(contents), "LWPOLYLINE"); got < 2 {
		t.Fatalf("disjoint union output has %d polylines, want at least 2", got)
	}
}
