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
