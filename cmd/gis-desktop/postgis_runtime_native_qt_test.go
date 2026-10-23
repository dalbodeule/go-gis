//go:build qt && native

package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"testing"

	"gogis/drivers/postgis"
	"gogis/internal/core"
)

func TestPostGISDesktopRuntimePersistsRecordAddAndDelete(t *testing.T) {
	dsn := os.Getenv("GOGIS_TEST_POSTGIS_DSN")
	if dsn == "" {
		t.Skip("set GOGIS_TEST_POSTGIS_DSN to run the live desktop PostGIS test")
	}
	t.Setenv(postGISDSNEnvironment, dsn)

	var suffix [6]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatal(err)
	}
	table := "public.gogis_desktop_" + hex.EncodeToString(suffix[:])
	ctx := context.Background()
	store, err := postgis.Open(ctx, dsn, table)
	if err != nil {
		t.Fatalf("open PostGIS table: %v", err)
	}
	t.Cleanup(func() {
		_, _ = store.DB.ExecContext(context.Background(), "DROP TABLE IF EXISTS "+quotePostGISTestTable(table))
		_ = store.Close()
	})

	initial := core.Layer{
		Name: table,
		CRS:  core.CRS{AuthorityCode: "EPSG:4326"},
		Features: []core.Feature{{
			ID: 1, Geometry: core.WKTGeometry{WKT: "POINT (127 37)"},
			Properties: map[string]any{"name": "original"},
		}},
	}
	if err := store.SaveLayer(ctx, initial); err != nil {
		t.Fatalf("save initial PostGIS layer: %v", err)
	}

	runtime, err := loadPostGISRuntime(ctx, table, "", false)
	if err != nil {
		t.Fatalf("load desktop PostGIS runtime: %v", err)
	}
	t.Cleanup(func() {
		if runtime.closeAttributeSource != nil {
			runtime.closeAttributeSource()
		}
	})
	if runtime.service == nil || !runtime.service.HasFeature(table, 1) {
		t.Fatal("desktop runtime did not load the initial feature")
	}

	if err := runtime.service.BeginEdit(); err != nil {
		t.Fatal(err)
	}
	if err := runtime.service.AddFeature(table, core.Feature{
		ID: 2, Geometry: core.WKTGeometry{WKT: "POINT (128 38)"},
		Properties: map[string]any{"name": "added"},
	}); err != nil {
		_ = runtime.service.Rollback()
		t.Fatal(err)
	}
	if err := runtime.service.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := runtime.persist(ctx, table); err != nil {
		t.Fatalf("persist added desktop feature: %v", err)
	}

	if err := runtime.service.BeginEdit(); err != nil {
		t.Fatal(err)
	}
	if err := runtime.service.DeleteFeature(table, 1); err != nil {
		_ = runtime.service.Rollback()
		t.Fatal(err)
	}
	if err := runtime.service.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := runtime.persist(ctx, table); err != nil {
		t.Fatalf("persist deleted desktop feature: %v", err)
	}

	got, err := store.ReadLayer(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Features) != 1 || got.Features[0].ID != 2 || got.Features[0].Properties["name"] != "added" {
		t.Fatalf("unexpected persisted desktop PostGIS features: %+v", got.Features)
	}
}

func TestPostGISDesktopRuntimeUsesViewportForLargeTable(t *testing.T) {
	dsn := os.Getenv("GOGIS_TEST_POSTGIS_DSN")
	if dsn == "" {
		t.Skip("set GOGIS_TEST_POSTGIS_DSN to run the live desktop PostGIS test")
	}
	t.Setenv(postGISDSNEnvironment, dsn)

	var suffix [6]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatal(err)
	}
	table := "public.gogis_large_" + hex.EncodeToString(suffix[:])
	ctx := context.Background()
	store, err := postgis.Open(ctx, dsn, table)
	if err != nil {
		t.Fatalf("open PostGIS table: %v", err)
	}
	t.Cleanup(func() {
		_, _ = store.DB.ExecContext(context.Background(), "DROP TABLE IF EXISTS "+quotePostGISTestTable(table))
		_ = store.Close()
	})

	if _, err := store.DB.ExecContext(ctx, "CREATE EXTENSION IF NOT EXISTS postgis"); err != nil {
		t.Fatalf("enable PostGIS extension: %v", err)
	}
	if _, err := store.DB.ExecContext(ctx, "CREATE TABLE "+quotePostGISTestTable(table)+" (id BIGINT PRIMARY KEY, geom geometry(Point, 4326) NOT NULL, properties JSONB NOT NULL DEFAULT '{}'::jsonb)"); err != nil {
		t.Fatalf("create large PostGIS table: %v", err)
	}
	if _, err := store.DB.ExecContext(ctx, "INSERT INTO "+quotePostGISTestTable(table)+" (id, geom, properties) SELECT n, ST_SetSRID(ST_MakePoint(-180 + (n % 360)::double precision, -80 + (n % 160)::double precision), 4326), jsonb_build_object('name', 'point-' || n) FROM generate_series(1, 100000) AS n"); err != nil {
		t.Fatalf("populate large PostGIS table: %v", err)
	}

	runtime, err := loadPostGISRuntime(ctx, table, "", false)
	if err != nil {
		t.Fatalf("load large desktop PostGIS runtime: %v", err)
	}
	t.Cleanup(func() {
		if runtime.closeAttributeSource != nil {
			runtime.closeAttributeSource()
		}
	})
	if !runtime.viewportReadOnly || !runtime.readOnly || len(runtime.features) != 0 {
		t.Fatalf("large PostGIS table was materialized: viewport=%t readOnly=%t residentFeatures=%d", runtime.viewportReadOnly, runtime.readOnly, len(runtime.features))
	}
	if runtime.attributePageReader == nil || runtime.attributeFeatureReader == nil {
		t.Fatal("large PostGIS runtime did not install delayed attribute readers")
	}
	page, total, err := runtime.attributePageReader(ctx, table, 99999, 4)
	if err != nil {
		t.Fatalf("read delayed PostGIS attribute page: %v", err)
	}
	if total != 100000 || len(page.Features) != 1 || page.Features[0].ID != 100000 {
		t.Fatalf("unexpected delayed attribute page: total=%d features=%+v", total, page.Features)
	}
	feature, err := runtime.attributeFeatureReader(ctx, table, 100000)
	if err != nil || feature.ID != 100000 {
		t.Fatalf("read delayed PostGIS feature: feature=%+v err=%v", feature, err)
	}

	window, err := store.ReadWindow(ctx, [4]float64{9, -1, 11, 1}, false, 64, 1<<20)
	if err != nil {
		t.Fatalf("read bounded PostGIS window: %v", err)
	}
	if len(window.Features) == 0 || len(window.Features) > 64 {
		t.Fatalf("bounded PostGIS window returned %d features", len(window.Features))
	}
}

func quotePostGISTestTable(table string) string {
	parts := splitPostGISTestTable(table)
	quoted := make([]string, len(parts))
	for index, part := range parts {
		quoted[index] = `"` + part + `"`
	}
	return stringsJoinPostGISTestParts(quoted)
}

func splitPostGISTestTable(table string) []string {
	for index := len(table) - 1; index >= 0; index-- {
		if table[index] == '.' {
			return []string{table[:index], table[index+1:]}
		}
	}
	return []string{table}
}

func stringsJoinPostGISTestParts(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	result := parts[0]
	for _, part := range parts[1:] {
		result += "." + part
	}
	return result
}
