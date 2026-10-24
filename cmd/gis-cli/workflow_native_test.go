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

func TestPolygonWorkflowUsesLoadedPointGeometryAndPreservesCRS(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	input := filepath.Join(directory, "points.gpkg")
	output := filepath.Join(directory, "polygon.gpkg")
	points := core.Layer{Name: "points", CRS: core.CRS{AuthorityCode: "EPSG:5186"}, GeometryType: "POINT", Features: []core.Feature{
		{ID: 1, Geometry: core.WKTGeometry{WKT: "POINT (0 0)"}},
		{ID: 2, Geometry: core.WKTGeometry{WKT: "POINT (10 0)"}},
		{ID: 3, Geometry: core.WKTGeometry{WKT: "POINT (0 10)"}},
	}}
	if err := (gdal.Writer{}).Write(ctx, input, points); err != nil {
		t.Fatal(err)
	}
	if err := runWorkflow(ctx, "polygon", workflowOptions{input: input, layer: "points", featureIDs: "1,2,3", output: output, outputLayer: "area"}); err != nil {
		t.Fatal(err)
	}
	result, err := (gdal.Reader{}).Open(ctx, output, "area")
	if err != nil {
		t.Fatal(err)
	}
	if result.CRS.AuthorityCode != "EPSG:5186" || len(result.Features) != 1 || result.Features[0].Geometry.GeometryType() != "POLYGON" {
		t.Fatalf("unexpected polygon output: %+v", result)
	}
}

func TestJoinWorkflowRejectsDuplicateKeysBeforeWriting(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	input := filepath.Join(directory, "points.gpkg")
	table := filepath.Join(directory, "table.csv")
	output := filepath.Join(directory, "joined.gpkg")
	layer := core.Layer{Name: "points", CRS: core.CRS{AuthorityCode: "EPSG:4326"}, GeometryType: "POINT", Fields: []core.Field{{Name: "key", Type: core.FieldTypeText}}, Features: []core.Feature{{ID: 1, Geometry: core.WKTGeometry{WKT: "POINT (1 2)"}, Properties: map[string]any{"key": "001"}}}}
	if err := (gdal.Writer{}).Write(ctx, input, layer); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(table, []byte("id,grade\n001,A\n001,B\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := runWorkflow(ctx, "join", workflowOptions{input: input, layer: "points", table: table, layerKey: "key", tableKey: "id", output: output}); err == nil {
		t.Fatal("duplicate table keys were accepted")
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatalf("join wrote an output despite duplicate keys: %v", err)
	}
}

func TestImportMultipleCSVFilesWithBoundariesRoundTrips(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	first := filepath.Join(directory, "first.csv")
	second := filepath.Join(directory, "second.csv")
	for path, data := range map[string]string{
		first:  "block,x,y\nA,0,0\nA,1,0\nA,0,1\n",
		second: "block,x,y\nB,2,0\nB,3,0\nB,2,1\n",
	} {
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	output := filepath.Join(directory, "combined.gpkg")
	if err := runWorkflow(ctx, "import-csv", workflowOptions{input: first, inputs: []string{first, second}, output: output, outputLayer: "survey", xField: "x", yField: "y", crs: "EPSG:5186", delimiter: ",", encoding: "UTF-8", buildBoundary: true, boundaryGroup: "block"}); err != nil {
		t.Fatal(err)
	}
	points, err := (gdal.Reader{}).Open(ctx, output, "survey")
	if err != nil {
		t.Fatal(err)
	}
	boundaries, err := (gdal.Reader{}).Open(ctx, output, "survey_boundaries")
	if err != nil {
		t.Fatal(err)
	}
	if len(points.Features) != 6 || len(boundaries.Features) != 2 || points.Features[0].Properties["shape_id"] != "B000001" {
		t.Fatalf("unexpected merged output: %d points, %d boundaries, first=%+v", len(points.Features), len(boundaries.Features), points.Features[0])
	}
	shp := filepath.Join(directory, "parcel.shp")
	parcel := core.Layer{Name: "parcel", CRS: core.CRS{AuthorityCode: "EPSG:5186"}, GeometryType: "POLYGON", Features: []core.Feature{{ID: 1, Geometry: core.WKTGeometry{WKT: "POLYGON ((0 0, 3 0, 3 1, 0 1, 0 0))"}}}}
	if err := (gdal.Writer{}).Write(ctx, shp, parcel); err != nil {
		t.Fatal(err)
	}
	cut := filepath.Join(directory, "cut.gpkg")
	if err := runWorkflow(ctx, "split-boundary", workflowOptions{input: shp, boundaryFile: output, boundaryLayer: "survey_boundaries", output: cut, outputLayer: "cut"}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"B000001", "B000002"} {
		layer, err := (gdal.Reader{}).Open(ctx, cut, "cut_"+id)
		if err != nil || len(layer.Features) != 1 || layer.Features[0].Properties["shape_id"] != id {
			t.Fatalf("cut %s: %+v, %v", id, layer, err)
		}
	}
}
