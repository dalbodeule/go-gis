//go:build qt && native

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gogis/drivers/gdal"
	"gogis/internal/core"

	"github.com/airbusgeo/godal"
)

func TestDesktopLoadsOnlySelectedGeoPackageLayer(t *testing.T) {
	godal.RegisterAll()
	path := filepath.Join(t.TempDir(), "layers.gpkg")
	dataset, err := godal.CreateVector(godal.GeoPackage, path)
	if err != nil {
		t.Fatal(err)
	}
	spatialRef, err := godal.NewSpatialRef("EPSG:4326")
	if err != nil {
		_ = dataset.Close()
		t.Fatal(err)
	}
	defer spatialRef.Close()
	for _, name := range []string{"first", "selected"} {
		layer, err := dataset.CreateLayer(name, spatialRef, godal.GTPoint)
		if err != nil {
			_ = dataset.Close()
			t.Fatal(err)
		}
		geometry, err := godal.NewGeometryFromWKT("POINT (127 37)", spatialRef)
		if err != nil {
			_ = dataset.Close()
			t.Fatal(err)
		}
		feature, err := layer.NewFeature(geometry)
		geometry.Close()
		if err != nil {
			_ = dataset.Close()
			t.Fatal(err)
		}
		if err := layer.UpdateFeature(feature); err != nil {
			feature.Close()
			_ = dataset.Close()
			t.Fatal(err)
		}
		feature.Close()
	}
	if err := dataset.Close(); err != nil {
		t.Fatal(err)
	}
	for _, readOnly := range []bool{true, false} {
		runtime, err := loadDataRuntimeModeContext(context.Background(), path, "selected", "", "", "", readOnly)
		if err != nil {
			t.Fatalf("readOnly=%t: %v", readOnly, err)
		}
		if names := runtime.service.LayerNames(); len(names) != 1 || names[0] != "selected" {
			t.Fatalf("readOnly=%t: layer names = %v", readOnly, names)
		}
		if len(runtime.features) != 1 {
			t.Fatalf("readOnly=%t: features = %d", readOnly, len(runtime.features))
		}
		if runtime.closeAttributeSource != nil {
			runtime.closeAttributeSource()
		}
	}
}

func TestPreviewBoundsRequiresLargeCommonCRSDataset(t *testing.T) {
	overviews := []gdal.LayerOverview{
		{Name: "west", CRS: core.CRS{AuthorityCode: "EPSG:4326"}, Bounds: [4]float64{0, 0, 10, 10}, HasBounds: true, FeatureCount: previewMinimumFeatures - 1},
		{Name: "east", CRS: core.CRS{AuthorityCode: "EPSG:4326"}, Bounds: [4]float64{20, -5, 30, 5}, HasBounds: true, FeatureCount: 1},
	}
	bounds, ok := previewBounds(overviews, "", "", "")
	if !ok || bounds != ([4]float64{0, -5, 30, 10}) {
		t.Fatalf("combined preview bounds = %v, %t", bounds, ok)
	}
	if _, ok := previewBounds(overviews, "west", "", ""); ok {
		t.Fatal("small selected layer was previewed")
	}
	if _, ok := previewBounds(overviews, "missing", "", ""); ok {
		t.Fatal("missing selected layer was previewed")
	}
	if _, ok := previewBounds(overviews, "", "", "EPSG:3857"); ok {
		t.Fatal("preview accepted a target CRS requiring transformation")
	}
	overviews[1].CRS.AuthorityCode = "EPSG:3857"
	if _, ok := previewBounds(overviews, "", "", ""); ok {
		t.Fatal("preview accepted mixed source CRSs")
	}
	if _, ok := previewBounds(overviews, "", "EPSG:4326", ""); !ok {
		t.Fatal("explicit source CRS should allow a common coordinate space")
	}
	overviews[1].HasBounds = false
	if _, ok := previewBounds(overviews, "", "EPSG:4326", ""); ok {
		t.Fatal("preview accepted a layer without bounds")
	}
	manySmall := make([]gdal.LayerOverview, 100)
	for index := range manySmall {
		manySmall[index] = gdal.LayerOverview{Name: fmt.Sprintf("layer-%d", index), CRS: core.CRS{AuthorityCode: "EPSG:4326"}, Bounds: [4]float64{0, 0, 1, 1}, HasBounds: true, FeatureCount: 500}
	}
	if _, ok := previewBounds(manySmall, "", "", ""); ok {
		t.Fatal("preview would duplicate a full read across many small layers")
	}
}

func TestStalePreviewDoesNotReplaceRuntime(t *testing.T) {
	current := &demoRuntime{loadGeneration: 2}
	stale := &demoRuntime{}
	current.replaceWithPreview(stale, 1)
	if current.previewLoading || current.scheduler != nil || current.loadGeneration != 2 {
		t.Fatal("stale preview changed current runtime")
	}
}

func TestLargeReadOnlyLoadPublishesStablePreview(t *testing.T) {
	path := largeReadOnlyFixture(t)
	var preview *demoRuntime
	full, err := loadDataRuntimeModeContextWithPreview(context.Background(), path, "", "", "", "", true, func(next *demoRuntime) {
		preview = next
	})
	if err != nil {
		t.Fatal(err)
	}
	defer full.closeAttributeSource()
	if preview == nil || len(preview.features) != previewFeatureLimit || len(full.features) != previewMinimumFeatures {
		t.Fatalf("preview/full feature counts = %d/%d", len(preview.features), len(full.features))
	}
	if preview.features[previewFeatureLimit-1].Vertices[0] != full.features[previewFeatureLimit-1].Vertices[0] {
		t.Fatalf("preview/full coordinates differ: %v / %v", preview.features[previewFeatureLimit-1].Vertices[0], full.features[previewFeatureLimit-1].Vertices[0])
	}
	if preview.closeAttributeSource != nil || preview.attributeFeatureReader != nil {
		t.Fatal("preview retained the full loader's attribute session")
	}
}

func largeReadOnlyFixture(tb testing.TB) string {
	tb.Helper()
	path := filepath.Join(tb.TempDir(), "large.geojson")
	var fixture strings.Builder
	fixture.Grow(previewMinimumFeatures * 100)
	fixture.WriteString(`{"type":"FeatureCollection","features":[`)
	for index := 0; index < previewMinimumFeatures; index++ {
		if index > 0 {
			fixture.WriteByte(',')
		}
		fmt.Fprintf(&fixture, `{"type":"Feature","properties":{"name":"point-%d"},"geometry":{"type":"Point","coordinates":[%d,%d]}}`, index, index, index)
	}
	fixture.WriteString(`]}`)
	if err := os.WriteFile(path, []byte(fixture.String()), 0o600); err != nil {
		tb.Fatal(err)
	}
	return path
}

func TestDesktopInputArgsIncludesCRSOverrides(t *testing.T) {
	input, layer, source, target := desktopInputArgs([]string{
		"gogis-desktop", "--input", "roads.gpkg", "--layer", "roads",
		"--source-crs", "EPSG:4326", "--target-crs", "EPSG:5179",
	})
	if input != "roads.gpkg" || layer != "roads" || source != "EPSG:4326" || target != "EPSG:5179" {
		t.Fatalf("parsed desktop args = %q, %q, %q, %q", input, layer, source, target)
	}
}

func TestDesktopReadOnlyFlag(t *testing.T) {
	if !desktopReadOnly([]string{"gogis-desktop", "--read-only"}) {
		t.Fatal("read-only flag was not detected")
	}
	if desktopReadOnly([]string{"gogis-desktop", "--input", "roads.gpkg"}) {
		t.Fatal("read-only flag was detected unexpectedly")
	}
}

func TestLayerMetadataOnlyDropsLargeGeometrySnapshot(t *testing.T) {
	layers := layerMetadataOnly([]core.Layer{{
		Name: "roads", CRS: core.CRS{AuthorityCode: "EPSG:4326"},
		Fields:   []core.Field{{Name: "name"}},
		Features: []core.Feature{{ID: 1, Geometry: core.WKTGeometry{WKT: "POINT (1 2)"}, Properties: map[string]any{"name": "road"}}},
		Editable: true,
	}})
	if len(layers) != 1 || layers[0].Name != "roads" || layers[0].CRS.AuthorityCode != "EPSG:4326" {
		t.Fatalf("metadata layers = %#v", layers)
	}
	if layers[0].Fields != nil || layers[0].Features != nil || layers[0].Editable {
		t.Fatalf("large snapshot was retained: %#v", layers[0])
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

func TestReadOnlyFeatureNameCacheAvoidsRepeatedLookup(t *testing.T) {
	lookups := 0
	runtime := &demoRuntime{
		readOnly: true,
		attributeFeatureReader: func(context.Context, string, uint64) (core.Feature, error) {
			lookups++
			return core.Feature{Properties: map[string]any{"name": "road"}}, nil
		},
	}
	if got := runtime.featureName("roads", 7); got != "road" {
		t.Fatalf("first feature name = %q", got)
	}
	if got := runtime.featureName("roads", 7); got != "road" {
		t.Fatalf("cached feature name = %q", got)
	}
	if lookups != 1 {
		t.Fatalf("feature lookups = %d, want 1", lookups)
	}
	if got := runtime.featureName("roads", 8); got != "road" {
		t.Fatalf("second feature name = %q", got)
	}
	if lookups != 2 {
		t.Fatalf("feature lookups after different ID = %d, want 2", lookups)
	}
}
