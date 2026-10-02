//go:build native

package gdal

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"gogis/internal/core"

	"github.com/airbusgeo/godal"
)

func BenchmarkGDALGeoJSONWindowEnvelope1M(b *testing.B) {
	benchmarkGDALGeoJSONWindow1M(b, false)
}

func BenchmarkGDALGeoJSONWindowSpatialFilter1M(b *testing.B) {
	benchmarkGDALGeoJSONWindow1M(b, true)
}

func BenchmarkGDALGeoJSONWindowStream1M(b *testing.B) {
	path := benchmarkGeoJSONMillionPoints(b)
	b.ReportAllocs()
	b.ResetTimer()
	var resultCount int
	for iteration := 0; iteration < b.N; iteration++ {
		result, err := readGeoJSONWindow(context.Background(), path, "", [4]float64{0, 0, 0.0625, 0.0625}, true, 20_000, 32<<20)
		if err != nil {
			b.Fatal(err)
		}
		resultCount = len(result.Features)
		if resultCount != 3969 {
			b.Fatalf("window feature count = %d, want 3969", resultCount)
		}
	}
	peakRSS := benchmarkGDALProcessMaxRSSBytes()
	b.StopTimer()
	b.ReportMetric(1_000_000, "features/op")
	b.ReportMetric(float64(resultCount), "window-features/op")
	if peakRSS > 0 {
		b.ReportMetric(float64(peakRSS)/(1<<20), "process-maxrss-MiB")
	}
}

func BenchmarkGDALGeoJSONIndexedWindow1M(b *testing.B) {
	benchmarkGDALGeoJSONIndexedWindow1M(b, false, [4]float64{0, 0, 0.0625, 0.0625}, 3_969)
}

func BenchmarkGDALGeoJSONSpatialGridWindow1M(b *testing.B) {
	benchmarkGDALGeoJSONIndexedWindow1M(b, true, [4]float64{0, 0, 0.0625, 0.0625}, 3_969)
}

func BenchmarkGDALGeoJSONIndexedWindow1MZoomed(b *testing.B) {
	benchmarkGDALGeoJSONIndexedWindow1M(b, false, [4]float64{0.123, 0.456, 0.12301, 0.45601}, 1)
}

func BenchmarkGDALGeoJSONSpatialGridWindow1MZoomed(b *testing.B) {
	benchmarkGDALGeoJSONIndexedWindow1M(b, true, [4]float64{0.123, 0.456, 0.12301, 0.45601}, 1)
}

func BenchmarkGeoJSONSpatialCandidateIndexBuild1M(b *testing.B) {
	path := benchmarkGeoJSONMillionPoints(b)
	overviews, index, _, _, err := inspectGeoJSONCollectionWithIndexLimitAndTailBlocks(
		context.Background(), path, maxGeoJSONInMemoryIndexFeatures)
	if err != nil || len(index) != 1_000_000 || len(overviews) != 1 {
		b.Fatalf("index GeoJSON fixture: overview=%#v index=%d err=%v", overviews, len(index), err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		if spatial := newGeoJSONSpatialCandidateIndex(index, overviews[0].Bounds, overviews[0].HasBounds); spatial == nil {
			b.Fatal("spatial candidate index was not built")
		}
	}
	b.ReportMetric(1_000_000, "features/source")
}

func benchmarkGDALGeoJSONIndexedWindow1M(b *testing.B, useSpatialGrid bool, bounds [4]float64, expectedCount int) {
	path := benchmarkGeoJSONMillionPoints(b)
	ctx := context.Background()
	overviews, index, tailBlocks, _, err := inspectGeoJSONCollectionWithIndexLimitAndTailBlocks(
		ctx, path, maxGeoJSONInMemoryIndexFeatures)
	if err != nil || len(overviews) != 1 || overviews[0].FeatureCount != 1_000_000 || len(index) != 1_000_000 {
		b.Fatalf("index GeoJSON fixture: overview=%#v index=%d err=%v", overviews, len(index), err)
	}
	var spatial *geoJSONSpatialCandidateIndex
	if useSpatialGrid {
		spatial = newGeoJSONSpatialCandidateIndex(index, overviews[0].Bounds, overviews[0].HasBounds)
		if spatial == nil {
			b.Fatal("spatial candidate index was not built")
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	var resultCount int
	for iteration := 0; iteration < b.N; iteration++ {
		var result core.Layer
		if useSpatialGrid {
			result, err = readGeoJSONIndexedWindowWithSpatialIndex(ctx, path, overviews[0], index,
				tailBlocks, spatial, bounds, true, 20_000, 32<<20)
		} else {
			result, err = readGeoJSONIndexedWindowWithTailBlocks(ctx, path, overviews[0], index,
				tailBlocks, bounds, true, 20_000, 32<<20)
		}
		if err != nil {
			b.Fatal(err)
		}
		resultCount = len(result.Features)
		if resultCount != expectedCount {
			b.Fatalf("window feature count = %d, want %d", resultCount, expectedCount)
		}
	}
	b.ReportMetric(1_000_000, "features/source")
	b.ReportMetric(float64(resultCount), "window-features/op")
}

func BenchmarkGeoJSONTailFeatureLookup1MPlus100K(b *testing.B) {
	const featureCount = 1_100_000
	path := benchmarkGeoJSONPoints(b, featureCount)
	session, err := OpenAttributeSession(path)
	if err != nil {
		b.Fatal(err)
	}
	defer session.Close()
	overviews, err := session.Inspect(context.Background())
	if err != nil || len(overviews) != 1 || overviews[0].FeatureCount != featureCount {
		b.Fatalf("inspect tail lookup fixture=%#v err=%v", overviews, err)
	}
	wantName := fmt.Sprintf("p%d", featureCount-1)
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		feature, err := session.OpenFeature(context.Background(), "", featureCount)
		if err != nil || feature.ID != uint64(featureCount) || feature.Properties["name"] != wantName {
			b.Fatalf("tail feature lookup=%#v err=%v", feature, err)
		}
	}
	b.StopTimer()
	b.ReportMetric(float64(featureCount), "features/source")
	b.ReportMetric(float64(geoJSONTailBlockFeatureCount), "max-tail-scan-features")
}

func BenchmarkGeoJSONGeometryBoundsPoint(b *testing.B) {
	geometry := []byte(`{"type":"Point","coordinates":[127.123456,37.654321]}`)
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		if _, hasCoordinates, err := geoJSONGeometryBounds(geometry); err != nil || !hasCoordinates {
			b.Fatalf("point bounds failed: has=%t err=%v", hasCoordinates, err)
		}
	}
}

func benchmarkGDALGeoJSONWindow1M(b *testing.B, useSpatialFilter bool) {
	path := benchmarkGeoJSONMillionPoints(b)
	godal.RegisterAll()
	b.ReportAllocs()
	b.ResetTimer()
	dataset, err := godal.Open(path, godal.ConfigOption("OGR_GEOJSON_MAX_OBJ_SIZE=64"))
	if err != nil {
		b.Fatal(err)
	}
	defer dataset.Close()
	layers := dataset.Layers()
	if len(layers) != 1 {
		b.Fatalf("layers = %d, want 1", len(layers))
	}
	layer := layers[0]
	openRSS := benchmarkGDALProcessMaxRSSBytes()
	count, err := layer.FeatureCount()
	if err != nil || count != 1_000_000 {
		b.Fatalf("FeatureCount=%d err=%v", count, err)
	}
	bounds, err := layer.Bounds()
	if err != nil {
		b.Fatal(err)
	}
	metadataRSS := benchmarkGDALProcessMaxRSSBytes()
	windowBounds := [4]float64{bounds[0], bounds[1], bounds[0] + (bounds[2]-bounds[0])*0.0625, bounds[1] + (bounds[3]-bounds[1])*0.0625}
	var resultCount int
	for iteration := 0; iteration < b.N; iteration++ {
		var resultLayer core.Layer
		if useSpatialFilter {
			resultLayer, err = openWindowSpatialFilterLimited(context.Background(), dataset, layer, layer.Name(), windowBounds, true, 20_000, 32<<20)
		} else {
			resultLayer, err = readWindowLayer(context.Background(), layer, layer.Name(), windowBounds, true, 20_000, 32<<20)
		}
		if err != nil {
			b.Fatal(err)
		}
		resultCount = len(resultLayer.Features)
		if resultCount == 0 || resultCount > 20_000 {
			b.Fatalf("window feature count = %d", resultCount)
		}
	}
	windowRSS := benchmarkGDALProcessMaxRSSBytes()
	b.StopTimer()
	b.ReportMetric(float64(count), "features/op")
	b.ReportMetric(float64(resultCount), "window-features/op")
	if openRSS > 0 {
		b.ReportMetric(float64(openRSS)/(1<<20), "rss-after-open-MiB")
	}
	if metadataRSS > 0 {
		b.ReportMetric(float64(metadataRSS)/(1<<20), "rss-after-metadata-MiB")
	}
	if windowRSS > 0 {
		b.ReportMetric(float64(windowRSS)/(1<<20), "process-maxrss-MiB")
		b.ReportMetric(float64(windowRSS)/(1<<20), "rss-after-window-MiB")
	}
}

func benchmarkGeoJSONMillionPoints(b *testing.B) string {
	return benchmarkGeoJSONPoints(b, 1_000_000)
}

func benchmarkGeoJSONPoints(b *testing.B, featureCount int) string {
	b.Helper()
	path := filepath.Join(b.TempDir(), "points.geojson")
	file, err := os.Create(path)
	if err != nil {
		b.Fatal(err)
	}
	writer := bufio.NewWriterSize(file, 1<<20)
	if _, err := writer.WriteString(`{"type":"FeatureCollection","features":[`); err != nil {
		_ = file.Close()
		b.Fatal(err)
	}
	for index := 0; index < featureCount; index++ {
		if index != 0 {
			if err := writer.WriteByte(','); err != nil {
				_ = file.Close()
				b.Fatal(err)
			}
		}
		x, y := index%1000, index/1000
		if _, err := fmt.Fprintf(writer, `{"type":"Feature","properties":{"name":"p%d"},"geometry":{"type":"Point","coordinates":[0.%03d,0.%03d]}}`, index, x, y); err != nil {
			_ = file.Close()
			b.Fatal(err)
		}
	}
	if _, err := writer.WriteString(`]}`); err != nil {
		_ = file.Close()
		b.Fatal(err)
	}
	if err := writer.Flush(); err != nil {
		_ = file.Close()
		b.Fatal(err)
	}
	if err := file.Close(); err != nil {
		b.Fatal(err)
	}
	return path
}
