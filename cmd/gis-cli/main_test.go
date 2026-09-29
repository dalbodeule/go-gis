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
