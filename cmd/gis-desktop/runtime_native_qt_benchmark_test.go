//go:build qt && native

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gogis/drivers/gdal"
	"gogis/internal/render"
)

// This measures the lower bound of a Go-only point decoder. It deliberately
// omits CRS, geometry variants, and the GDAL attribute-session contract, so
// it must not be compared with the full desktop loader as a drop-in path.
func BenchmarkDesktopGoJSONPointDecodeGeoJSON10K(b *testing.B) {
	path := desktopBenchmarkGeoJSON10KPath(b)
	type pointFeature struct {
		Geometry struct {
			Type        string     `json:"type"`
			Coordinates [2]float64 `json:"coordinates"`
		} `json:"geometry"`
	}
	type pointCollection struct {
		Features []pointFeature `json:"features"`
	}
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		data, err := os.ReadFile(path)
		if err != nil {
			b.Fatal(err)
		}
		var collection pointCollection
		if err := json.Unmarshal(data, &collection); err != nil {
			b.Fatal(err)
		}
		if len(collection.Features) != 10_000 {
			b.Fatalf("features = %d", len(collection.Features))
		}
	}
}

func desktopBenchmarkGeoJSON10KPath(b *testing.B) string {
	b.Helper()
	path := filepath.Join(b.TempDir(), "points.geojson")
	var fixture strings.Builder
	fixture.Grow(1_200_000)
	fixture.WriteString(`{"type":"FeatureCollection","features":[`)
	for index := 0; index < 10_000; index++ {
		if index != 0 {
			fixture.WriteByte(',')
		}
		fmt.Fprintf(&fixture, `{"type":"Feature","properties":{"name":"point-%d"},"geometry":{"type":"Point","coordinates":[127.%04d,37.%04d]}}`, index, index, index)
	}
	fixture.WriteString(`]}`)
	if err := os.WriteFile(path, []byte(fixture.String()), 0o600); err != nil {
		b.Fatal(err)
	}
	return path
}

func BenchmarkDesktopGDALSnapshotGeoJSON10K(b *testing.B) {
	path := desktopBenchmarkGeoJSON10KPath(b)
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		session, err := gdal.OpenAttributeSession(path)
		if err != nil {
			b.Fatal(err)
		}
		_, err = session.OpenAllGeometryOnly(context.Background())
		closeErr := session.Close()
		if err != nil || closeErr != nil {
			b.Fatalf("read: %v, close: %v", err, closeErr)
		}
	}
}

func BenchmarkDesktopRenderSourcesGeoJSON10K(b *testing.B) {
	path := desktopBenchmarkGeoJSON10KPath(b)
	session, err := gdal.OpenAttributeSession(path)
	if err != nil {
		b.Fatal(err)
	}
	layers, err := session.OpenAllGeometryOnly(context.Background())
	closeErr := session.Close()
	if err != nil || closeErr != nil {
		b.Fatalf("read: %v, close: %v", err, closeErr)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, err := render.NewLayerSources(layers); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDesktopReadOnlyLoadGeoJSON10K(b *testing.B) {
	path := desktopBenchmarkGeoJSON10KPath(b)
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		runtime, err := loadDataRuntimeModeContext(context.Background(), path, "", "", "", "", true)
		if err != nil {
			b.Fatal(err)
		}
		runtime.closeAttributeSource()
	}
}
