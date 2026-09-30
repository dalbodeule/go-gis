//go:build native

package gdal

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gogis/internal/core"

	"github.com/airbusgeo/godal"
)

func TestReaderOpensGeoJSONFixtureThroughGDAL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "roads.geojson")
	fixture := `{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"name":"한글 도로","speed":40},"geometry":{"type":"Point","coordinates":[127.1,37.4]}}]}`
	if err := os.WriteFile(path, []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}

	layer, err := (Reader{}).Open(context.Background(), path, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(layer.Features) != 1 {
		t.Fatalf("features = %d, want 1", len(layer.Features))
	}
	if got := layer.Features[0].Properties["name"]; got != "한글 도로" {
		t.Fatalf("name = %v", got)
	}
}

func TestReaderOpenWindowPushesSpatialFilterThroughGDAL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "roads.geojson")
	fixture := `{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"name":"inside"},"geometry":{"type":"Point","coordinates":[127.1,37.4]}},{"type":"Feature","properties":{"name":"outside"},"geometry":{"type":"Point","coordinates":[128.1,38.4]}}]}`
	if err := os.WriteFile(path, []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}

	layer, err := (Reader{}).OpenWindow(context.Background(), path, "", [4]float64{127, 37, 127.2, 37.6})
	if err != nil {
		t.Fatal(err)
	}
	if len(layer.Features) != 1 {
		t.Fatalf("features = %d, want 1", len(layer.Features))
	}
	if got := layer.Features[0].Properties["name"]; got != "inside" {
		t.Fatalf("name = %v, want inside", got)
	}
}

func TestReaderOpenWindowGeometryOnlySkipsProperties(t *testing.T) {
	path := filepath.Join(t.TempDir(), "roads.geojson")
	fixture := `{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"name":"inside"},"geometry":{"type":"Point","coordinates":[127.1,37.4]}},{"type":"Feature","properties":{"name":"outside"},"geometry":{"type":"Point","coordinates":[128.1,38.4]}}]}`
	if err := os.WriteFile(path, []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}

	layer, err := (Reader{}).OpenWindowGeometryOnly(context.Background(), path, "", [4]float64{127, 37, 127.2, 37.6})
	if err != nil {
		t.Fatal(err)
	}
	if len(layer.Features) != 1 || layer.Features[0].Properties != nil || len(layer.Fields) != 0 {
		t.Fatalf("geometry-only window layer = %#v", layer)
	}
}

func TestReaderOpenGeometryOnlySkipsProperties(t *testing.T) {
	path := filepath.Join(t.TempDir(), "roads.geojson")
	fixture := `{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"name":"inside"},"geometry":{"type":"Point","coordinates":[127.1,37.4]}}]}`
	if err := os.WriteFile(path, []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}
	layer, err := (Reader{}).OpenGeometryOnly(context.Background(), path, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(layer.Features) != 1 || layer.Features[0].Properties != nil || len(layer.Fields) != 0 {
		t.Fatalf("geometry-only layer retained attributes: fields=%#v properties=%#v", layer.Fields, layer.Features[0].Properties)
	}
}

func TestReaderOpenFeatureLoadsOneFeatureByID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "roads.geojson")
	fixture := `{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"name":"first"},"geometry":{"type":"Point","coordinates":[127.1,37.4]}},{"type":"Feature","properties":{"name":"second"},"geometry":{"type":"Point","coordinates":[128.1,38.4]}}]}`
	if err := os.WriteFile(path, []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}
	feature, err := (Reader{}).OpenFeature(context.Background(), path, "", 2)
	if err != nil {
		t.Fatal(err)
	}
	if feature.ID != 2 || feature.Properties["name"] != "second" || feature.Geometry == nil {
		t.Fatalf("feature = %#v", feature)
	}
}

func TestReaderOpenAttributePageSkipsGeometry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "roads.geojson")
	fixture := `{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"name":"first"},"geometry":{"type":"Point","coordinates":[127.1,37.4]}},{"type":"Feature","properties":{"name":"second"},"geometry":{"type":"Point","coordinates":[128.1,38.4]}}]}`
	if err := os.WriteFile(path, []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}
	layer, total, err := (Reader{}).OpenAttributePage(context.Background(), path, "", 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || len(layer.Features) != 1 || layer.Features[0].ID != 2 || layer.Features[0].Geometry != nil || layer.Features[0].Properties["name"] != "second" {
		t.Fatalf("layer=%#v total=%d", layer, total)
	}
}

func TestAttributeSessionReusesDatasetAndCloses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "roads.geojson")
	fixture := `{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"name":"first"},"geometry":{"type":"Point","coordinates":[127.1,37.4]}}]}`
	if err := os.WriteFile(path, []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}
	session, err := OpenAttributeSession(path)
	if err != nil {
		t.Fatal(err)
	}
	layer, total, err := session.OpenAttributePage(context.Background(), "", 0, 1)
	if err != nil || total != 1 || len(layer.Features) != 1 || layer.Features[0].Properties["name"] != "first" {
		t.Fatalf("session page = layer=%#v total=%d err=%v", layer, total, err)
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := session.OpenAttributePage(context.Background(), "", 0, 1); err == nil {
		t.Fatal("closed attribute session accepted a request")
	}
}

func TestAttributeSessionFeatureCursorReusesSequentialScan(t *testing.T) {
	path := filepath.Join(t.TempDir(), "roads.geojson")
	fixture := `{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"name":"first"},"geometry":{"type":"Point","coordinates":[127.1,37.4]}},{"type":"Feature","properties":{"name":"second"},"geometry":{"type":"Point","coordinates":[127.2,37.5]}},{"type":"Feature","properties":{"name":"third"},"geometry":{"type":"Point","coordinates":[127.3,37.6]}}]}`
	if err := os.WriteFile(path, []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}
	session, err := OpenAttributeSession(path)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	for featureID, want := range []string{"first", "second", "third"} {
		feature, err := session.OpenFeature(context.Background(), "", uint64(featureID+1))
		if err != nil || feature.Properties["name"] != want {
			t.Fatalf("feature %d = %#v, err=%v", featureID+1, feature, err)
		}
	}
	feature, err := session.OpenFeature(context.Background(), "", 1)
	if err != nil || feature.Properties["name"] != "first" {
		t.Fatalf("backward feature = %#v, err=%v", feature, err)
	}
}

func TestAttributeSessionJSONPageCursorResetsForRandomPage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "roads.geojson")
	fixture := `{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"name":"first"},"geometry":{"type":"Point","coordinates":[127.1,37.4]}},{"type":"Feature","properties":{"name":"second"},"geometry":{"type":"Point","coordinates":[127.2,37.5]}},{"type":"Feature","properties":{"name":"third"},"geometry":{"type":"Point","coordinates":[127.3,37.6]}}]}`
	if err := os.WriteFile(path, []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}
	session, err := OpenAttributeSession(path)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	first, total, err := session.OpenAttributePage(context.Background(), "", 0, 1)
	if err != nil || total != 3 || first.Features[0].Properties["name"] != "first" {
		t.Fatalf("first page = %#v, err=%v", first, err)
	}
	second, total, err := session.OpenAttributePage(context.Background(), "", 1, 1)
	if err != nil || total != 3 || second.Features[0].ID != 2 || second.Features[0].Properties["name"] != "second" {
		t.Fatalf("second page = %#v, err=%v", second, err)
	}
	random, total, err := session.OpenAttributePage(context.Background(), "", 0, 1)
	if err != nil || total != 3 || random.Features[0].ID != 1 || random.Features[0].Properties["name"] != "first" {
		t.Fatalf("random page = %#v, err=%v", random, err)
	}
	third, total, err := session.OpenAttributePage(context.Background(), "", 2, 1)
	if err != nil || total != 3 || third.Features[0].ID != 3 || third.Features[0].Properties["name"] != "third" {
		t.Fatalf("non-sequential page = %#v, err=%v", third, err)
	}
}

func TestAttributeSessionSchemaCacheIsDetached(t *testing.T) {
	path := filepath.Join(t.TempDir(), "roads.geojson")
	fixture := `{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"name":"first"},"geometry":{"type":"Point","coordinates":[127.1,37.4]}},{"type":"Feature","properties":{"name":"second"},"geometry":{"type":"Point","coordinates":[127.2,37.5]}}]}`
	if err := os.WriteFile(path, []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}
	session, err := OpenAttributeSession(path)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	first, _, err := session.OpenAttributePage(context.Background(), "", 0, 1)
	if err != nil || len(first.Fields) != 1 {
		t.Fatalf("first page = %#v, err=%v", first, err)
	}
	first.Fields[0].Name = "mutated"
	second, _, err := session.OpenAttributePage(context.Background(), "", 0, 1)
	if err != nil || len(second.Fields) != 1 || second.Fields[0].Name != "name" {
		t.Fatalf("cached schema was mutated: %#v, err=%v", second.Fields, err)
	}
}

func TestGeometrySessionReusesDatasetAndCloses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "roads.geojson")
	fixture := `{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"name":"inside"},"geometry":{"type":"Point","coordinates":[127.1,37.4]}}]}`
	if err := os.WriteFile(path, []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}
	session, err := OpenGeometrySession(path)
	if err != nil {
		t.Fatal(err)
	}
	layer, err := session.OpenWindowGeometryOnly(context.Background(), "", [4]float64{127, 37, 128, 38})
	if err != nil || len(layer.Features) != 1 || layer.Features[0].Properties != nil {
		t.Fatalf("session window = layer=%#v err=%v", layer, err)
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := session.OpenWindowGeometryOnly(context.Background(), "", [4]float64{127, 37, 128, 38}); err == nil {
		t.Fatal("closed geometry session accepted a request")
	}
}

func TestReaderOpenAllGeometryOnlySkipsProperties(t *testing.T) {
	path := filepath.Join(t.TempDir(), "roads.geojson")
	fixture := `{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"name":"first"},"geometry":{"type":"Point","coordinates":[127.1,37.4]}}]}`
	if err := os.WriteFile(path, []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}
	layers, err := (Reader{}).OpenAllGeometryOnly(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if len(layers) != 1 || len(layers[0].Features) != 1 || layers[0].Features[0].Properties != nil {
		t.Fatalf("layers = %#v", layers)
	}
}

func multiLayerGeoPackage(tb testing.TB, featuresPerLayer int) string {
	tb.Helper()
	registerDrivers()
	path := filepath.Join(tb.TempDir(), "layers.gpkg")
	dataset, err := godal.CreateVector(godal.GeoPackage, path)
	if err != nil {
		tb.Fatal(err)
	}
	spatialRef, err := godal.NewSpatialRef("EPSG:4326")
	if err != nil {
		_ = dataset.Close()
		tb.Fatal(err)
	}
	defer spatialRef.Close()
	for _, name := range []string{"first", "selected", "last"} {
		layer, err := dataset.CreateLayer(name, spatialRef, godal.GTPoint)
		if err != nil {
			_ = dataset.Close()
			tb.Fatal(err)
		}
		for index := 0; index < featuresPerLayer; index++ {
			geometry, err := godal.NewGeometryFromWKT(fmt.Sprintf("POINT (%d %d)", index, index), nil)
			if err != nil {
				_ = dataset.Close()
				tb.Fatal(err)
			}
			feature, err := layer.NewFeature(geometry)
			geometry.Close()
			if err != nil {
				_ = dataset.Close()
				tb.Fatal(err)
			}
			if err := layer.UpdateFeature(feature); err != nil {
				feature.Close()
				_ = dataset.Close()
				tb.Fatal(err)
			}
			feature.Close()
		}
	}
	if err := dataset.Close(); err != nil {
		tb.Fatal(err)
	}
	return path
}

func TestAttributeSessionOpenGeometryOnlySelectsOneLayer(t *testing.T) {
	path := multiLayerGeoPackage(t, 2)
	session, err := OpenAttributeSession(path)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	layer, err := session.OpenGeometryOnly(context.Background(), "selected")
	if err != nil {
		t.Fatal(err)
	}
	if layer.Name != "selected" || len(layer.Features) != 2 || layer.CRS.AuthorityCode != "EPSG:4326" {
		t.Fatalf("selected layer = %#v", layer)
	}
	if layer.Features[0].Geometry == nil || layer.Features[0].Properties != nil {
		t.Fatalf("geometry-only feature = %#v", layer.Features[0])
	}
	page, total, err := session.OpenAttributePage(context.Background(), "selected", 0, 1)
	if err != nil || total != 2 || len(page.Features) != 1 {
		t.Fatalf("attribute page = %#v, total=%d, err=%v", page, total, err)
	}
	if _, err := session.OpenGeometryOnly(context.Background(), "missing"); err == nil {
		t.Fatal("missing layer was accepted")
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := session.OpenGeometryOnly(canceled, "selected"); err != context.Canceled {
		t.Fatalf("canceled read error = %v", err)
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := session.OpenGeometryOnly(context.Background(), "selected"); err == nil {
		t.Fatal("closed session accepted a geometry request")
	}
}

func TestAttributeSessionOverviewAndPrefixPreserveFullRead(t *testing.T) {
	path := multiLayerGeoPackage(t, 3)
	session, err := OpenAttributeSession(path)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	overviews, err := session.Inspect(context.Background())
	if err != nil || len(overviews) != 3 {
		t.Fatalf("overviews=%#v err=%v", overviews, err)
	}
	for _, overview := range overviews {
		if !overview.HasBounds || overview.Bounds != [4]float64{0, 0, 2, 2} || overview.FeatureCount != 3 || overview.CRS.AuthorityCode != "EPSG:4326" {
			t.Fatalf("unexpected overview: %#v", overview)
		}
	}
	preview, err := session.OpenAllGeometryPrefix(context.Background(), 2)
	if err != nil || len(preview) != 3 {
		t.Fatalf("preview=%#v err=%v", preview, err)
	}
	for _, layer := range preview {
		if len(layer.Features) != 2 || layer.Features[0].ID != 1 || layer.Features[1].ID != 2 || layer.Features[0].Properties != nil {
			t.Fatalf("invalid prefix layer: %#v", layer)
		}
	}
	selected, err := session.OpenGeometryPrefix(context.Background(), "selected", 2)
	if err != nil || selected.Name != "selected" || len(selected.Features) != 2 {
		t.Fatalf("selected prefix=%#v err=%v", selected, err)
	}
	full, err := session.OpenAllGeometryOnly(context.Background())
	if err != nil || len(full) != 3 {
		t.Fatalf("full layers=%#v err=%v", full, err)
	}
	for _, layer := range full {
		if len(layer.Features) != 3 || layer.Features[2].ID != 3 {
			t.Fatalf("full read changed by prefix: %#v", layer)
		}
	}
	_, total, err := session.OpenAttributePage(context.Background(), "selected", 0, 1)
	if err != nil || total != 3 {
		t.Fatalf("attribute page after preview: total=%d err=%v", total, err)
	}
	if _, err := session.OpenAllGeometryPrefix(context.Background(), 0); err == nil {
		t.Fatal("zero prefix limit was accepted")
	}
}

func TestValidLayerBoundsRejectsInvalidValues(t *testing.T) {
	for _, bounds := range [][4]float64{{1, 0, 0, 1}, {0, 1, 1, 0}, {0, 0, 1, math.NaN()}, {0, 0, math.Inf(1), 1}} {
		if validLayerBounds(bounds) {
			t.Fatalf("accepted invalid bounds %v", bounds)
		}
	}
}

func BenchmarkAttributeSessionOpenAllGeometryOnlyMultiLayerGeoPackage(b *testing.B) {
	path := multiLayerGeoPackage(b, 3_000)
	session, err := OpenAttributeSession(path)
	if err != nil {
		b.Fatal(err)
	}
	defer session.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, err := session.OpenAllGeometryOnly(context.Background()); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkAttributeSessionOpenGeometryOnlyMultiLayerGeoPackage(b *testing.B) {
	path := multiLayerGeoPackage(b, 3_000)
	session, err := OpenAttributeSession(path)
	if err != nil {
		b.Fatal(err)
	}
	defer session.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, err := session.OpenGeometryOnly(context.Background(), "selected"); err != nil {
			b.Fatal(err)
		}
	}
}

func TestGDALSQLAttributeOffsetForGeoJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "roads.geojson")
	fixture := `{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"name":"first"},"geometry":{"type":"Point","coordinates":[127.1,37.4]}},{"type":"Feature","properties":{"name":"second"},"geometry":{"type":"Point","coordinates":[128.1,38.4]}}]}`
	if err := os.WriteFile(path, []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}
	godal.RegisterAll()
	dataset, err := godal.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer dataset.Close()
	layers := dataset.Layers()
	if len(layers) != 1 {
		t.Fatalf("layers = %d", len(layers))
	}
	result, err := dataset.ExecuteSQL(`SELECT fid AS gogis_fid, * FROM "roads" LIMIT 1 OFFSET 1`, godal.OGRSQLDialect())
	if err != nil {
		t.Fatal(err)
	}
	defer result.Close()
	feature := result.NextFeature()
	if feature == nil {
		t.Fatal("SQL offset returned no feature")
	}
	defer feature.Close()
	fields := feature.Fields()
	if got := fields["name"].String(); got != "second" {
		t.Fatalf("name = %q", got)
	}
	if fid := fields["gogis_fid"].Int(); fid == 0 {
		t.Fatalf("FID was not exposed: %#v", fields)
	}
}

func BenchmarkReaderOpenGeoJSON10KPoints(b *testing.B) {
	benchmarkReaderOpenGeoJSON10KPoints(b, false)
}

func BenchmarkGDALOpenGeoJSON10KPoints(b *testing.B) {
	path := benchmarkGeoJSON10KPath(b)
	registerDrivers()
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		dataset, err := godal.Open(path)
		if err != nil {
			b.Fatal(err)
		}
		if err := dataset.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkGDALOpenVectorOnlyGeoJSON10KPoints(b *testing.B) {
	path := benchmarkGeoJSON10KPath(b)
	registerDrivers()
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		dataset, err := godal.Open(path, godal.VectorOnly())
		if err != nil {
			b.Fatal(err)
		}
		if err := dataset.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkGDALOpenAndBoundsGeoJSON10KPoints(b *testing.B) {
	path := benchmarkGeoJSON10KPath(b)
	registerDrivers()
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		dataset, err := godal.Open(path)
		if err != nil {
			b.Fatal(err)
		}
		layers := dataset.Layers()
		if len(layers) != 1 {
			b.Fatal("expected one layer")
		}
		if _, err := layers[0].Bounds(); err != nil {
			b.Fatal(err)
		}
		if err := dataset.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkGDALOpenAndFeatureCountGeoJSON10KPoints(b *testing.B) {
	path := benchmarkGeoJSON10KPath(b)
	registerDrivers()
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		dataset, err := godal.Open(path)
		if err != nil {
			b.Fatal(err)
		}
		layers := dataset.Layers()
		if len(layers) != 1 {
			b.Fatalf("layers = %d", len(layers))
		}
		if _, err := layers[0].FeatureCount(); err != nil {
			b.Fatal(err)
		}
		if err := dataset.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

var benchmarkGeometryAccessSink int

func BenchmarkGDALNextFeature10KPoints(b *testing.B) {
	benchmarkGDALGeometryAccess(b, 0)
}

func BenchmarkGDALFeatureGeometry10KPoints(b *testing.B) {
	benchmarkGDALGeometryAccess(b, 1)
}

func BenchmarkGDALFeatureWKB10KPoints(b *testing.B) {
	benchmarkGDALGeometryAccess(b, 2)
}

func benchmarkGDALGeometryAccess(b *testing.B, mode int) {
	path := benchmarkGeoJSON10KPath(b)
	godal.RegisterAll()
	dataset, err := godal.Open(path)
	if err != nil {
		b.Fatal(err)
	}
	defer dataset.Close()
	layers := dataset.Layers()
	if len(layers) != 1 {
		b.Fatalf("layers = %d", len(layers))
	}
	layer := layers[0]
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		layer.ResetReading()
		for {
			feature := layer.NextFeature()
			if feature == nil {
				break
			}
			if mode > 0 {
				geometry := feature.Geometry()
				if geometry != nil {
					if mode > 1 {
						wkb, wkbErr := geometry.WKB()
						if wkbErr != nil {
							feature.Close()
							geometry.Close()
							b.Fatal(wkbErr)
						}
						benchmarkGeometryAccessSink += len(wkb)
					}
					geometry.Close()
				}
			}
			feature.Close()
		}
	}
}

func BenchmarkReaderOpenGeometryOnlyGeoJSON10KPoints(b *testing.B) {
	benchmarkReaderOpenGeoJSON10KPoints(b, true)
}

func BenchmarkReaderAttributeSessionOpenAllGeometryOnlyGeoJSON10KPoints(b *testing.B) {
	path := benchmarkGeoJSON10KPath(b)
	session, err := OpenAttributeSession(path)
	if err != nil {
		b.Fatal(err)
	}
	defer session.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, err := session.OpenAllGeometryOnly(context.Background()); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkReaderAttributeSessionFirstPageAfterGeometryGeoJSON10KPoints(b *testing.B) {
	path := benchmarkGeoJSON10KPath(b)
	b.ReportAllocs()
	for index := 0; index < b.N; index++ {
		b.StopTimer()
		session, err := OpenAttributeSession(path)
		if err != nil {
			b.Fatal(err)
		}
		if _, err := session.OpenAllGeometryOnly(context.Background()); err != nil {
			_ = session.Close()
			b.Fatal(err)
		}
		b.StartTimer()
		page, total, err := session.OpenAttributePage(context.Background(), "", 0, 200)
		b.StopTimer()
		closeErr := session.Close()
		if err != nil || closeErr != nil || total != 10_000 || len(page.Features) != 200 {
			b.Fatalf("page=%d total=%d read=%v close=%v", len(page.Features), total, err, closeErr)
		}
	}
}

func BenchmarkReaderAttributeSessionFeatureCountAfterGeometryGeoJSON10KPoints(b *testing.B) {
	path := benchmarkGeoJSON10KPath(b)
	b.ReportAllocs()
	for index := 0; index < b.N; index++ {
		b.StopTimer()
		session, err := OpenAttributeSession(path)
		if err != nil {
			b.Fatal(err)
		}
		if _, err := session.OpenAllGeometryOnly(context.Background()); err != nil {
			_ = session.Close()
			b.Fatal(err)
		}
		layers := session.dataset.Layers()
		if len(layers) != 1 {
			b.Fatal("expected one layer")
		}
		b.StartTimer()
		count, err := layers[0].FeatureCount()
		b.StopTimer()
		closeErr := session.Close()
		if err != nil || closeErr != nil || count != 10_000 {
			b.Fatalf("count=%d read=%v close=%v", count, err, closeErr)
		}
	}
}

func BenchmarkReaderAttributeSessionRandomLastPageGeoJSON10KPoints(b *testing.B) {
	path := benchmarkGeoJSON10KPath(b)
	session, err := OpenAttributeSession(path)
	if err != nil {
		b.Fatal(err)
	}
	defer session.Close()
	if _, err := session.OpenAllGeometryOnly(context.Background()); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		page, total, err := session.OpenAttributePage(context.Background(), "", 9_800, 200)
		if err != nil || total != 10_000 || len(page.Features) != 200 {
			b.Fatalf("page=%d total=%d err=%v", len(page.Features), total, err)
		}
	}
}

func BenchmarkReaderAttributeSessionRandomLastPageCursorGeoJSON10KPoints(b *testing.B) {
	path := benchmarkGeoJSON10KPath(b)
	session, err := OpenAttributeSession(path)
	if err != nil {
		b.Fatal(err)
	}
	defer session.Close()
	if _, err := session.OpenAllGeometryOnly(context.Background()); err != nil {
		b.Fatal(err)
	}
	layers := session.dataset.Layers()
	if len(layers) != 1 {
		b.Fatal("expected one layer")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		page, _, err := readAttributePageLayerCursor(context.Background(), layers[0], 9_800, 200, 1, true, nil)
		if err != nil || len(page.Features) != 200 {
			b.Fatalf("page=%d err=%v", len(page.Features), err)
		}
	}
}

func BenchmarkReaderOpenAllThenAttributeSessionGeoJSON10KPoints(b *testing.B) {
	path := benchmarkGeoJSON10KPath(b)
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, err := (Reader{}).OpenAllGeometryOnly(context.Background(), path); err != nil {
			b.Fatal(err)
		}
		session, err := OpenAttributeSession(path)
		if err != nil {
			b.Fatal(err)
		}
		if err := session.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkReaderOpenWindowGeometryOnlyGeoJSON10KPoints(b *testing.B) {
	path := benchmarkGeoJSON10KPath(b)
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, err := (Reader{}).OpenWindowGeometryOnly(context.Background(), path, "", [4]float64{127, 37, 127.1, 37.1}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkReaderGeometrySessionWindowGeoJSON10KPoints(b *testing.B) {
	path := benchmarkGeoJSON10KPath(b)
	session, err := OpenGeometrySession(path)
	if err != nil {
		b.Fatal(err)
	}
	defer session.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, err := session.OpenWindowGeometryOnly(context.Background(), "", [4]float64{127, 37, 127.1, 37.1}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkReaderOpenGeometryOnlyGeoPackage10KPoints(b *testing.B) {
	path := benchmarkGeoPackage10KPath(b)
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, err := (Reader{}).OpenGeometryOnly(context.Background(), path, "points"); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkAttributeSessionPreviewGeoPackage10KPoints(b *testing.B) {
	path := benchmarkGeoPackage10KPath(b)
	session, err := OpenAttributeSession(path)
	if err != nil {
		b.Fatal(err)
	}
	defer session.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		overviews, err := session.Inspect(context.Background())
		if err != nil || len(overviews) != 1 || !overviews[0].HasBounds {
			b.Fatalf("overviews=%#v err=%v", overviews, err)
		}
		prefix, err := session.OpenGeometryPrefix(context.Background(), "points", 1_000)
		if err != nil || len(prefix.Features) != 1_000 {
			b.Fatalf("prefix=%d err=%v", len(prefix.Features), err)
		}
	}
}

func BenchmarkAttributeSessionFullGeoPackage10KPoints(b *testing.B) {
	path := benchmarkGeoPackage10KPath(b)
	session, err := OpenAttributeSession(path)
	if err != nil {
		b.Fatal(err)
	}
	defer session.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		layers, err := session.OpenAllGeometryOnly(context.Background())
		if err != nil || len(layers) != 1 || len(layers[0].Features) != 10_000 {
			b.Fatalf("layers=%d err=%v", len(layers), err)
		}
	}
}

func BenchmarkGDALOpenGeoPackage10KPoints(b *testing.B) {
	path := benchmarkGeoPackage10KPath(b)
	registerDrivers()
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		dataset, err := godal.Open(path)
		if err != nil {
			b.Fatal(err)
		}
		if err := dataset.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkGDALOpenAndBoundsGeoPackage10KPoints(b *testing.B) {
	path := benchmarkGeoPackage10KPath(b)
	registerDrivers()
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		dataset, err := godal.Open(path)
		if err != nil {
			b.Fatal(err)
		}
		layers := dataset.Layers()
		if len(layers) != 1 {
			b.Fatal("expected one layer")
		}
		if _, err := layers[0].Bounds(); err != nil {
			b.Fatal(err)
		}
		if err := dataset.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkReaderOpenWindowGeometryOnlyGeoPackage10KPoints(b *testing.B) {
	path := benchmarkGeoPackage10KPath(b)
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, err := (Reader{}).OpenWindowGeometryOnly(context.Background(), path, "points", [4]float64{127, 37, 127.1, 37.1}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkReaderGeometrySessionWindowGeoPackage10KPoints(b *testing.B) {
	path := benchmarkGeoPackage10KPath(b)
	session, err := OpenGeometrySession(path)
	if err != nil {
		b.Fatal(err)
	}
	defer session.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, err := session.OpenWindowGeometryOnly(context.Background(), "points", [4]float64{127, 37, 127.1, 37.1}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkReaderOpenGeometryOnlyShapefile10KPoints(b *testing.B) {
	path := benchmarkShapefile10KPath(b)
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, err := (Reader{}).OpenGeometryOnly(context.Background(), path, "points"); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkReaderOpenWindowGeometryOnlyShapefile10KPoints(b *testing.B) {
	path := benchmarkShapefile10KPath(b)
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, err := (Reader{}).OpenWindowGeometryOnly(context.Background(), path, "points", [4]float64{127, 37, 127.1, 37.1}); err != nil {
			b.Fatal(err)
		}
	}
}

func benchmarkReaderOpenGeoJSON10KPoints(b *testing.B, geometryOnly bool) {
	path := benchmarkGeoJSON10KPath(b)
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		var err error
		if geometryOnly {
			_, err = (Reader{}).OpenGeometryOnly(context.Background(), path, "")
		} else {
			_, err = (Reader{}).Open(context.Background(), path, "")
		}
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkReaderOpenAttributePageGeoJSON10KPoints(b *testing.B) {
	path := benchmarkGeoJSON10KPath(b)
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, _, err := (Reader{}).OpenAttributePage(context.Background(), path, "", (index%50)*200, 200); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkReaderAttributeSessionGeoJSON10KPoints(b *testing.B) {
	path := benchmarkGeoJSON10KPath(b)
	session, err := OpenAttributeSession(path)
	if err != nil {
		b.Fatal(err)
	}
	defer session.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, _, err := session.OpenAttributePage(context.Background(), "", (index%50)*200, 200); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkReaderAttributeSessionGeoPackage10KPoints(b *testing.B) {
	path := benchmarkGeoPackage10KPath(b)
	session, err := OpenAttributeSession(path)
	if err != nil {
		b.Fatal(err)
	}
	defer session.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		for page := 0; page < 50; page++ {
			if _, _, err := session.OpenAttributePage(context.Background(), "points", page*200, 200); err != nil {
				b.Fatal(err)
			}
		}
	}
}

func BenchmarkReaderAttributeSessionGeoPackageRandomPages10KPoints(b *testing.B) {
	path := benchmarkGeoPackage10KPath(b)
	session, err := OpenAttributeSession(path)
	if err != nil {
		b.Fatal(err)
	}
	defer session.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		page := (index * 17) % 50
		if _, _, err := session.OpenAttributePage(context.Background(), "points", page*200, 200); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkReaderAttributeSessionGeoPackageRandomPagesSQLBaseline10KPoints(b *testing.B) {
	path := benchmarkGeoPackage10KPath(b)
	session, err := OpenAttributeSession(path)
	if err != nil {
		b.Fatal(err)
	}
	defer session.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		page := (index * 17) % 50
		session.mu.Lock()
		_, _, err := openAttributePageDataset(context.Background(), session.dataset, session.source, "points", page*200, 200)
		session.mu.Unlock()
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkReaderAttributeSessionGeoJSONSequentialPages10KPoints(b *testing.B) {
	path := benchmarkGeoJSON10KPath(b)
	session, err := OpenAttributeSession(path)
	if err != nil {
		b.Fatal(err)
	}
	defer session.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		for page := 0; page < 50; page++ {
			if _, _, err := session.OpenAttributePage(context.Background(), "", page*200, 200); err != nil {
				b.Fatal(err)
			}
		}
	}
}

func BenchmarkReaderAttributeSessionSequentialPageGeoJSON10KPoints(b *testing.B) {
	path := benchmarkGeoJSON10KPath(b)
	godal.RegisterAll()
	dataset, err := godal.Open(path)
	if err != nil {
		b.Fatal(err)
	}
	defer dataset.Close()
	layers := dataset.Layers()
	if len(layers) != 1 {
		b.Fatalf("layers = %d", len(layers))
	}
	layer := layers[0]
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		offset := (index % 50) * 200
		layer.ResetReading()
		ordinal := 0
		for {
			feature := layer.NextFeature()
			if feature == nil {
				break
			}
			if ordinal < offset {
				feature.Close()
				ordinal++
				continue
			}
			if ordinal >= offset+200 {
				feature.Close()
				break
			}
			fields := feature.Fields()
			benchmarkGeometryAccessSink += len(fields)
			feature.Close()
			ordinal++
		}
	}
}

func BenchmarkReaderOpenFeatureGeoJSON10KPoints(b *testing.B) {
	path := benchmarkGeoJSON10KPath(b)
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, err := (Reader{}).OpenFeature(context.Background(), path, "", uint64(index%10_000+1)); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkReaderAttributeSessionFeatureGeoJSON10KPoints(b *testing.B) {
	path := benchmarkGeoJSON10KPath(b)
	session, err := OpenAttributeSession(path)
	if err != nil {
		b.Fatal(err)
	}
	defer session.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, err := session.OpenFeature(context.Background(), "", uint64(index%10_000+1)); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkReaderAttributeSessionLastFeatureGeoJSON10KPoints(b *testing.B) {
	path := benchmarkGeoJSON10KPath(b)
	session, err := OpenAttributeSession(path)
	if err != nil {
		b.Fatal(err)
	}
	defer session.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		feature, err := session.OpenFeature(context.Background(), "", 10_000)
		if err != nil || feature.ID != 10_000 {
			b.Fatalf("feature=%#v err=%v", feature, err)
		}
		// A repeated click should exercise a non-adjacent lookup rather than
		// the session's sequential cursor fast path.
		session.featureCursorReady = false
	}
}

func BenchmarkReaderAttributeSessionFeatureGeoPackage10KPoints(b *testing.B) {
	path := benchmarkGeoPackage10KPath(b)
	session, err := OpenAttributeSession(path)
	if err != nil {
		b.Fatal(err)
	}
	defer session.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		for featureID := uint64(1); featureID <= 10_000; featureID++ {
			if _, err := session.OpenFeature(context.Background(), "points", featureID); err != nil {
				b.Fatal(err)
			}
		}
	}
}

func BenchmarkReaderAttributeSessionLastFeatureGeoPackage10KPoints(b *testing.B) {
	path := benchmarkGeoPackage10KPath(b)
	session, err := OpenAttributeSession(path)
	if err != nil {
		b.Fatal(err)
	}
	defer session.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		feature, err := session.OpenFeature(context.Background(), "points", 10_000)
		if err != nil || feature.ID != 10_000 {
			b.Fatalf("feature=%#v err=%v", feature, err)
		}
		session.featureCursorReady = false
	}
}

func benchmarkGeoJSON10KPath(b *testing.B) string {
	b.Helper()
	path := filepath.Join(b.TempDir(), "points.geojson")
	var fixture strings.Builder
	fixture.WriteString(`{"type":"FeatureCollection","features":[`)
	for index := 0; index < 10_000; index++ {
		if index > 0 {
			fixture.WriteByte(',')
		}
		fmt.Fprintf(&fixture, `{"type":"Feature","properties":{"name":"road-%d","value":%d},"geometry":{"type":"Point","coordinates":[127.%04d,37.%04d]}}`, index, index, index%10_000, index%10_000)
	}
	fixture.WriteString(`]}`)
	if err := os.WriteFile(path, []byte(fixture.String()), 0o600); err != nil {
		b.Fatal(err)
	}
	return path
}

func benchmarkGeoPackage10KPath(b *testing.B) string {
	b.Helper()
	return benchmarkVector10KPath(b, ".gpkg")
}

func benchmarkShapefile10KPath(b *testing.B) string {
	b.Helper()
	return benchmarkVector10KPath(b, ".shp")
}

func benchmarkVector10KPath(b *testing.B, extension string) string {
	b.Helper()
	path := filepath.Join(b.TempDir(), "points"+extension)
	features := make([]core.Feature, 10_000)
	for index := range features {
		features[index] = core.Feature{
			ID:       uint64(index + 1),
			Geometry: core.WKTGeometry{WKT: fmt.Sprintf("POINT (127.%04d 37.%04d)", index, index)},
		}
	}
	if err := (Writer{}).Write(context.Background(), path, core.Layer{
		Name:     "points",
		CRS:      core.CRS{AuthorityCode: "EPSG:4326"},
		Features: features,
	}); err != nil {
		b.Fatal(err)
	}
	return path
}
