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
