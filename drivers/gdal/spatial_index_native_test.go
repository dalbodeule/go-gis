//go:build native

package gdal

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"gogis/internal/core"
)

func TestPrepareIndexedShapefileCacheLeavesSourceUntouchedAndReusesCache(t *testing.T) {
	root := t.TempDir()
	t.Setenv("TMPDIR", root)
	source := filepath.Join(root, "source", "parcels.shp")
	if err := os.MkdirAll(filepath.Dir(source), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := (Writer{}).Write(context.Background(), source, qixTestLayer()); err != nil {
		t.Fatal(err)
	}
	if HasShapefileSpatialIndex(source) {
		t.Fatal("new test source unexpectedly has a QIX index")
	}

	indexed, created, err := PrepareIndexedShapefileCache(context.Background(), source, "parcels")
	if err != nil {
		t.Fatal(err)
	}
	if !created || indexed == source {
		t.Fatalf("cache preparation returned path=%q created=%t", indexed, created)
	}
	if !HasShapefileSpatialIndex(indexed) {
		t.Fatalf("temporary source %q has no QIX sidecar", indexed)
	}
	if HasShapefileSpatialIndex(source) {
		t.Fatal("cache mode modified the original source directory")
	}
	reader := Reader{}
	loaded, err := reader.Open(context.Background(), indexed, "parcels")
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Features) != 3 {
		t.Fatalf("cached source features = %d, want 3", len(loaded.Features))
	}

	reusedPath, created, err := PrepareIndexedShapefileCache(context.Background(), source, "parcels")
	if err != nil {
		t.Fatal(err)
	}
	if created || reusedPath != indexed {
		t.Fatalf("cache reuse returned path=%q created=%t, want %q and false", reusedPath, created, indexed)
	}
}

func TestCreateShapefileSpatialIndexWritesSiblingQIX(t *testing.T) {
	source := filepath.Join(t.TempDir(), "parcels.shp")
	if err := (Writer{}).Write(context.Background(), source, qixTestLayer()); err != nil {
		t.Fatal(err)
	}
	if err := CreateShapefileSpatialIndex(context.Background(), source, "parcels"); err != nil {
		t.Fatal(err)
	}
	if !HasShapefileSpatialIndex(source) {
		t.Fatal("source directory has no QIX sidecar after index creation")
	}
	loaded, err := (Reader{}).Open(context.Background(), source, "parcels")
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Features) != 3 {
		t.Fatalf("indexed source features = %d, want 3", len(loaded.Features))
	}
}

func qixTestLayer() core.Layer {
	return core.Layer{
		Name: "parcels", CRS: core.CRS{AuthorityCode: "EPSG:5186"},
		Fields: []core.Field{{Name: "name", Type: core.FieldTypeText}},
		Features: []core.Feature{
			{Geometry: core.WKTGeometry{WKT: "POLYGON ((0 0, 10 0, 10 10, 0 10, 0 0))"}, Properties: map[string]any{"name": "a"}},
			{Geometry: core.WKTGeometry{WKT: "POLYGON ((20 0, 30 0, 30 10, 20 10, 20 0))"}, Properties: map[string]any{"name": "b"}},
			{Geometry: core.WKTGeometry{WKT: "POLYGON ((40 0, 50 0, 50 10, 40 10, 40 0))"}, Properties: map[string]any{"name": "c"}},
		},
	}
}
