//go:build qt && native

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"
	"time"

	"gogis/drivers/gdal"
	"gogis/internal/core"
	"gogis/internal/render"
	"gogis/ui/qt/native"

	"github.com/airbusgeo/godal"
)

func BenchmarkAttachPolygonFill10KConvex(b *testing.B) {
	const featureCount = 10_000
	layer := core.Layer{Name: "areas", Style: core.DefaultLayerStyle(), Features: make([]core.Feature, featureCount)}
	for index := range layer.Features {
		x := float64(index%100) / 100
		y := float64(index/100) / 100
		x1, y1 := x+0.008, y+0.008
		layer.Features[index] = core.Feature{
			ID: uint64(index + 1),
			Geometry: core.WKTGeometry{WKT: fmt.Sprintf(
				"POLYGON ((%f %f, %f %f, %f %f, %f %f, %f %f))",
				x, y, x1, y, x1, y1, x, y1, x, y)},
		}
	}
	sources, _, err := render.NewLayerSourcesWithExtent([]core.Layer{layer}, [4]float64{0, 0, 1, 1})
	if err != nil {
		b.Fatal(err)
	}
	baseSource := sources[layer.Name]
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		sources := map[string]render.LayerSource{layer.Name: baseSource}
		if err := attachPolygonFillGeometry(context.Background(), []core.Layer{layer}, sources); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*featureCount), "ns/feature")
}

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

func BenchmarkDesktopReadOnlyLoadGeoJSON50K(b *testing.B) {
	path := largeReadOnlyFixture(b)
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

func BenchmarkDesktopReadOnlyPreviewGeoJSON50K(b *testing.B) {
	path := largeReadOnlyFixture(b)
	var previewElapsed time.Duration
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		start := time.Now()
		seenPreview := false
		runtime, err := loadDataRuntimeModeContextWithPreview(context.Background(), path, "", "", "", "", true, func(preview *demoRuntime) {
			seenPreview = true
			previewElapsed += time.Since(start)
		})
		if err != nil {
			b.Fatal(err)
		}
		runtime.closeAttributeSource()
		if !seenPreview {
			b.Fatal("large dataset did not publish a preview")
		}
	}
	b.ReportMetric(float64(previewElapsed.Microseconds())/1000/float64(b.N), "first-preview-ms/op")
}

// BenchmarkDesktopReadOnlyLoadGeoJSON1M exercises the actual desktop policy
// with a million-feature source while keeping fixture generation out of the
// timed region. Run explicitly with -benchtime=1x; the temporary fixture is
// roughly 100 MB and the GDAL scan is intentionally representative of a large
// streaming source.
func BenchmarkDesktopReadOnlyLoadGeoJSON1M(b *testing.B) {
	benchmarkDesktopReadOnlyMillionPath(b, desktopBenchmarkGeoJSONFixture(b, 1_000_000), 1_000_000, true)
}

// BenchmarkDesktopReadOnlyLoadGeoJSONAboveIndexCap measures the first source
// size that cannot retain the complete one-million-entry in-memory window
// index. Loading remains bounded; each viewport checks the indexed prefix and
// streams only the unindexed tail. Keep this boundary visible in performance runs.
func BenchmarkDesktopReadOnlyLoadGeoJSONAboveIndexCap(b *testing.B) {
	featureCount := 1_000_001 // GDAL stream index retains at most 1,000,000 entries.
	benchmarkDesktopReadOnlyMillionPath(b, desktopBenchmarkGeoJSONFixture(b, featureCount), featureCount, true)
}

func BenchmarkDesktopReadOnlyLoadGeoJSON1MPlus100K(b *testing.B) {
	featureCount := 1_100_000
	benchmarkDesktopReadOnlyMillionPath(b, desktopBenchmarkGeoJSONFixture(b, featureCount), featureCount, true)
}

// BenchmarkDesktopReadOnlyLoadGeoJSON1MPlus100KShuffled measures viewport
// cost when nearby features are not adjacent in file order. The affine
// coordinate permutation is deterministic and avoids a million-entry shuffle
// allocation in the benchmark process.
func BenchmarkDesktopReadOnlyLoadGeoJSON1MPlus100KShuffled(b *testing.B) {
	const featureCount = 1_100_000
	benchmarkDesktopReadOnlyMillionPath(b, desktopBenchmarkGeoJSONFixtureWithOrder(b, featureCount, true), featureCount, true)
}

func BenchmarkDesktopReadOnlyLoadGeoJSONSeqAboveIndexCap(b *testing.B) {
	featureCount := 1_000_001
	benchmarkDesktopReadOnlyMillionPath(b, desktopBenchmarkGeoJSONSeqFixture(b, featureCount), featureCount, true)
}

// BenchmarkDesktopReadOnlyLoadGeoJSONSeq1M exercises the bounded record-stream
// reader and desktop read-only policy on a million-feature sequence source.
// RSS is measured rather than inferred from the format's incremental records.
func BenchmarkDesktopReadOnlyLoadGeoJSONSeq1M(b *testing.B) {
	benchmarkDesktopReadOnlyMillionPath(b, desktopBenchmarkGeoJSONSeqFixture(b, 1_000_000), 1_000_000, true)
}

// Run explicitly with -benchtime=1x. This exercises the desktop read-only
// policy, source inspection, runtime construction, and one viewport chunk
// above ten million features without checking the generated source into Git.
func BenchmarkDesktopReadOnlyLoadGeoJSONSeq10MPlusOne(b *testing.B) {
	const featureCount = 10_000_001
	benchmarkDesktopReadOnlyMillionPath(b, desktopBenchmarkGeoJSONSeqFixture(b, featureCount), featureCount, true)
}

// Run explicitly with -benchtime=1x. This probes a dense 10M+ source plus a
// separate distant file: initial fit, index budget, exact-limit retry, and
// bounded overview all run through the desktop runtime.
func BenchmarkDesktopDenseGeoJSONSeq10MWithDistantFile(b *testing.B) {
	const featureCount = 10_000_001
	dir := b.TempDir()
	mainPath := filepath.Join(dir, "dense.geojsonl")
	file, err := os.Create(mainPath)
	if err != nil {
		b.Fatal(err)
	}
	writer := bufio.NewWriterSize(file, 1<<20)
	const denseRecord = "{\"type\":\"Feature\",\"geometry\":{\"type\":\"Point\",\"coordinates\":[127,37]},\"properties\":{}}\n"
	for i := 0; i < featureCount-1; i++ {
		if _, err := writer.WriteString(denseRecord); err != nil {
			_ = file.Close()
			b.Fatal(err)
		}
	}
	if _, err := writer.WriteString("{\"type\":\"Feature\",\"geometry\":{\"type\":\"Point\",\"coordinates\":[127.001,37.001]},\"properties\":{}}\n"); err != nil {
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
	distantPath := filepath.Join(dir, "distant.geojsonl")
	if err := os.WriteFile(distantPath, []byte("{\"type\":\"Feature\",\"geometry\":{\"type\":\"Point\",\"coordinates\":[170,80]},\"properties\":{}}\n"), 0o600); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		started := time.Now()
		runtime, err := loadReadOnlyDataRuntime(context.Background(), []vectorSourceSpec{{Path: mainPath}, {Path: distantPath}}, "")
		if err != nil {
			b.Fatal(err)
		}
		if !runtime.viewportReadOnly || len(runtime.features) != 0 || runtime.mapExtent[2] < 170 || runtime.mapFitExtent[2] > 128 {
			runtime.closeAttributeSource()
			b.Fatalf("dense fit full=%v preferred=%v viewport=%t features=%d", runtime.mapExtent, runtime.mapFitExtent, runtime.viewportReadOnly, len(runtime.features))
		}
		b.ReportMetric(float64(time.Since(started).Milliseconds()), "runtime-load-ms/op")
		if rss, ok := benchmarkProcessMaxRSSBytes(); ok {
			b.ReportMetric(float64(rss)/(1<<20), "rss-after-load-MiB")
		}
		name := runtime.service.LayerNames()[0]
		key := chunkKeyForPoint(runtime.mapExtent, readOnlyWindowChunkSize(0), name, render.Point{X: 127, Y: 37})
		key.ZoomBucket = 0
		runtime.mu.Lock()
		runtime.windowVisibleKeys[key] = struct{}{}
		runtime.mu.Unlock()
		started = time.Now()
		if _, err := runtime.builder(context.Background(), key); err == nil || !strings.Contains(err.Error(), "sampled overview retry available") {
			runtime.closeAttributeSource()
			b.Fatalf("dense exact window retry = %v", err)
		}
		b.ReportMetric(float64(time.Since(started).Milliseconds()), "exact-retry-ms/op")
		if rss, ok := benchmarkProcessMaxRSSBytes(); ok {
			b.ReportMetric(float64(rss)/(1<<20), "rss-after-exact-MiB")
		}
		forced := context.WithValue(context.Background(), forceReadOnlyOverviewContextKey{}, true)
		started = time.Now()
		chunk, err := runtime.builder(forced, key)
		if err != nil || len(chunk.Vertices) == 0 || len(chunk.Vertices) > render.MaxChunkVertices {
			runtime.closeAttributeSource()
			b.Fatalf("dense sampled window vertices=%d err=%v", len(chunk.Vertices), err)
		}
		b.ReportMetric(float64(time.Since(started).Milliseconds()), "sampled-window-ms/op")
		fit, extent := runtime.mapFitExtent, runtime.mapExtent
		spanX, spanY := extent[2]-extent[0], extent[3]-extent[1]
		viewport := render.Viewport{
			Center: render.Point{X: ((fit[0]+fit[2])/2 - extent[0]) / spanX,
				Y: ((fit[1]+fit[3])/2 - extent[1]) / spanY},
			Zoom:        0.9 / max((fit[2]-fit[0])/spanX, (fit[3]-fit[1])/spanY),
			ScreenWidth: 1172, ScreenHeight: 732, CanvasWidth: 640, CanvasHeight: 732,
		}
		var before []struct{ Stream, Message string }
		if err := json.Unmarshal([]byte(native.DiagnosticLogJSON()), &before); err != nil {
			runtime.closeAttributeSource()
			b.Fatal(err)
		}
		started = time.Now()
		runtime.refresh(context.Background(), viewport)
		terminal := ""
		deadline := time.Now().Add(60 * time.Second)
		for time.Now().Before(deadline) {
			var entries []struct{ Stream, Message string }
			if err := json.Unmarshal([]byte(native.DiagnosticLogJSON()), &entries); err != nil {
				runtime.closeAttributeSource()
				b.Fatal(err)
			}
			for _, entry := range entries[min(len(before), len(entries)):] {
				if entry.Stream == "render" && (strings.Contains(entry.Message, "ready generation=") ||
					strings.Contains(entry.Message, "incomplete generation=")) {
					terminal = entry.Message
				}
			}
			if terminal != "" {
				break
			}
			time.Sleep(25 * time.Millisecond)
		}
		if !strings.Contains(terminal, "ready generation=") {
			runtime.closeAttributeSource()
			b.Fatalf("dense full viewport did not finish: %s", terminal)
		}
		metric := func(name string) int {
			for _, field := range strings.Fields(terminal) {
				if strings.HasPrefix(field, name+"=") {
					var value int
					if _, err := fmt.Sscanf(strings.TrimPrefix(field, name+"="), "%d", &value); err == nil {
						return value
					}
				}
			}
			b.Fatalf("missing %s in full-viewport diagnostic: %s", name, terminal)
			return 0
		}
		if metric("nonempty") == 0 || metric("vertices") == 0 {
			runtime.closeAttributeSource()
			b.Fatalf("dense full viewport contained no geometry: %s", terminal)
		}
		b.ReportMetric(float64(metric("chunks")), "viewport-chunks/op")
		b.ReportMetric(float64(metric("vertices")), "viewport-vertices/op")
		b.ReportMetric(float64(time.Since(started).Milliseconds()), "full-viewport-ms/op")
		goruntime.GC()
		var memory goruntime.MemStats
		goruntime.ReadMemStats(&memory)
		b.ReportMetric(float64(memory.HeapAlloc)/(1<<20), "retained-heap-MiB/op")
		if rss, ok := benchmarkProcessMaxRSSBytes(); ok {
			b.ReportMetric(float64(rss)/(1<<20), "process-maxrss-MiB")
		}
		runtime.closeAttributeSource()
	}
	b.ReportMetric(float64(featureCount), "dense-features/op")
}

// BenchmarkGeoJSONMetadataStages1M measures the raw godal/GDAL GeoJSON driver
// open, FeatureCount, and Bounds costs. It deliberately bypasses the desktop
// Reader's bounded JSON stream path and is a diagnostic baseline, not the
// application's large-source loading path.
func BenchmarkGeoJSONMetadataStages1M(b *testing.B) {
	path := desktopBenchmarkGeoJSONFixture(b, 1_000_000)
	godal.RegisterAll()
	registrationRSS, _ := benchmarkProcessMaxRSSBytes()
	dataset, err := godal.Open(path)
	if err != nil {
		b.Fatal(err)
	}
	layers := dataset.Layers()
	if len(layers) != 1 {
		_ = dataset.Close()
		b.Fatalf("metadata layers = %d, want 1", len(layers))
	}
	maxRSS := func() uint64 {
		if value, ok := benchmarkProcessMaxRSSBytes(); ok {
			return value
		}
		return 0
	}
	openedRSS := maxRSS()
	b.ResetTimer()
	featureCount, err := layers[0].FeatureCount()
	if err != nil || featureCount != 1_000_000 {
		_ = dataset.Close()
		b.Fatalf("GeoJSON FeatureCount=%d err=%v", featureCount, err)
	}
	countRSS := maxRSS()
	if _, err := layers[0].Bounds(); err != nil {
		_ = dataset.Close()
		b.Fatal(err)
	}
	boundsRSS := maxRSS()
	b.StopTimer()
	if err := dataset.Close(); err != nil {
		b.Fatal(err)
	}
	b.ReportMetric(float64(registrationRSS)/(1<<20), "rss-after-register-MiB")
	b.ReportMetric(float64(openedRSS)/(1<<20), "rss-after-open-MiB")
	b.ReportMetric(float64(countRSS)/(1<<20), "rss-after-count-MiB")
	b.ReportMetric(float64(boundsRSS)/(1<<20), "rss-after-bounds-MiB")
}

// BenchmarkGeoJSONSeqMetadataStages1M measures the raw GDAL GeoJSONSeq driver
// open and metadata scans. The desktop app routes GeoJSONSeq through its own
// bounded stream reader instead of this driver path for large vector reads.
func BenchmarkGeoJSONSeqMetadataStages1M(b *testing.B) {
	path := desktopBenchmarkGeoJSONSeqFixture(b, 1_000_000)
	godal.RegisterAll()
	registrationRSS, _ := benchmarkProcessMaxRSSBytes()
	dataset, err := godal.Open(path)
	if err != nil {
		b.Fatal(err)
	}
	layers := dataset.Layers()
	if len(layers) != 1 {
		_ = dataset.Close()
		b.Fatalf("metadata layers = %d, want 1", len(layers))
	}
	maxRSS := func() uint64 {
		if value, ok := benchmarkProcessMaxRSSBytes(); ok {
			return value
		}
		return 0
	}
	openedRSS := maxRSS()
	b.ResetTimer()
	featureCount, err := layers[0].FeatureCount()
	if err != nil || featureCount != 1_000_000 {
		_ = dataset.Close()
		b.Fatalf("GeoJSONSeq FeatureCount=%d err=%v", featureCount, err)
	}
	countRSS := maxRSS()
	if _, err := layers[0].Bounds(); err != nil {
		_ = dataset.Close()
		b.Fatal(err)
	}
	boundsRSS := maxRSS()
	b.StopTimer()
	if err := dataset.Close(); err != nil {
		b.Fatal(err)
	}
	b.ReportMetric(float64(registrationRSS)/(1<<20), "rss-after-register-MiB")
	b.ReportMetric(float64(openedRSS)/(1<<20), "rss-after-open-MiB")
	b.ReportMetric(float64(countRSS)/(1<<20), "rss-after-count-MiB")
	b.ReportMetric(float64(boundsRSS)/(1<<20), "rss-after-bounds-MiB")
}

// BenchmarkDesktopReadOnlyLoadGeoPackage1M compares the same point population
// after GDAL's streaming translation to a GeoPackage with its RTree enabled.
// Conversion is fixture setup and excluded from the timed loader/window path.
func BenchmarkDesktopReadOnlyLoadGeoPackage1M(b *testing.B) {
	const featureCount = 1_000_000
	input := desktopBenchmarkGeoJSONFixture(b, featureCount)
	output := filepath.Join(b.TempDir(), "million-points.gpkg")
	registered, err := gdal.OpenAttributeSession(input)
	if err != nil {
		b.Fatal(err)
	}
	if err := registered.Close(); err != nil {
		b.Fatal(err)
	}
	// GeoJSON AttributeSession uses the streaming parser and does not register
	// GDAL. The fixture conversion below calls godal directly.
	godal.RegisterAll()
	source, err := godal.Open(input)
	if err != nil {
		b.Fatal(err)
	}
	converted, err := source.VectorTranslate(output, []string{"-nln", "million_points", "-lco", "SPATIAL_INDEX=YES"}, godal.DriverName("GPKG"))
	closeErr := source.Close()
	if err != nil || closeErr != nil {
		if converted != nil {
			_ = converted.Close()
		}
		b.Fatalf("translate million-feature fixture: translate=%v close source=%v", err, closeErr)
	}
	if err := converted.Close(); err != nil {
		b.Fatal(err)
	}
	packageDataset, err := godal.Open(output)
	if err != nil {
		b.Fatal(err)
	}
	rtree, err := packageDataset.ExecuteSQL("SELECT name FROM sqlite_master WHERE type='table' AND name='rtree_million_points_geom'", godal.SQLiteDialect())
	if err != nil || rtree == nil {
		_ = packageDataset.Close()
		b.Fatalf("GeoPackage RTree table missing: result=%v err=%v", rtree, err)
	}
	rtreeCount, countErr := rtree.FeatureCount()
	rtreeCloseErr := rtree.Close()
	datasetCloseErr := packageDataset.Close()
	if rtreeCount != 1 || countErr != nil || rtreeCloseErr != nil || datasetCloseErr != nil {
		b.Fatalf("GeoPackage RTree verification count=%d countErr=%v resultClose=%v datasetClose=%v", rtreeCount, countErr, rtreeCloseErr, datasetCloseErr)
	}
	verification, err := gdal.OpenAttributeSession(output)
	if err != nil {
		b.Fatal(err)
	}
	overviews, inspectErr := verification.Inspect(context.Background())
	closeErr = verification.Close()
	if inspectErr != nil || closeErr != nil || len(overviews) != 1 || overviews[0].FeatureCount != featureCount {
		b.Fatalf("translated GeoPackage overview=%#v inspect=%v close=%v", overviews, inspectErr, closeErr)
	}
	benchmarkDesktopReadOnlyMillionPath(b, output, featureCount, false)
	b.StopTimer()
	if rss := benchmarkReadOnlyChildPeakRSSMiB(b, output); rss > 0 {
		b.ReportMetric(float64(rss), "loader-child-maxrss-MiB")
	}
}

// BenchmarkDesktopReadOnlyLoadShapefile1M measures the same dataset after
// GDAL creates a QIX sidecar, which is the important distinction for large
// SHP viewport filters.
func BenchmarkDesktopReadOnlyLoadShapefile1M(b *testing.B) {
	path := desktopBenchmarkShapefile1MFixture(b, true)
	benchmarkDesktopReadOnlyMillionPath(b, path, 1_000_000, false)
	b.StopTimer()
	if rss := benchmarkReadOnlyChildPeakRSSMiB(b, path); rss > 0 {
		b.ReportMetric(float64(rss), "loader-child-maxrss-MiB")
	}
}

// BenchmarkDesktopReadOnlyLoadShapefile1MNoIndex provides a control case for
// the same SHP without its optional spatial index.
func BenchmarkDesktopReadOnlyLoadShapefile1MNoIndex(b *testing.B) {
	path := desktopBenchmarkShapefile1MFixture(b, false)
	benchmarkDesktopReadOnlyMillionPath(b, path, 1_000_000, false)
	b.StopTimer()
	if rss := benchmarkReadOnlyChildPeakRSSMiB(b, path); rss > 0 {
		b.ReportMetric(float64(rss), "loader-child-maxrss-MiB")
	}
}

// Run explicitly with -benchtime=1x. A streamed 10M+1 SHP and QIX are kept
// only in b.TempDir; the timed region measures desktop load, a complete
// initial overview, two detail views, and a final attribute page.
func BenchmarkDesktopShapefile10MPlusOneOverview(b *testing.B) {
	const featureCount = 10_000_001
	path := filepath.Join(b.TempDir(), "ten-million-points.shp")
	godal.RegisterAll()
	dataset, err := godal.CreateVector(godal.Shapefile, path)
	if err != nil {
		b.Fatal(err)
	}
	spatialRef, err := godal.NewSpatialRef("EPSG:4326")
	if err != nil {
		_ = dataset.Close()
		b.Fatal(err)
	}
	defer spatialRef.Close()
	layer, err := dataset.CreateLayer("ten_million_points", spatialRef, godal.GTPoint)
	if err != nil {
		_ = dataset.Close()
		b.Fatal(err)
	}
	for y := 0; y < 100; y++ {
		for x := 0; x < 100; x++ {
			geometry, geometryErr := godal.NewGeometryFromWKT(fmt.Sprintf("POINT (%g %g)", float64(x)/100, float64(y)/100), spatialRef)
			if geometryErr != nil {
				_ = dataset.Close()
				b.Fatal(geometryErr)
			}
			for i := 0; i < 1000; i++ {
				feature, featureErr := layer.NewFeature(geometry)
				if featureErr != nil {
					geometry.Close()
					_ = dataset.Close()
					b.Fatal(featureErr)
				}
				featureErr = layer.UpdateFeature(feature)
				feature.Close()
				if featureErr != nil {
					geometry.Close()
					_ = dataset.Close()
					b.Fatal(featureErr)
				}
			}
			geometry.Close()
		}
	}
	extra, err := godal.NewGeometryFromWKT("POINT (0.995 0.995)", spatialRef)
	if err != nil {
		_ = dataset.Close()
		b.Fatal(err)
	}
	feature, err := layer.NewFeature(extra)
	if err != nil {
		extra.Close()
		_ = dataset.Close()
		b.Fatal(err)
	}
	err = layer.UpdateFeature(feature)
	feature.Close()
	extra.Close()
	if closeErr := dataset.Close(); err != nil || closeErr != nil {
		b.Fatalf("finish SHP fixture: write=%v close=%v", err, closeErr)
	}
	if err := gdal.CreateShapefileSpatialIndex(context.Background(), path, ""); err != nil {
		b.Fatal(err)
	}
	benchmarkDesktopTenMillionOverview(b, path, featureCount)
}

// The GeoJSONSeq input streams through GDAL's translation path; its generated
// 10M+1 GeoPackage and RTree stay under b.TempDir and are not checked in.
// GOGIS_BENCH_GPKG_PATH reuses a previously generated synthetic fixture.
func BenchmarkDesktopGeoPackage10MPlusOneOverview(b *testing.B) {
	const featureCount = 10_000_001
	output := os.Getenv("GOGIS_BENCH_GPKG_PATH")
	godal.RegisterAll()
	if output == "" {
		input := desktopBenchmarkGeoJSONSeqFixture(b, featureCount)
		output = filepath.Join(b.TempDir(), "ten-million-points.gpkg")
		source, err := godal.Open(input)
		if err != nil {
			b.Fatal(err)
		}
		converted, err := source.VectorTranslate(output, []string{"-nln", "ten_million_points", "-lco", "SPATIAL_INDEX=YES"}, godal.DriverName("GPKG"))
		closeErr := source.Close()
		if err != nil || closeErr != nil {
			if converted != nil {
				_ = converted.Close()
			}
			b.Fatalf("translate 10M GeoJSONSeq to GPKG: translate=%v close=%v", err, closeErr)
		}
		if err := converted.Close(); err != nil {
			b.Fatal(err)
		}
	}
	dataset, err := godal.Open(output)
	if err != nil {
		b.Fatal(err)
	}
	rtree, err := dataset.ExecuteSQL("SELECT name FROM sqlite_master WHERE type='table' AND name='rtree_ten_million_points_geom'", godal.SQLiteDialect())
	if err != nil || rtree == nil {
		_ = dataset.Close()
		b.Fatalf("10M GPKG RTree missing: %v", err)
	}
	rtreeCount, countErr := rtree.FeatureCount()
	rtreeCloseErr, datasetCloseErr := rtree.Close(), dataset.Close()
	if rtreeCount != 1 || countErr != nil || rtreeCloseErr != nil || datasetCloseErr != nil {
		b.Fatalf("10M GPKG RTree check: count=%d countErr=%v close=%v datasetClose=%v", rtreeCount, countErr, rtreeCloseErr, datasetCloseErr)
	}
	stageStart := time.Now()
	probe, err := gdal.OpenAttributeSession(output)
	if err != nil {
		b.Fatal(err)
	}
	probeOpenMS := time.Since(stageStart).Milliseconds()
	stageStart = time.Now()
	overviews, err := probe.Inspect(context.Background())
	if err != nil || len(overviews) != 1 || overviews[0].FeatureCount != featureCount {
		_ = probe.Close()
		b.Fatalf("10M GPKG overview=%#v err=%v", overviews, err)
	}
	probeInspectMS := time.Since(stageStart).Milliseconds()
	stageStart = time.Now()
	samples, err := probe.SampleGeometryEnvelopes(context.Background(), overviews[0].Name, featureCount, 256)
	if err != nil {
		_ = probe.Close()
		b.Fatal(err)
	}
	probeSampleMS := time.Since(stageStart).Milliseconds()
	probeSampleCount := len(samples)
	stageStart = time.Now()
	if _, _, err := probe.OpenAttributePage(context.Background(), overviews[0].Name, 0, 1); err != nil {
		_ = probe.Close()
		b.Fatal(err)
	}
	probePageMS := time.Since(stageStart).Milliseconds()
	if err := probe.Close(); err != nil {
		b.Fatal(err)
	}
	benchmarkDesktopTenMillionOverview(b, output, featureCount)
	b.StopTimer()
	b.ReportMetric(float64(probeOpenMS), "probe-open-ms")
	b.ReportMetric(float64(probeInspectMS), "probe-inspect-ms")
	b.ReportMetric(float64(probeSampleMS), "probe-sample-ms")
	b.ReportMetric(float64(probeSampleCount), "probe-samples")
	b.ReportMetric(float64(probePageMS), "probe-page-ms")
	if rss := benchmarkReadOnlyChildPeakRSSMiB(b, output); rss > 0 {
		b.ReportMetric(float64(rss), "loader-child-maxrss-MiB")
	}
}

func benchmarkDesktopTenMillionOverview(b *testing.B, path string, featureCount int) {
	b.Helper()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		started := time.Now()
		runtime, loadErr := loadReadOnlyDataRuntime(context.Background(), []vectorSourceSpec{{Path: path}}, "")
		if loadErr != nil {
			b.Fatal(loadErr)
		}
		if !runtime.viewportReadOnly || len(runtime.features) != 0 {
			runtime.closeAttributeSource()
			b.Fatalf("10M runtime retained %d source features", len(runtime.features))
		}
		b.ReportMetric(float64(time.Since(started).Milliseconds()), "load-ms/op")
		fit := initialFitWorkspaceView(runtime, native.Viewport{ViewportWidth: 1172, ViewportHeight: 732})
		if fit == nil {
			runtime.closeAttributeSource()
			b.Fatal("missing 10M initial fit")
		}
		viewport := render.Viewport{Center: render.Point{X: fit.CenterX, Y: fit.CenterY}, Zoom: fit.Zoom,
			ScreenWidth: 1172, ScreenHeight: 732, CanvasWidth: 585, CanvasHeight: 732}
		var before []struct{ Stream, Message string }
		if err := json.Unmarshal([]byte(native.DiagnosticLogJSON()), &before); err != nil {
			runtime.closeAttributeSource()
			b.Fatal(err)
		}
		started = time.Now()
		runtime.refresh(context.Background(), viewport)
		terminal := ""
		deadline := time.Now().Add(90 * time.Second)
		for time.Now().Before(deadline) {
			var entries []struct{ Stream, Message string }
			if err := json.Unmarshal([]byte(native.DiagnosticLogJSON()), &entries); err != nil {
				runtime.closeAttributeSource()
				b.Fatal(err)
			}
			for _, entry := range entries[min(len(before), len(entries)):] {
				if entry.Stream == "render" && (strings.Contains(entry.Message, "ready generation=") || strings.Contains(entry.Message, "incomplete generation=")) {
					terminal = entry.Message
				}
			}
			if terminal != "" {
				break
			}
			time.Sleep(25 * time.Millisecond)
		}
		if !strings.Contains(terminal, "ready generation=") ||
			(strings.EqualFold(filepath.Ext(path), ".shp") && !strings.Contains(terminal, "world_occupied_cells=256/256")) {
			runtime.closeAttributeSource()
			b.Fatalf("10M overview incomplete or lost spatial coverage: %s", terminal)
		}
		var generation, chunks, nonempty int
		if _, err := fmt.Sscanf(terminal, "ready generation=%d chunks=%d nonempty=%d", &generation, &chunks, &nonempty); err != nil || chunks == 0 || nonempty == 0 {
			runtime.closeAttributeSource()
			b.Fatalf("10M overview has no visible features: %s (%v)", terminal, err)
		}
		b.ReportMetric(float64(chunks), "chunks/op")
		b.ReportMetric(float64(nonempty), "nonempty-chunks/op")
		b.ReportMetric(float64(time.Since(started).Milliseconds()), "overview-ms/op")
		b.Logf("10M %s overview: %s", filepath.Ext(path), terminal)
		// Opening the source is only the first part of the desktop workflow.
		// Exercise exact city-scale detail, a distant pan, and a page at the
		// end of the attribute table on the same 10M source.
		for viewIndex, center := range []render.Point{{X: 0.5, Y: 0.5}, {X: 0.75, Y: 0.75}} {
			viewport.Center, viewport.Zoom = center, 64
			before = nil
			if err := json.Unmarshal([]byte(native.DiagnosticLogJSON()), &before); err != nil {
				runtime.closeAttributeSource()
				b.Fatal(err)
			}
			started = time.Now()
			runtime.refresh(context.Background(), viewport)
			terminal = ""
			deadline = time.Now().Add(90 * time.Second)
			for time.Now().Before(deadline) {
				var entries []struct{ Stream, Message string }
				if err := json.Unmarshal([]byte(native.DiagnosticLogJSON()), &entries); err != nil {
					runtime.closeAttributeSource()
					b.Fatal(err)
				}
				for _, entry := range entries[min(len(before), len(entries)):] {
					if entry.Stream == "render" && (strings.Contains(entry.Message, "ready generation=") || strings.Contains(entry.Message, "incomplete generation=")) {
						terminal = entry.Message
					}
				}
				if terminal != "" {
					break
				}
				time.Sleep(25 * time.Millisecond)
			}
			if !strings.Contains(terminal, "ready generation=") {
				runtime.closeAttributeSource()
				b.Fatalf("10M detail view %d incomplete: %s", viewIndex, terminal)
			}
			b.ReportMetric(float64(time.Since(started).Milliseconds()), fmt.Sprintf("detail-%d-ms/op", viewIndex+1))
			b.Logf("10M %s detail %d: %s", filepath.Ext(path), viewIndex+1, terminal)
		}
		started = time.Now()
		page, total, pageErr := runtime.attributePageReader(context.Background(), runtime.service.LayerNames()[0], featureCount-128, 128)
		if pageErr != nil || total != featureCount || len(page.Features) != 128 {
			runtime.closeAttributeSource()
			b.Fatalf("10M last attribute page: rows=%d total=%d err=%v", len(page.Features), total, pageErr)
		}
		b.ReportMetric(float64(time.Since(started).Milliseconds()), "last-page-ms/op")
		if rss, ok := benchmarkProcessMaxRSSBytes(); ok {
			b.ReportMetric(float64(rss)/(1<<20), "maxrss-MiB")
		}
		runtime.closeAttributeSource()
	}
	b.ReportMetric(float64(featureCount), "features/source")
}

func benchmarkReadOnlyChildPeakRSSMiB(b *testing.B, path string) uint64 {
	b.Helper()
	command := exec.Command(os.Args[0], "-test.run=^TestDesktopReadOnlyRSSHelper$")
	command.Env = append(os.Environ(), "GOGIS_RSS_FIXTURE="+path)
	output, err := command.CombinedOutput()
	if err != nil {
		b.Fatalf("run isolated read-only RSS helper: %v\n%s", err, output)
	}
	const marker = "GOGIS_RSS_RESULT="
	index := strings.LastIndex(string(output), marker)
	if index < 0 {
		if strings.Contains(string(output), "GOGIS_RSS_UNAVAILABLE") {
			return 0
		}
		b.Fatalf("RSS helper did not return a measurement:\n%s", output)
	}
	var rssMiB uint64
	if _, err := fmt.Sscanf(string(output[index+len(marker):]), "%d", &rssMiB); err != nil || rssMiB == 0 {
		b.Fatalf("parse RSS helper result: rss=%d err=%v output=%s", rssMiB, err, output)
	}
	return rssMiB
}

// TestDesktopReadOnlyRSSHelper runs only as a subprocess from the million-SHP
// and GeoPackage benchmarks so fixture generation and conversion do not
// contaminate the loader's peak-RSS measurement.
func TestDesktopReadOnlyRSSHelper(t *testing.T) {
	path := os.Getenv("GOGIS_RSS_FIXTURE")
	if path == "" {
		return
	}
	ctx := context.Background()
	count, unknown, session, err := inspectSingleSourceFeatureCount(ctx, vectorSourceSpec{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if !shouldOpenLargeDatasetReadOnly(count, unknown, false) || count < largeDatasetReadOnlyThreshold {
		if session != nil {
			_ = session.Close()
		}
		t.Fatalf("million-feature policy did not select read-only: count=%d unknown=%t", count, unknown)
	}
	runtime, err := loadDataRuntimeModeContextWithEncodingAndSession(ctx, path, "", "", "", "", "", true, nil, session)
	if err != nil {
		t.Fatal(err)
	}
	if !runtime.viewportReadOnly || len(runtime.features) != 0 {
		runtime.closeAttributeSource()
		t.Fatalf("runtime viewport=%t residentFeatures=%d", runtime.viewportReadOnly, len(runtime.features))
	}
	key := render.ChunkKey{Layer: runtime.service.LayerNames()[0], ZoomBucket: 2, X: 0, Y: 0}
	runtime.mu.Lock()
	runtime.windowVisibleKeys[key] = struct{}{}
	runtime.mu.Unlock()
	if _, err := runtime.builder(ctx, key); err != nil {
		runtime.closeAttributeSource()
		t.Fatal(err)
	}
	goruntime.GC()
	if rss, ok := benchmarkProcessMaxRSSBytes(); ok {
		runtime.closeAttributeSource()
		fmt.Printf("GOGIS_RSS_RESULT=%d\n", rss/(1<<20))
		return
	}
	runtime.closeAttributeSource()
	fmt.Println("GOGIS_RSS_UNAVAILABLE")
}

func desktopBenchmarkShapefile1MFixture(b *testing.B, withSpatialIndex bool) string {
	const featureCount = 1_000_000
	input := desktopBenchmarkGeoJSONFixture(b, featureCount)
	output := filepath.Join(b.TempDir(), "million-points.shp")
	registered, err := gdal.OpenAttributeSession(input)
	if err != nil {
		b.Fatal(err)
	}
	if err := registered.Close(); err != nil {
		b.Fatal(err)
	}
	godal.RegisterAll()
	source, err := godal.Open(input)
	if err != nil {
		b.Fatal(err)
	}
	switches := []string{"-nln", "million_points"}
	if withSpatialIndex {
		switches = append(switches, "-lco", "SPATIAL_INDEX=YES")
	}
	converted, err := source.VectorTranslate(output, switches, godal.DriverName("ESRI Shapefile"))
	closeErr := source.Close()
	if err != nil || closeErr != nil {
		if converted != nil {
			_ = converted.Close()
		}
		b.Fatalf("translate million-feature SHP fixture: translate=%v close source=%v", err, closeErr)
	}
	if err := converted.Close(); err != nil {
		b.Fatal(err)
	}
	_, indexErr := os.Stat(strings.TrimSuffix(output, filepath.Ext(output)) + ".qix")
	if withSpatialIndex && indexErr != nil {
		b.Fatalf("Shapefile QIX sidecar missing: %v", indexErr)
	}
	if !withSpatialIndex && indexErr == nil {
		b.Fatal("no-index Shapefile fixture unexpectedly contains a QIX sidecar")
	}
	verification, err := gdal.OpenAttributeSession(output)
	if err != nil {
		b.Fatal(err)
	}
	overviews, inspectErr := verification.Inspect(context.Background())
	closeErr = verification.Close()
	if inspectErr != nil || closeErr != nil || len(overviews) != 1 || overviews[0].FeatureCount != featureCount {
		b.Fatalf("translated Shapefile overview=%#v inspect=%v close=%v", overviews, inspectErr, closeErr)
	}
	return output
}

func benchmarkDesktopReadOnlyMillionPath(b *testing.B, path string, featureCount int, trackPeakRSS bool) {
	var firstWindowElapsed time.Duration
	var retainedHeapBytes uint64
	var peakRSSBytes uint64
	var peakRSSAfterInspect uint64
	var peakRSSAfterLoad uint64
	var peakRSSAfterWindow uint64
	var visibleFeatureCount int
	var visiblePayloadBytes int64
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		ctx := context.Background()
		sources := []vectorSourceSpec{{Path: path}}
		count, unknownCount, inspectionSession, err := inspectSingleSourceFeatureCount(ctx, sources[0])
		if err != nil {
			b.Fatalf("inspect million-feature source: %v", err)
		}
		if trackPeakRSS {
			if rss, ok := benchmarkProcessMaxRSSBytes(); ok && rss > peakRSSAfterInspect {
				peakRSSAfterInspect = rss
			}
		}
		if !shouldOpenLargeDatasetReadOnly(count, unknownCount, false) || count < largeDatasetReadOnlyThreshold {
			if inspectionSession != nil {
				_ = inspectionSession.Close()
			}
			b.Fatalf("million-feature policy did not select read-only: count=%d unknown=%t", count, unknownCount)
		}
		runtime, err := loadDataRuntimeModeContextWithEncodingAndSession(ctx, path, "", "", "", "", "", true, nil, inspectionSession)
		if err != nil {
			b.Fatalf("load million-feature read-only runtime: %v", err)
		}
		if !runtime.viewportReadOnly || len(runtime.features) != 0 {
			runtime.closeAttributeSource()
			b.Fatalf("million-feature runtime viewport=%t residentFeatures=%d", runtime.viewportReadOnly, len(runtime.features))
		}
		if trackPeakRSS {
			if rss, ok := benchmarkProcessMaxRSSBytes(); ok && rss > peakRSSAfterLoad {
				peakRSSAfterLoad = rss
			}
		}
		key := render.ChunkKey{Layer: runtime.service.LayerNames()[0], ZoomBucket: 2, X: 0, Y: 0}
		runtime.mu.Lock()
		runtime.windowVisibleKeys[key] = struct{}{}
		runtime.mu.Unlock()
		windowStart := time.Now()
		if _, err := runtime.builder(context.Background(), key); err != nil {
			runtime.closeAttributeSource()
			b.Fatalf("build first million-feature viewport chunk: %v", err)
		}
		firstWindowElapsed += time.Since(windowStart)
		if trackPeakRSS {
			if rss, ok := benchmarkProcessMaxRSSBytes(); ok && rss > peakRSSAfterWindow {
				peakRSSAfterWindow = rss
			}
		}
		if len(runtime.features) > maxReadOnlyVisibleFeatures || runtime.windowVisiblePayloadBytes > maxReadOnlyVisibleBytes {
			runtime.closeAttributeSource()
			b.Fatalf("million-feature viewport exceeded visible limits: features=%d bytes=%d", len(runtime.features), runtime.windowVisiblePayloadBytes)
		}
		visibleFeatureCount = len(runtime.features)
		visiblePayloadBytes = runtime.windowVisiblePayloadBytes
		goruntime.GC()
		var memory goruntime.MemStats
		goruntime.ReadMemStats(&memory)
		retainedHeapBytes = memory.HeapAlloc
		if trackPeakRSS {
			if rss, ok := benchmarkProcessMaxRSSBytes(); ok && rss > peakRSSBytes {
				peakRSSBytes = rss
			}
		}
		runtime.closeAttributeSource()
	}
	b.ReportMetric(float64(featureCount), "features/op")
	b.ReportMetric(float64(firstWindowElapsed.Microseconds())/1000/float64(b.N), "first-window-ms/op")
	b.ReportMetric(float64(retainedHeapBytes)/(1<<20), "retained-heap-MiB/op")
	b.ReportMetric(float64(visibleFeatureCount), "visible-features/op")
	b.ReportMetric(float64(visiblePayloadBytes)/(1<<20), "visible-payload-MiB/op")
	if trackPeakRSS && peakRSSBytes > 0 {
		b.ReportMetric(float64(peakRSSBytes)/(1<<20), "process-maxrss-MiB")
		b.ReportMetric(float64(peakRSSAfterInspect)/(1<<20), "rss-after-inspect-MiB")
		b.ReportMetric(float64(peakRSSAfterLoad)/(1<<20), "rss-after-readonly-load-MiB")
		b.ReportMetric(float64(peakRSSAfterWindow)/(1<<20), "rss-after-first-window-MiB")
	}
}

func desktopBenchmarkGeoJSONFixture(b testing.TB, featureCount int) string {
	return desktopBenchmarkGeoJSONFixtureWithOrder(b, featureCount, false)
}

func desktopBenchmarkGeoJSONFixtureWithOrder(b testing.TB, featureCount int, shuffled bool) string {
	b.Helper()
	path := filepath.Join(b.TempDir(), "million-points.geojson")
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
		if index > 0 {
			if err := writer.WriteByte(','); err != nil {
				_ = file.Close()
				b.Fatal(err)
			}
		}
		x, y := index%1000, index/1000
		if shuffled {
			x = (index * 919) % 1000
			y = (index*729 + index/1000) % 1000
		}
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

func desktopBenchmarkGeoJSONSeqFixture(b *testing.B, featureCount int) string {
	b.Helper()
	path := filepath.Join(b.TempDir(), "million-points.geojsonl")
	file, err := os.Create(path)
	if err != nil {
		b.Fatal(err)
	}
	writer := bufio.NewWriterSize(file, 1<<20)
	for index := 0; index < featureCount; index++ {
		x, y := index%1000, index/1000
		if _, err := fmt.Fprintf(writer, `{"type":"Feature","properties":{"name":"p%d"},"geometry":{"type":"Point","coordinates":[0.%03d,0.%03d]}}`+"\n", index, x, y); err != nil {
			_ = file.Close()
			b.Fatal(err)
		}
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
