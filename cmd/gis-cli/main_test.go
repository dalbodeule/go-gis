package main

import "testing"

func TestParseConvertOptions(t *testing.T) {
	options, err := parseConvertOptions([]string{"--input", "roads.gpkg", "--output", "roads.dxf", "--layer", "roads", "--target-crs", "EPSG:5179", "--profile", "ares-cp949"})
	if err != nil {
		t.Fatal(err)
	}
	if options.input != "roads.gpkg" || options.output != "roads.dxf" || options.layer != "roads" || options.targetCRS != "EPSG:5179" || options.profile != "ares-cp949" {
		t.Fatalf("options = %#v", options)
	}
}

func TestParseConvertOptionsRequiresPaths(t *testing.T) {
	if _, err := parseConvertOptions([]string{"--input", "roads.gpkg"}); err == nil {
		t.Fatal("missing output was accepted")
	}
}

func TestParseSpatialOptions(t *testing.T) {
	options, err := parseSpatialOptions([]string{
		"--operation", "buffer", "--input", "roads.gpkg", "--output", "roads-buffer.gpkg", "--distance", "10",
	})
	if err != nil {
		t.Fatal(err)
	}
	if options.operation != "buffer" || options.distance != 10 {
		t.Fatalf("unexpected spatial options: %#v", options)
	}
}

func TestParseSpatialOptionsRequiresRightInputForBinaryOperation(t *testing.T) {
	if _, err := parseSpatialOptions([]string{"--operation", "union", "--input", "left.gpkg", "--output", "out.gpkg"}); err == nil {
		t.Fatal("expected right input validation error")
	}
}

func TestParseMergeOptionsAcceptsRepeatedInputs(t *testing.T) {
	options, err := parseMergeOptions([]string{
		"--input", "a.gpkg", "--input", "b.gpkg", "--layer", "a", "--layer", "b", "--output", "merged.gpkg",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(options.inputs) != 2 || options.layers[1] != "b" || options.output != "merged.gpkg" {
		t.Fatalf("unexpected merge options: %#v", options)
	}
}

func TestParseMergeOptionsRequiresMatchingLayerCount(t *testing.T) {
	if _, err := parseMergeOptions([]string{"--input", "a.gpkg", "--input", "b.gpkg", "--layer", "a", "--output", "merged.gpkg"}); err == nil {
		t.Fatal("expected layer count validation error")
	}
}

func TestParseFilterOptions(t *testing.T) {
	options, err := parseFilterOptions([]string{"--input", "roads.gpkg", "--layer", "roads", "--field", "kind", "--value", "road", "--output", "roads-only.gpkg"})
	if err != nil {
		t.Fatal(err)
	}
	if options.field != "kind" || options.value != "road" {
		t.Fatalf("unexpected filter options: %#v", options)
	}
}

func TestParseLabelOptions(t *testing.T) {
	options, err := parseLabelOptions([]string{"--input", "roads.gpkg", "--field", "name", "--rotation-field", "angle", "--output", "roads.dxf", "--height", "2.5", "--style", "Korean"})
	if err != nil {
		t.Fatal(err)
	}
	if options.height != 2.5 || options.style != "Korean" || options.rotationField != "angle" {
		t.Fatalf("unexpected label options: %#v", options)
	}
}

func TestParseScriptOptions(t *testing.T) {
	options, err := parseScriptOptions([]string{
		"--input", "roads.gpkg", "--layer", "roads",
		"--input", "parcels.gpkg", "--layer", "parcels",
		"--script", "workflow.lua", "--output", "result.gpkg", "--output-layer", "wide_roads",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(options.inputs) != 2 || len(options.layers) != 2 || options.script != "workflow.lua" || options.output != "result.gpkg" || options.outputLayer != "wide_roads" {
		t.Fatalf("unexpected script options: %#v", options)
	}
}

func TestParseScriptOptionsRequiresLayerCountToMatchInputs(t *testing.T) {
	if _, err := parseScriptOptions([]string{"--input", "roads.gpkg", "--input", "parcels.gpkg", "--layer", "roads", "--script", "workflow.lua"}); err == nil {
		t.Fatal("mismatched --layer count was accepted")
	}
}

func TestParseScriptOptionsRequiresOutputLayerForShapefile(t *testing.T) {
	args := []string{"--input", "roads.gpkg", "--script", "workflow.lua", "--output", "result.shp"}
	if _, err := parseScriptOptions(args); err == nil {
		t.Fatal("Shapefile output without --output-layer was accepted")
	}
	args = append(args, "--output-layer", "major_roads")
	if _, err := parseScriptOptions(args); err != nil {
		t.Fatalf("Shapefile output with --output-layer was rejected: %v", err)
	}
}

func TestParsePostGISImportOptions(t *testing.T) {
	options, err := parsePostGISOptions("postgis-import", []string{
		"--input", "roads.gpkg", "--dsn", "postgres://gis", "--table", "public.roads", "--layer", "roads",
	})
	if err != nil {
		t.Fatal(err)
	}
	if options.input != "roads.gpkg" || options.dsn != "postgres://gis" || options.table != "public.roads" || options.layer != "roads" {
		t.Fatalf("unexpected PostGIS options: %#v", options)
	}
}

func TestParsePostGISExportRequiresOutput(t *testing.T) {
	if _, err := parsePostGISOptions("postgis-export", []string{"--dsn", "postgres://gis", "--table", "public.roads"}); err == nil {
		t.Fatal("expected missing output error")
	}
}

func TestParseWorkflowOptions(t *testing.T) {
	options, err := parseWorkflowOptions("import-csv", []string{"--input", "points.csv", "--output", "points.gpkg", "--x-field", "X", "--y-field", "Y", "--crs", "EPSG:4326"})
	if err != nil || options.xField != "X" || options.crs != "EPSG:4326" {
		t.Fatalf("unexpected import options: %#v, %v", options, err)
	}
	if _, err := parseWorkflowOptions("join", []string{"--input", "a.gpkg", "--output", "b.gpkg"}); err == nil {
		t.Fatal("join accepted missing table keys")
	}
	merged, err := parseWorkflowOptions("import-csv", []string{"--input", "one.csv", "--input", "two.csv", "--output", "survey.gpkg", "--x-field", "X", "--y-field", "Y", "--crs", "EPSG:5186", "--boundary", "--boundary-group", "block"})
	if err != nil || len(merged.inputs) != 2 || !merged.buildBoundary || merged.boundaryGroup != "block" {
		t.Fatalf("multi-CSV boundary options = %+v, err=%v", merged, err)
	}
	if _, err := parseWorkflowOptions("split-boundary", []string{"--input", "parcels.shp", "--boundary-file", "survey.gpkg", "--boundary-layer", "survey_boundaries", "--output", "cut.gpkg"}); err != nil {
		t.Fatalf("valid boundary split options rejected: %v", err)
	}
}

func TestParsePostGISInsertAndDeleteOptions(t *testing.T) {
	insert, err := parsePostGISOptions("postgis-insert", []string{
		"--dsn", "postgres://gis", "--table", "public.roads", "--id", "7",
		"--wkt", "POINT (127 37)", "--crs", "EPSG:4326", "--properties", `{"name":"new"}`,
	})
	if err != nil || insert.id != 7 || insert.crs != "EPSG:4326" {
		t.Fatalf("unexpected insert options: %#v, err=%v", insert, err)
	}
	if _, err := parsePostGISOptions("postgis-delete", []string{"--dsn", "postgres://gis", "--table", "public.roads", "--id", "7"}); err != nil {
		t.Fatalf("valid delete options rejected: %v", err)
	}
}

func TestParsePostGISListOnlyRequiresDSN(t *testing.T) {
	if _, err := parsePostGISOptions("postgis-list", []string{"--dsn", "postgres://gis", "--schema", "public"}); err != nil {
		t.Fatalf("valid list options rejected: %v", err)
	}
	if _, err := parsePostGISOptions("postgis-list", nil); err == nil {
		t.Fatal("missing DSN was accepted")
	}
}
