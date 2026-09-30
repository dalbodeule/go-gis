//go:build native

package gdal

import (
	"context"
	"encoding/hex"
	"path/filepath"
	"testing"

	"gogis/internal/commands"
	"gogis/internal/core"
)

func BenchmarkNewOGRGeometryWKBPoint(b *testing.B) {
	data, err := hex.DecodeString("0101000000000000000000f03f0000000000000040")
	if err != nil {
		b.Fatal(err)
	}
	geometry := core.WKBGeometry{WKB: data}
	b.ReportAllocs()
	for index := 0; index < b.N; index++ {
		ogrGeometry, err := newOGRGeometry(geometry, nil)
		if err != nil {
			b.Fatal(err)
		}
		ogrGeometry.Close()
	}
}

func BenchmarkNewOGRGeometryWKTPoint(b *testing.B) {
	geometry := core.WKTGeometry{WKT: "POINT (1 2)"}
	b.ReportAllocs()
	for index := 0; index < b.N; index++ {
		ogrGeometry, err := newOGRGeometry(geometry, nil)
		if err != nil {
			b.Fatal(err)
		}
		ogrGeometry.Close()
	}
}

func TestWriterRoundTripsGeoPackage(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "roads.gpkg")
	want := writableLayer()
	if err := (Writer{}).Write(context.Background(), destination, want); err != nil {
		t.Fatal(err)
	}
	got, err := (Reader{}).Open(context.Background(), destination, "roads")
	if err != nil {
		t.Fatal(err)
	}
	assertRoundTrip(t, got)
}

func TestWriterRoundTripsShapefile(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "roads.shp")
	want := writableLayer()
	if err := (Writer{}).Write(context.Background(), destination, want); err != nil {
		t.Fatal(err)
	}
	got, err := (Reader{}).Open(context.Background(), destination, "roads")
	if err != nil {
		t.Fatal(err)
	}
	assertRoundTrip(t, got)
}

func TestWriterRoundTripsWKBGeometry(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "wkb.gpkg")
	data, err := hex.DecodeString("0101000000000000000000f03f0000000000000040")
	if err != nil {
		t.Fatal(err)
	}
	layer := writableLayer()
	layer.Features[0].Geometry = core.WKBGeometry{WKB: data}
	if err := (Writer{}).Write(context.Background(), destination, layer); err != nil {
		t.Fatal(err)
	}
	got, err := (Reader{}).Open(context.Background(), destination, "roads")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Features) != 1 || got.Features[0].Geometry == nil {
		t.Fatalf("WKB round trip = %#v", got)
	}
}

func TestWriterRejectsUnsupportedOutput(t *testing.T) {
	err := (Writer{}).Write(context.Background(), filepath.Join(t.TempDir(), "roads.json"), writableLayer())
	if err == nil {
		t.Fatal("unsupported output was accepted")
	}
}

func TestProjectServiceSavesThroughGDALWriter(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "project.gpkg")
	service := commands.NewProjectService("demo", core.CRS{AuthorityCode: "EPSG:4326"})
	if err := service.BeginEdit(); err != nil {
		t.Fatal(err)
	}
	if err := service.AddLayer(writableLayer()); err != nil {
		t.Fatal(err)
	}
	if err := service.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := service.SaveLayer(context.Background(), Writer{}, destination, "roads"); err != nil {
		t.Fatal(err)
	}
	if _, err := (Reader{}).Open(context.Background(), destination, "roads"); err != nil {
		t.Fatal(err)
	}
}

func writableLayer() core.Layer {
	return core.Layer{
		Name: "roads",
		CRS:  core.CRS{AuthorityCode: "EPSG:4326"},
		Fields: []core.Field{
			{Name: "name", Type: core.FieldTypeText},
			{Name: "speed", Type: core.FieldTypeNumber},
			{Name: "open", Type: core.FieldTypeBool},
		},
		Features: []core.Feature{{
			ID:       7,
			Geometry: core.WKTGeometry{WKT: "POINT (127.1 37.4)"},
			Properties: map[string]any{
				"name":  "한글 도로",
				"speed": 40.5,
				"open":  true,
			},
		}},
	}
}

func assertRoundTrip(t *testing.T, layer core.Layer) {
	t.Helper()
	if layer.CRS.AuthorityCode != "EPSG:4326" {
		t.Fatalf("CRS = %q", layer.CRS.AuthorityCode)
	}
	if len(layer.Features) != 1 || layer.Features[0].ID != 1 {
		t.Fatalf("features = %#v", layer.Features)
	}
	feature := layer.Features[0]
	if feature.Properties["name"] != "한글 도로" {
		t.Fatalf("name = %v", feature.Properties["name"])
	}
	if speed, ok := feature.Properties["speed"].(float64); !ok || speed < 40.4 || speed > 40.6 {
		t.Fatalf("speed = %#v", feature.Properties["speed"])
	}
	switch open := feature.Properties["open"].(type) {
	case int:
		if open != 1 {
			t.Fatalf("open = %#v", feature.Properties["open"])
		}
	case int64:
		if open != 1 {
			t.Fatalf("open = %#v", feature.Properties["open"])
		}
	default:
		t.Fatalf("open = %#v", feature.Properties["open"])
	}
}
