//go:build qt && native

package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"gogis/drivers/gdal"
	"gogis/internal/core"
)

func TestAutomaticShapefileIndexPolicyThresholdAndCache(t *testing.T) {
	root := t.TempDir()
	t.Setenv("TMPDIR", root)
	source := filepath.Join(root, "source", "parcels.shp")
	if err := os.MkdirAll(filepath.Dir(source), 0o700); err != nil {
		t.Fatal(err)
	}
	layer := core.Layer{
		Name: "parcels", CRS: core.CRS{AuthorityCode: "EPSG:5186"},
		Features: []core.Feature{
			{Geometry: core.WKTGeometry{WKT: "POLYGON ((0 0, 10 0, 10 10, 0 10, 0 0))"}},
			{Geometry: core.WKTGeometry{WKT: "POLYGON ((20 0, 30 0, 30 10, 20 10, 20 0))"}},
			{Geometry: core.WKTGeometry{WKT: "POLYGON ((40 0, 50 0, 50 10, 40 10, 40 0))"}},
		},
	}
	if err := (gdal.Writer{}).Write(context.Background(), source, layer); err != nil {
		t.Fatal(err)
	}
	previousPolicy := activeShapefileIndexPolicy
	activeShapefileIndexPolicy = shapefileIndexPolicy{threshold: 3, location: "cache"}
	t.Cleanup(func() { activeShapefileIndexPolicy = previousPolicy })

	openSession := func() *gdal.AttributeSession {
		t.Helper()
		session, err := gdal.OpenAttributeSession(source)
		if err != nil {
			t.Fatal(err)
		}
		return session
	}
	below := openSession()
	unchanged, path, created, err := prepareAutomaticShapefileIndex(context.Background(),
		vectorSourceSpec{Path: source}, "parcels", 2, below)
	if err != nil {
		t.Fatal(err)
	}
	if unchanged != below || path != source || created || gdal.HasShapefileSpatialIndex(source) {
		t.Fatalf("below-threshold policy changed source: sameSession=%t path=%q created=%t sourceIndexed=%t",
			unchanged == below, path, created, gdal.HasShapefileSpatialIndex(source))
	}
	_ = below.Close()

	indexedSession, path, created, err := prepareAutomaticShapefileIndex(context.Background(),
		vectorSourceSpec{Path: source}, "parcels", 3, openSession())
	if err != nil {
		t.Fatal(err)
	}
	defer indexedSession.Close()
	if !created || path == source || !gdal.HasShapefileSpatialIndex(path) {
		t.Fatalf("threshold policy returned path=%q created=%t indexed=%t", path, created, gdal.HasShapefileSpatialIndex(path))
	}
	if gdal.HasShapefileSpatialIndex(source) {
		t.Fatal("cache policy modified the original SHP directory")
	}
}

func TestConfigureShapefileIndexPolicyCommandLineOverridesEnvironment(t *testing.T) {
	t.Setenv("GOGIS_SHAPEFILE_INDEX_THRESHOLD", "50000")
	t.Setenv("GOGIS_SHAPEFILE_INDEX_LOCATION", "source")
	previousPolicy := activeShapefileIndexPolicy
	t.Cleanup(func() { activeShapefileIndexPolicy = previousPolicy })
	configureShapefileIndexPolicy([]string{
		"gogis-desktop", "--spatial-index-threshold=250000", "--spatial-index-location", "cache",
	})
	if activeShapefileIndexPolicy != (shapefileIndexPolicy{threshold: 250000, location: "cache"}) {
		t.Fatalf("configured SHP index policy = %+v", activeShapefileIndexPolicy)
	}
}

func TestConfigureShapefileIndexPolicyDefaultsToCacheAtTenThousandFeatures(t *testing.T) {
	t.Setenv("GOGIS_SHAPEFILE_INDEX_THRESHOLD", "")
	t.Setenv("GOGIS_SHAPEFILE_INDEX_LOCATION", "")
	previousPolicy := activeShapefileIndexPolicy
	t.Cleanup(func() { activeShapefileIndexPolicy = previousPolicy })
	configureShapefileIndexPolicy(nil)
	if want := (shapefileIndexPolicy{threshold: 10_000, location: "cache"}); activeShapefileIndexPolicy != want {
		t.Fatalf("default SHP index policy = %+v, want %+v", activeShapefileIndexPolicy, want)
	}
}

func TestConfigureShapefileIndexPolicyUsesSavedThresholdUnlessOverridden(t *testing.T) {
	t.Setenv("GOGIS_SHAPEFILE_INDEX_THRESHOLD", "")
	t.Setenv("GOGIS_SHAPEFILE_INDEX_LOCATION", "")
	previousPolicy := activeShapefileIndexPolicy
	t.Cleanup(func() { activeShapefileIndexPolicy = previousPolicy })

	configureShapefileIndexPolicyWithDefaults(nil, 50_000, "cache")
	if want := (shapefileIndexPolicy{threshold: 50_000, location: "cache"}); activeShapefileIndexPolicy != want {
		t.Fatalf("saved preference policy = %+v, want %+v", activeShapefileIndexPolicy, want)
	}
	configureShapefileIndexPolicyWithDefaults([]string{"app", "--spatial-index-threshold=250000"}, 50_000, "cache")
	if want := (shapefileIndexPolicy{threshold: 250_000, location: "cache"}); activeShapefileIndexPolicy != want {
		t.Fatalf("command-line override policy = %+v, want %+v", activeShapefileIndexPolicy, want)
	}
}
