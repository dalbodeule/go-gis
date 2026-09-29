//go:build qt && native

package main

import (
	"context"
	"testing"

	"gogis/internal/core"
)

func TestDesktopInputArgsIncludesCRSOverrides(t *testing.T) {
	input, layer, source, target := desktopInputArgs([]string{
		"gogis-desktop", "--input", "roads.gpkg", "--layer", "roads",
		"--source-crs", "EPSG:4326", "--target-crs", "EPSG:5179",
	})
	if input != "roads.gpkg" || layer != "roads" || source != "EPSG:4326" || target != "EPSG:5179" {
		t.Fatalf("parsed desktop args = %q, %q, %q, %q", input, layer, source, target)
	}
}

func TestAlignLayerCRSTransformsToExplicitTarget(t *testing.T) {
	layers, err := alignLayerCRS(context.Background(), []core.Layer{{
		Name: "roads", CRS: core.CRS{AuthorityCode: "EPSG:4326"},
		Features: []core.Feature{{ID: 1, Geometry: core.WKTGeometry{WKT: "POINT (127 37)"}}},
	}}, "EPSG:3857")
	if err != nil {
		t.Fatal(err)
	}
	if layers[0].CRS.AuthorityCode != "EPSG:3857" {
		t.Fatalf("aligned CRS = %q", layers[0].CRS.AuthorityCode)
	}
	geometry := layers[0].Features[0].Geometry.(core.WKTGeometry)
	if geometry.WKT == "POINT (127 37)" {
		t.Fatalf("geometry was not transformed: %s", geometry.WKT)
	}
}
