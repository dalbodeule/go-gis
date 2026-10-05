//go:build qt && native

package main

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"gogis/drivers/gdal"
	geosdriver "gogis/drivers/geos"
	projdriver "gogis/drivers/proj"
	"gogis/internal/core"
	"gogis/internal/render"
	"gogis/internal/workspace"
	"gogis/ui/qt/native"

	"github.com/airbusgeo/godal"
)

func TestContextSemaphoreHonorsCapacityAndCancellation(t *testing.T) {
	semaphore := newContextSemaphore(2)
	first, err := semaphore.acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	second, err := semaphore.acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := semaphore.acquire(ctx); err != context.Canceled {
		t.Fatalf("acquire with canceled context = %v; want context.Canceled", err)
	}
	first()
	third, err := semaphore.acquire(context.Background())
	if err != nil {
		t.Fatalf("acquire after permit release: %v", err)
	}
	third()
	second()
	second() // release callbacks are idempotent
}

func TestReadOnlyWindowBuildConcurrencyKeepsDetailBounded(t *testing.T) {
	if got := cap(readOnlyWindowBuildSemaphore.permits); got != readOnlyOverviewWorkerLimit() {
		t.Fatalf("total read-only build slots=%d, want %d", got, readOnlyOverviewWorkerLimit())
	}
	if got := cap(readOnlyDetailBuildSemaphore.permits); got != 2 {
		t.Fatalf("detail build slots=%d, want 2", got)
	}
}

func TestReadOnlyOverviewWorkerLimit(t *testing.T) {
	for _, test := range []struct {
		goos        string
		logicalCPUs int
		want        int
	}{
		{"windows", 1, 1},
		{"windows", 4, 2},
		{"windows", 12, 6},
		{"windows", 16, 8},
		{"windows", 32, 8},
		{"darwin", 16, 4},
		{"linux", 16, 4},
	} {
		if got := readOnlyOverviewWorkerLimitFor(test.goos, test.logicalCPUs); got != test.want {
			t.Errorf("worker limit for %s/%d = %d, want %d", test.goos, test.logicalCPUs, got, test.want)
		}
	}
}

func TestMoveSelectedVertexUpdatesProjectAndRenderSource(t *testing.T) {
	wkb := make([]byte, 1+4+4+4*8)
	wkb[0] = 1
	binary.LittleEndian.PutUint32(wkb[1:5], 2)
	binary.LittleEndian.PutUint32(wkb[5:9], 2)
	for index, value := range []float64{0, 0, 10, 10} {
		binary.LittleEndian.PutUint64(wkb[9+index*8:], math.Float64bits(value))
	}
	runtime, err := buildDataRuntime(context.Background(), []core.Layer{{
		Name: "roads", Editable: true,
		Features: []core.Feature{{ID: 42, Geometry: core.WKBGeometry{WKB: wkb}}},
	}}, "", "", "", "", "", false, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	persistedLayer := ""
	runtime.persist = func(_ context.Context, layerName string) error {
		persistedLayer = layerName
		return nil
	}
	runtime.selected = render.HitResult{Layer: "roads", FeatureID: 42}
	if err := runtime.moveSelectedVertex(runtime.selected, `{"vertexIndex":1,"x":8,"y":9}`); err != nil {
		t.Fatal(err)
	}
	geometry, ok := runtime.service.FeatureGeometry("roads", 42)
	if !ok {
		t.Fatal("edited feature geometry not found")
	}
	parts, err := geometry.(core.WKBGeometry).Parts()
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) != 1 || parts[0][1] != (core.WKBPoint{X: 8, Y: 9}) {
		t.Fatalf("committed feature geometry = %#v", parts)
	}
	found := false
	for _, feature := range runtime.features {
		if feature.Layer == "roads" && feature.FeatureID == 42 {
			if len(feature.Vertices) != 2 || feature.Vertices[1] != (render.Point{X: 0.8, Y: 0.9}) {
				t.Fatalf("rebuilt hit-test geometry = %#v", feature.Vertices)
			}
			found = true
		}
	}
	if !found {
		t.Fatal("edited feature was not published to render hit-test source")
	}
	if persistedLayer != "roads" {
		t.Fatalf("persisted layer = %q, want roads", persistedLayer)
	}
}

func TestUniqueSourcePathsRemovesRepeatedFiles(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "roads.gpkg")
	got := uniqueSourcePaths([]string{path, filepath.Join(directory, ".", "roads.gpkg"), "", "   "})
	if len(got) != 1 || normalizedSourcePath(got[0]) != normalizedSourcePath(path) {
		t.Fatalf("unique source paths = %v, want one normalized %q", got, path)
	}
}

func TestRenderChunkBoundsMapsUnitTileToSourceCRS(t *testing.T) {
	extent := [4]float64{10, 20, 110, 220}
	bounds, ok := renderChunkBounds(extent, 0.25, render.ChunkKey{X: 1, Y: 2})
	if !ok || bounds != [4]float64{35, 120, 60, 170} {
		t.Fatalf("chunk bounds = %v, valid=%t", bounds, ok)
	}
	if _, ok := renderChunkBounds(extent, 0.25, render.ChunkKey{X: 4, Y: 0}); ok {
		t.Fatal("chunk entirely outside normalized data extent was accepted")
	}
	fineBounds, ok := renderChunkBounds(extent, 0.125, render.ChunkKey{X: 6, Y: 4})
	if !ok || fineBounds != [4]float64{85, 120, 97.5, 145} {
		t.Fatalf("fine chunk bounds = %v, valid=%t", fineBounds, ok)
	}
}

func TestLayerSourceBoundsArePerLayerNotProjectUnion(t *testing.T) {
	runtime, err := buildDataRuntime(context.Background(), []core.Layer{
		{Name: "parcel", Features: []core.Feature{{Geometry: core.WKTGeometry{WKT: "POLYGON ((10 20, 30 20, 30 40, 10 40, 10 20))"}}}},
		{Name: "survey-point", Features: []core.Feature{{Geometry: core.WKTGeometry{WKT: "POINT (90 80)"}}}},
	}, "", "", "", "", "", false, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := runtime.layerBounds["parcel"], [4]float64{10, 20, 30, 40}; got != want {
		t.Fatalf("parcel bounds = %v, want %v", got, want)
	}
	if got, want := runtime.layerBounds["survey-point"], [4]float64{90, 80, 90, 80}; got != want {
		t.Fatalf("survey point bounds = %v, want %v", got, want)
	}
	if got, want := runtime.mapFitExtent, [4]float64{10, 20, 30, 40}; got != want {
		t.Fatalf("preferred fit extent = %v, want polygon bounds %v", got, want)
	}
}

func TestReadOnlyWindowChunkSizeKeepsQueryWindowsBounded(t *testing.T) {
	for _, test := range []struct {
		zoom   float64
		bucket int
		size   float64
	}{{0.5, -1, 0.03125}, {1, 0, 0.03125}, {2, 1, 0.015625}, {4, 2, 0.0078125}, {16, 4, 0.0078125},
		{64, 6, 0.001953125}, {128, 7, 0.001953125}, {256, 8, 0.001953125},
		{512, 9, 0.0009765625}, {math.Inf(1), 0, 0.03125}} {
		if bucket := readOnlyWindowZoomBucket(test.zoom); bucket != test.bucket {
			t.Errorf("zoom bucket for %v = %d, want %d", test.zoom, bucket, test.bucket)
		}
		if size := readOnlyWindowChunkSize(test.bucket); size != test.size {
			t.Errorf("chunk size for bucket %d = %v, want %v", test.bucket, size, test.size)
		}
	}
	if size := readOnlyWindowChunkSize(100); size != 0.000000476837158203125 {
		t.Fatalf("extreme zoom chunk size = %v, want minimum size 1/2097152", size)
	}
}

func TestReadOnlyKeyPruningSkipsTilesOutsideLayerExtent(t *testing.T) {
	extent := [4]float64{211_407.24, 43_257.02, 2_287_874.9, 459_484.82}
	bounds := map[string][4]float64{
		"parcels":   {211_407.24, 423_223.66, 236_805.50, 459_484.82},
		"buildings": {211_761.86, 423_384.00, 236_739.80, 459_353.81},
		"points":    {212_159.77, 43_257.02, 2_287_874.9, 459_421.8},
	}
	viewport := render.Viewport{
		Center: render.Point{X: 0.010537, Y: 0.956298}, Zoom: 24.684293245689016,
		ScreenWidth: 1166, ScreenHeight: 726, CanvasWidth: 1166, CanvasHeight: 234,
	}
	planner := render.NewChunkPlanner()
	planner.ChunkSize = readOnlyWindowChunkSize(readOnlyWindowZoomBucket(viewport.Zoom))
	planner.Domain = [4]float64{0, 0, 1, 1}
	planner.HasDomain = true
	var keys []render.ChunkKey
	for _, name := range []string{"parcels", "buildings", "points"} {
		keys = planner.VisibleKeysInto(keys, viewport, name)
	}
	planned := len(keys)
	keys = filterReadOnlyKeysByLayerBounds(keys, extent, planner.ChunkSize, bounds)
	if planned < 200 || len(keys) >= planned-50 {
		t.Fatalf("outlier-extent key pruning kept %d/%d keys; expected many known-empty polygon tiles to be removed", len(keys), planned)
	}
	for _, key := range keys {
		window, ok := renderChunkBounds(extent, planner.ChunkSize, key)
		if !ok || window[0] > bounds[key.Layer][2] || window[2] < bounds[key.Layer][0] ||
			window[1] > bounds[key.Layer][3] || window[3] < bounds[key.Layer][1] {
			t.Fatalf("kept disjoint key %v with window %v", key, window)
		}
	}
	unknown := []render.ChunkKey{{Layer: "unindexed", X: 0, Y: 0}}
	if got := filterReadOnlyKeysByLayerBounds(unknown, extent, planner.ChunkSize, bounds); len(got) != 1 {
		t.Fatal("unknown layer extent must remain fail-open")
	}
}

func TestDenseParcelOverviewAtMunicipalityScale(t *testing.T) {
	for _, test := range []struct {
		lod, count int
		geometry   string
		forced     bool
		want       bool
	}{
		{lod: 0, count: 208_015, geometry: "Polygon", want: true},
		{lod: 1, count: 208_015, geometry: "Polygon", want: true},
		{lod: 2, count: 208_015, geometry: "Polygon"},
		{lod: 1, count: 99_999, geometry: "Polygon"},
		{lod: 1, count: 208_015, geometry: "Point"},
		{lod: 1, count: 208_015, geometry: "Polygon", forced: true},
	} {
		if got := useDenseParcelOverview(test.lod, test.count, test.geometry, test.forced); got != test.want {
			t.Errorf("dense parcel overview at lod=%d features=%d geometry=%s forced=%t = %t, want %t",
				test.lod, test.count, test.geometry, test.forced, got, test.want)
		}
	}
}

func TestSelectVisibleParcelOutlinesKeepsOnlyResolvableBoundaries(t *testing.T) {
	layer := core.Layer{Features: []core.Feature{
		{ID: 1, Geometry: core.WKTGeometry{WKT: "POLYGON ((0 0, 20 0, 20 20, 0 20, 0 0))"}},
		{ID: 2, Geometry: core.WKTGeometry{WKT: "POLYGON ((0 0, 1 0, 1 1, 0 1, 0 0))"}},
	}}
	normalized := []render.HitFeature{
		{Vertices: []render.Point{{X: 0, Y: 0}, {X: 0.2, Y: 0.2}}},
		{Vertices: []render.Point{{X: 0, Y: 0}, {X: 0.01, Y: 0.01}}},
	}
	selected, err := selectVisibleParcelOutlines(context.Background(), layer, normalized,
		[4]float64{0, 0, 100, 100}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(selected.Features) != 1 || selected.Features[0].ID != 1 || len(layer.Features) != 2 {
		t.Fatalf("selected %v; source has %d features", selected.Features, len(layer.Features))
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := selectVisibleParcelOutlines(ctx, layer, normalized, [4]float64{0, 0, 100, 100}, 1); err != context.Canceled {
		t.Fatalf("canceled selection error = %v", err)
	}
}

func TestDenseReadOnlyOverviewKeepsParcelFillAndVisibleBoundaries(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "parcels.shp")
	layer := core.Layer{Name: "parcels", CRS: core.CRS{AuthorityCode: "EPSG:5186"}, Features: []core.Feature{
		{ID: 1, Geometry: core.WKTGeometry{WKT: "POLYGON ((0 0, 2 0, 2 2, 0 2, 0 0))"}},
		{ID: 2, Geometry: core.WKTGeometry{WKT: "POLYGON ((2 0, 4 0, 4 2, 2 2, 2 0))"}},
	}}
	if err := (gdal.Writer{}).Write(ctx, path, layer); err != nil {
		t.Fatal(err)
	}
	runtime, err := loadReadOnlyDataRuntime(ctx, []vectorSourceSpec{{Path: path}}, "")
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.closeAttributeSource()
	binding := runtime.readOnlyBindings["parcels"]
	binding.featureCount = 100_000 // Exercise the dense-layer presentation with a small redistributable fixture.
	runtime.readOnlyBindings["parcels"] = binding
	key := chunkKeyForPoint(runtime.mapExtent, readOnlyWindowChunkSize(-1), "parcels", render.Point{X: 2, Y: 1})
	key.ZoomBucket = -1
	runtime.mu.Lock()
	runtime.windowVisibleKeys[key] = struct{}{}
	runtime.mu.Unlock()
	chunk, err := runtime.builder(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	fillVertices, lineVertices := 0, 0
	for _, vertex := range chunk.Vertices {
		if vertex.Kind == render.VertexFill {
			fillVertices++
		} else if vertex.Kind == render.VertexLine {
			lineVertices++
		}
	}
	if fillVertices == 0 {
		t.Fatal("dense overview lost the parcel fill")
	}
	if lineVertices == 0 {
		t.Fatal("dense overview lost the screen-resolvable parcel boundaries")
	}
}

func TestDenseReadOnlyOverviewRealSourceSamples(t *testing.T) {
	path := os.Getenv("GOGIS_TEST_DENSE_POLYGON_SOURCE")
	if path == "" {
		t.Skip("set GOGIS_TEST_DENSE_POLYGON_SOURCE to a local large polygon SHP")
	}
	timeout := 60 * time.Second
	if os.Getenv("GOGIS_TEST_OVERVIEW_FULL") == "1" {
		timeout = 180 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	runtime, err := loadReadOnlyDataRuntime(ctx, []vectorSourceSpec{{Path: path}}, "")
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.closeAttributeSource()
	name := runtime.service.LayerNames()[0]
	if runtime.readOnlyBindings[name].featureCount < 100_000 {
		t.Fatalf("source has too few features for the dense overview: %d", runtime.readOnlyBindings[name].featureCount)
	}
	zoom := 0.9
	if requested := os.Getenv("GOGIS_TEST_OVERVIEW_ZOOM"); requested != "" {
		parsed, parseErr := strconv.ParseFloat(requested, 64)
		if parseErr != nil || parsed <= 0 || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
			t.Fatalf("invalid GOGIS_TEST_OVERVIEW_ZOOM %q", requested)
		}
		zoom = parsed
	}
	viewport := render.Viewport{
		Center: render.Point{X: 0.5, Y: 0.5}, Zoom: zoom,
		ScreenWidth: 1166, ScreenHeight: 726, CanvasWidth: 509, CanvasHeight: 726,
	}
	planner := runtime.planner
	rawBucket := readOnlyWindowZoomBucket(viewport.Zoom)
	semanticLOD := readOnlyOverviewZoomBucket(rawBucket, runtime.mapExtent, runtime.mapFitExtent)
	planner.ChunkSize = readOnlyWindowChunkSize(rawBucket)
	keys := planner.VisibleKeys(viewport, name)
	t.Logf("dense overview viewport zoom=%g raw-bucket=%d semantic-lod=%d", zoom, rawBucket, semanticLOD)
	started := time.Now()
	testedChunks, filledChunks, vertices, fillVertices := 0, 0, 0, 0
	var fillAreaByQuadrant [4]float64
	var signedFillAreaByQuadrant [4][2]float64
	var fillAreaByCell [16][16]float64
	sampleLimit := 16
	fullViewport := os.Getenv("GOGIS_TEST_OVERVIEW_FULL") == "1"
	var batchStore *render.BatchStore
	if fullViewport {
		sampleLimit = len(keys)
		batchStore = render.NewBatchStore()
		batchStore.BeginGenerationWithVisible(1, keys)
	} else if requested := os.Getenv("GOGIS_TEST_OVERVIEW_SAMPLE_LIMIT"); requested != "" {
		parsed, parseErr := strconv.Atoi(requested)
		if parseErr != nil || parsed < 1 || parsed > 16 {
			t.Fatalf("invalid GOGIS_TEST_OVERVIEW_SAMPLE_LIMIT %q", requested)
		}
		sampleLimit = parsed
	}
	for _, key := range keys {
		if !fullViewport && (key.X < 0 || key.Y < 0 || key.X%8 != 4 || key.Y%8 != 4) {
			continue
		}
		if testedChunks >= sampleLimit {
			break
		}
		testedChunks++
		chunk, err := runtime.builder(ctx, key)
		if err != nil {
			t.Fatalf("build overview chunk %v: %v", key, err)
		}
		vertices += len(chunk.Vertices)
		if fullViewport {
			if applied, err := batchStore.ApplyImmutableChecked(render.ChunkResult{Generation: 1, Key: key, Chunk: chunk}); !applied || err != nil {
				t.Fatalf("apply overview chunk %v to display batch: applied=%t err=%v", key, applied, err)
			}
		}
		chunkHasFill := false
		for _, vertex := range chunk.Vertices {
			if vertex.Kind == render.VertexFill {
				fillVertices++
				chunkHasFill = true
			}
		}
		if fullViewport {
			for index := 0; index+2 < len(chunk.Vertices); index += 3 {
				a, b, c := chunk.Vertices[index], chunk.Vertices[index+1], chunk.Vertices[index+2]
				if a.Kind != render.VertexFill || b.Kind != render.VertexFill || c.Kind != render.VertexFill {
					break
				}
				signedArea := (float64(b.X-a.X)*float64(c.Y-a.Y) - float64(b.Y-a.Y)*float64(c.X-a.X)) / 2
				area := math.Abs(signedArea)
				x, y := (float64(a.X)+float64(b.X)+float64(c.X))/3, (float64(a.Y)+float64(b.Y)+float64(c.Y))/3
				quadrant := 0
				if x >= 0.5 {
					quadrant++
				}
				if y >= 0.5 {
					quadrant += 2
				}
				fillAreaByQuadrant[quadrant] += area
				if signedArea < 0 {
					signedFillAreaByQuadrant[quadrant][0] += area
				} else {
					signedFillAreaByQuadrant[quadrant][1] += area
				}
				cellX, cellY := min(15, max(0, int(x*16))), min(15, max(0, int(y*16)))
				fillAreaByCell[cellY][cellX] += area
			}
		}
		if chunkHasFill {
			filledChunks++
		}
	}
	t.Logf("dense overview samples: planned_chunks=%d tested_chunks=%d filled_chunks=%d vertices=%d fill_vertices=%d elapsed=%s",
		len(keys), testedChunks, filledChunks, vertices, fillVertices, time.Since(started))
	if fullViewport {
		t.Logf("fill area by quadrant (normalized): %v", fillAreaByQuadrant)
		t.Logf("fill area by quadrant, negative/positive winding: %v", signedFillAreaByQuadrant)
		_, flattened := batchStore.Current()
		if len(flattened) != vertices {
			t.Fatalf("final display batch vertices=%d, built=%d", len(flattened), vertices)
		}
		var flattenedAreaByQuadrant [4]float64
		for index := 0; index < len(flattened); {
			if flattened[index].Kind != render.VertexFill {
				index += 2
				continue
			}
			if index+2 >= len(flattened) || flattened[index+1].Kind != render.VertexFill || flattened[index+2].Kind != render.VertexFill {
				t.Fatalf("incomplete fill triangle at flattened vertex %d", index)
			}
			a, b, c := flattened[index], flattened[index+1], flattened[index+2]
			area := math.Abs(float64(b.X-a.X)*float64(c.Y-a.Y)-float64(b.Y-a.Y)*float64(c.X-a.X)) / 2
			x, y := (float64(a.X)+float64(b.X)+float64(c.X))/3, (float64(a.Y)+float64(b.Y)+float64(c.Y))/3
			quadrant := 0
			if x >= 0.5 {
				quadrant++
			}
			if y >= 0.5 {
				quadrant += 2
			}
			flattenedAreaByQuadrant[quadrant] += area
			index += 3
		}
		for quadrant := range fillAreaByQuadrant {
			if math.Abs(flattenedAreaByQuadrant[quadrant]-fillAreaByQuadrant[quadrant]) > 1e-8 {
				t.Fatalf("display batch quadrant %d fill area=%.9f, built=%.9f", quadrant, flattenedAreaByQuadrant[quadrant], fillAreaByQuadrant[quadrant])
			}
		}
		for _, row := range fillAreaByCell {
			var line strings.Builder
			for _, area := range row {
				switch {
				case area > 0.0005:
					line.WriteByte('#')
				case area > 0.00005:
					line.WriteByte('+')
				case area > 0.000005:
					line.WriteByte('.')
				default:
					line.WriteByte(' ')
				}
			}
			t.Log(line.String())
		}
	}
	if filledChunks < min(4, max(1, sampleLimit/2)) || fillVertices == 0 || vertices > render.MaxBatchVertices {
		t.Fatalf("overview coverage is incomplete or exceeds the batch budget: filled_chunks=%d fill_vertices=%d vertices=%d",
			filledChunks, fillVertices, vertices)
	}
}

func TestDenseReadOnlyOverviewParallelWorkerComparison(t *testing.T) {
	if os.Getenv("GOGIS_TEST_OVERVIEW_PARALLEL") != "1" {
		t.Skip("set GOGIS_TEST_OVERVIEW_PARALLEL=1 with GOGIS_TEST_DENSE_POLYGON_SOURCE")
	}
	path := os.Getenv("GOGIS_TEST_DENSE_POLYGON_SOURCE")
	if path == "" {
		t.Skip("set GOGIS_TEST_DENSE_POLYGON_SOURCE to an indexed large polygon SHP")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	runtime, err := loadReadOnlyDataRuntime(ctx, []vectorSourceSpec{{Path: path}}, "")
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.closeAttributeSource()
	name := runtime.service.LayerNames()[0]
	if runtime.readOnlyBindings[name].featureCount < 100_000 {
		t.Fatal("parallel comparison needs a dense polygon source")
	}
	viewport := render.Viewport{
		Center: render.Point{X: 0.5, Y: 0.5}, Zoom: 0.9,
		ScreenWidth: 1166, ScreenHeight: 726, CanvasWidth: 509, CanvasHeight: 726,
	}
	runtime.planner.ChunkSize = readOnlyWindowChunkSize(readOnlyWindowZoomBucket(viewport.Zoom))
	keys := runtime.planner.VisibleKeys(viewport, name)
	keys = filterReadOnlyKeysByLayerBounds(keys, runtime.mapExtent, runtime.planner.ChunkSize, runtime.layerBounds)
	sampleCount := 128
	if os.Getenv("GOGIS_TEST_OVERVIEW_PARALLEL_FULL") == "1" {
		sampleCount = len(keys)
	}
	if len(keys) < sampleCount {
		t.Fatalf("need %d overview windows, got %d", sampleCount, len(keys))
	}
	sampled := make([]render.ChunkKey, 0, sampleCount)
	for index := 0; index < sampleCount; index++ {
		sampled = append(sampled, keys[index*len(keys)/sampleCount])
	}
	previousSemaphore := readOnlyWindowBuildSemaphore
	defer func() { readOnlyWindowBuildSemaphore = previousSemaphore }()
	build := func(workers int) (time.Duration, int, int, error) {
		readOnlyWindowBuildSemaphore = newContextSemaphore(workers)
		jobs := make(chan render.ChunkKey, len(sampled))
		for _, key := range sampled {
			jobs <- key
		}
		close(jobs)
		type result struct {
			vertices int
			err      error
		}
		results := make(chan result, len(sampled))
		var group sync.WaitGroup
		started := time.Now()
		for range workers {
			group.Add(1)
			go func() {
				defer group.Done()
				for key := range jobs {
					chunk, buildErr := runtime.builder(ctx, key)
					results <- result{vertices: len(chunk.Vertices), err: buildErr}
				}
			}()
		}
		group.Wait()
		close(results)
		totalVertices, nonempty := 0, 0
		for item := range results {
			if item.err != nil {
				return 0, 0, 0, item.err
			}
			totalVertices += item.vertices
			if item.vertices > 0 {
				nonempty++
			}
		}
		return time.Since(started), totalVertices, nonempty, nil
	}
	firstElapsed, firstVertices, firstNonempty, err := build(2)
	if err != nil {
		t.Fatal(err)
	}
	secondElapsed, secondVertices, secondNonempty, err := build(4)
	if err != nil {
		t.Fatal(err)
	}
	halfWorkers := max(1, goruntime.NumCPU()/2)
	halfElapsed, halfVertices, halfNonempty, err := build(halfWorkers)
	if err != nil {
		t.Fatal(err)
	}
	if firstVertices != secondVertices || firstNonempty != secondNonempty ||
		firstVertices != halfVertices || firstNonempty != halfNonempty {
		t.Fatalf("2/4/%d workers changed overview result: vertices=%d/%d/%d nonempty=%d/%d/%d",
			halfWorkers, firstVertices, secondVertices, halfVertices, firstNonempty, secondNonempty, halfNonempty)
	}
	t.Logf("indexed overview %d windows (%d nonempty): two workers=%s four workers=%s half cores (%d workers)=%s vertices=%d",
		len(sampled), firstNonempty, firstElapsed.Round(time.Millisecond), secondElapsed.Round(time.Millisecond),
		halfWorkers, halfElapsed.Round(time.Millisecond), firstVertices)
}

func TestDenseReadOnlyWindowQuerySessionComparison(t *testing.T) {
	if goruntime.GOOS != "windows" {
		t.Skip("parallel indexed SHP window readers are enabled on Windows")
	}
	if os.Getenv("GOGIS_TEST_QUERY_SESSIONS") != "1" {
		t.Skip("set GOGIS_TEST_QUERY_SESSIONS=1 with GOGIS_TEST_DENSE_POLYGON_SOURCE")
	}
	path := os.Getenv("GOGIS_TEST_DENSE_POLYGON_SOURCE")
	if path == "" {
		t.Skip("set GOGIS_TEST_DENSE_POLYGON_SOURCE to an indexed large polygon SHP")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	runtime, err := loadReadOnlyDataRuntime(ctx, []vectorSourceSpec{{Path: path}}, "")
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.closeAttributeSource()
	name := runtime.service.LayerNames()[0]
	readerCount := min(4, readOnlyOverviewWorkerLimit())
	if pool := runtime.readOnlyBindings[name].windowPool; pool == nil || cap(pool) != readerCount || len(pool) != readerCount {
		t.Fatalf("indexed dense SHP window pool = %v; want %d ready readers", pool, readerCount)
	}
	viewport := render.Viewport{Center: render.Point{X: 0.5, Y: 0.5}, Zoom: 0.9,
		ScreenWidth: 1166, ScreenHeight: 726, CanvasWidth: 509, CanvasHeight: 726}
	chunkSize := readOnlyWindowChunkSize(readOnlyWindowZoomBucket(viewport.Zoom))
	runtime.planner.ChunkSize = chunkSize
	keys := filterReadOnlyKeysByLayerBounds(runtime.planner.VisibleKeys(viewport, name),
		runtime.mapExtent, chunkSize, runtime.layerBounds)
	const sampleCount = 128
	if len(keys) < sampleCount {
		t.Fatalf("need %d windows, got %d", sampleCount, len(keys))
	}
	sampled := make([]render.ChunkKey, 0, sampleCount)
	for index := 0; index < sampleCount; index++ {
		sampled = append(sampled, keys[index*len(keys)/sampleCount])
	}
	const parallelSessions = 4
	sessions := make([]*gdal.AttributeSession, parallelSessions)
	for index := range sessions {
		sessions[index], err = gdal.OpenAttributeSession(path, "")
		if err != nil {
			t.Fatal(err)
		}
		defer sessions[index].Close()
	}
	query := func(workers int) (time.Duration, int, error) {
		jobs := make(chan render.ChunkKey, len(sampled))
		for _, key := range sampled {
			jobs <- key
		}
		close(jobs)
		type result struct {
			count int
			err   error
		}
		results := make(chan result, len(sampled))
		var group sync.WaitGroup
		started := time.Now()
		for worker := 0; worker < workers; worker++ {
			group.Add(1)
			go func(session *gdal.AttributeSession) {
				defer group.Done()
				for key := range jobs {
					bounds, ok := renderChunkBounds(runtime.mapExtent, chunkSize, key)
					if !ok {
						results <- result{}
						continue
					}
					layer, queryErr := session.OpenWindowWithLimits(ctx, runtime.readOnlyBindings[name].sourceName,
						bounds, false, maxReadOnlyWindowFeatures, maxReadOnlyWindowBytes)
					results <- result{len(layer.Features), queryErr}
				}
			}(sessions[worker])
		}
		group.Wait()
		close(results)
		count := 0
		for item := range results {
			if item.err != nil {
				return 0, 0, item.err
			}
			count += item.count
		}
		return time.Since(started), count, nil
	}
	oneElapsed, oneCount, err := query(1)
	if err != nil {
		t.Fatal(err)
	}
	fourElapsed, fourCount, err := query(parallelSessions)
	if err != nil {
		t.Fatal(err)
	}
	if oneCount != fourCount {
		t.Fatalf("one/four GDAL sessions returned %d/%d features", oneCount, fourCount)
	}
	t.Logf("indexed query %d windows: one session=%s four sessions=%s features=%d",
		len(sampled), oneElapsed.Round(time.Millisecond), fourElapsed.Round(time.Millisecond), oneCount)
}

func TestReadOnlyDetailWorkspaceTransparentFillRealSource(t *testing.T) {
	path := os.Getenv("GOGIS_TEST_DETAIL_WORKSPACE")
	if path == "" {
		t.Skip("set GOGIS_TEST_DETAIL_WORKSPACE to a local multi-layer workspace")
	}
	previousIndexPolicy := activeShapefileIndexPolicy
	configureShapefileIndexPolicy(nil)
	defer func() { activeShapefileIndexPolicy = previousIndexPolicy }()
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()
	runtime, err := loadWorkspaceRuntime(ctx, path, true, "")
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.closeAttributeSource()
	if runtime.workspaceView == nil {
		t.Fatal("workspace has no saved viewport")
	}
	view := runtime.workspaceView
	viewport := render.Viewport{
		Center: render.Point{X: view.CenterX, Y: view.CenterY}, Zoom: view.Zoom,
		ScreenWidth: 1166, ScreenHeight: 726, CanvasWidth: 1166, CanvasHeight: 234,
	}
	if lod := readOnlyOverviewZoomBucket(readOnlyWindowZoomBucket(viewport.Zoom),
		runtime.mapExtent, runtime.mapFitExtent); lod <= 1 {
		t.Fatalf("workspace viewport semantic LOD=%d, want detail", lod)
	}
	planner := runtime.planner
	planner.ChunkSize = readOnlyWindowChunkSize(readOnlyWindowZoomBucket(viewport.Zoom))
	keys := make([]render.ChunkKey, 0)
	for _, name := range runtime.service.LayerNames() {
		if runtime.visibility.IsVisible(name) {
			keys = append(keys, planner.VisibleKeys(viewport, name)...)
		}
	}
	keys = filterReadOnlyKeysByLayerBounds(keys, runtime.mapExtent, planner.ChunkSize, runtime.layerBounds)
	if len(keys) == 0 {
		t.Fatal("saved viewport planned no detail windows")
	}
	if requested := os.Getenv("GOGIS_TEST_DETAIL_WINDOW_LIMIT"); requested != "" {
		limit, parseErr := strconv.Atoi(requested)
		if parseErr != nil || limit < 1 {
			t.Fatalf("invalid GOGIS_TEST_DETAIL_WINDOW_LIMIT %q", requested)
		}
		if limit < len(keys) {
			selected := make([]render.ChunkKey, 0, limit)
			for index := range limit {
				selected = append(selected, keys[index*len(keys)/limit])
			}
			keys = selected
		}
	}
	runtime.mu.Lock()
	runtime.windowVisibleKeys = make(map[render.ChunkKey]struct{}, len(keys))
	for _, key := range keys {
		runtime.windowVisibleKeys[key] = struct{}{}
	}
	runtime.mu.Unlock()
	started := time.Now()
	vertices, zeroAlphaFills, completed := 0, 0, 0
	fillByLayer := make(map[string]int)
	for result := range runtime.scheduler.RequestUnique(ctx, keys, runtime.builder) {
		completed++
		if result.Err != nil {
			t.Fatalf("build detail window %v: %v", result.Key, result.Err)
		}
		vertices += len(result.Chunk.Vertices)
		for _, vertex := range result.Chunk.Vertices {
			if vertex.Kind == render.VertexFill {
				fillByLayer[result.Key.Layer]++
				if vertex.Color&0xff == 0 {
					zeroAlphaFills++
				}
			}
		}
	}
	t.Logf("detail workspace windows=%d completed=%d vertices=%d fills=%v zero_alpha_fills=%d elapsed=%s workers=%d detail_limit=%d",
		len(keys), completed, vertices, fillByLayer, zeroAlphaFills, time.Since(started).Round(time.Millisecond),
		runtime.scheduler.MaxWorkers(), cap(readOnlyDetailBuildSemaphore.permits))
	if err := ctx.Err(); err != nil {
		t.Fatal(err)
	}
	if vertices == 0 || zeroAlphaFills != 0 {
		t.Fatalf("detail workspace vertices=%d zero-alpha fills=%d", vertices, zeroAlphaFills)
	}
}

func TestDenseReadOnlyParcelOutlineFeasibility(t *testing.T) {
	if os.Getenv("GOGIS_TEST_PARCEL_OUTLINES") != "1" {
		t.Skip("set GOGIS_TEST_PARCEL_OUTLINES=1 with GOGIS_TEST_DENSE_POLYGON_SOURCE")
	}
	path := os.Getenv("GOGIS_TEST_DENSE_POLYGON_SOURCE")
	if path == "" {
		t.Skip("set GOGIS_TEST_DENSE_POLYGON_SOURCE to an indexed large polygon SHP")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	runtime, err := loadReadOnlyDataRuntime(ctx, []vectorSourceSpec{{Path: path}}, "")
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.closeAttributeSource()
	name := runtime.service.LayerNames()[0]
	binding := runtime.readOnlyBindings[name]
	viewport := render.Viewport{
		Center: render.Point{X: 0.5, Y: 0.5}, Zoom: 0.9,
		ScreenWidth: 1166, ScreenHeight: 726, CanvasWidth: 509, CanvasHeight: 726,
	}
	chunkSize := readOnlyWindowChunkSize(readOnlyWindowZoomBucket(viewport.Zoom))
	runtime.planner.ChunkSize = chunkSize
	keys := runtime.planner.VisibleKeys(viewport, name)
	keys = filterReadOnlyKeysByLayerBounds(keys, runtime.mapExtent, chunkSize, runtime.layerBounds)
	const sampleCount = 1024
	if len(keys) < sampleCount {
		t.Fatalf("need %d overview windows, got %d", sampleCount, len(keys))
	}
	jobs := make(chan render.ChunkKey, sampleCount)
	for index := 0; index < sampleCount; index++ {
		jobs <- keys[index*len(keys)/sampleCount]
	}
	close(jobs)
	type result struct {
		features        int
		vertices        int
		fillVertices    int
		hybridVertices  int
		outlineFeatures int
		selected        [3]int
		outlineHint     [3]int
		err             error
	}
	results := make(chan result, sampleCount)
	tolerance := readOnlyOverviewSimplificationTolerance(runtime.mapFitExtent, -1)
	var group sync.WaitGroup
	started := time.Now()
	for range 4 {
		group.Add(1)
		go func() {
			defer group.Done()
			operator := geosdriver.NewOperator()
			for key := range jobs {
				bounds, ok := renderChunkBounds(runtime.mapExtent, chunkSize, key)
				if !ok {
					results <- result{}
					continue
				}
				window, _, buildErr := openReadOnlyWindowWithSubdivision(ctx, bounds, func(bounds [4]float64) (core.Layer, error) {
					return binding.session.OpenWindowWithLimits(ctx, binding.sourceName, bounds, false,
						maxReadOnlyWindowFeatures, maxReadOnlyWindowBytes)
				})
				if buildErr != nil || len(window.Features) == 0 {
					results <- result{err: buildErr}
					continue
				}
				features := len(window.Features)
				window.Name, window.CRS = name, core.CRS{AuthorityCode: binding.sourceCRS}
				window.Style = binding.layer.Style
				window, buildErr = operator.SimplifyForDisplay(ctx, window, tolerance)
				if buildErr != nil {
					results <- result{err: buildErr}
					continue
				}
				sources, _, buildErr := render.NewLayerSourcesWithExtentAndChunkSizeForChunkDeduplicatedOutlines(
					[]core.Layer{window}, runtime.mapExtent, chunkSize, key)
				if buildErr != nil {
					results <- result{err: buildErr}
					continue
				}
				var selected, outlineHint [3]int
				for _, hit := range sources[name].Features {
					minX, minY := math.Inf(1), math.Inf(1)
					maxX, maxY := math.Inf(-1), math.Inf(-1)
					for _, point := range hit.Vertices {
						minX, minY = math.Min(minX, point.X), math.Min(minY, point.Y)
						maxX, maxY = math.Max(maxX, point.X), math.Max(maxY, point.Y)
					}
					pixelWidth := (maxX - minX) * viewport.CanvasWidth * viewport.Zoom
					pixelHeight := (maxY - minY) * viewport.CanvasHeight * viewport.Zoom
					for index, minimumArea := range []float64{1, 4, 16} {
						if pixelWidth >= 1 && pixelHeight >= 1 && pixelWidth*pixelHeight >= minimumArea {
							selected[index]++
							outlineHint[index] += 2 * len(hit.Vertices)
						}
					}
				}
				buildErr = attachPolygonFillGeometryForChunk(ctx, []core.Layer{window}, sources, &key)
				if buildErr != nil {
					results <- result{err: buildErr}
					continue
				}
				chunk, buildErr := sources[name].Builder(ctx, key)
				fillVertices := 0
				for _, vertex := range chunk.Vertices {
					if vertex.Kind == render.VertexFill {
						fillVertices++
					}
				}
				outlineLayer, buildErr := selectVisibleParcelOutlines(ctx, window, sources[name].Features,
					runtime.mapExtent, tolerance)
				if buildErr != nil {
					results <- result{err: buildErr}
					continue
				}
				outlineVertices := 0
				if len(outlineLayer.Features) > 0 {
					outlineSources, _, outlineErr := render.NewLayerSourcesWithExtentAndChunkSizeForChunkDeduplicatedOutlines(
						[]core.Layer{outlineLayer}, runtime.mapExtent, chunkSize, key)
					if outlineErr == nil {
						outline, err := outlineSources[name].Builder(ctx, key)
						outlineErr = err
						outlineVertices = len(outline.Vertices)
					}
					if outlineErr != nil {
						results <- result{err: outlineErr}
						continue
					}
				}
				results <- result{features: features, vertices: len(chunk.Vertices), fillVertices: fillVertices,
					hybridVertices: fillVertices + outlineVertices, outlineFeatures: len(outlineLayer.Features),
					selected: selected, outlineHint: outlineHint, err: buildErr}
			}
		}()
	}
	group.Wait()
	close(results)
	features, vertices, fillVertices, hybridVertices, outlineFeatures := 0, 0, 0, 0, 0
	var selected, outlineHint [3]int
	for item := range results {
		if item.err != nil {
			t.Fatal(item.err)
		}
		features += item.features
		vertices += item.vertices
		fillVertices += item.fillVertices
		hybridVertices += item.hybridVertices
		outlineFeatures += item.outlineFeatures
		for index := range selected {
			selected[index] += item.selected[index]
			outlineHint[index] += item.outlineHint[index]
		}
	}
	t.Logf("simplified parcels %d windows: query_features=%d vertices=%d fill_vertices=%d elapsed=%s tolerance=%g",
		sampleCount, features, vertices, fillVertices, time.Since(started).Round(time.Millisecond), tolerance)
	t.Logf("screen-selected parcel outlines: minimum pixel areas 1/4/16 selected=%v outline_vertex_hints=%v",
		selected, outlineHint)
	t.Logf("hybrid parcel overview: filled_features=%d outlined_features=%d vertices=%d fill_vertices=%d",
		features, outlineFeatures, hybridVertices, fillVertices)
	if hybridVertices <= fillVertices || hybridVertices >= vertices || hybridVertices > render.MaxBatchVertices {
		t.Fatalf("hybrid batch did not retain bounded parcel boundaries: full=%d hybrid=%d fill=%d",
			vertices, hybridVertices, fillVertices)
	}
}

func TestReadOnlyOverviewSamplingGetsDenserOnZoomIn(t *testing.T) {
	for _, test := range []struct{ bucket, stride int }{{-7, 256}, {-2, 8}, {-1, 1}, {0, 1}, {1, 1}, {2, 1}} {
		if got := readOnlyOverviewStride(test.bucket); got != test.stride {
			t.Errorf("overview stride at bucket %d = %d, want %d", test.bucket, got, test.stride)
		}
	}
	layer := core.Layer{Features: make([]core.Feature, 16)}
	for index := range layer.Features {
		layer.Features[index] = core.Feature{ID: uint64(index + 1)}
	}
	sampled := sampleReadOnlyOverviewFeatures(layer, 4)
	if len(sampled.Features) != 4 {
		t.Fatalf("sampled feature count = %d, want 4", len(sampled.Features))
	}
	for index, feature := range sampled.Features {
		if want := uint64((index + 1) * 4); feature.ID != want {
			t.Fatalf("sampled feature %d ID = %d, want %d", index, feature.ID, want)
		}
	}
	if len(layer.Features) != 16 {
		t.Fatalf("source layer was mutated to %d features", len(layer.Features))
	}
	sparseIDs := core.Layer{Features: make([]core.Feature, 12)}
	for index := range sparseIDs.Features {
		sparseIDs.Features[index] = core.Feature{ID: uint64(101 + index*17)}
	}
	sampled = sampleReadOnlyOverviewFeatures(sparseIDs, 3)
	for index, feature := range sampled.Features {
		if want := uint64(101 + (index*3+2)*17); feature.ID != want {
			t.Fatalf("sparse-FID sample %d = %d, want ordinally spaced FID %d", index, feature.ID, want)
		}
	}
}

func TestReadOnlyOverviewSimplificationToleranceTracksZoom(t *testing.T) {
	extent := [4]float64{0, 0, 20_000, 10_000}
	for _, test := range []struct {
		bucket int
		want   float64
	}{{-1, 6}, {0, 3}, {1, 1.5}, {2, 0.75}} {
		if got := readOnlyOverviewSimplificationTolerance(extent, test.bucket); math.Abs(got-test.want) > 1e-9 {
			t.Errorf("simplification tolerance at bucket %d = %g, want %g", test.bucket, got, test.want)
		}
	}
}

func TestReadOnlyOverviewZoomIsRelativeToPreferredExtent(t *testing.T) {
	data := [4]float64{211_407.24, 43_257.02, 2_287_874.9, 459_484.82}
	fit := [4]float64{211_407.24, 423_223.66, 236_805.50, 459_484.82}
	if got := readOnlyOverviewZoomBucket(5, data, fit); got != 1 {
		t.Fatalf("fit-scale LOD bucket at raw bucket 5 = %d, want overview bucket 1", got)
	}
	if got := readOnlyOverviewZoomBucket(6, data, fit); got != 2 {
		t.Fatalf("fit-scale LOD bucket at raw bucket 6 = %d, want detail bucket 2", got)
	}
}

func TestReadOnlyOverviewStrideBoundsDenseLayerAndRestoresDetailOnZoom(t *testing.T) {
	for _, test := range []struct {
		bucket, features, stride int
	}{{-1, 208_015, 1}, {0, 208_015, 1}, {2, 208_015, 1}, {3, 208_015, 1}, {4, 208_015, 1}, {5, 208_015, 1}, {6, 208_015, 1}, {3, 1_000_000, 5}} {
		if got := readOnlyOverviewStrideForFeatureCount(test.bucket, test.features); got != test.stride {
			t.Errorf("overview stride for bucket=%d features=%d = %d, want %d",
				test.bucket, test.features, got, test.stride)
		}
	}
	const sejongFeatures = 208_015
	stride := readOnlyOverviewStrideForFeatureCount(-1, sejongFeatures)
	retained := sejongFeatures / stride
	if retained < maxReadOnlyOverviewFeatures/2 || retained > maxReadOnlyOverviewFeatures {
		t.Fatalf("full-extent overview sample retains %d features at stride %d; want a dense sample within the %d-feature budget",
			retained, stride, maxReadOnlyOverviewFeatures)
	}
	fit := preferredMapFitExtent(
		[4]float64{211_407.24, 43_257.02, 2_287_874.9, 459_484.82},
		map[string][4]float64{
			"parcels":       [4]float64{211_407.24, 423_223.66, 236_805.50, 459_484.82},
			"survey-points": [4]float64{212_159.77, 43_257.02, 2_287_874.9, 459_421.8},
		},
		map[string]string{"parcels": "Polygon", "survey-points": "Point"},
	)
	if want := [4]float64{211_407.24, 423_223.66, 236_805.50, 459_484.82}; fit != want {
		t.Fatalf("preferred fit extent = %v, want parcel extent %v", fit, want)
	}
}

func TestDesktopLoadsDXFAsVectorLayer(t *testing.T) {
	path, err := filepath.Abs(filepath.Join("..", "..", "testdata", "ares", "sample-utf8.dxf"))
	if err != nil {
		t.Fatal(err)
	}
	runtime, readOnly, count, err := loadDataRuntimeFilesWithLargePolicy(
		context.Background(), []string{path}, nil, "", false)
	if err != nil {
		t.Fatalf("load DXF into desktop runtime: %v", err)
	}
	if readOnly || count != 4 {
		t.Fatalf("DXF load policy = readOnly:%t featureCount:%d; want materialized 4-feature import", readOnly, count)
	}
	names := runtime.service.LayerNames()
	if len(names) != 1 || names[0] != "entities" {
		t.Fatalf("DXF layers = %v, want [entities]", names)
	}
}

func TestReadOnlyZoomedOutBuilderSimplifiesPolygonFill(t *testing.T) {
	coordinates := make([]string, 0, 84)
	for index := 0; index <= 80; index++ {
		y := 0.0
		if index%2 == 1 {
			y = 0.02
		}
		coordinates = append(coordinates, fmt.Sprintf("%.2f %.2f", float64(index)/20, y))
	}
	coordinates = append(coordinates, "4 4", "0 4", "0 0")
	path := filepath.Join(t.TempDir(), "detailed-parcel.shp")
	layer := core.Layer{
		Name: "parcels", CRS: core.CRS{AuthorityCode: "EPSG:5186"},
		Features: []core.Feature{{ID: 1, Geometry: core.WKTGeometry{WKT: "POLYGON ((" + strings.Join(coordinates, ", ") + "))"}}},
	}
	if err := (gdal.Writer{}).Write(context.Background(), path, layer); err != nil {
		t.Fatal(err)
	}
	runtime, err := loadReadOnlyDataRuntime(context.Background(), []vectorSourceSpec{{Path: path}}, "")
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.closeAttributeSource()
	name := runtime.service.LayerNames()[0]
	build := func(bucket int) render.Chunk {
		t.Helper()
		key := chunkKeyForPoint(runtime.mapExtent, readOnlyWindowChunkSize(bucket), name, render.Point{X: 0.5, Y: 0.005})
		key.ZoomBucket = bucket
		chunk, buildErr := runtime.builder(context.Background(), key)
		if buildErr != nil {
			t.Fatalf("build zoom bucket %d: %v", bucket, buildErr)
		}
		return chunk
	}
	full := build(2)
	overview := build(-7)
	if len(full.Vertices) == 0 || len(overview.Vertices) == 0 {
		t.Fatalf("missing polygon render vertices: full=%d overview=%d", len(full.Vertices), len(overview.Vertices))
	}
	countFillVertices := func(vertices []render.Vertex) int {
		count := 0
		for _, vertex := range vertices {
			if vertex.Kind == render.VertexFill {
				count++
			}
		}
		return count
	}
	fullFillVertices, overviewFillVertices := countFillVertices(full.Vertices), countFillVertices(overview.Vertices)
	if fullFillVertices == 0 || overviewFillVertices == 0 {
		t.Fatalf("missing polygon fill vertices: full=%d overview=%d", fullFillVertices, overviewFillVertices)
	}
	if overviewFillVertices >= fullFillVertices {
		t.Fatalf("overview fill was not simplified: full=%d overview=%d", fullFillVertices, overviewFillVertices)
	}
	if len(overview.Vertices) >= len(full.Vertices) {
		t.Fatalf("overview did not simplify fill: full=%d overview=%d", len(full.Vertices), len(overview.Vertices))
	}
}

func TestReadOnlyWindowRequiresCRSAndExtent(t *testing.T) {
	for _, test := range []struct {
		name    string
		crs     string
		bounds  bool
		wantErr bool
	}{{"known", "EPSG:5186", true, false}, {"unknown-crs", "", true, true}, {"unknown-bounds", "EPSG:5186", false, true}} {
		err := validateReadOnlyWindowMetadata("parcels", test.crs, test.bounds)
		if (err != nil) != test.wantErr {
			t.Errorf("validate metadata (%s, %t) error = %v, wantErr=%t", test.crs, test.bounds, err, test.wantErr)
		}
		if err != nil && !strings.Contains(err.Error(), "refusing a full-geometry fallback") {
			t.Errorf("unsafe fallback error is not explicit: %v", err)
		}
	}
}

func TestReadOnlyWindowPayloadEstimateSaturatesAtBudget(t *testing.T) {
	layer := core.Layer{Features: []core.Feature{{
		Geometry: core.WKBGeometry{WKB: make([]byte, maxReadOnlyWindowBytes+1)},
	}}}
	if got := estimateReadOnlyWindowPayloadBytes(layer); got <= maxReadOnlyWindowBytes {
		t.Fatalf("oversized geometry estimate = %d, want > %d", got, maxReadOnlyWindowBytes)
	}
	if got := estimateReadOnlyWindowPayloadBytes(core.Layer{Features: []core.Feature{{
		Geometry: core.WKBGeometry{WKB: []byte{1, 2, 3}}, Properties: map[string]any{"name": "short"},
	}}}); got != int64(len([]byte{1, 2, 3})+len("name")+16+len("short")+24) {
		t.Fatalf("small geometry/property estimate = %d", got)
	}
	largeNested := map[string]any{"items": []any{map[string]any{"value": strings.Repeat("x", maxReadOnlyWindowBytes+1)}}}
	if got := estimateReadOnlyWindowPayloadBytes(core.Layer{Features: []core.Feature{{
		Properties: map[string]any{"nested": largeNested},
	}}}); got <= maxReadOnlyWindowBytes {
		t.Fatalf("oversized nested property estimate = %d, want > %d", got, maxReadOnlyWindowBytes)
	}
	cyclic := map[string]any{}
	cyclic["self"] = cyclic
	if got := estimateReadOnlyWindowPayloadBytes(core.Layer{Features: []core.Feature{{
		Properties: map[string]any{"cycle": cyclic},
	}}}); got <= maxReadOnlyWindowBytes {
		t.Fatalf("cyclic property estimate = %d, want saturated estimate", got)
	}
}

func TestReadOnlyVisiblePayloadBudgetIsSeparateFromWindowBudget(t *testing.T) {
	if maxReadOnlyWindowBytes != 32<<20 || maxReadOnlyVisibleBytes != 256<<20 {
		t.Fatalf("read-only byte budgets: window=%d visible=%d", maxReadOnlyWindowBytes, maxReadOnlyVisibleBytes)
	}
	if maxReadOnlyWindowFeatures != 20_000 || maxReadOnlyVisibleFeatures != 300_000 {
		t.Fatalf("read-only feature budgets: window=%d visible=%d", maxReadOnlyWindowFeatures, maxReadOnlyVisibleFeatures)
	}
	for _, test := range []struct {
		current, additional int64
		want                bool
	}{
		{32 << 20, 16 << 20, true},
		{64 << 20, 32 << 20, true},
		{96 << 20, 32 << 20, true},
		{96 << 20, 32<<20 + 1, true},
		{224 << 20, 32 << 20, true},
		{224 << 20, 32<<20 + 1, false},
		{256 << 20, 1, false},
		{-1, 1, false},
	} {
		if got := readOnlyVisiblePayloadFits(test.current, test.additional); got != test.want {
			t.Errorf("visible payload current=%d additional=%d: got %t, want %t", test.current, test.additional, got, test.want)
		}
	}
}

func TestReadOnlyVisibleFeatureBudgetAcceptsUpToThreeHundredThousand(t *testing.T) {
	for _, test := range []struct {
		current, additional int
		want                bool
	}{
		{50_000, 1, true},
		{80_000, 20_000, true},
		{99_999, 1, true},
		{100_000, 1, true},
		{200_000, 20_000, true},
		{280_000, 20_000, true},
		{300_000, 1, false},
		{280_000, 20_001, false},
		{-1, 1, false},
	} {
		if got := readOnlyVisibleFeaturesFit(test.current, test.additional); got != test.want {
			t.Errorf("visible features current=%d additional=%d: got %t, want %t", test.current, test.additional, got, test.want)
		}
	}
}

func TestReadOnlyWindowSubdivisionRecursivelyMergesAndDeduplicates(t *testing.T) {
	shared := core.Feature{
		Geometry:   core.WKBGeometry{WKB: []byte{1, 2, 3}},
		Properties: map[string]any{"name": "crosses-subcell-boundary"},
	}
	queries := 0
	window, subdivided, err := openReadOnlyWindowWithSubdivision(context.Background(), [4]float64{0, 0, 1, 1},
		func(bounds [4]float64) (core.Layer, error) {
			queries++
			if bounds[2]-bounds[0] > 0.5 {
				return core.Layer{}, fmt.Errorf("feature limit of %d features exceeded", maxReadOnlyWindowFeatures)
			}
			unique := core.Feature{
				Geometry:   core.WKBGeometry{WKB: []byte{byte(bounds[0]*2 + bounds[1]*4 + 10)}},
				Properties: map[string]any{"cell": bounds},
			}
			return core.Layer{Name: "roads", Fields: []core.Field{{Name: "name"}}, Features: []core.Feature{shared, unique}}, nil
		})
	if err != nil {
		t.Fatal(err)
	}
	if !subdivided {
		t.Fatal("overfull root window was not subdivided")
	}
	if queries != 5 {
		t.Fatalf("subdivision queried %d windows, want one failed parent plus four children", queries)
	}
	if window.Name != "roads" || len(window.Fields) != 1 || len(window.Features) != 5 {
		t.Fatalf("merged subdivided window = layer %q, fields %d, features %d; want roads, 1, 5", window.Name, len(window.Fields), len(window.Features))
	}
}

func TestReadOnlyWindowSubdivisionBoundsQueryCount(t *testing.T) {
	queries := 0
	_, _, err := openReadOnlyWindowWithSubdivision(context.Background(), [4]float64{0, 0, 1, 1},
		func(bounds [4]float64) (core.Layer, error) {
			queries++
			if (bounds[2]-bounds[0])*(bounds[3]-bounds[1]) > 1.0/65536 {
				return core.Layer{}, fmt.Errorf("feature limit of %d features exceeded", maxReadOnlyWindowFeatures)
			}
			return core.Layer{}, nil
		})
	if err == nil || !strings.Contains(err.Error(), "subdivision exceeded") {
		t.Fatalf("over-dense window error = %v, want bounded subdivision failure", err)
	}
	if queries != maxReadOnlyWindowSubdivisionCalls {
		t.Fatalf("over-dense window attempted %d queries, want bounded count %d", queries, maxReadOnlyWindowSubdivisionCalls)
	}
}

func TestMaterializedRuntimeUsageEnforcesAggregateFeatureAndByteBudgets(t *testing.T) {
	layer := core.Layer{Name: "roads", Features: []core.Feature{{
		ID: 1, Geometry: core.WKBGeometry{WKB: []byte{1, 2, 3, 4}},
		Properties: map[string]any{"name": "road", "tags": []any{"primary", 4}},
	}}}
	features, bytes, err := accumulateMaterializedRuntimeLayerUsage(0, 0, layer, 2, 1<<20)
	if err != nil || features != 1 || bytes <= 0 {
		t.Fatalf("first layer usage features=%d bytes=%d err=%v", features, bytes, err)
	}
	if _, _, err := accumulateMaterializedRuntimeLayerUsage(features, bytes, layer, 1, 1<<20); err == nil || !strings.Contains(err.Error(), "feature safety limit") {
		t.Fatalf("aggregate feature cap error = %v", err)
	}
	if _, _, err := accumulateMaterializedRuntimeLayerUsage(0, 0, layer, 10, bytes-1); err == nil || !strings.Contains(err.Error(), "payload safety limit") {
		t.Fatalf("aggregate payload cap error = %v", err)
	}
}

func TestPolygonFillVertexAppenderHonorsChunkLimit(t *testing.T) {
	points := []render.Point{{X: 0, Y: 0}, {X: 1, Y: 0}, {X: 0, Y: 1}}
	vertices, ok := appendPolygonFillPoints(nil, points[:2], 0xff00ffff, 3)
	if !ok || len(vertices) != 2 {
		t.Fatalf("bounded polygon fill append = %d vertices, ok=%t", len(vertices), ok)
	}
	unchanged, ok := appendPolygonFillPoints(vertices, points[2:], 0xff00ffff, 2)
	if ok || len(unchanged) != 2 {
		t.Fatalf("over-budget polygon fill append = %d vertices, ok=%t", len(unchanged), ok)
	}
}

func TestLargeDatasetReadOnlyThresholdAndOverride(t *testing.T) {
	if !shouldOpenLargeDatasetReadOnly(largeDatasetReadOnlyThreshold, false, false) {
		t.Fatal("dataset at threshold was not selected for read-only mode")
	}
	if !shouldOpenLargeDatasetReadOnly(0, true, false) {
		t.Fatal("dataset with unknown feature count was not selected for read-only mode")
	}
	if shouldOpenLargeDatasetReadOnly(largeDatasetReadOnlyThreshold, false, true) {
		t.Fatal("--editable-large override did not preserve editable mode")
	}
	if !desktopAllowLargeEditable([]string{"gogis-desktop-native", "--editable-large"}) {
		t.Fatal("--editable-large was not recognized")
	}
	if strings.Contains(largeDatasetReadOnlyStatus(largeDatasetReadOnlyThreshold), "--editable-large") {
		t.Fatal("read-only status must not imply that --editable-large bypasses snapshot safety limits")
	}
}

func TestDatasetPreflightDetectsLargeFeatureCount(t *testing.T) {
	path := largeGeoJSONFixture(t, largeDatasetReadOnlyThreshold+3)
	count, large, err := inspectSourceFeatureCount(context.Background(), []vectorSourceSpec{{Path: path}})
	if err != nil {
		t.Fatal(err)
	}
	if !large || count != largeDatasetReadOnlyThreshold {
		t.Fatalf("preflight count/large = %d/%t, want threshold/%t", count, large, true)
	}

	runtime, autoReadOnly, count, err := loadDataRuntimeFilesWithLargePolicy(
		context.Background(), []string{path}, nil, "", false)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.closeAttributeSource()
	if !autoReadOnly || !runtime.readOnly || count != largeDatasetReadOnlyThreshold {
		t.Fatalf("large load policy = read-only:%t runtime:%t count:%d", autoReadOnly, runtime.readOnly, count)
	}
	base := core.Layer{Name: "existing", CRS: core.CRS{AuthorityCode: "EPSG:4326"}, Visible: true,
		Fields:   []core.Field{{Name: "name", Type: "String"}},
		Features: []core.Feature{{ID: 1, Geometry: core.WKTGeometry{WKT: "POINT (1 1)"}, Properties: map[string]any{"name": "keep"}}}}
	baseExtent := [4]float64{0, 0, float64(largeDatasetReadOnlyThreshold + 2), float64(largeDatasetReadOnlyThreshold + 2)}
	appended, appendedReadOnly, appendedCount, appendErr := loadDataRuntimeFilesWithLargePolicyAndBaseExtent(
		context.Background(), []string{path}, []core.Layer{base}, "", false, &baseExtent)
	if appendErr != nil {
		t.Fatal(appendErr)
	}
	defer appended.closeAttributeSource()
	if !appendedReadOnly || !appended.readOnly || !appended.viewportReadOnly || appendedCount != largeDatasetReadOnlyThreshold {
		t.Fatalf("large source append result = runtime:%v read-only:%t viewport:%t count:%d", appended != nil, appendedReadOnly, appended.viewportReadOnly, appendedCount)
	}
	if names := appended.service.LayerNames(); len(names) != 2 {
		t.Fatalf("mixed read-only layer names = %v; want existing and large", names)
	}
	page, total, err := appended.attributePageReader(context.Background(), "existing", 0, 1)
	if err != nil || total != 1 || len(page.Features) != 1 || page.Features[0].Properties["name"] != "keep" {
		t.Fatalf("base-layer attributes = total:%d page:%v err:%v", total, page.Features, err)
	}
	appended.mu.Lock()
	appended.rebuildWindowFeaturesLocked()
	baseHitCount := len(appended.features)
	appended.mu.Unlock()
	if baseHitCount != 1 {
		t.Fatalf("base-layer hit features after window rebuild = %d; want 1", baseHitCount)
	}
	if appended.nextWindowFeatureID < base.Features[0].ID {
		t.Fatalf("window feature ID seed = %d; must not collide with base feature ID %d", appended.nextWindowFeatureID, base.Features[0].ID)
	}
	if names := runtime.service.LayerNames(); len(names) != 1 {
		t.Fatalf("loaded layer names = %v", names)
	} else {
		page, total, pageErr := runtime.attributePageReader(context.Background(), names[0], 0, 1)
		if pageErr != nil {
			t.Fatal(pageErr)
		}
		if total != largeDatasetReadOnlyThreshold+3 || len(page.Features) != 1 || page.Features[0].Properties["name"] != "point-0" {
			t.Fatalf("lazy attribute page total/features = %d/%v", total, page.Features)
		}
	}

	workspacePath := filepath.Join(t.TempDir(), "large.gogis")
	doc := workspace.FromProject(core.Project{
		Name: "large", CRS: core.CRS{AuthorityCode: "EPSG:4326"},
		Layers: []core.Layer{{Name: "large", SourcePath: path, SourceLayerName: "large",
			SourceCRS: "EPSG:4326", CRS: core.CRS{AuthorityCode: "EPSG:4326"}, Visible: true,
			Style: core.DefaultLayerStyle(), Labels: core.DefaultLabelSettings()}},
	})
	if err := workspace.Save(workspacePath, doc); err != nil {
		t.Fatal(err)
	}
	workspaceRuntime, workspaceAutoReadOnly, workspaceCount, err := loadWorkspaceRuntimeWithLargePolicy(
		context.Background(), workspacePath, false, "", false)
	if err != nil {
		t.Fatal(err)
	}
	defer workspaceRuntime.closeAttributeSource()
	if !workspaceAutoReadOnly || !workspaceRuntime.readOnly || workspaceCount != largeDatasetReadOnlyThreshold {
		t.Fatalf("large workspace policy = read-only:%t runtime:%t count:%d", workspaceAutoReadOnly, workspaceRuntime.readOnly, workspaceCount)
	}
}

func TestCancelCurrentOperationCancelsLoadAndInvalidatesResult(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	runtime := &demoRuntime{loadCancel: cancel, loadGeneration: 4, previewLoading: true}
	runtime.cancelCurrentRender()
	if ctx.Err() == nil {
		t.Fatal("load context was not cancelled")
	}
	if runtime.loadCancel != nil || runtime.loadGeneration != 5 || runtime.previewLoading {
		t.Fatalf("cancelled load state = cancel:%v generation:%d preview:%v", runtime.loadCancel != nil, runtime.loadGeneration, runtime.previewLoading)
	}
}

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
	allLayers, err := loadDataRuntimeModeContext(context.Background(), path, "", "", "", "", false)
	if err != nil {
		t.Fatalf("load every dataset layer: %v", err)
	}
	if names := allLayers.service.LayerNames(); len(names) != 2 || names[0] != "first" || names[1] != "selected" {
		t.Fatalf("all layer names = %v", names)
	}
	if len(allLayers.features) != 2 {
		t.Fatalf("all layer feature count = %d, want 2", len(allLayers.features))
	}
	if allLayers.mapExtent != [4]float64{126.995, 36.995, 127.005, 37.005} {
		t.Fatalf("common map extent = %v", allLayers.mapExtent)
	}
	readOnlyAll, err := loadDataRuntimeModeContext(context.Background(), path, "", "", "", "", true)
	if err != nil {
		t.Fatalf("load read-only multi-layer GeoPackage: %v", err)
	}
	if err := readOnlyAll.startRemoveLayer("first"); err != nil {
		t.Fatalf("remove one GeoPackage layer: %v", err)
	}
	removedDeadline := time.Now().Add(4 * time.Second)
	removed := false
	for time.Now().Before(removedDeadline) {
		readOnlyAll.mu.Lock()
		names := readOnlyAll.service.LayerNames()
		readOnlyAll.mu.Unlock()
		if len(names) == 1 && names[0] == "selected" {
			removed = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !removed {
		t.Fatalf("removing one GeoPackage layer left %v", readOnlyAll.service.LayerNames())
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("removing layer deleted GeoPackage: %v", err)
	}
	for _, readOnly := range []bool{true, false} {
		runtime, err := loadDataRuntimeModeContext(context.Background(), path, "selected", "", "", "", readOnly)
		if err != nil {
			t.Fatalf("readOnly=%t: %v", readOnly, err)
		}
		if names := runtime.service.LayerNames(); len(names) != 1 || names[0] != "selected" {
			t.Fatalf("readOnly=%t: layer names = %v", readOnly, names)
		}
		if readOnly && runtime.viewportReadOnly {
			if len(runtime.features) != 0 {
				t.Fatalf("windowed read-only eagerly loaded %d features", len(runtime.features))
			}
			key := render.ChunkKey{Layer: "selected", X: 2, Y: 2}
			runtime.mu.Lock()
			runtime.windowVisibleKeys[key] = struct{}{}
			runtime.mu.Unlock()
			if _, err := runtime.builder(context.Background(), key); err != nil {
				t.Fatalf("read-only spatial window: %v", err)
			}
		}
		if !readOnly || !runtime.viewportReadOnly {
			if len(runtime.features) != 1 {
				t.Fatalf("readOnly=%t: features = %d", readOnly, len(runtime.features))
			}
		}
		if runtime.closeAttributeSource != nil {
			runtime.closeAttributeSource()
		}
	}
}

func TestDesktopSourceEncodingAppliesToLazyAttributeReads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "roads.shp")
	layer := core.Layer{
		Name: "roads", CRS: core.CRS{AuthorityCode: "EPSG:4326"},
		Fields: []core.Field{{Name: "name", Type: core.FieldTypeText}},
		Features: []core.Feature{{
			ID: 1, Geometry: core.WKTGeometry{WKT: "POINT (127 37)"},
			Properties: map[string]any{"name": "한글 도로"},
		}},
	}
	if err := (gdal.Writer{}).Write(context.Background(), path, layer); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(strings.TrimSuffix(path, filepath.Ext(path))+".cpg", []byte("CP949\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	runtime, err := loadDataRuntimeModeContextWithEncoding(context.Background(), path, "roads", "", "", "", "UTF-8", false, nil)
	if err != nil {
		t.Fatal(err)
	}
	page, total, err := runtime.attributePageReader(context.Background(), "roads", 0, 10)
	if err != nil || total != 1 || page.Features[0].Properties["name"] != "한글 도로" {
		t.Fatalf("encoded lazy attribute page=%#v total=%d err=%v", page, total, err)
	}
	feature, err := runtime.attributeFeatureReader(context.Background(), "roads", 1)
	if err != nil || feature.Properties["name"] != "한글 도로" {
		t.Fatalf("encoded lazy feature=%#v err=%v", feature, err)
	}
}

func TestDesktopAddsMultipleVectorFilesAsLayers(t *testing.T) {
	root := t.TempDir()
	firstDirectory := filepath.Join(root, "first")
	secondDirectory := filepath.Join(root, "second")
	for _, directory := range []string{firstDirectory, secondDirectory} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	firstPath := filepath.Join(firstDirectory, "roads.shp")
	secondPath := filepath.Join(secondDirectory, "roads.shp")
	firstLayer := core.Layer{
		Name:   "roads",
		CRS:    core.CRS{AuthorityCode: "EPSG:4326"},
		Fields: []core.Field{{Name: "name", Type: core.FieldTypeText}},
		Features: []core.Feature{{
			ID: 1, Geometry: core.WKTGeometry{WKT: "POINT (127 37)"},
			Properties: map[string]any{"name": "first"},
		}},
	}
	secondLayer := firstLayer.Clone()
	secondLayer.Features[0].Geometry = core.WKTGeometry{WKT: "POINT (128 38)"}
	secondLayer.Features[0].Properties["name"] = "second"
	for _, item := range []struct {
		path  string
		layer core.Layer
	}{{firstPath, firstLayer}, {secondPath, secondLayer}} {
		if err := (gdal.Writer{}).Write(context.Background(), item.path, item.layer); err != nil {
			t.Fatal(err)
		}
	}
	first, err := loadDataRuntimeFiles(context.Background(), []string{firstPath}, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	added, err := loadDataRuntimeFiles(context.Background(), []string{secondPath}, first.service.Project().Layers, "")
	if err != nil {
		t.Fatal(err)
	}
	if names := added.service.LayerNames(); len(names) != 2 || names[0] != "roads" || names[1] != "roads_roads" {
		t.Fatalf("combined layer names = %v", names)
	}
	if len(added.features) != 2 {
		t.Fatalf("combined feature count = %d, want 2", len(added.features))
	}
	roads, ok := added.service.Layer("roads")
	if !ok || roads.SourcePath != firstPath || roads.SourceLayerName != "roads" || roads.Style != core.DefaultLayerStyle() {
		t.Fatalf("first layer source/presentation = %+v, found=%t", roads, ok)
	}
	addedRoads, ok := added.service.Layer("roads_roads")
	if !ok || addedRoads.SourcePath != secondPath || addedRoads.SourceLayerName != "roads" {
		t.Fatalf("added layer source identity = %+v, found=%t", addedRoads, ok)
	}
	if added.mapExtent != [4]float64{127, 37, 128, 38} {
		t.Fatalf("combined extent = %v", added.mapExtent)
	}
	page, total, err := added.attributePageReader(context.Background(), "roads_roads", 0, 10)
	if err != nil || total != 1 || page.Features[0].Properties["name"] != "second" {
		t.Fatalf("added layer attributes page=%#v total=%d err=%v", page, total, err)
	}
	readOnlyFirst, err := loadReadOnlyDataRuntime(context.Background(), []vectorSourceSpec{{Path: firstPath}}, "")
	if err != nil {
		t.Fatal(err)
	}
	defer readOnlyFirst.closeAttributeSource()
	readOnlyAdded, err := loadReadOnlyDataRuntime(context.Background(), []vectorSourceSpec{
		{Path: firstPath}, {Path: secondPath},
	}, readOnlyFirst.mapCRS)
	if err != nil {
		t.Fatal(err)
	}
	defer readOnlyAdded.closeAttributeSource()
	if !readOnlyAdded.readOnly || !readOnlyAdded.viewportReadOnly || len(readOnlyAdded.features) != 0 {
		t.Fatalf("read-only combined runtime mode=%t viewport-backed=%t initial features=%d", readOnlyAdded.readOnly, readOnlyAdded.viewportReadOnly, len(readOnlyAdded.features))
	}
	windowKeys := []render.ChunkKey{
		chunkKeyForPoint(readOnlyAdded.mapExtent, readOnlyWindowChunkSize(2), "roads", render.Point{X: 127, Y: 37}),
		chunkKeyForPoint(readOnlyAdded.mapExtent, readOnlyWindowChunkSize(2), "roads_roads", render.Point{X: 128, Y: 38}),
	}
	for index := range windowKeys {
		windowKeys[index].ZoomBucket = 2
	}
	readOnlyAdded.mu.Lock()
	for _, key := range windowKeys {
		readOnlyAdded.windowVisibleKeys[key] = struct{}{}
	}
	readOnlyAdded.mu.Unlock()
	for _, key := range windowKeys {
		if _, err := readOnlyAdded.builder(context.Background(), key); err != nil {
			t.Fatalf("build windowed chunk %v: %v", key, err)
		}
	}
	if len(readOnlyAdded.features) != 2 {
		t.Fatalf("windowed hit feature count = %d, want 2", len(readOnlyAdded.features))
	}
	page, total, err = readOnlyAdded.attributePageReader(context.Background(), "roads_roads", 0, 10)
	if err != nil || total != 1 || page.Features[0].Properties["name"] != "second" {
		t.Fatalf("read-only added layer page=%#v total=%d err=%v", page, total, err)
	}
	var selected render.HitFeature
	for _, hit := range readOnlyAdded.features {
		if hit.Layer == "roads_roads" {
			selected = hit
			break
		}
	}
	feature, err := readOnlyAdded.attributeFeatureReader(context.Background(), selected.Layer, selected.FeatureID)
	if err != nil || feature.Properties["name"] != "second" {
		t.Fatalf("windowed selected feature name=%v err=%v", feature.Properties["name"], err)
	}
}

func chunkKeyForPoint(extent [4]float64, chunkSize float64, layer string, point render.Point) render.ChunkKey {
	spanX, spanY := extent[2]-extent[0], extent[3]-extent[1]
	maxChunk := int(math.Ceil(1/chunkSize)) - 1
	x := int(math.Floor(((point.X - extent[0]) / spanX) / chunkSize))
	y := int(math.Floor(((point.Y - extent[1]) / spanY) / chunkSize))
	return render.ChunkKey{Layer: layer, X: max(0, min(maxChunk, x)), Y: max(0, min(maxChunk, y))}
}

func TestWindowedReadOnlySupportsMixedCRSWithoutFullGeometryLoad(t *testing.T) {
	ctx := context.Background()
	wgs84 := core.Layer{
		Name: "wgs84", CRS: core.CRS{AuthorityCode: "EPSG:4326"},
		Fields:   []core.Field{{Name: "name", Type: core.FieldTypeText}},
		Features: []core.Feature{{ID: 1, Geometry: core.WKTGeometry{WKT: "POINT (127 37)"}, Properties: map[string]any{"name": "origin"}}},
	}
	webMercator, err := (projdriver.Transformer{}).Transform(ctx, wgs84.CRS, core.CRS{AuthorityCode: "EPSG:3857"}, wgs84)
	if err != nil {
		t.Fatal(err)
	}
	firstPath := filepath.Join(t.TempDir(), "wgs84.gpkg")
	secondPath := filepath.Join(t.TempDir(), "mercator.gpkg")
	if err := (gdal.Writer{}).Write(ctx, firstPath, wgs84); err != nil {
		t.Fatal(err)
	}
	if err := (gdal.Writer{}).Write(ctx, secondPath, webMercator); err != nil {
		t.Fatal(err)
	}
	runtime, err := loadReadOnlyDataRuntime(ctx, []vectorSourceSpec{{Path: firstPath}, {Path: secondPath}}, "EPSG:3857")
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.closeAttributeSource()
	if !runtime.viewportReadOnly || runtime.mapCRS != "EPSG:3857" || len(runtime.features) != 0 {
		t.Fatalf("mixed-CRS runtime viewport=%t CRS=%q features=%d", runtime.viewportReadOnly, runtime.mapCRS, len(runtime.features))
	}
	names := runtime.service.LayerNames()
	if len(names) != 2 {
		t.Fatalf("layer names = %v", names)
	}
	var x, y float64
	if _, err := fmt.Sscanf(webMercator.Features[0].Geometry.(core.WKTGeometry).WKT, "POINT (%f %f)", &x, &y); err != nil {
		t.Fatalf("parse projected fixture point: %v", err)
	}
	projectedKey := chunkKeyForPoint(runtime.mapExtent, readOnlyWindowChunkSize(2), names[1], render.Point{X: x, Y: y})
	firstKey := chunkKeyForPoint(runtime.mapExtent, readOnlyWindowChunkSize(2), names[0], render.Point{X: x, Y: y})
	projectedKey.ZoomBucket, firstKey.ZoomBucket = 2, 2
	keys := []render.ChunkKey{firstKey, projectedKey}
	runtime.mu.Lock()
	for _, key := range keys {
		runtime.windowVisibleKeys[key] = struct{}{}
	}
	runtime.mu.Unlock()
	for _, key := range keys {
		if _, err := runtime.builder(ctx, key); err != nil {
			t.Fatalf("build mixed-CRS window %v: %v", key, err)
		}
	}
	if len(runtime.features) != 2 {
		t.Fatalf("mixed-CRS window hit count = %d, want 2; extent=%v point=(%v,%v) key=(%d,%d) hits=%#v", len(runtime.features), runtime.mapExtent, x, y, projectedKey.X, projectedKey.Y, runtime.features)
	}
	firstPoint, secondPoint := runtime.features[0].Vertices[0], runtime.features[1].Vertices[0]
	if math.Abs(firstPoint.X-secondPoint.X) > 1e-6 || math.Abs(firstPoint.Y-secondPoint.Y) > 1e-6 {
		t.Fatalf("same source point differs after window reprojection: %v vs %v", firstPoint, secondPoint)
	}
}

func TestDesktopReloadingOneSourcePreservesOtherEditedLayersAndOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "roads.gpkg")
	reloaded := core.Layer{
		Name: "roads", CRS: core.CRS{AuthorityCode: "EPSG:4326"},
		Fields: []core.Field{{Name: "name", Type: core.FieldTypeText}},
		Features: []core.Feature{{
			ID: 1, Geometry: core.WKTGeometry{WKT: "LINESTRING (127 37, 127.1 37.1)"},
			Properties: map[string]any{"name": "reloaded source"},
		}},
	}
	if err := (gdal.Writer{}).Write(context.Background(), path, reloaded); err != nil {
		t.Fatal(err)
	}
	baseLayers := []core.Layer{{
		Name: "buildings", DisplayName: "Edited buildings", SourcePath: "buildings.gpkg",
		SourceLayerName: "buildings", CRS: core.CRS{AuthorityCode: "EPSG:4326"},
		Visible: true, Style: core.DefaultLayerStyle(), Labels: core.DefaultLabelSettings(),
		Fields: []core.Field{{Name: "status", Type: core.FieldTypeText}},
		Features: []core.Feature{{
			ID: 9, Geometry: core.WKTGeometry{WKT: "POINT (127.2 37.2)"},
			Properties: map[string]any{"status": "unsaved edit"},
		}},
	}}
	visible := true
	runtime, err := loadDataRuntimeSourcesWithCRS(context.Background(), []vectorSourceSpec{{
		Path: path, LayerName: "roads", Name: "roads", DisplayName: "Relinked roads",
		Visible: &visible, Style: core.DefaultLayerStyle(), Labels: core.DefaultLabelSettings(),
		InsertAt: 0, InsertAtSet: true,
	}}, baseLayers, "", "EPSG:4326")
	if err != nil {
		t.Fatal(err)
	}
	project := runtime.service.Project()
	if len(project.Layers) != 2 || project.Layers[0].Name != "roads" || project.Layers[1].Name != "buildings" {
		t.Fatalf("reloaded project layer order = %+v", runtime.service.LayerNames())
	}
	if project.Layers[0].DisplayName != "Relinked roads" || project.Layers[0].Features[0].Properties["name"] != "reloaded source" {
		t.Fatalf("replacement source layer = %+v", project.Layers[0])
	}
	if project.Layers[1].DisplayName != "Edited buildings" || project.Layers[1].Features[0].Properties["status"] != "unsaved edit" {
		t.Fatalf("unrelated edited layer was not preserved: %+v", project.Layers[1])
	}
}

func TestWorkspaceReopensOriginalSourcesAndLayerSettings(t *testing.T) {
	directory := t.TempDir()
	sourcePath := filepath.Join(directory, "roads.gpkg")
	layer := core.Layer{
		Name: "roads", CRS: core.CRS{AuthorityCode: "EPSG:4326"},
		Fields:   []core.Field{{Name: "name", Type: core.FieldTypeText}},
		Features: []core.Feature{{ID: 1, Geometry: core.WKTGeometry{WKT: "LINESTRING (127 37, 127.1 37.1)"}, Properties: map[string]any{"name": "길"}}},
	}
	if err := (gdal.Writer{}).Write(context.Background(), sourcePath, layer); err != nil {
		t.Fatal(err)
	}
	runtime, err := loadDataRuntimeFiles(context.Background(), []string{sourcePath}, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	settings, _ := runtime.service.LayerProperties("roads")
	settings.DisplayName = "Local roads"
	settings.SourceEncoding = "UTF-8"
	settings.Visible = false
	settings.Style.PointColor = "#aabbcc"
	settings.Style.PointSizeMM = 3.2
	settings.Style.LineColor = "#0066cc"
	settings.Style.LineWidthMM = 1.4
	settings.Style.PolygonColor = "#123456"
	settings.Style.FillOpacity = 0.6
	settings.Labels = core.LabelSettings{
		Enabled: true, Expression: "${name}", Rule: `return feature.kind == "primary"`,
		LuaScript: `return feature.name .. " (" .. feature.kind .. ")"`, Placement: "free-angle",
		RotationField: "angle", HeightMM: 2.5, MinScale: 1000, MaxScale: 50000,
	}
	if err := runtime.service.UpdateLayerSettings("roads", settings); err != nil {
		t.Fatal(err)
	}
	projectName, projectCRS := runtime.service.ProjectInfo()
	workspacePath := filepath.Join(directory, "field.gogis")
	projectLayers := runtime.service.ProjectLayerProperties()
	projectLayers = append(projectLayers, core.Layer{
		Name: "buildings", DisplayName: "Buildings", SourcePath: filepath.Join(directory, "moved", "buildings.gpkg"),
		SourceLayerName: "buildings", SourceCRS: "EPSG:4326", CRS: projectCRS,
		Visible: true, Style: core.DefaultLayerStyle(), Labels: core.DefaultLabelSettings(),
	})
	doc := workspace.FromProject(core.Project{Name: projectName, CRS: projectCRS, Layers: projectLayers})
	doc.View = &workspace.ViewState{CenterX: 0.35, CenterY: 0.72, Zoom: 2.5, ActiveLayer: "roads"}
	if err := workspace.Save(workspacePath, doc); err != nil {
		t.Fatal(err)
	}
	reopened, err := loadWorkspaceRuntime(context.Background(), workspacePath, false, "")
	if err != nil {
		t.Fatal(err)
	}
	properties, ok := reopened.service.LayerProperties("roads")
	if !ok || properties.SourcePath != sourcePath || properties.DisplayName != "Local roads" || properties.SourceEncoding != "UTF-8" ||
		properties.Visible || properties.Style != settings.Style || properties.Labels != settings.Labels {
		t.Fatalf("workspace layer settings were not restored: %+v, found=%t", properties, ok)
	}
	if reopened.workspaceView == nil || *reopened.workspaceView != *doc.View {
		t.Fatalf("workspace view was not restored: %+v, want %+v", reopened.workspaceView, doc.View)
	}
	missing, ok := reopened.service.LayerProperties("buildings")
	if !ok || missing.SourcePath != filepath.Join(directory, "moved", "buildings.gpkg") || reopened.unavailableSources["buildings"].Reason == "" {
		t.Fatalf("missing source was not retained for relinking: layer=%+v unavailable=%+v", missing, reopened.unavailableSources)
	}
	if got := len(reopened.service.LayerNames()); got != 2 {
		t.Fatalf("workspace layer count = %d, want both loaded and missing layers", got)
	}
	if reopened.readOnly {
		t.Fatal("an unavailable, empty workspace source incorrectly forced read-only mode")
	}
	readOnly, err := loadWorkspaceRuntime(context.Background(), workspacePath, true, "")
	if err != nil {
		t.Fatal(err)
	}
	if readOnly.closeAttributeSource != nil {
		defer readOnly.closeAttributeSource()
	}
	if _, ok := readOnly.service.LayerProperties("buildings"); !ok || readOnly.unavailableSources["buildings"].Reason == "" {
		t.Fatalf("read-only workspace did not retain missing source: %+v", readOnly.unavailableSources)
	}
}

func TestWorkspaceViewNormalizesPanAndZoom(t *testing.T) {
	got := workspaceViewFromViewport(native.Viewport{PanX: 120, PanY: -60, Zoom: 2, Width: 800, Height: 400}, "roads")
	want := workspace.ViewState{CenterX: 0.425, CenterY: 0.425, Zoom: 2, ActiveLayer: "roads"}
	if got != want {
		t.Fatalf("saved view state = %+v, want %+v", got, want)
	}
	runtime := &demoRuntime{workspaceView: &got}
	if viewport := runtimeInitialViewport(runtime); viewport.Center != (render.Point{X: want.CenterX, Y: want.CenterY}) || viewport.Zoom != want.Zoom {
		t.Fatalf("restored initial viewport = %+v, want center (%v, %v) zoom %v", viewport, want.CenterX, want.CenterY, want.Zoom)
	}
}

func TestProjectCRSChangeReprojectsLayersAndUpdatesWorkspaceCRS(t *testing.T) {
	runtime, err := buildDataRuntime(context.Background(), []core.Layer{{
		Name: "points", CRS: core.CRS{AuthorityCode: "EPSG:4326"},
		Features: []core.Feature{{ID: 1, Geometry: core.WKTGeometry{WKT: "POINT (127 37)"}}},
	}}, "", "", "EPSG:4326", "", "", false, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	runtime.service.SetProjectInfo("Survey", core.CRS{AuthorityCode: "EPSG:4326"})
	if err := runtime.startProjectCRSChange("invalid-crs"); err == nil {
		t.Fatal("invalid project CRS was accepted")
	}
	if err := runtime.startProjectCRSChange("EPSG:3857"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		runtime.mu.Lock()
		service := runtime.service
		finished := runtime.loadCancel == nil
		var name string
		var crs core.CRS
		if service != nil {
			name, crs = service.ProjectInfo()
		}
		runtime.mu.Unlock()
		if finished && crs.AuthorityCode == "EPSG:3857" {
			if name != "Survey" || runtime.mapCRS != "EPSG:3857" {
				t.Fatalf("project identity/CRS after reproject = (%q, %q, %q)", name, crs.AuthorityCode, runtime.mapCRS)
			}
			project := service.Project()
			wkt, ok := project.Layers[0].Features[0].Geometry.(core.WKTGeometry)
			if !ok {
				t.Fatalf("reprojected geometry type = %T", project.Layers[0].Features[0].Geometry)
			}
			coordinates := strings.Fields(strings.Trim(strings.TrimSuffix(strings.TrimPrefix(wkt.WKT, "POINT ("), ")"), " "))
			if len(coordinates) != 2 {
				t.Fatalf("reprojected point WKT = %q", wkt.WKT)
			}
			x, xErr := strconv.ParseFloat(coordinates[0], 64)
			y, yErr := strconv.ParseFloat(coordinates[1], 64)
			if xErr != nil || yErr != nil || math.Abs(x-14_150_000) > 100_000 || math.Abs(y-4_440_000) > 100_000 {
				t.Fatalf("reprojected coordinates = (%v,%v), errors=(%v,%v)", x, y, xErr, yErr)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("project CRS reproject did not complete within 10 seconds")
}

func TestRemoveLayerKeepsRemainingProjectAndClearsLastLayer(t *testing.T) {
	layers := []core.Layer{
		{Name: "west", CRS: core.CRS{AuthorityCode: "EPSG:5186"}, Visible: true,
			Features: []core.Feature{{ID: 1, Geometry: core.WKTGeometry{WKT: "POINT (100 100)"}, Properties: map[string]any{"name": "west feature"}}}},
		{Name: "east", CRS: core.CRS{AuthorityCode: "EPSG:5186"}, Visible: true,
			Features: []core.Feature{{ID: 2, Geometry: core.WKTGeometry{WKT: "POINT (1000 1000)"}}}},
	}
	runtime, err := buildDataRuntime(context.Background(), layers, "", "", "EPSG:5186", "", "", false, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.startRemoveLayer("missing"); err == nil {
		t.Fatal("missing layer removal should fail")
	}
	if err := applyLayerSettings(runtime, `{"operation":"unknown","name":"east"}`); err == nil {
		t.Fatal("unknown layer action should be rejected")
	}
	runtime.mu.Lock()
	runtime.loadCancel = func() {}
	runtime.mu.Unlock()
	if err := runtime.startRemoveLayer("east"); err == nil {
		t.Fatal("removing a layer during another load should be rejected")
	}
	runtime.mu.Lock()
	runtime.loadCancel = nil
	runtime.mu.Unlock()
	waitForLayers := func(want []string) {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			runtime.mu.Lock()
			got := runtime.service.LayerNames()
			runtime.mu.Unlock()
			if len(got) == len(want) {
				matching := true
				for i := range got {
					matching = matching && got[i] == want[i]
				}
				if matching {
					return
				}
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("layer removal did not produce %v", want)
	}
	if err := applyLayerSettings(runtime, `{"operation":"remove","name":"east"}`); err != nil {
		t.Fatal(err)
	}
	waitForLayers([]string{"west"})
	page, total, err := runtime.attributePageReader(context.Background(), "west", 0, 10)
	if err != nil || total != 1 || len(page.Features) != 1 || page.Features[0].Properties["name"] != "west feature" {
		t.Fatalf("remaining layer attribute page = %+v, total %d, err %v", page, total, err)
	}
	if runtime.attributeFeatureReader != nil {
		t.Fatal("editable layer should read current feature properties from the service")
	}
	if _, exists := runtime.layerStyles["east"]; exists {
		t.Fatal("removed layer style is still resident")
	}
	if err := runtime.startRemoveLayer("west"); err != nil {
		t.Fatal(err)
	}
	waitForLayers(nil)
}

func TestRapidVectorSelectionsQueueUntilCurrentLoadPublishes(t *testing.T) {
	directory := t.TempDir()
	paths := []string{filepath.Join(directory, "first.geojson"), filepath.Join(directory, "second.geojson")}
	for index, path := range paths {
		content := fmt.Sprintf(`{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"name":"feature %d"},"geometry":{"type":"Point","coordinates":[%d,37]}}]}`, index, 126+index)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	first, err := loadDataRuntimeFiles(context.Background(), paths[:1], nil, "")
	if err != nil {
		t.Fatal(err)
	}
	runtime := newEmptyProjectRuntime()
	runtime.loadGeneration = 1
	runtime.loadCancel = func() {}
	runtime.startDataLoadPaths([]string{filepath.Join(directory, "missing.geojson")})
	runtime.startDataLoadPaths(paths[1:])
	runtime.startDataLoadPaths(paths[1:])
	if len(runtime.pendingLoadBatches) != 3 || runtime.pendingLoadBatches[1][0] != paths[1] {
		t.Fatalf("queued selections = %v, want invalid then two second-source selections", runtime.pendingLoadBatches)
	}
	runtime.replaceWithLoaded(first, 1)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		runtime.mu.Lock()
		count := len(runtime.service.LayerNames())
		loading := runtime.loadCancel != nil
		queued := len(runtime.pendingLoadBatches)
		runtime.mu.Unlock()
		if count == 2 && !loading {
			if queued != 0 {
				t.Fatalf("queue not drained: %d pending selections", queued)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	runtime.mu.Lock()
	names := runtime.service.LayerNames()
	runtime.mu.Unlock()
	t.Fatalf("queued second source was not added; layers = %v", names)
}

func TestCancellingLoadDiscardsQueuedVectorSelections(t *testing.T) {
	runtime := newEmptyProjectRuntime()
	cancelled := false
	runtime.loadCancel = func() { cancelled = true }
	runtime.startDataLoadPaths([]string{filepath.Join(t.TempDir(), "later.geojson")})
	if len(runtime.pendingLoadBatches) != 1 {
		t.Fatalf("queued selections = %v, want one", runtime.pendingLoadBatches)
	}
	runtime.cancelCurrentRender()
	if !cancelled || runtime.loadCancel != nil || len(runtime.pendingLoadBatches) != 0 {
		t.Fatalf("cancellation left a pending load: cancelled=%v pending=%v", cancelled, runtime.pendingLoadBatches)
	}
}

func TestRemoveReadOnlyLayerKeepsOtherSourceFile(t *testing.T) {
	directory := t.TempDir()
	paths := []string{filepath.Join(directory, "west.geojson"), filepath.Join(directory, "east.geojson")}
	for index, path := range paths {
		content := fmt.Sprintf(`{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"value":%d},"geometry":{"type":"Point","coordinates":[%d,37]}}]}`, index, 126+index)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	runtime, err := loadReadOnlyDataRuntimeWithBaseLayers(context.Background(), []vectorSourceSpec{{Path: paths[0]}, {Path: paths[1]}}, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	names := runtime.service.LayerNames()
	if len(names) != 2 {
		t.Fatalf("loaded layers = %v; want two", names)
	}
	if err := runtime.startRemoveLayer(names[0]); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		runtime.mu.Lock()
		remaining := runtime.service.LayerNames()
		runtime.mu.Unlock()
		if len(remaining) == 1 && remaining[0] == names[1] {
			runtime.mu.Lock()
			visible := runtime.visibleLayers[names[1]]
			runtime.mu.Unlock()
			if !visible {
				t.Fatalf("retained layer %q became hidden", names[1])
			}
			if _, err := os.Stat(paths[0]); err != nil {
				t.Fatalf("removing project layer deleted its source file: %v", err)
			}
			if _, err := os.Stat(paths[1]); err != nil {
				t.Fatalf("remaining source file unavailable: %v", err)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("read-only layer removal did not leave %q", names[1])
}

func TestPreserveMapWorldViewAcrossExpandedLayerExtent(t *testing.T) {
	view := preserveMapWorldView(
		native.Viewport{PanX: 50, PanY: 25, Zoom: 2, Width: 100, Height: 100,
			ViewportWidth: 200, ViewportHeight: 100},
		[4]float64{0, 0, 100, 100},
		[4]float64{-100, -100, 300, 200},
		"roads",
	)
	if view == nil || math.Abs(view.CenterX-0.3125) > 1e-9 || math.Abs(view.CenterY-0.5416666666666666) > 1e-9 || math.Abs(view.Zoom-6) > 1e-9 || view.ActiveLayer != "roads" {
		t.Fatalf("preserved world view = %+v", view)
	}
}

func TestRapidLayerAppendUsesPendingViewWhenCanvasSnapshotIsStale(t *testing.T) {
	oldExtent := [4]float64{0, 0, 400, 100}
	newExtent := [4]float64{0, 0, 800, 400}
	staleCanvas := native.Viewport{Width: 100, Height: 100, Zoom: 1, ViewportWidth: 800, ViewportHeight: 400}
	if !viewportCanvasAspectIsStale(staleCanvas, oldExtent, "EPSG:5186") {
		t.Fatal("stale canvas aspect was not detected")
	}
	pending := workspace.ViewState{CenterX: 0.3, CenterY: 0.4, Zoom: 2}
	view := preserveSavedMapWorldView(pending, staleCanvas, oldExtent, newExtent, "EPSG:5186", "west")
	if math.Abs(view.CenterX-0.15) > 1e-9 || math.Abs(view.CenterY-0.1) > 1e-9 ||
		math.Abs(view.Zoom-4) > 1e-9 || view.ActiveLayer != "west" {
		t.Fatalf("rapid append changed position or map scale: %+v", view)
	}
}

func TestInitialFitViewExistsBeforeFirstLayerMetadataIsApplied(t *testing.T) {
	layers := []core.Layer{{Name: "parcels", CRS: core.CRS{AuthorityCode: "EPSG:5186"}, Visible: true,
		Features: []core.Feature{
			{ID: 1, Geometry: core.WKTGeometry{WKT: "POINT (0 0)"}},
			{ID: 2, Geometry: core.WKTGeometry{WKT: "POINT (200 100)"}},
		}}}
	runtime, err := buildDataRuntime(context.Background(), layers, "", "", "EPSG:5186", "", "", false, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	viewport := native.Viewport{ViewportWidth: 800, ViewportHeight: 400}
	view := initialFitWorkspaceView(runtime, viewport)
	if view == nil || view.ActiveLayer != "parcels" || math.Abs(view.CenterX-0.5) > 1e-9 ||
		math.Abs(view.CenterY-0.5) > 1e-9 || math.Abs(view.Zoom-0.9) > 1e-9 {
		t.Fatalf("initial fit view = %+v", view)
	}
	if got := initialFitWorkspaceView(runtime, native.Viewport{}); got != nil {
		t.Fatalf("headless viewport should defer first fit to QML, got %+v", got)
	}
}

func TestGeographicCanvasAspectUsesMetersAtExtentCenter(t *testing.T) {
	extent := [4]float64{126, 36, 128, 38}
	want := math.Cos(37 * math.Pi / 180)
	if got := extentAspectMeters(extent, "EPSG:4326"); math.Abs(got-want) > 1e-9 {
		t.Fatalf("geographic meter aspect = %g; want %g", got, want)
	}
}

func TestPreserveMapWorldViewWithDistantPointOutlier(t *testing.T) {
	oldExtent := [4]float64{211407.24, 423223.66, 236805.50, 459484.82}
	newExtent := [4]float64{211407.24, 43257.02, 2287874.90, 459484.82}
	oldCanvasWidth := 1000 * (oldExtent[2] - oldExtent[0]) / (oldExtent[3] - oldExtent[1])
	oldView := native.Viewport{
		PanX: 120, PanY: -80, Zoom: 18,
		Width: oldCanvasWidth, Height: 1000,
		ViewportWidth: 1500, ViewportHeight: 1000,
	}
	view := preserveMapWorldView(oldView, oldExtent, newExtent, "parcels")
	oldState := workspaceViewFromViewport(oldView, "parcels")
	oldWorldX := oldExtent[0] + oldState.CenterX*(oldExtent[2]-oldExtent[0])
	oldWorldY := oldExtent[1] + oldState.CenterY*(oldExtent[3]-oldExtent[1])
	newWorldX := newExtent[0] + view.CenterX*(newExtent[2]-newExtent[0])
	newWorldY := newExtent[1] + view.CenterY*(newExtent[3]-newExtent[1])
	newCanvasWidth := math.Min(oldView.ViewportWidth,
		oldView.ViewportHeight*(newExtent[2]-newExtent[0])/(newExtent[3]-newExtent[1]))
	oldUnitsPerPixel := (oldExtent[2] - oldExtent[0]) / (oldView.Width * oldView.Zoom)
	newUnitsPerPixel := (newExtent[2] - newExtent[0]) / (newCanvasWidth * view.Zoom)
	if math.Abs(newWorldX-oldWorldX) > 1e-6 || math.Abs(newWorldY-oldWorldY) > 1e-6 ||
		math.Abs(newUnitsPerPixel-oldUnitsPerPixel) > 1e-9 || view.ActiveLayer != "parcels" {
		t.Fatalf("outlier layer changed current position or scale: old=(%g,%g,%g) new=(%g,%g,%g)",
			oldWorldX, oldWorldY, oldUnitsPerPixel, newWorldX, newWorldY, newUnitsPerPixel)
	}
}

func TestIsWorkspacePathUsesExtensionCaseInsensitively(t *testing.T) {
	for path, want := range map[string]bool{
		"field.gogis": true, "FIELD.GOGIS": true, "field.gpkg": false, "folder.gogis/data.gpkg": false,
	} {
		if got := isWorkspacePath(path); got != want {
			t.Errorf("isWorkspacePath(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestApplyLayerSettingsRejectsEmptyDisplayName(t *testing.T) {
	err := applyLayerSettings(&demoRuntime{}, `{"name":"roads","displayName":"   "}`)
	if err == nil || !strings.Contains(err.Error(), "display name") {
		t.Fatalf("empty display name error = %v", err)
	}
}

func TestApplyLayerSettingsRejectsInvalidLuaBeforeProjectMutation(t *testing.T) {
	labels := core.DefaultLabelSettings()
	labels.Enabled = true
	labels.LuaScript = `return (`
	payload, err := json.Marshal(layerSettingsRequest{
		Name: "roads", DisplayName: "roads", SourcePath: "roads.shp", SourceLayerName: "roads",
		Style: core.DefaultLayerStyle(), Labels: labels,
	})
	if err != nil {
		t.Fatal(err)
	}
	err = applyLayerSettings(&demoRuntime{}, string(payload))
	if err == nil || !strings.Contains(err.Error(), "invalid Lua label settings") {
		t.Fatalf("invalid Lua settings error = %v", err)
	}
}

func TestApplyLayerSettingsRejectsInvalidFeatureDisplayRule(t *testing.T) {
	payload, err := json.Marshal(layerSettingsRequest{
		Name: "roads", DisplayName: "roads", SourcePath: "roads.shp", SourceLayerName: "roads",
		Style: core.DefaultLayerStyle(), Labels: core.DefaultLabelSettings(), DisplayRule: `return feature.kind ==`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := applyLayerSettings(&demoRuntime{}, string(payload)); err == nil || !strings.Contains(err.Error(), "invalid Lua feature display rule") {
		t.Fatalf("invalid feature display rule error = %v", err)
	}
}

func TestFeatureDisplayRuleFiltersRenderSnapshotAndKeepsProjectData(t *testing.T) {
	layer := core.Layer{
		Name: "roads", Visible: true, Style: core.DefaultLayerStyle(), Labels: core.DefaultLabelSettings(),
		DisplayRule: `return feature.visible == true`,
		Features: []core.Feature{
			{ID: 1, Geometry: core.WKTGeometry{WKT: "POINT (0 0)"}, Properties: map[string]any{"visible": true}},
			{ID: 2, Geometry: core.WKTGeometry{WKT: "POINT (1 1)"}, Properties: map[string]any{"visible": false}},
		},
	}
	runtime, err := buildDataRuntime(context.Background(), []core.Layer{layer}, "", "", "", "", "", false, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(runtime.sources["roads"].Features); got != 1 || runtime.sources["roads"].Features[0].FeatureID != 1 {
		t.Fatalf("render source features = %+v, want only feature 1", runtime.sources["roads"].Features)
	}
	if got := runtime.sources["roads"].Extent; got != [4]float64{0, 0, 1, 1} {
		t.Fatalf("feature filter changed the source extent: %v", got)
	}
	projectLayer, ok := runtime.service.ProjectLayerRenderSnapshot("roads")
	if !ok || len(projectLayer.Features) != 2 {
		t.Fatalf("project source data was filtered: found=%t features=%d", ok, len(projectLayer.Features))
	}
}

func TestReadOnlyMaterializedLayerRebuildsLabelsAfterSettingsChange(t *testing.T) {
	runtime, err := buildDataRuntime(context.Background(), []core.Layer{{
		Name: "parcels", DisplayName: "Parcels", SourcePath: "parcels.gpkg", SourceLayerName: "parcels",
		Visible: true, Style: core.DefaultLayerStyle(), Labels: core.DefaultLabelSettings(),
		Features: []core.Feature{{ID: 1, Geometry: core.WKTGeometry{WKT: "POLYGON ((0 0, 2 0, 2 2, 0 2, 0 0))"}, Properties: map[string]any{"name": "Parcel A", "show": true}}},
	}}, "", "", "", "", "", false, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	runtime.readOnly = true
	labels := core.LabelSettings{
		Enabled: true, Expression: "${name}", LuaScript: "return feature.name",
		Placement: "center", HeightMM: 2.5,
	}
	payload, err := json.Marshal(layerSettingsRequest{
		Name: "parcels", DisplayName: "Parcels", SourcePath: "parcels.gpkg", SourceLayerName: "parcels",
		Visible: true, Style: core.DefaultLayerStyle(), Labels: labels,
		DisplayRule: "return feature.show == true",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := applyLayerSettings(runtime, string(payload)); err != nil {
		t.Fatal(err)
	}
	if got := runtime.sources["parcels"].Labels; len(got) != 1 || got[0].Text != "Parcel A" {
		t.Fatalf("read-only materialized labels were not rebuilt: %+v", got)
	}
	if got := len(runtime.sources["parcels"].Features); got != 1 {
		t.Fatalf("display filter produced %d rendered features, want 1", got)
	}
}

func TestDesktopDXFExportConnectsProjectLayerToExporter(t *testing.T) {
	layer := core.Layer{
		Name: "roads", DisplayName: "Roads", Visible: true, Style: core.DefaultLayerStyle(),
		Labels:   core.LabelSettings{Enabled: true, Expression: "${name}", Placement: "center", HeightMM: 2.5},
		Features: []core.Feature{{ID: 3, Geometry: core.WKTGeometry{WKT: "LINESTRING (127 37, 127.1 37.1)"}, Properties: map[string]any{"name": "세종로"}}},
	}
	runtime, err := buildDataRuntime(context.Background(), []core.Layer{layer}, "", "", "", "", "", false, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "roads.dxf")
	if err := runtime.exportProjectLayersDXF(path, "ares-utf8"); err != nil {
		t.Fatalf("export active layer: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, expected := range []string{"LWPOLYLINE", "TEXT", "세종로"} {
		if !strings.Contains(text, expected) {
			t.Errorf("desktop DXF output missing %q", expected)
		}
	}
}

func TestDesktopDXFExportIncludesEveryProjectLayerByDisplayName(t *testing.T) {
	layers := []core.Layer{
		{Name: "parcels_source", DisplayName: "0-연속지적도", Visible: true, Style: core.DefaultLayerStyle(),
			Features: []core.Feature{{ID: 1, Geometry: core.WKTGeometry{WKT: "POLYGON ((0 0, 1 0, 1 1, 0 0))"}}}},
		{Name: "control_source", DisplayName: "0-지적도근점", Visible: true, Style: core.DefaultLayerStyle(),
			Features: []core.Feature{{ID: 2, Geometry: core.WKTGeometry{WKT: "POINT (2 2)"}}}},
		{Name: "building_source", DisplayName: "0-건물", Visible: true, Style: core.DefaultLayerStyle(),
			Labels:   core.LabelSettings{Enabled: true, Expression: "${name}", Placement: "center", HeightMM: 2.5},
			Features: []core.Feature{{ID: 3, Geometry: core.WKTGeometry{WKT: "POLYGON ((3 3, 4 3, 4 4, 3 3))"}, Properties: map[string]any{"name": "건물A"}}}},
	}
	runtime, err := buildDataRuntime(context.Background(), layers, "", "", "", "", "", false, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "project.dxf")
	if err := runtime.exportProjectLayersDXF(path, "ares-utf8"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	for _, name := range []string{"0-연속지적도", "0-지적도근점", "0-건물"} {
		if !strings.Contains(content, "\n0\nLAYER\n") || !strings.Contains(content, "\n2\n"+name+"\n") || !strings.Contains(content, "\n8\n"+name+"\n") {
			t.Errorf("CAD layer %q missing from layer table or entities", name)
		}
	}
	for _, sourceName := range []string{"parcels_source", "control_source", "building_source"} {
		if strings.Contains(content, "\n8\n"+sourceName+"\n") {
			t.Errorf("internal source name %q leaked into CAD entities", sourceName)
		}
	}
	if !strings.Contains(content, "\n0\nPOINT\n") || !strings.Contains(content, "\n0\nLWPOLYLINE\n") || !strings.Contains(content, "\n1\n건물A\n") {
		t.Fatal("project geometry or label missing from DXF")
	}
	if strings.Contains(content, "\n2\nLABEL\n") {
		t.Fatal("unexpected shared LABEL layer")
	}
	textStart := strings.Index(content, "\n0\nTEXT\n")
	if textStart < 0 || !strings.Contains(content[textStart:], "\n8\n0-건물\n") {
		t.Fatal("building TEXT was not assigned to its project CAD layer")
	}
	roundTrip, err := (gdal.Reader{}).Open(context.Background(), path, "")
	if err != nil {
		t.Fatalf("GDAL could not reopen multi-layer DXF: %v", err)
	}
	if len(roundTrip.Features) < 4 {
		t.Fatalf("GDAL reopened %d entities, want three geometries and one label", len(roundTrip.Features))
	}
}

func TestDesktopDXFExportOptionsSelectAndInterpretLayers(t *testing.T) {
	layers := []core.Layer{
		{Name: "parcels", DisplayName: "Parcels", Visible: true, Style: core.DefaultLayerStyle(),
			Features: []core.Feature{{ID: 1, Geometry: core.WKTGeometry{WKT: "POLYGON ((0 0, 1 0, 1 1, 0 0))"}}}},
		{Name: "points", DisplayName: "Points", Visible: true, Style: core.DefaultLayerStyle(),
			Labels:   core.LabelSettings{Enabled: true, Expression: "${name}", Placement: "center", HeightMM: 2.5},
			Features: []core.Feature{{ID: 2, Geometry: core.WKTGeometry{WKT: "POINT (2 2)"}, Properties: map[string]any{"name": "P-1"}}}},
		{Name: "buildings", DisplayName: "Buildings", Visible: true, Style: core.DefaultLayerStyle(),
			Features: []core.Feature{{ID: 3, Geometry: core.WKTGeometry{WKT: "POLYGON ((3 3, 4 3, 4 4, 3 3))"}}}},
	}
	runtime, err := buildDataRuntime(context.Background(), layers, "", "", "", "", "", false, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	options := `[{"name":"parcels","cadName":"Land CAD","include":true,"geometry":true,"labels":false},` +
		`{"name":"points","cadName":"Survey CAD","include":true,"geometry":false,"labels":true},` +
		`{"name":"buildings","cadName":"Buildings CAD","include":false,"geometry":true,"labels":true}]`
	path := filepath.Join(t.TempDir(), "selected.dxf")
	if err := runtime.exportProjectLayersDXFWithOptions(path, "ares-utf8", options); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, "\n2\nLand CAD\n") || !strings.Contains(content, "\n2\nSurvey CAD\n") ||
		strings.Contains(content, "\n2\nBuildings CAD\n") || strings.Contains(content, "\n0\nPOINT\n") ||
		!strings.Contains(content, "\n1\nP-1\n") || !strings.Contains(content, "\n0\nLWPOLYLINE\n") {
		t.Fatalf("selected CAD layer/geometry/label output is incorrect")
	}
	if err := runtime.exportProjectLayersDXFWithOptions(path, "ares-utf8", `[{"name":"parcels","include":true}]`); err == nil {
		t.Fatal("stale partial DXF option plan was accepted")
	}
}

func TestDesktopReadOnlyDXFExportIncludesEverySource(t *testing.T) {
	directory := t.TempDir()
	paths := []string{filepath.Join(directory, "points.shp"), filepath.Join(directory, "buildings.shp")}
	layers := []core.Layer{
		{Name: "points", CRS: core.CRS{AuthorityCode: "EPSG:5186"}, Features: []core.Feature{{ID: 1, Geometry: core.WKTGeometry{WKT: "POINT (200000 400000)"}}}},
		{Name: "buildings", CRS: core.CRS{AuthorityCode: "EPSG:5186"}, Features: []core.Feature{{ID: 2, Geometry: core.WKTGeometry{WKT: "POLYGON ((200001 400001, 200002 400001, 200002 400002, 200001 400001))"}}}},
	}
	for index := range paths {
		if err := (gdal.Writer{}).Write(context.Background(), paths[index], layers[index]); err != nil {
			t.Fatal(err)
		}
	}
	runtime, err := loadReadOnlyDataRuntime(context.Background(), []vectorSourceSpec{{Path: paths[0]}, {Path: paths[1]}}, "")
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.closeAttributeSource()
	names := runtime.service.LayerNames()
	if len(names) != 2 {
		t.Fatalf("source layers = %v", names)
	}
	if err := runtime.service.RenameLayer(names[0], "0-지적도근점"); err != nil {
		t.Fatal(err)
	}
	if err := runtime.service.RenameLayer(names[1], "0-건물"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "project.dxf")
	if err := runtime.exportProjectLayersDXF(path, "ares-utf8"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	for _, name := range []string{"0-지적도근점", "0-건물"} {
		if !strings.Contains(content, "\n8\n"+name+"\n") {
			t.Errorf("missing source CAD layer %q", name)
		}
	}
	if !strings.Contains(content, "\n0\nPOINT\n") || !strings.Contains(content, "\n0\nLWPOLYLINE\n") {
		t.Fatal("read-only source geometry missing from project DXF")
	}
}

// Set GOGIS_TEST_DXF_WORKSPACE to a local three-layer workspace to exercise
// the full read-only export without checking private GIS data into the repo.
func TestDesktopDXFIntegrationWorkspace(t *testing.T) {
	path := os.Getenv("GOGIS_TEST_DXF_WORKSPACE")
	if path == "" {
		t.Skip("set GOGIS_TEST_DXF_WORKSPACE to a local workspace")
	}
	runtime, err := loadWorkspaceRuntime(context.Background(), path, true, "")
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.closeAttributeSource()
	destination := os.Getenv("GOGIS_TEST_DXF_OUTPUT_PATH")
	if destination == "" {
		destination = filepath.Join(t.TempDir(), "all-layers.dxf")
	}
	if err := runtime.exportProjectLayersDXF(destination, "ares-utf8"); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(destination)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	counts := make(map[string]int)
	entityTypes := make(map[string]map[string]int)
	entityBytes := make(map[string]int64)
	polylineVertices := make(map[string]int64)
	complexPolylines := make(map[string]int)
	declaredLayers := make(map[string]bool)
	seenHandles := make(map[string]bool)
	var kind, handle, owner, layerName, subclass string
	var vertices, xValues, yValues int
	var textX, textY float64
	var textHasX, textHasY bool
	var hatchDeclaredPaths, hatchPaths, hatchExternalPaths int
	var hatchExpectedVertices, hatchXValues, hatchYValues int
	var solidXMask, solidYMask uint8
	var sectionName, headerVariable string
	var viewCenterX, viewCenterY, viewSize float64
	var viewCenterXSet, viewCenterYSet, viewSizeSet bool
	var activeViewport, viewportXSet, viewportYSet, viewportHeightSet bool
	var viewportX, viewportY, viewportHeight float64
	var recordBytes int64
	checkRecord := func() {
		if kind != "POINT" && kind != "LINE" && kind != "LWPOLYLINE" && kind != "HATCH" && kind != "SOLID" && kind != "TEXT" {
			return
		}
		if handle == "" || owner != "1F" || layerName == "" || subclass == "" {
			t.Fatalf("incomplete %s record: handle=%q owner=%q layer=%q subclass=%q", kind, handle, owner, layerName, subclass)
		}
		if kind == "LWPOLYLINE" && (vertices <= 0 || vertices != xValues || vertices != yValues) {
			t.Fatalf("invalid %s vertex count: declared=%d x=%d y=%d", kind, vertices, xValues, yValues)
		}
		if kind == "LWPOLYLINE" {
			polylineVertices[layerName] += int64(vertices)
			if vertices > 500 {
				complexPolylines[layerName]++
			}
		}
		if kind == "TEXT" && layerName == "0-지적도근점" {
			if !textHasX || !textHasY {
				t.Fatal("survey-point TEXT label has no explicit insertion coordinate")
			}
			if math.Abs(textX) < 1e-9 && math.Abs(textY) < 1e-9 {
				t.Fatalf("survey-point TEXT label unexpectedly uses the origin (%g, %g)", textX, textY)
			}
		}
		if kind == "HATCH" {
			if hatchDeclaredPaths == 0 || hatchPaths != hatchDeclaredPaths {
				t.Fatalf("HATCH declares %d paths but contains %d", hatchDeclaredPaths, hatchPaths)
			}
			if hatchXValues != hatchExpectedVertices || hatchYValues != hatchExpectedVertices {
				t.Fatalf("final HATCH path declares %d vertices but has %d X/%d Y coordinates", hatchExpectedVertices, hatchXValues, hatchYValues)
			}
			if hatchExternalPaths != 1 {
				t.Fatalf("HATCH contains %d external paths, want exactly one", hatchExternalPaths)
			}
		}
		if kind == "SOLID" && (solidXMask != 15 || solidYMask != 15 || subclass != "AcDbTrace") {
			t.Fatalf("SOLID corners/subclass are incomplete: X=%b Y=%b subclass=%q", solidXMask, solidYMask, subclass)
		}
		counts[layerName]++
		if entityTypes[layerName] == nil {
			entityTypes[layerName] = make(map[string]int)
		}
		entityTypes[layerName][kind]++
	}
	for scanner.Scan() {
		code := strings.TrimSpace(scanner.Text())
		if !scanner.Scan() {
			t.Fatal("truncated DXF group pair")
		}
		value := strings.TrimSpace(scanner.Text())
		recordBytes += int64(len(code) + len(value) + 2)
		if code == "0" {
			checkRecord()
			if (kind == "POINT" || kind == "LINE" || kind == "LWPOLYLINE" || kind == "HATCH" || kind == "SOLID" || kind == "TEXT") && layerName != "" {
				entityBytes[layerName+"/"+kind] += recordBytes - int64(len(code)+len(value)+2)
			}
			recordBytes = int64(len(code) + len(value) + 2)
			kind, handle, owner, layerName, subclass = value, "", "", "", ""
			vertices, xValues, yValues = 0, 0, 0
			textX, textY, textHasX, textHasY = 0, 0, false, false
			hatchDeclaredPaths, hatchPaths, hatchExternalPaths = 0, 0, 0
			hatchExpectedVertices, hatchXValues, hatchYValues = 0, 0, 0
			solidXMask, solidYMask = 0, 0
			continue
		}
		switch code {
		case "2":
			if kind == "SECTION" {
				sectionName = value
			}
			if kind == "LAYER" {
				declaredLayers[value] = true
			}
			if kind == "VPORT" && value == "*ACTIVE" {
				activeViewport = true
			}
		case "9":
			if sectionName == "HEADER" {
				headerVariable = value
			}
		case "5":
			if seenHandles[value] {
				t.Fatalf("duplicate DXF handle %q", value)
			}
			seenHandles[value] = true
			handle = value
		case "330":
			owner = value
		case "8":
			layerName = value
		case "100":
			if strings.HasPrefix(value, "AcDb") && value != "AcDbEntity" {
				subclass = value
			}
		case "90":
			if kind == "LWPOLYLINE" {
				vertices, err = strconv.Atoi(value)
				if err != nil {
					t.Fatalf("invalid DXF polyline vertex count %q: %v", value, err)
				}
			}
		case "91":
			if kind == "HATCH" {
				hatchDeclaredPaths, err = strconv.Atoi(value)
				if err != nil || hatchDeclaredPaths < 1 {
					t.Fatalf("invalid HATCH boundary path count %q: %v", value, err)
				}
			}
		case "92":
			if kind == "HATCH" {
				if hatchPaths > 0 && (hatchXValues != hatchExpectedVertices || hatchYValues != hatchExpectedVertices) {
					t.Fatalf("HATCH path %d declares %d vertices but has %d X/%d Y coordinates", hatchPaths, hatchExpectedVertices, hatchXValues, hatchYValues)
				}
				hatchPaths++
				hatchExpectedVertices, hatchXValues, hatchYValues = 0, 0, 0
				if hatchPaths == 1 {
					if value != "3" {
						t.Fatalf("HATCH outer path flag = %q, want 3", value)
					}
					hatchExternalPaths++
				} else if value != "2" {
					t.Fatalf("HATCH inner path flag = %q, want 2", value)
				}
			}
		case "93":
			if kind == "HATCH" {
				hatchExpectedVertices, err = strconv.Atoi(value)
				if err != nil || hatchExpectedVertices < 3 {
					t.Fatalf("invalid HATCH boundary vertex count %q: %v", value, err)
				}
			}
		case "10":
			if sectionName == "HEADER" && headerVariable == "$VIEWCTR" {
				viewCenterX, err = strconv.ParseFloat(value, 64)
				if err != nil {
					t.Fatalf("invalid $VIEWCTR X %q: %v", value, err)
				}
				viewCenterXSet = true
			} else if kind == "LWPOLYLINE" {
				xValues++
			} else if kind == "HATCH" {
				hatchXValues++
			} else if kind == "SOLID" {
				solidXMask |= 1
			} else if kind == "TEXT" {
				textX, err = strconv.ParseFloat(value, 64)
				if err != nil || math.IsNaN(textX) || math.IsInf(textX, 0) {
					t.Fatalf("invalid TEXT insertion X %q: %v", value, err)
				}
				textHasX = true
			}
		case "20":
			if sectionName == "HEADER" && headerVariable == "$VIEWCTR" {
				viewCenterY, err = strconv.ParseFloat(value, 64)
				if err != nil {
					t.Fatalf("invalid $VIEWCTR Y %q: %v", value, err)
				}
				viewCenterYSet = true
			} else if kind == "LWPOLYLINE" {
				yValues++
			} else if kind == "HATCH" {
				hatchYValues++
			} else if kind == "SOLID" {
				solidYMask |= 1
			} else if kind == "TEXT" {
				textY, err = strconv.ParseFloat(value, 64)
				if err != nil || math.IsNaN(textY) || math.IsInf(textY, 0) {
					t.Fatalf("invalid TEXT insertion Y %q: %v", value, err)
				}
				textHasY = true
			}
		case "40":
			if sectionName == "HEADER" && headerVariable == "$VIEWSIZE" {
				viewSize, err = strconv.ParseFloat(value, 64)
				if err != nil {
					t.Fatalf("invalid $VIEWSIZE %q: %v", value, err)
				}
				viewSizeSet = true
			}
		case "11", "12", "13":
			if kind == "SOLID" {
				solidXMask |= 1 << (code[1] - '0')
			} else if kind == "VPORT" && code == "12" {
				viewportX, err = strconv.ParseFloat(value, 64)
				viewportXSet = err == nil
			}
		case "21", "22", "23":
			if kind == "SOLID" {
				solidYMask |= 1 << (code[1] - '0')
			} else if kind == "VPORT" && code == "22" {
				viewportY, err = strconv.ParseFloat(value, 64)
				viewportYSet = err == nil
			}
		case "45":
			if kind == "VPORT" {
				viewportHeight, err = strconv.ParseFloat(value, 64)
				viewportHeightSet = err == nil
			}
		}
	}
	checkRecord()
	if layerName != "" {
		entityBytes[layerName+"/"+kind] += recordBytes
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if !viewCenterXSet || !viewCenterYSet || !viewSizeSet || viewSize <= 0 {
		t.Fatalf("DXF has no valid opening view: center=(%g, %g) size=%g", viewCenterX, viewCenterY, viewSize)
	}
	if !activeViewport || !viewportXSet || !viewportYSet || !viewportHeightSet ||
		math.Abs(viewportX-viewCenterX) > 1e-6 || math.Abs(viewportY-viewCenterY) > 1e-6 || math.Abs(viewportHeight-viewSize) > 1e-6 {
		t.Fatalf("DXF active viewport disagrees with opening view: active=%t center=(%g,%g) height=%g", activeViewport, viewportX, viewportY, viewportHeight)
	}
	if validMapExtent(runtime.mapFitExtent) {
		wantX := runtime.mapFitExtent[0] + (runtime.mapFitExtent[2]-runtime.mapFitExtent[0])/2
		wantY := runtime.mapFitExtent[1] + (runtime.mapFitExtent[3]-runtime.mapFitExtent[1])/2
		if math.Abs(viewCenterX-wantX) > 1e-6 || math.Abs(viewCenterY-wantY) > 1e-6 {
			t.Fatalf("DXF opening view center=(%g, %g), want project geometry center=(%g, %g)", viewCenterX, viewCenterY, wantX, wantY)
		}
	}
	for name := range counts {
		if !declaredLayers[name] {
			t.Errorf("DXF entity layer %q has no LAYER table record", name)
		}
	}
	godal.RegisterAll()
	// GDAL's DXF driver may not recognize $DWGCODEPAGE="UTF-8" and can fall
	// back to ANSI_1252. Override its reader encoding without altering the
	// AC1021 UTF-8 bytes that CAD applications expect.
	parsed, err := godal.Open(destination, godal.VectorOnly(), godal.ConfigOption("DXF_ENCODING=UTF-8"))
	if err != nil {
		t.Fatalf("GDAL cannot open real project DXF: %v", err)
	}
	defer parsed.Close()
	parsedLayers := parsed.Layers()
	if len(parsedLayers) != 1 {
		t.Fatalf("GDAL DXF entity layers = %d, want 1", len(parsedLayers))
	}
	parsedCounts := make(map[string]int)
	for {
		feature := parsedLayers[0].NextFeature()
		if feature == nil {
			break
		}
		if field, exists := feature.Fields()["Layer"]; exists {
			parsedCounts[field.String()]++
		}
		feature.Close()
	}
	for _, layer := range runtime.service.ProjectLayerProperties() {
		name := layer.DisplayName
		if name == "" {
			name = layer.Name
		}
		if counts[name] == 0 {
			t.Errorf("DXF CAD layer %q has no entities; counts=%v", name, counts)
		}
		if parsedCounts[name] != counts[name] {
			t.Errorf("GDAL parsed %d entities on %q, DXF contains %d", parsedCounts[name], name, counts[name])
		}
		if layer.Labels.Enabled && entityTypes[name]["TEXT"] == 0 {
			t.Errorf("label-enabled layer %q contains no TEXT entities", name)
		}
		if strings.Contains(layer.DisplayName, "지적도근점") && entityTypes[name]["POINT"] == 0 {
			t.Errorf("survey-point layer %q contains no POINT entities", name)
		}
		if strings.Contains(layer.DisplayName, "지적도근점") && entityTypes[name]["POINT"] != entityTypes[name]["TEXT"] {
			t.Errorf("survey-point layer %q has %d point symbols but %d labels", name, entityTypes[name]["POINT"], entityTypes[name]["TEXT"])
		}
		if strings.Contains(layer.DisplayName, "건물") && layer.Style.FillOpacity > 0 && (entityTypes[name]["SOLID"] == 0 || entityTypes[name]["HATCH"] != 0) {
			t.Errorf("filled building layer %q should contain SOLID triangles and no HATCH: %v", name, entityTypes[name])
		}
	}
	t.Logf("DXF/GDAL entities by CAD layer: %v / %v", counts, parsedCounts)
	t.Logf("DXF entity types by CAD layer: %v", entityTypes)
	t.Logf("DXF entity payload bytes by layer/type: %v", entityBytes)
	t.Logf("DXF polyline vertices by layer: %v; polylines >500 vertices: %v", polylineVertices, complexPolylines)
	cp949Destination := filepath.Join(t.TempDir(), "all-layers-cp949.dxf")
	if err := runtime.exportProjectLayersDXF(cp949Destination, "ares-cp949"); err != nil {
		t.Fatalf("CP949 export of actual project: %v", err)
	}
	cp949Dataset, err := godal.Open(cp949Destination, godal.VectorOnly())
	if err != nil {
		t.Fatalf("GDAL cannot open CP949 project DXF: %v", err)
	}
	defer cp949Dataset.Close()
	cp949Layers := cp949Dataset.Layers()
	if len(cp949Layers) != 1 {
		t.Fatalf("GDAL CP949 DXF entity layers = %d, want 1", len(cp949Layers))
	}
	cp949Counts := make(map[string]int)
	for {
		feature := cp949Layers[0].NextFeature()
		if feature == nil {
			break
		}
		if field, exists := feature.Fields()["Layer"]; exists {
			cp949Counts[field.String()]++
		}
		feature.Close()
	}
	for name, count := range counts {
		if cp949Counts[name] != count {
			t.Errorf("GDAL CP949 parsed %d entities on %q, UTF-8 output contains %d", cp949Counts[name], name, count)
		}
	}
	t.Logf("GDAL CP949 entities by CAD layer: %v", cp949Counts)

	// Exercise the exact settings-dialog payload against the real three-source
	// workspace as well: every configured CAD name must replace the display
	// name without dropping geometry from either of the other layers.
	plan := make([]map[string]any, 0, len(runtime.service.ProjectLayerProperties()))
	renamedCounts := make(map[string]int)
	for _, property := range runtime.service.ProjectLayerProperties() {
		oldName := property.DisplayName
		if oldName == "" {
			oldName = property.Name
		}
		cadName := "CAD-" + oldName
		plan = append(plan, map[string]any{
			"name": property.Name, "cadName": cadName, "include": true,
			"geometry": true, "labels": true,
		})
		renamedCounts[cadName] = counts[oldName]
	}
	optionsJSON, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	renamedDestination := filepath.Join(t.TempDir(), "renamed-all-layers.dxf")
	if err := runtime.exportProjectLayersDXFWithOptions(renamedDestination, "ares-utf8", string(optionsJSON)); err != nil {
		t.Fatalf("DXF export with settings-dialog layer plan: %v", err)
	}
	renamedDataset, err := godal.Open(renamedDestination, godal.VectorOnly(), godal.ConfigOption("DXF_ENCODING=UTF-8"))
	if err != nil {
		t.Fatalf("GDAL cannot open renamed project DXF: %v", err)
	}
	defer renamedDataset.Close()
	renamedLayers := renamedDataset.Layers()
	if len(renamedLayers) != 1 {
		t.Fatalf("GDAL renamed DXF entity layers = %d, want 1", len(renamedLayers))
	}
	actualRenamedCounts := make(map[string]int)
	for {
		feature := renamedLayers[0].NextFeature()
		if feature == nil {
			break
		}
		if field, exists := feature.Fields()["Layer"]; exists {
			actualRenamedCounts[field.String()]++
		}
		feature.Close()
	}
	if len(actualRenamedCounts) != len(renamedCounts) {
		t.Fatalf("renamed DXF layers = %v, want %v", actualRenamedCounts, renamedCounts)
	}
	for name, want := range renamedCounts {
		if got := actualRenamedCounts[name]; got != want {
			t.Errorf("renamed DXF layer %q has %d entities, want %d", name, got, want)
		}
	}
	t.Logf("GDAL renamed CAD layers: %v", actualRenamedCounts)
}

func TestDesktopDXFExportKeepsAllOverlappingLabels(t *testing.T) {
	layer := core.Layer{
		Name: "routes", Visible: true, Style: core.DefaultLayerStyle(),
		Labels: core.LabelSettings{Enabled: true, Expression: "${name}", Placement: "free-angle", HeightMM: 2.5},
		Features: []core.Feature{
			{ID: 1, Geometry: core.WKTGeometry{WKT: "LINESTRING (0 0, 2 0, 2 10)"}, Properties: map[string]any{"name": "one"}},
			{ID: 2, Geometry: core.WKTGeometry{WKT: "LINESTRING (0 0, 2 0, 2 10)"}, Properties: map[string]any{"name": "two"}},
		},
	}
	runtime, err := buildDataRuntime(context.Background(), []core.Layer{layer}, "", "", "", "", "", false, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	view := native.Viewport{Width: 1000, Height: 1000, ViewportWidth: 1000, ViewportHeight: 1000, Zoom: 1}
	if visible := declutterViewportLabelsAtScale(runtime.mapLabels, view, 100000); len(visible) != 1 {
		t.Fatalf("display labels = %+v, want one after collision removal", visible)
	}
	path := filepath.Join(t.TempDir(), "all-labels.dxf")
	if err := runtime.exportProjectLayersDXF(path, "ares-utf8"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"\n0\nTEXT\n", "\n1\none\n", "\n1\ntwo\n", "\n50\n90\n"} {
		if !strings.Contains(string(data), expected) {
			t.Fatalf("DXF missing %q despite two overlapping source labels", expected)
		}
	}
	if got := strings.Count(string(data), "\n0\nTEXT\n"); got != 2 {
		t.Fatalf("DXF TEXT count = %d, want both overlapping labels", got)
	}
}

func TestApplyLayerSettingsUpdatesDisplayNameAndVisibility(t *testing.T) {
	style := core.DefaultLayerStyle()
	updatedStyle := style
	updatedStyle.LineColor = "#123456"
	labels := core.DefaultLabelSettings()
	labels.Enabled = true
	labels.Expression = "${street} ${number}"
	labels.Rule = `return feature.active == true`
	labels.LuaScript = `return feature.name`
	labels.Placement = "free-angle"
	labels.RotationField = "angle"
	labels.HeightMM = 2.5
	labels.MinScale = 1000
	labels.MaxScale = 50000
	runtime, err := buildDataRuntime(context.Background(), []core.Layer{{
		Name: "roads", DisplayName: "roads", SourcePath: "roads.shp", SourceLayerName: "roads",
		Visible: true, Style: style, Labels: core.DefaultLabelSettings(),
		Fields:   []core.Field{{Name: "street", Type: core.FieldTypeText}, {Name: "number", Type: core.FieldTypeNumber}, {Name: "active", Type: core.FieldTypeBool}, {Name: "name", Type: core.FieldTypeText}},
		Features: []core.Feature{{ID: 1, Geometry: core.WKTGeometry{WKT: "LINESTRING (0 0, 2 0)"}, Properties: map[string]any{"street": "Main", "number": 4, "active": true, "name": "Main", "angle": 0}}},
	}}, "", "", "", "", "", false, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	service := runtime.service
	payload, err := json.Marshal(layerSettingsRequest{
		Name: "roads", DisplayName: "Cadastral Roads", SourcePath: "roads.shp",
		SourceLayerName: "roads", Visible: false, Style: updatedStyle, Labels: labels,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := applyLayerSettings(runtime, string(payload)); err != nil {
		t.Fatal(err)
	}
	layer, ok := service.LayerProperties("roads")
	if !ok || layer.DisplayName != "Cadastral Roads" || layer.Visible || layer.Labels != labels || layer.Style != updatedStyle {
		t.Fatalf("updated layer properties = %+v, found=%t", layer, ok)
	}
	if got := runtime.sources["roads"].Labels; len(got) != 1 || got[0].Text != "Main" || got[0].HeightMM != 2.5 {
		t.Fatalf("updated label settings were not rebuilt into render source: %+v", got)
	}
	wantLineColor := render.ColorForGeometry(updatedStyle, "LINESTRING")
	foundLine := false
	for x := 0; x < 4 && !foundLine; x++ {
		for y := 0; y < 4 && !foundLine; y++ {
			chunk, err := runtime.builder(context.Background(), render.ChunkKey{Layer: "roads", X: x, Y: y})
			if err != nil {
				t.Fatalf("build updated style chunk: %v", err)
			}
			for _, vertex := range chunk.Vertices {
				if vertex.Kind == render.VertexLine {
					foundLine = true
					if vertex.Color != wantLineColor {
						t.Fatalf("line color = %#08x, want %#08x", vertex.Color, wantLineColor)
					}
				}
			}
		}
	}
	if !foundLine {
		t.Fatal("style test chunk did not contain line vertices")
	}
	if runtime.loadGeneration != 0 {
		t.Fatalf("non-source layer settings triggered a source reload (generation %d)", runtime.loadGeneration)
	}
	if runtime.visibleLayers["roads"] || runtime.visibility.IsVisible("roads") {
		t.Fatal("layer visibility was not updated in render state")
	}
}

func TestPolygonRuntimeBuilderPublishesClippedFillMeshWithOpacity(t *testing.T) {
	style := core.DefaultLayerStyle()
	layers := []core.Layer{{
		Name: "areas", Style: style,
		Features: []core.Feature{{ID: 7, Geometry: core.WKTGeometry{WKT: "POLYGON ((0 0, 10 0, 10 10, 0 10, 0 0))"}}},
	}}
	runtime, err := buildDataRuntime(context.Background(), layers, "", "", "", "", "", false, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	chunk, err := runtime.builder(context.Background(), render.ChunkKey{Layer: "areas", X: 0, Y: 0})
	if err != nil {
		t.Fatal(err)
	}
	fills := 0
	for _, vertex := range chunk.Vertices {
		if vertex.Kind == render.VertexFill {
			fills++
			if vertex.Color != render.ColorForPolygonFill(style) {
				t.Fatalf("polygon fill color/opacity = %#08x, want %#08x", vertex.Color, render.ColorForPolygonFill(style))
			}
		}
	}
	if fills == 0 || fills%3 != 0 {
		t.Fatalf("polygon fill mesh has %d vertices, expected triangle groups", fills)
	}
}

func TestConvexPolygonFillFastPathTriangulatesAndRejectsConcavity(t *testing.T) {
	ring := []render.Point{{X: 0, Y: 0}, {X: 1, Y: 0}, {X: 1, Y: 1}, {X: 0, Y: 1}, {X: 0, Y: 0}}
	triangles := make([][3]render.Point, 0, 2)
	if !appendConvexPolygonTriangles(ring, func(triangle [3]render.Point) {
		triangles = append(triangles, triangle)
	}) {
		t.Fatal("closed convex ring did not use the direct triangulation path")
	}
	if len(triangles) != 2 {
		t.Fatalf("convex square triangles = %d, want 2", len(triangles))
	}
	area := 0.0
	for _, triangle := range triangles {
		area += math.Abs((triangle[1].X-triangle[0].X)*(triangle[2].Y-triangle[0].Y)-
			(triangle[2].X-triangle[0].X)*(triangle[1].Y-triangle[0].Y)) / 2
	}
	if math.Abs(area-1) > 1e-12 {
		t.Fatalf("convex square triangle area = %v, want 1", area)
	}
	concave := []render.Point{{X: 0, Y: 0}, {X: 1, Y: 0}, {X: 0.4, Y: 0.4}, {X: 1, Y: 1}, {X: 0, Y: 1}, {X: 0, Y: 0}}
	if appendConvexPolygonTriangles(concave, func([3]render.Point) {
		t.Fatal("concave ring emitted direct triangles")
	}) {
		t.Fatal("concave ring was accepted by the convex fast path")
	}
}

func TestPolygonFillCapacityEstimateUsesNormalizedTileBounds(t *testing.T) {
	layer := core.Layer{
		Name: "areas",
		Features: []core.Feature{{ID: 1, Geometry: core.WKTGeometry{
			WKT: "POLYGON ((0 0, 1 0, 1 1, 0 1, 0 0))",
		}}},
	}
	sources, _, err := render.NewLayerSourcesWithExtent([]core.Layer{layer}, [4]float64{-1, -1, 9, 9})
	if err != nil {
		t.Fatal(err)
	}
	capacities := estimatePolygonFillCapacities(layer, sources["areas"], nil)
	if capacities[[2]int{0, 0}] != 6 {
		t.Fatalf("single-tile polygon fill capacity = %d, want 6", capacities[[2]int{0, 0}])
	}
	if len(capacities) != 1 {
		t.Fatalf("unexpected fill capacity cells: %v", capacities)
	}
}

func TestReadOnlyPolygonVertexBudgetRejectsComplexWindowBeforeGEOS(t *testing.T) {
	layer := core.Layer{Name: "areas", Features: []core.Feature{{Geometry: core.WKTGeometry{
		WKT: "POLYGON ((0 0, 1 0, 1 1, 0 1, 0 0))",
	}}}}
	source := render.LayerSource{Features: []render.HitFeature{{Vertices: make([]render.Point, 4)}}}
	if err := validateReadOnlyPolygonVertexBudget(layer, source, 3); err == nil || !strings.Contains(err.Error(), "triangulation safety limit") {
		t.Fatalf("over-budget polygon error = %v", err)
	}
	if err := validateReadOnlyPolygonVertexBudget(layer, source, 4); err != nil {
		t.Fatalf("polygon at budget was rejected: %v", err)
	}
}

func TestPolygonFillProjectBudgetIncludesPreviouslyBuiltLayerMeshes(t *testing.T) {
	makeLayer := func(name string, offset float64) core.Layer {
		return core.Layer{Name: name, Style: core.DefaultLayerStyle(), Features: []core.Feature{{
			ID: 1,
			Geometry: core.WKTGeometry{WKT: fmt.Sprintf(
				"POLYGON ((%f %f, %f %f, %f %f, %f %f, %f %f))",
				offset, 0.1, offset+0.1, 0.1, offset+0.1, 0.2, offset, 0.2, offset, 0.1)},
		}}}
	}
	first := makeLayer("first", 0.1)
	firstSources, _, err := render.NewLayerSourcesWithExtent([]core.Layer{first}, [4]float64{0, 0, 1, 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := attachPolygonFillGeometryWithLimit(context.Background(), []core.Layer{first}, firstSources, nil, 6); err != nil {
		t.Fatalf("build first layer fill: %v", err)
	}
	if got := firstSources[first.Name].PolygonFillVertices; got != 6 {
		t.Fatalf("first layer fill vertices = %d, want 6", got)
	}

	second := makeLayer("second", 0.4)
	secondSources, _, err := render.NewLayerSourcesWithExtent([]core.Layer{second}, [4]float64{0, 0, 1, 1})
	if err != nil {
		t.Fatal(err)
	}
	projectSources := map[string]render.LayerSource{
		first.Name:  firstSources[first.Name],
		second.Name: secondSources[second.Name],
	}
	if err := attachPolygonFillGeometryWithLimit(context.Background(), []core.Layer{second}, projectSources, nil, 11); err == nil || !strings.Contains(err.Error(), "project safety limit") {
		t.Fatalf("aggregate polygon-fill budget error = %v, want project limit", err)
	}
	if got := projectSources[first.Name].PolygonFillVertices; got != 6 {
		t.Fatalf("previous layer accounting changed to %d, want 6", got)
	}
	if got := projectSources[second.Name].PolygonFillVertices; got != 0 {
		t.Fatalf("rejected layer retained %d fill vertices, want 0", got)
	}
}

func TestReadOnlyPolygonFillBuildsOnlyRequestedChunk(t *testing.T) {
	layer := core.Layer{Name: "areas", Features: []core.Feature{{ID: 1, Geometry: core.WKTGeometry{
		WKT: "POLYGON ((0.65 0.35, 0.95 0.35, 0.95 0.7, 0.65 0.7, 0.65 0.35))",
	}}}}
	target := render.ChunkKey{Layer: "areas", X: 6, Y: 4}
	sources, _, err := render.NewLayerSourcesWithExtentAndChunkSizeForChunk([]core.Layer{layer}, [4]float64{0, 0, 1, 1}, 0.125, target)
	if err != nil {
		t.Fatal(err)
	}
	if err := attachPolygonFillGeometryForChunk(context.Background(), []core.Layer{layer}, sources, &target); err != nil {
		t.Fatal(err)
	}
	requested, err := sources["areas"].Builder(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	other, err := sources["areas"].Builder(context.Background(), render.ChunkKey{Layer: "areas", X: 5, Y: 4})
	if err != nil {
		t.Fatal(err)
	}
	fillCount := 0
	for _, vertex := range requested.Vertices {
		if vertex.Kind == render.VertexFill {
			fillCount++
		}
	}
	if fillCount == 0 || len(other.Vertices) != 0 {
		t.Fatalf("target fill vertices=%d, off-target vertices=%d", fillCount, len(other.Vertices))
	}
}

func TestPolygonWithHoleKeepsGEOSFillFallback(t *testing.T) {
	runtime, err := buildDataRuntime(context.Background(), []core.Layer{{
		Name:     "areas",
		Features: []core.Feature{{ID: 1, Geometry: core.WKTGeometry{WKT: "POLYGON ((0 0, 10 0, 10 10, 0 10, 0 0), (3 3, 3 7, 7 7, 7 3, 3 3))"}}},
	}}, "", "", "", "", "", false, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	area, fillVertices := 0.0, 0
	for cellY := 0; cellY < 4; cellY++ {
		for cellX := 0; cellX < 4; cellX++ {
			chunk, err := runtime.builder(context.Background(), render.ChunkKey{Layer: "areas", X: cellX, Y: cellY})
			if err != nil {
				t.Fatal(err)
			}
			triangle := [3]render.Vertex{}
			triangleSize := 0
			for _, vertex := range chunk.Vertices {
				if vertex.Kind != render.VertexFill {
					continue
				}
				triangle[triangleSize] = vertex
				triangleSize++
				fillVertices++
				if triangleSize == len(triangle) {
					a, b, c := triangle[0], triangle[1], triangle[2]
					area += math.Abs(float64(b.X-a.X)*float64(c.Y-a.Y)-
						float64(c.X-a.X)*float64(b.Y-a.Y)) / 2
					triangleSize = 0
				}
			}
			if triangleSize != 0 {
				t.Fatalf("cell (%d,%d) returned an incomplete fill triangle", cellX, cellY)
			}
		}
	}
	if fillVertices == 0 || math.Abs(area-0.84) > 1e-5 {
		t.Fatalf("polygon fill vertices/area = %d/%.8f, want non-empty mesh with area 0.84", fillVertices, area)
	}
}

func TestPolygonFillCanBeEnabledAfterLoadingWithZeroOpacity(t *testing.T) {
	style := core.DefaultLayerStyle()
	style.FillOpacity = 0
	runtime, err := buildDataRuntime(context.Background(), []core.Layer{{
		Name: "areas", DisplayName: "areas", SourcePath: "areas.gpkg", SourceLayerName: "areas", Visible: true, Style: style,
		Features: []core.Feature{{ID: 1, Geometry: core.WKTGeometry{WKT: "POLYGON ((0 0, 4 0, 4 4, 0 4, 0 0))"}}},
	}}, "", "", "", "", "", false, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	before, err := runtime.builder(context.Background(), render.ChunkKey{Layer: "areas", X: 0, Y: 0})
	if err != nil {
		t.Fatal(err)
	}
	beforeOutlines := 0
	for _, vertex := range before.Vertices {
		if vertex.Kind == render.VertexFill {
			t.Fatal("fully transparent polygon generated an invisible fill mesh")
		}
		if vertex.Kind == render.VertexLine {
			beforeOutlines++
		}
	}
	if beforeOutlines == 0 {
		t.Fatal("transparent polygon lost its visible outline")
	}
	updated := style
	updated.FillOpacity = 0.6
	payload, err := json.Marshal(layerSettingsRequest{
		Name: "areas", DisplayName: "areas", SourcePath: "areas.gpkg", SourceLayerName: "areas", Visible: true, Style: updated,
		Labels: core.DefaultLabelSettings(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := applyLayerSettings(runtime, string(payload)); err != nil {
		t.Fatalf("apply opacity setting: %v", err)
	}
	chunk, err := runtime.builder(context.Background(), render.ChunkKey{Layer: "areas", X: 0, Y: 0})
	if err != nil {
		t.Fatal(err)
	}
	want := render.ColorForPolygonFill(updated)
	for _, vertex := range chunk.Vertices {
		if vertex.Kind == render.VertexFill && vertex.Color == want {
			return
		}
	}
	t.Fatalf("fill mesh did not become visible after opacity update; expected fill color %#08x", want)
}

func TestReadOnlyPolygonFillOpacityChangeUpdatesWindowBuilder(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "parcels.shp")
	if err := (gdal.Writer{}).Write(ctx, path, core.Layer{
		Name: "parcels", CRS: core.CRS{AuthorityCode: "EPSG:5186"},
		Features: []core.Feature{{ID: 1, Geometry: core.WKTGeometry{
			WKT: "POLYGON ((0 0, 4 0, 4 4, 0 4, 0 0))",
		}}},
	}); err != nil {
		t.Fatal(err)
	}
	style := core.DefaultLayerStyle()
	style.FillOpacity = 0
	runtime, err := loadReadOnlyDataRuntime(ctx, []vectorSourceSpec{{Path: path, Style: style}}, "")
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.closeAttributeSource()
	name := runtime.service.LayerNames()[0]
	properties, ok := runtime.service.LayerProperties(name)
	if !ok {
		t.Fatalf("missing read-only layer %q", name)
	}
	key := chunkKeyForPoint(runtime.mapExtent, readOnlyWindowChunkSize(0), name, render.Point{X: 2, Y: 2})
	before, err := runtime.builder(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	updated := properties.Style
	updated.FillOpacity = 0.6
	displayName := properties.DisplayName
	if displayName == "" {
		displayName = name
	}
	payload, err := json.Marshal(layerSettingsRequest{
		Name: name, DisplayName: displayName, SourcePath: properties.SourcePath,
		SourceLayerName: properties.SourceLayerName, SourceEncoding: properties.SourceEncoding,
		SourceCRS: properties.SourceCRS, Visible: properties.Visible, Style: updated,
		Labels: properties.Labels, DisplayRule: properties.DisplayRule,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := applyLayerSettings(runtime, string(payload)); err != nil {
		t.Fatal(err)
	}
	after, err := runtime.builder(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	countFills := func(chunk render.Chunk) (int, int) {
		fills, zeroAlpha := 0, 0
		for _, vertex := range chunk.Vertices {
			if vertex.Kind == render.VertexFill {
				fills++
				if vertex.Color&0xff == 0 {
					zeroAlpha++
				}
			}
		}
		return fills, zeroAlpha
	}
	beforeFills, beforeZeroAlpha := countFills(before)
	afterFills, afterZeroAlpha := countFills(after)
	if beforeFills != 0 || beforeZeroAlpha != 0 || afterFills == 0 || afterZeroAlpha != 0 {
		t.Fatalf("read-only fill alpha before=%d/%d after=%d/%d; expected no invisible mesh before and visible fill after style update",
			beforeZeroAlpha, beforeFills, afterZeroAlpha, afterFills)
	}
	if got := runtime.readOnlyBindings[name].layer.Style.FillOpacity; got != updated.FillOpacity {
		t.Fatalf("window binding fill opacity=%g, want %g", got, updated.FillOpacity)
	}
	if len(runtime.readOnlySources) != 1 || runtime.readOnlySources[0].Style.FillOpacity != updated.FillOpacity {
		t.Fatalf("reload source kept stale style: %#v", runtime.readOnlySources)
	}
}

func TestReadOnlyOverviewKeepsAttributesForConfiguredLabels(t *testing.T) {
	labels := core.LabelSettings{Enabled: true, Expression: "${JIBUN}", Placement: "center", HeightMM: 2.5}
	if !readOnlyWindowNeedsProperties(labels, true, false) {
		t.Fatal("labeled overview must read attributes")
	}
	if readOnlyWindowNeedsProperties(core.DefaultLabelSettings(), true, false) {
		t.Fatal("unlabeled overview should retain geometry-only reads")
	}
	if readOnlyWindowNeedsProperties(labels, true, true) {
		t.Fatal("dissolved polygon boundary has no parcel label anchors")
	}
	if !readOnlyWindowNeedsProperties(core.DefaultLabelSettings(), false, false) {
		t.Fatal("detailed view needs attributes for feature inspection")
	}
	layers := []core.Layer{{Name: "parcels", Labels: labels, Features: []core.Feature{{
		ID: 1, Geometry: core.WKTGeometry{WKT: "POLYGON ((0 0, 10 0, 10 10, 0 10, 0 0))"},
		Properties: map[string]any{"JIBUN": "54-60 대"},
	}}}}
	if err := prepareLayerLabels(context.Background(), layers); err != nil {
		t.Fatal(err)
	}
	if got := layers[0].Features[0].Label; got == nil || got.Text != "54-60 대" {
		t.Fatalf("overview label = %+v", got)
	}
}

func TestReadOnlyOverviewLabelCandidatesStayBounded(t *testing.T) {
	window := core.Layer{Labels: core.LabelSettings{Enabled: true, Expression: "${JIBUN}", Placement: "center", HeightMM: 2.5}}
	for index := 0; index < 100; index++ {
		window.Features = append(window.Features, core.Feature{ID: uint64(index), Properties: map[string]any{"JIBUN": fmt.Sprint(index)}})
	}
	if err := prepareReadOnlyWindowLabels(context.Background(), &window, true, 0); err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, feature := range window.Features {
		if feature.Label != nil {
			count++
		}
	}
	if count != maxReadOnlyOverviewLabelsPerChunk || window.Features[0].Label.Text != "0" ||
		window.Features[3].Label.Text != "3" || window.Features[96].Label.Text != "96" {
		t.Fatalf("overview label candidates: count=%d first=%+v next=%+v last=%+v", count,
			window.Features[0].Label, window.Features[3].Label, window.Features[96].Label)
	}
}

func TestReadOnlyCityScaleLabelCandidatesStayBounded(t *testing.T) {
	for _, test := range []struct {
		lod, want int
	}{{2, 8}, {3, 32}, {4, 128}, {5, 200}} {
		window := core.Layer{Labels: core.LabelSettings{Enabled: true, Expression: "${JIBUN}", Placement: "center", HeightMM: 2.5}}
		for index := 0; index < 200; index++ {
			window.Features = append(window.Features, core.Feature{ID: uint64(index), Properties: map[string]any{"JIBUN": fmt.Sprint(index)}})
		}
		if err := prepareReadOnlyWindowLabels(context.Background(), &window, false, test.lod); err != nil {
			t.Fatal(err)
		}
		count := 0
		for _, feature := range window.Features {
			if feature.Label != nil {
				count++
			}
		}
		if count != test.want {
			t.Errorf("LOD %d label candidates = %d, want %d", test.lod, count, test.want)
		}
	}
}

func TestReadOnlyOverviewBuildsLabelsFromSourceAttributes(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "parcels.shp")
	labels := core.LabelSettings{Enabled: true, Expression: "${JIBUN}", Placement: "center", HeightMM: 2.5}
	layer := core.Layer{
		Name: "parcels", CRS: core.CRS{AuthorityCode: "EPSG:5186"},
		Fields: []core.Field{{Name: "JIBUN", Type: core.FieldTypeText}},
		Features: []core.Feature{{ID: 1, Geometry: core.WKTGeometry{WKT: "POLYGON ((0 0, 0 10, 10 10, 10 0, 0 0))"},
			Properties: map[string]any{"JIBUN": "54-60"}}},
	}
	if err := (gdal.Writer{}).Write(ctx, path, layer); err != nil {
		t.Fatal(err)
	}
	runtime, err := loadReadOnlyDataRuntime(ctx, []vectorSourceSpec{{Path: path, Labels: labels}}, "")
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.closeAttributeSource()
	key := chunkKeyForPoint(runtime.mapExtent, readOnlyWindowChunkSize(-7), "parcels", render.Point{X: 5, Y: 5})
	key.ZoomBucket = -7
	runtime.mu.Lock()
	runtime.windowVisibleKeys[key] = struct{}{}
	runtime.mu.Unlock()
	if _, err := runtime.builder(ctx, key); err != nil {
		t.Fatal(err)
	}
	runtime.mu.Lock()
	got := append([]render.LayerLabel(nil), runtime.windowLabels[key]...)
	retained := runtime.windowPayloadBytes[key]
	runtime.removeWindowChunkLocked(key)
	released := runtime.windowVisiblePayloadBytes
	runtime.mu.Unlock()
	if len(got) == 0 || got[0].Text != "54-60" {
		t.Fatalf("overview source labels = %+v", got)
	}
	if retained == 0 || released != 0 {
		t.Fatalf("overview label payload accounting: retained=%d released=%d", retained, released)
	}
}

func TestReplacedReadOnlyRuntimePublishesWindowLabelsToActiveRuntime(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "parcels.shp")
	layer := core.Layer{
		Name: "parcels", CRS: core.CRS{AuthorityCode: "EPSG:5186"},
		Fields: []core.Field{{Name: "JIBUN", Type: core.FieldTypeText}},
		Features: []core.Feature{{ID: 1, Geometry: core.WKTGeometry{WKT: "POLYGON ((0 0, 0 10, 10 10, 10 0, 0 0))"},
			Properties: map[string]any{"JIBUN": "54-60"}}},
	}
	if err := (gdal.Writer{}).Write(ctx, path, layer); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadReadOnlyDataRuntime(ctx, []vectorSourceSpec{{Path: path, Labels: core.LabelSettings{
		Enabled: true, Expression: "${JIBUN}", Placement: "center", HeightMM: 2.5,
	}}}, "")
	if err != nil {
		t.Fatal(err)
	}
	active := newEmptyProjectRuntime()
	active.replaceWithLoaded(loaded, 0)
	defer active.closeAttributeSource()
	active.mu.Lock()
	if active.cancel != nil {
		active.cancel()
	}
	active.scheduler.AdvanceGeneration()
	key := chunkKeyForPoint(active.mapExtent, readOnlyWindowChunkSize(-7), "parcels", render.Point{X: 5, Y: 5})
	key.ZoomBucket = -7
	active.windowVisibleKeys[key] = struct{}{}
	active.mu.Unlock()
	if _, err := active.builder(ctx, key); err != nil {
		t.Fatal(err)
	}
	active.mu.Lock()
	labels := append([]render.LayerLabel(nil), active.mapLabels...)
	active.mu.Unlock()
	if len(labels) == 0 || labels[0].Text != "54-60" {
		t.Fatalf("active runtime labels after build = %+v", labels)
	}
	if len(loaded.mapLabels) != 0 {
		t.Fatalf("temporary runtime retained built labels: %+v", loaded.mapLabels)
	}
	active.mu.Lock()
	active.windowFeatureNames[123] = "active parcel"
	active.mu.Unlock()
	feature, err := active.attributeFeatureReader(ctx, "parcels", 123)
	if err != nil || feature.Properties["name"] != "active parcel" {
		t.Fatalf("active runtime feature lookup = %+v, %v", feature, err)
	}
}

func TestReadOnlySavedWorkspaceLabelsAtSavedView(t *testing.T) {
	path := os.Getenv("GOGIS_TEST_LABEL_WORKSPACE")
	if path == "" {
		t.Skip("set GOGIS_TEST_LABEL_WORKSPACE to a local labeled .gogis project")
	}
	runtime, err := loadWorkspaceRuntime(context.Background(), path, true, "")
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.closeAttributeSource()
	if runtime.workspaceView == nil {
		t.Fatal("workspace has no saved view")
	}
	view := *runtime.workspaceView
	bucket := readOnlyWindowZoomBucket(view.Zoom)
	center := render.Point{
		X: runtime.mapExtent[0] + view.CenterX*(runtime.mapExtent[2]-runtime.mapExtent[0]),
		Y: runtime.mapExtent[1] + view.CenterY*(runtime.mapExtent[3]-runtime.mapExtent[1]),
	}
	found := false
	for _, name := range runtime.service.LayerNames() {
		layer, ok := runtime.service.LayerProperties(name)
		if !ok || !layer.Labels.Enabled {
			continue
		}
		key := chunkKeyForPoint(runtime.mapExtent, readOnlyWindowChunkSize(bucket), name, center)
		key.ZoomBucket = bucket
		runtime.mu.Lock()
		runtime.windowVisibleKeys[key] = struct{}{}
		runtime.mu.Unlock()
		if _, err := runtime.builder(context.Background(), key); err != nil {
			t.Fatalf("build %s saved-view chunk: %v", name, err)
		}
		runtime.mu.Lock()
		labels := append([]render.LayerLabel(nil), runtime.windowLabels[key]...)
		runtime.mu.Unlock()
		for _, label := range labels {
			if label.Text != "" {
				found = true
				break
			}
		}
	}
	if !found {
		t.Fatalf("no nonempty labels near saved view center %v at zoom %g", center, view.Zoom)
	}
	viewport := native.Viewport{Zoom: view.Zoom, Width: 1440, Height: 960, ViewportWidth: 1440, ViewportHeight: 960}
	viewport.PanX = (0.5 - view.CenterX) * viewport.Width * viewport.Zoom
	viewport.PanY = (view.CenterY - 0.5) * viewport.Height * viewport.Zoom
	visible, _ := viewportLayerLabels(append([]render.LayerLabel(nil), runtime.mapLabels...), viewport, maxLayerLabelCount)
	if len(visible) == 0 {
		t.Fatalf("generated %d labels but none pass viewport culling at saved center %v zoom %g", len(runtime.mapLabels), center, view.Zoom)
	}
	inside := 0
	for _, label := range visible {
		x := viewport.ViewportWidth/2 + (label.X-view.CenterX)*viewport.Width*viewport.Zoom
		y := viewport.ViewportHeight/2 - (label.Y-view.CenterY)*viewport.Height*viewport.Zoom
		if x >= 0 && x <= viewport.ViewportWidth && y >= 0 && y <= viewport.ViewportHeight {
			inside++
		}
	}
	if inside == 0 {
		t.Fatalf("generated %d labels, but none appear within the saved viewport", len(runtime.mapLabels))
	}
	t.Logf("saved view labels: generated=%d viewport=%d on-screen=%d decluttered=%d first=%+v", len(runtime.mapLabels), len(visible), inside, len(declutterViewportLabels(visible, viewport)), visible[0])
}

func TestConfiguredLabelsUseTemplateLuaRuleAndRenderPlacement(t *testing.T) {
	layers := []core.Layer{{
		Name: "roads",
		Labels: core.LabelSettings{
			Enabled: true, Expression: "${street} ${number}", Rule: `return feature.kind == "primary"`,
			Placement: "center-rotated", RotationField: "angle", HeightMM: 2.5, MinScale: 1000, MaxScale: 50000,
		},
		Features: []core.Feature{{ID: 7, Geometry: core.WKTGeometry{WKT: "LINESTRING (0 0, 2 0)"}, Properties: map[string]any{
			"street": "한강로", "number": 12, "kind": "primary", "angle": 30,
		}}},
	}}
	if err := prepareLayerLabels(context.Background(), layers); err != nil {
		t.Fatal(err)
	}
	source, err := render.NewLayerSource(layers[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(source.Labels) != 1 {
		t.Fatalf("layer labels = %#v", source.Labels)
	}
	label := source.Labels[0]
	if label.Text != "한강로 12" || label.FeatureID != 7 || label.Rotation != 30 || label.HeightMM != 2.5 || label.MinScale != 1000 || label.MaxScale != 50000 {
		t.Fatalf("configured label = %#v", label)
	}
}

func TestConfiguredLabelOrientationOptions(t *testing.T) {
	for _, test := range []struct {
		placement string
		angle     float64
		x, y      float64
	}{
		{placement: "center", angle: 0},
		{placement: "vertical", angle: 90},
		{placement: "free-angle", angle: 90, x: 10, y: 10},
	} {
		t.Run(test.placement, func(t *testing.T) {
			layers := []core.Layer{{
				Name: "road", Labels: core.LabelSettings{Enabled: true, Expression: "${name}", Placement: test.placement, RotationField: "angle", HeightMM: 2.5},
				Features: []core.Feature{{ID: 1, Geometry: core.WKTGeometry{WKT: "LINESTRING (0 0, 10 0, 10 20)"}, Properties: map[string]any{"name": "main"}}},
			}}
			if err := prepareLayerLabels(context.Background(), layers); err != nil {
				t.Fatal(err)
			}
			label := layers[0].Features[0].Label
			if label == nil || label.Rotation != test.angle {
				t.Fatalf("prepared label = %+v, want rotation %g", label, test.angle)
			}
			if test.placement == "free-angle" && (!label.AnchorSet || label.X != test.x || label.Y != test.y) {
				t.Fatalf("free-angle anchor = %+v, want longest-segment center", label)
			}
			source, err := render.NewLayerSource(layers[0])
			if err != nil {
				t.Fatal(err)
			}
			if len(source.Labels) != 1 || source.Labels[0].Rotation != test.angle {
				t.Fatalf("render labels = %+v, want rotation %g", source.Labels, test.angle)
			}
		})
	}
}

func TestPointFeatureLabelUsesFeatureCoordinates(t *testing.T) {
	layers := []core.Layer{{
		Name:   "survey-control-points",
		Labels: core.LabelSettings{Enabled: true, Expression: "${DOGEUN_POI}", Placement: "center", HeightMM: 2.5},
		Features: []core.Feature{{
			ID: 17, Geometry: core.WKTGeometry{WKT: "POINT (225359.85 440025.25)"},
			Properties: map[string]any{"DOGEUN_POI": "도근점-17"},
		}},
	}}
	if err := prepareLayerLabels(context.Background(), layers); err != nil {
		t.Fatal(err)
	}
	label := layers[0].Features[0].Label
	if label == nil || !label.AnchorSet {
		t.Fatalf("point label has no explicit geometry anchor: %+v", label)
	}
	if math.Abs(label.X-225359.85) > 1e-6 || math.Abs(label.Y-440025.25) > 1e-6 {
		t.Fatalf("point label anchor = (%v, %v), want feature coordinate", label.X, label.Y)
	}
}

func TestFreeAnglePolygonKeepsInteriorAnchor(t *testing.T) {
	layers := []core.Layer{{
		Name: "parcel", Labels: core.LabelSettings{Enabled: true, Expression: "${name}", Placement: "free-angle", HeightMM: 2.5},
		Features: []core.Feature{{ID: 1, Geometry: core.WKTGeometry{WKT: "POLYGON ((0 0, 5 0, 5 20, 0 20, 0 0))"}, Properties: map[string]any{"name": "parcel"}}},
	}}
	if err := prepareLayerLabels(context.Background(), layers); err != nil {
		t.Fatal(err)
	}
	label := layers[0].Features[0].Label
	if label == nil || !label.AnchorSet || label.Rotation != 90 || label.X <= 0 || label.X >= 5 || label.Y <= 0 || label.Y >= 20 {
		t.Fatalf("polygon label = %+v, want an interior anchor and north-south angle", label)
	}
}

func TestConfiguredLuaLabelComposerPreservesUnmatchedFeatures(t *testing.T) {
	layers := []core.Layer{{
		Name: "roads",
		Labels: core.LabelSettings{
			Enabled:   true,
			LuaScript: `return string.format("%s · %d차선", feature.name, feature.lanes)`,
			Rule:      `return feature.kind == "primary"`,
			HeightMM:  2.5,
		},
		Features: []core.Feature{
			{ID: 1, Geometry: core.WKTGeometry{WKT: "POINT (1 2)"}, Properties: map[string]any{"name": "한강로", "lanes": 4, "kind": "primary"}},
			{ID: 2, Geometry: core.WKTGeometry{WKT: "POINT (3 4)"}, Properties: map[string]any{"name": "세종로", "lanes": 2, "kind": "local"}},
		},
	}}
	if err := prepareLayerLabels(context.Background(), layers); err != nil {
		t.Fatal(err)
	}
	if len(layers[0].Features) != 2 {
		t.Fatalf("label rule filtered map features, got %d", len(layers[0].Features))
	}
	if label := layers[0].Features[0].Label; label == nil || label.Text != "한강로 · 4차선" || label.Height != 2.5 {
		t.Fatalf("composed Lua label = %+v", label)
	}
	if layers[0].Features[1].Label != nil {
		t.Fatalf("nonmatching feature should remain but not receive a label: %+v", layers[0].Features[1])
	}
}

func TestPreparedLabelsRespectPerFeatureAndProjectByteBudgets(t *testing.T) {
	settings := core.LabelSettings{Enabled: true, Expression: "${name}"}
	makeLayers := func(values ...string) []core.Layer {
		features := make([]core.Feature, len(values))
		for index, value := range values {
			features[index] = core.Feature{ID: uint64(index + 1), Properties: map[string]any{"name": value}}
		}
		return []core.Layer{{Name: "roads", Labels: settings, Features: features}}
	}
	if err := prepareLayerLabelsCoreWithBudget(context.Background(), makeLayers("12345"), 100, 4); err == nil || !strings.Contains(err.Error(), "feature 1 label exceeds") {
		t.Fatalf("per-feature label limit error = %v", err)
	}
	if err := prepareLayerLabelsCoreWithBudget(context.Background(), makeLayers("1234", "5678"), 6, 4); err == nil || !strings.Contains(err.Error(), "project safety limit") {
		t.Fatalf("aggregate label limit error = %v", err)
	}
	allowed := makeLayers("1234", "5678")
	if err := prepareLayerLabelsCoreWithBudget(context.Background(), allowed, 8, 4); err != nil {
		t.Fatalf("labels at exact budget rejected: %v", err)
	}
}

func TestConfiguredLuaLabelComposerHasExecutionLimit(t *testing.T) {
	layers := []core.Layer{{
		Name: "roads",
		Labels: core.LabelSettings{
			Enabled: true, LuaScript: `while true do end; return "never"`,
		},
		Features: []core.Feature{{ID: 1, Properties: map[string]any{"name": "road"}}},
	}}
	started := time.Now()
	err := prepareLayerLabelsWithMaximumDuration(context.Background(), layers, 25*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "context deadline exceeded") {
		t.Fatalf("non-terminating label script error = %v, want deadline exceeded", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("label script execution limit took %s", elapsed)
	}
}

func TestConfiguredLabelRotationRejectsMissingOrNonFiniteValues(t *testing.T) {
	tests := []struct {
		name       string
		properties map[string]any
		wantError  string
	}{
		{name: "missing field", properties: map[string]any{"name": "Road"}, wantError: `rotation field "angle" is missing`},
		{name: "non-numeric", properties: map[string]any{"name": "Road", "angle": "north"}, wantError: `rotation field "angle" must contain a finite number`},
		{name: "not finite", properties: map[string]any{"name": "Road", "angle": "NaN"}, wantError: `rotation field "angle" must contain a finite number`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			layers := []core.Layer{{
				Name: "roads", Labels: core.LabelSettings{Enabled: true, Expression: "${name}", Placement: "center-rotated", RotationField: "angle"},
				Features: []core.Feature{{ID: 9, Geometry: core.WKTGeometry{WKT: "POINT (1 2)"}, Properties: test.properties}},
			}}
			err := prepareLayerLabels(context.Background(), layers)
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("prepareLayerLabels error = %v, want substring %q", err, test.wantError)
			}
		})
	}
}

func TestPolygonLabelIsAnchoredOnConcaveInterior(t *testing.T) {
	layers := []core.Layer{{
		Name:   "areas",
		Labels: core.LabelSettings{Enabled: true, Expression: "${name}", Placement: "center", HeightMM: 2.5},
		Features: []core.Feature{{ID: 3, Geometry: core.WKTGeometry{WKT: "POLYGON ((0 0, 10 0, 10 4, 4 4, 4 10, 0 10, 0 0))"},
			Properties: map[string]any{"name": "L area"}}},
	}}
	if err := prepareLayerLabels(context.Background(), layers); err != nil {
		t.Fatal(err)
	}
	feature := layers[0].Features[0]
	if feature.Label == nil || !feature.Label.AnchorSet {
		t.Fatalf("polygon label has no explicit interior anchor: %+v", feature.Label)
	}
	anchorX, anchorY := feature.Label.X, feature.Label.Y
	if anchorX < 0 || anchorY < 0 || anchorX > 10 || anchorY > 10 || anchorX > 4 && anchorY > 4 {
		t.Fatalf("polygon label lies outside the concave polygon: (%v, %v)", anchorX, anchorY)
	}
	source, err := render.NewLayerSource(layers[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(source.Labels) != 1 || math.Abs(source.Labels[0].X-anchorX/10) > 1e-12 || math.Abs(source.Labels[0].Y-anchorY/10) > 1e-12 {
		t.Fatalf("normalized polygon label = %+v, expected (%v, %v)", source.Labels, anchorX/10, anchorY/10)
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

func TestLoadedRuntimeReplacementTransfersAndClearsSpatialState(t *testing.T) {
	key := render.ChunkKey{Layer: "large", X: 3, Y: 4}
	sourceMu := &sync.RWMutex{}
	loaded := &demoRuntime{
		sources:                   map[string]render.LayerSource{"large": {}},
		sourcesMu:                 sourceMu,
		mapExtent:                 [4]float64{0, 0, 100, 100},
		mapFitExtent:              [4]float64{10, 20, 30, 40},
		viewportReadOnly:          true,
		windowHits:                map[render.ChunkKey][]render.HitFeature{key: {{FeatureID: 77}}},
		windowFeatureCounts:       map[render.ChunkKey]int{key: 1},
		windowFeatureIDs:          map[render.ChunkKey][]uint64{key: {77}},
		windowVisibleFeatureCount: 1,
		windowPayloadBytes:        map[render.ChunkKey]int64{key: 128},
		windowVisiblePayloadBytes: 128,
		windowVisibleKeys:         map[render.ChunkKey]struct{}{key: {}},
		windowFeatureNames:        map[uint64]string{77: "parcel"},
		windowLabels:              map[render.ChunkKey][]render.LayerLabel{key: {{Text: "parcel"}}},
		nextWindowFeatureID:       77,
		readOnlyBaseLayers:        []core.Layer{{Name: "base"}},
		readOnlyBaseFeatures:      []render.HitFeature{{FeatureID: 8}},
		readOnlyBaseLabels:        []render.LayerLabel{{Text: "base"}},
	}
	current := &demoRuntime{
		sources:              map[string]render.LayerSource{"old": {}},
		sourcesMu:            &sync.RWMutex{},
		mapExtent:            [4]float64{-1, -1, 1, 1},
		mapFitExtent:         [4]float64{-1, -1, 1, 1},
		viewportReadOnly:     true,
		windowHits:           map[render.ChunkKey][]render.HitFeature{key: {{FeatureID: 1}}},
		windowFeatureNames:   map[uint64]string{1: "old"},
		readOnlyBaseFeatures: []render.HitFeature{{FeatureID: 1}},
	}

	current.mu.Lock()
	current.adoptLoadedSpatialStateLocked(loaded)
	current.mu.Unlock()
	if _, ok := current.sources["large"]; !current.viewportReadOnly || current.sourcesMu != sourceMu || !ok {
		t.Fatal("loaded render sources or viewport mode were not transferred")
	}
	if current.mapExtent != loaded.mapExtent || current.mapFitExtent != loaded.mapFitExtent {
		t.Fatalf("loaded canvas/fit extents were not transferred: canvas=%v fit=%v; want canvas=%v fit=%v",
			current.mapExtent, current.mapFitExtent, loaded.mapExtent, loaded.mapFitExtent)
	}
	if len(current.windowHits[key]) != 1 || current.windowHits[key][0].FeatureID != 77 ||
		current.windowFeatureCounts[key] != 1 || current.windowFeatureIDs[key][0] != 77 ||
		current.windowVisibleFeatureCount != 1 || current.windowPayloadBytes[key] != 128 ||
		current.windowVisiblePayloadBytes != 128 || len(current.windowVisibleKeys) != 1 ||
		current.windowFeatureNames[77] != "parcel" || current.windowLabels[key][0].Text != "parcel" ||
		current.nextWindowFeatureID != 77 || len(current.readOnlyBaseLayers) != 1 ||
		current.readOnlyBaseFeatures[0].FeatureID != 8 || current.readOnlyBaseLabels[0].Text != "base" {
		t.Fatal("loaded viewport feature state was not transferred completely")
	}

	// Replacing a windowed project with a normal runtime must release every
	// old window map and disable window-specific refresh behavior.
	plain := &demoRuntime{sources: map[string]render.LayerSource{}, sourcesMu: &sync.RWMutex{}}
	current.mu.Lock()
	current.adoptLoadedSpatialStateLocked(plain)
	current.mu.Unlock()
	if current.viewportReadOnly || len(current.windowHits) != 0 || len(current.windowFeatureCounts) != 0 ||
		len(current.windowFeatureIDs) != 0 || current.windowVisibleFeatureCount != 0 ||
		len(current.windowPayloadBytes) != 0 || current.windowVisiblePayloadBytes != 0 ||
		len(current.windowVisibleKeys) != 0 || len(current.windowFeatureNames) != 0 ||
		len(current.windowLabels) != 0 || current.nextWindowFeatureID != 0 ||
		len(current.readOnlyBaseLayers) != 0 || len(current.readOnlyBaseFeatures) != 0 || len(current.readOnlyBaseLabels) != 0 {
		t.Fatal("replacing with a non-windowed runtime retained stale viewport state")
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
	if preview == nil || len(preview.features) != previewFeatureLimit {
		got := 0
		if preview != nil {
			got = len(preview.features)
		}
		t.Fatalf("preview feature count = %d, want %d", got, previewFeatureLimit)
	}
	for _, name := range preview.service.LayerNames() {
		if !preview.visibleLayers[name] || !preview.visibility.IsVisible(name) {
			t.Fatalf("preview layer %q visibility diverged: tree=%t renderer=%t",
				name, preview.visibleLayers[name], preview.visibility.IsVisible(name))
		}
	}
	if !full.viewportReadOnly {
		t.Fatal("compatible large source did not use viewport-backed read-only loading")
	}
	if full.viewportReadOnly {
		if len(full.features) != 0 {
			t.Fatalf("windowed full runtime eagerly loaded %d features", len(full.features))
		}
		previewPoint := preview.features[previewFeatureLimit-1].Vertices[0]
		key := chunkKeyForPoint([4]float64{0, 0, 1, 1}, readOnlyWindowChunkSize(2), "large", previewPoint)
		key.ZoomBucket = 2
		full.mu.Lock()
		full.windowVisibleKeys[key] = struct{}{}
		full.mu.Unlock()
		if _, err := full.builder(context.Background(), key); err != nil {
			t.Fatalf("build preview comparison window: %v", err)
		}
		foundPreviewBoundaryFeature := false
		for _, feature := range full.features {
			if len(feature.Vertices) > 0 && math.Abs(feature.Vertices[0].X-previewPoint.X) < 1e-6 &&
				math.Abs(feature.Vertices[0].Y-previewPoint.Y) < 1e-6 {
				foundPreviewBoundaryFeature = true
				break
			}
		}
		if len(full.features) == 0 || !foundPreviewBoundaryFeature {
			t.Fatalf("preview feature not found in its full-data window: preview=%d window=%d point=%v key=%v", len(preview.features), len(full.features), previewPoint, key)
		}
	} else if len(full.features) != previewMinimumFeatures || preview.features[previewFeatureLimit-1].Vertices[0] != full.features[previewFeatureLimit-1].Vertices[0] {
		t.Fatalf("preview/full feature counts or coordinates differ: preview=%d full=%d", len(preview.features), len(full.features))
	}
	if preview.closeAttributeSource != nil || preview.attributeFeatureReader != nil {
		t.Fatal("preview retained the full loader's attribute session")
	}
}

func TestBuildDataRuntimeKeepsLayerVisibilityStatesInSync(t *testing.T) {
	runtime, err := buildDataRuntime(context.Background(), []core.Layer{{
		Name: "hidden",
		Features: []core.Feature{{
			ID:       1,
			Geometry: core.WKTGeometry{WKT: "POINT (0.5 0.5)"},
		}},
	}}, "", "", "", "", "", false, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.visibleLayers["hidden"] || runtime.visibility.IsVisible("hidden") {
		t.Fatalf("hidden layer visibility diverged: tree=%t renderer=%t",
			runtime.visibleLayers["hidden"], runtime.visibility.IsVisible("hidden"))
	}
}

func TestWindowedReadOnlyBaseLayerBudgetCheckedBeforeOpeningSources(t *testing.T) {
	var nested any = "leaf"
	for depth := 0; depth <= 64; depth++ {
		nested = []any{nested}
	}
	baseLayers := []core.Layer{{
		Name: "oversized-base",
		Features: []core.Feature{{
			ID: 1, Properties: map[string]any{"nested": nested},
		}},
	}}
	runtime, eligible, err := tryLoadWindowedReadOnlyRuntimeWithBaseLayers(
		context.Background(), []vectorSourceSpec{{Path: "source-does-not-exist.shp"}}, "", baseLayers, nil)
	if err == nil || !eligible || runtime != nil || !strings.Contains(err.Error(), "read-only base project") {
		t.Fatalf("windowed loader result runtime=%v eligible=%t err=%v; want early base budget rejection", runtime, eligible, err)
	}
}

func TestWindowedReadOnlyRuntimeTakesOwnershipOfPreviewSession(t *testing.T) {
	path := largeReadOnlyFixture(t)
	session, err := gdal.OpenAttributeSession(path)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	tryReadOnlyPreview(context.Background(), session, "", "", "", path, "", func(*demoRuntime) {})

	runtime, ok, err := tryLoadWindowedReadOnlyRuntimeWithSession(context.Background(), []vectorSourceSpec{{Path: path}}, "", session)
	if err != nil || !ok {
		t.Fatalf("windowed runtime = %v, %t, want success: %v", runtime, ok, err)
	}
	if _, err := session.Inspect(context.Background()); err != nil {
		t.Fatalf("transferred preview session is not live: %v", err)
	}
	runtime.closeAttributeSource()
	if _, err := session.Inspect(context.Background()); err == nil {
		t.Fatal("runtime close did not release the transferred GDAL session")
	}
}

func TestWindowedReadOnlyRejectsDenseChunkWithoutCachingPartialData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dense.geojson")
	const featureCount = maxReadOnlyWindowFeatures + 1
	var fixture strings.Builder
	fixture.Grow(featureCount * 115)
	fixture.WriteString(`{"type":"FeatureCollection","features":[`)
	for index := 0; index < featureCount; index++ {
		if index > 0 {
			fixture.WriteByte(',')
		}
		fmt.Fprintf(&fixture, `{"type":"Feature","properties":{"name":"p%d"},"geometry":{"type":"Point","coordinates":[127,37]}}`, index)
	}
	fixture.WriteString(`]}`)
	if err := os.WriteFile(path, []byte(fixture.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	runtime, err := loadReadOnlyDataRuntime(context.Background(), []vectorSourceSpec{{Path: path}}, "")
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.closeAttributeSource()
	if !runtime.viewportReadOnly || len(runtime.features) != 0 {
		t.Fatalf("initial runtime viewport-backed=%t features=%d", runtime.viewportReadOnly, len(runtime.features))
	}
	key := chunkKeyForPoint(runtime.mapExtent, readOnlyWindowChunkSize(0), "dense", render.Point{X: 127, Y: 37})
	runtime.mu.Lock()
	runtime.windowVisibleKeys[key] = struct{}{}
	runtime.mu.Unlock()
	if _, err := runtime.builder(context.Background(), key); err == nil || !strings.Contains(err.Error(), "limit of 20000") {
		t.Fatalf("dense chunk error = %v", err)
	}
	if len(runtime.features) != 0 || len(runtime.windowHits) != 0 || len(runtime.windowFeatureNames) != 0 {
		t.Fatalf("partial chunk escaped safety limit: features=%d hits=%d names=%d", len(runtime.features), len(runtime.windowHits), len(runtime.windowFeatureNames))
	}
}

func TestReadOnlyPolygonVertexBudgetPreflightsWKB(t *testing.T) {
	wkb := make([]byte, 9+4+3*16)
	wkb[0] = 1
	binary.LittleEndian.PutUint32(wkb[1:5], 3) // Polygon
	binary.LittleEndian.PutUint32(wkb[5:9], 1) // one ring
	binary.LittleEndian.PutUint32(wkb[9:13], 3)
	layer := core.Layer{Features: []core.Feature{{ID: 77, Geometry: core.WKBGeometry{WKB: wkb}}}}
	if err := validateReadOnlyWindowPolygonVertexBudget(layer, 3); err != nil {
		t.Fatalf("valid polygon WKB rejected: %v", err)
	}
	if err := validateReadOnlyWindowPolygonVertexBudget(layer, 2); err == nil || !strings.Contains(err.Error(), "triangulation safety limit") {
		t.Fatalf("oversized polygon WKB error = %v", err)
	}
}

func TestWindowedReadOnlyLargeSourceIntegration(t *testing.T) {
	paths := filepath.SplitList(os.Getenv("GOGIS_TEST_LARGE_VECTOR_SOURCES"))
	if len(paths) == 0 || paths[0] == "" {
		t.Skip("set GOGIS_TEST_LARGE_VECTOR_SOURCES to a platform path-list of real large vector sources")
	}
	runtime, autoReadOnly, totalFeatureCount, err := loadDataRuntimeFilesWithLargePolicy(
		context.Background(), paths, nil, "", false)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.closeAttributeSource()
	if !autoReadOnly || !runtime.readOnly || totalFeatureCount != largeDatasetReadOnlyThreshold {
		t.Fatalf("real-source load policy = read-only:%t runtime:%t count:%d; want automatic read-only at threshold", autoReadOnly, runtime.readOnly, totalFeatureCount)
	}
	if !runtime.viewportReadOnly || len(runtime.features) != 0 {
		t.Fatalf("large source runtime viewport=%t initial hits=%d", runtime.viewportReadOnly, len(runtime.features))
	}
	if workers := runtime.scheduler.MaxWorkers(); workers != readOnlyOverviewWorkerLimit() {
		t.Fatalf("read-only render workers = %d, want bounded concurrency of %d", workers, readOnlyOverviewWorkerLimit())
	}
	names := runtime.service.LayerNames()
	readableChunks := make(map[string]int, len(names))
	firstReadableChunks := make(map[string]render.ChunkKey, len(names))
	for _, name := range names {
		_, sourceFeatureCount, countErr := runtime.attributePageReader(context.Background(), name, 0, 1)
		if countErr != nil {
			t.Fatalf("read source feature count for %s: %v", name, countErr)
		}
		t.Logf("real source layer=%s feature-count=%d", name, sourceFeatureCount)
		// Bucket 2 is the first full-fidelity tier; overview buckets intentionally
		// omit attribute/hit-test payloads to stay bounded at small scales.
		for zoomBucket := 2; zoomBucket <= 4 && readableChunks[name] == 0; zoomBucket++ {
			tileCount := int(math.Ceil(1 / readOnlyWindowChunkSize(zoomBucket)))
			keys := make([]render.ChunkKey, 0, tileCount*tileCount)
			for y := 0; y < tileCount; y++ {
				for x := 0; x < tileCount; x++ {
					keys = append(keys, render.ChunkKey{Layer: name, ZoomBucket: zoomBucket, X: x, Y: y})
				}
			}
			runtime.mu.Lock()
			runtime.windowVisibleKeys = make(map[render.ChunkKey]struct{}, len(keys))
			runtime.windowHits = make(map[render.ChunkKey][]render.HitFeature)
			runtime.windowFeatureCounts = make(map[render.ChunkKey]int)
			runtime.windowFeatureIDs = make(map[render.ChunkKey][]uint64)
			runtime.windowPayloadBytes = make(map[render.ChunkKey]int64)
			runtime.windowLabels = make(map[render.ChunkKey][]render.LayerLabel)
			runtime.windowFeatureNames = make(map[uint64]string)
			runtime.windowVisibleFeatureCount = 0
			runtime.windowVisiblePayloadBytes = 0
			for _, key := range keys {
				runtime.windowVisibleKeys[key] = struct{}{}
			}
			runtime.mu.Unlock()
			for _, key := range keys {
				if readableChunks[name] > 0 {
					break
				}
				chunk, err := runtime.builder(context.Background(), key)
				if err != nil && !strings.Contains(err.Error(), "safety limit") && !strings.Contains(err.Error(), "payload budget") && !strings.Contains(err.Error(), "limit of") {
					t.Fatalf("real source chunk %v failed unexpectedly: %v", key, err)
				}
				if err == nil {
					hitCount := len(runtime.windowHits[key])
					if hitCount > 0 {
						readableChunks[name]++
						firstReadableChunks[name] = key
						t.Logf("real source layer=%s bucket=%d first readable chunk=%v hits=%d vertices=%d", key.Layer, zoomBucket, key, hitCount, len(chunk.Vertices))
					}
				} else {
					t.Logf("real source layer=%s bucket=%d correctly bounded: %v", key.Layer, zoomBucket, err)
				}
			}
		}
		if readableChunks[name] == 0 {
			t.Errorf("real source layer %s yielded no features through zoom bucket 4", name)
		}
	}
	const viewportMovesPerLayer = 128
	for _, name := range names {
		start, ok := firstReadableChunks[name]
		if !ok {
			continue
		}
		tileCount := int(math.Ceil(1 / readOnlyWindowChunkSize(start.ZoomBucket)))
		for move := 0; move < viewportMovesPerLayer; move++ {
			key := render.ChunkKey{
				Layer: name, ZoomBucket: start.ZoomBucket,
				X: (start.X + move) % tileCount,
				Y: (start.Y + move*3) % tileCount,
			}
			runtime.mu.Lock()
			runtime.retainVisibleWindowChunksLocked([]render.ChunkKey{key})
			runtime.mu.Unlock()
			if _, err := runtime.builder(context.Background(), key); err != nil &&
				!strings.Contains(err.Error(), "safety limit") &&
				!strings.Contains(err.Error(), "payload budget") &&
				!strings.Contains(err.Error(), "limit of") {
				t.Fatalf("real source viewport move %d for %s (%v) failed unexpectedly: %v", move, name, key, err)
			}
			runtime.mu.Lock()
			visibleKeys := len(runtime.windowVisibleKeys)
			retainedChunks := len(runtime.windowFeatureCounts)
			retainedHitChunks := len(runtime.windowHits)
			retainedIDChunks := len(runtime.windowFeatureIDs)
			retainedPayloadChunks := len(runtime.windowPayloadBytes)
			retainedLabelChunks := len(runtime.windowLabels)
			retainedNames := len(runtime.windowFeatureNames)
			retainedFeatures := len(runtime.features)
			visibleFeatures := runtime.windowVisibleFeatureCount
			visiblePayloadBytes := runtime.windowVisiblePayloadBytes
			if visibleKeys != 1 || retainedChunks > 1 || retainedHitChunks > 1 ||
				retainedIDChunks > 1 || retainedPayloadChunks > 1 || retainedLabelChunks > 1 ||
				retainedNames > maxReadOnlyVisibleFeatures || retainedFeatures > maxReadOnlyVisibleFeatures ||
				visibleFeatures > maxReadOnlyVisibleFeatures || visiblePayloadBytes > maxReadOnlyVisibleBytes {
				runtime.mu.Unlock()
				t.Fatalf("real source viewport move %d for %s retained unbounded state: keys=%d chunks=%d hits=%d ids=%d payloads=%d labels=%d names=%d features=%d visible-features=%d bytes=%d",
					move, name, visibleKeys, retainedChunks, retainedHitChunks, retainedIDChunks,
					retainedPayloadChunks, retainedLabelChunks, retainedNames, retainedFeatures,
					visibleFeatures, visiblePayloadBytes)
			}
			runtime.mu.Unlock()
		}
	}
	t.Logf("real-source viewport moves=%d per layer", viewportMovesPerLayer)
	if len(runtime.features) > maxReadOnlyVisibleFeatures || runtime.windowVisiblePayloadBytes > maxReadOnlyVisibleBytes {
		t.Fatalf("real-source safety caps exceeded: hits=%d payload=%d", len(runtime.features), runtime.windowVisiblePayloadBytes)
	}
	if peakRSS, ok := benchmarkProcessMaxRSSBytes(); ok {
		t.Logf("real source combined-layer test process peak-RSS-MiB=%d", peakRSS/(1<<20))
	}
}

func TestWindowedReadOnlyCityScaleFeatureBudgetRealSource(t *testing.T) {
	paths := filepath.SplitList(os.Getenv("GOGIS_TEST_LARGE_VECTOR_SOURCES"))
	if len(paths) < 2 || paths[0] == "" || paths[1] == "" {
		t.Skip("set GOGIS_TEST_LARGE_VECTOR_SOURCES to the parcel and survey-point SHPs")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	sources := make([]vectorSourceSpec, 0, len(paths))
	for index, path := range paths {
		source := vectorSourceSpec{Path: path}
		if os.Getenv("GOGIS_TEST_CITY_LABELS") == "1" {
			switch index {
			case 0:
				source.Labels = core.LabelSettings{Enabled: true, Expression: "${JIBUN}", Placement: "center", HeightMM: 2.5}
			case 1:
				source.Labels = core.LabelSettings{Enabled: true, Expression: "${DOGEUN_POI}", Placement: "center", HeightMM: 2.5}
			}
		}
		sources = append(sources, source)
	}
	runtime, err := loadReadOnlyDataRuntimeWithBaseLayers(ctx, sources, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.closeAttributeSource()
	data, fit := runtime.mapExtent, runtime.mapFitExtent
	const canvasWidth, canvasHeight = 1600.0, 1000.0
	width, height := canvasWidth, canvasHeight
	if requested := os.Getenv("GOGIS_TEST_CITY_VIEWPORT_MULTIPLIER"); requested != "" {
		multiplier, parseErr := strconv.ParseFloat(requested, 64)
		if parseErr != nil || multiplier <= 0 || multiplier > 4 || math.IsInf(multiplier, 0) || math.IsNaN(multiplier) {
			t.Fatalf("invalid GOGIS_TEST_CITY_VIEWPORT_MULTIPLIER %q", requested)
		}
		width *= multiplier
		height *= multiplier
	}
	denominator := 52_716.0
	if requested := os.Getenv("GOGIS_TEST_CITY_SCALE_DENOMINATOR"); requested != "" {
		denominator, err = strconv.ParseFloat(requested, 64)
		if err != nil || denominator <= 0 || math.IsInf(denominator, 0) || math.IsNaN(denominator) {
			t.Fatalf("invalid GOGIS_TEST_CITY_SCALE_DENOMINATOR %q", requested)
		}
	}
	viewport := render.Viewport{
		Center: render.Point{
			X: ((fit[0]+fit[2])/2 - data[0]) / (data[2] - data[0]),
			Y: ((fit[1]+fit[3])/2 - data[1]) / (data[3] - data[1]),
		},
		Zoom:        (data[2] - data[0]) / (canvasWidth * denominator * 0.0254 / 96),
		ScreenWidth: width, ScreenHeight: height, CanvasWidth: canvasWidth, CanvasHeight: canvasHeight,
	}
	planner := runtime.planner
	rawBucket := readOnlyWindowZoomBucket(viewport.Zoom)
	t.Logf("city-scale zoom=%g raw-bucket=%d semantic-lod=%d", viewport.Zoom, rawBucket, readOnlyOverviewZoomBucket(rawBucket, runtime.mapExtent, runtime.mapFitExtent))
	planner.ChunkSize = readOnlyWindowChunkSize(rawBucket)
	var keys []render.ChunkKey
	for _, name := range runtime.service.LayerNames() {
		keys = append(keys, planner.VisibleKeys(viewport, name)...)
	}
	if len(keys) == 0 {
		t.Fatal("city-scale viewport planned no chunks")
	}
	runtime.mu.Lock()
	runtime.windowVisibleKeys = make(map[render.ChunkKey]struct{}, len(keys))
	for _, key := range keys {
		runtime.windowVisibleKeys[key] = struct{}{}
	}
	runtime.mu.Unlock()
	var vertices int
	for _, key := range keys {
		chunk, err := runtime.builder(ctx, key)
		if err != nil {
			t.Fatalf("city-scale chunk %v of %d: %v", key, len(keys), err)
		}
		vertices += len(chunk.Vertices)
	}
	runtime.mu.Lock()
	features, payload := runtime.windowVisibleFeatureCount, runtime.windowVisiblePayloadBytes
	labels := len(runtime.mapLabels)
	runtime.mu.Unlock()
	t.Logf("city-scale viewport: scale=1:%.0f chunks=%d retained-features=%d retained-payload-MiB=%.1f vertices=%d labels=%d", denominator, len(keys), features, float64(payload)/(1<<20), vertices, labels)
	if features == 0 || features > maxReadOnlyVisibleFeatures || payload > maxReadOnlyVisibleBytes {
		t.Fatalf("city-scale viewport exceeded bounds: features=%d payload=%d", features, payload)
	}
	if vertices > render.MaxBatchVertices {
		t.Fatalf("city-scale viewport exceeds vertex batch limit: vertices=%d limit=%d", vertices, render.MaxBatchVertices)
	}
	if os.Getenv("GOGIS_TEST_CITY_LABELS") == "1" && labels == 0 {
		t.Fatal("city-scale labeled viewport generated no labels")
	}
	if peakRSS, ok := benchmarkProcessMaxRSSBytes(); ok {
		t.Logf("city-scale test process peak-RSS-MiB=%d", peakRSS/(1<<20))
	}
}

func TestWindowedReadOnlyLargeSourceInitialViewportRefresh(t *testing.T) {
	paths := filepath.SplitList(os.Getenv("GOGIS_TEST_LARGE_VECTOR_SOURCES"))
	if len(paths) == 0 || paths[0] == "" {
		t.Skip("set GOGIS_TEST_LARGE_VECTOR_SOURCES to a platform path-list of real large vector sources")
	}
	runtime, _, _, err := loadDataRuntimeFilesWithLargePolicy(context.Background(), paths, nil, "", false)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.closeAttributeSource()
	polygonLayer := ""
	for _, name := range runtime.service.LayerNames() {
		if strings.Contains(strings.ToUpper(runtime.layerGeometryTypes[name]), "POLYGON") {
			polygonLayer = name
			break
		}
	}
	if polygonLayer == "" {
		t.Fatalf("combined sources contain no polygon layer: %v", runtime.service.LayerNames())
	}
	polygonBounds, ok := runtime.layerBounds[polygonLayer]
	if !ok {
		t.Fatalf("polygon layer %q has no layer extent", polygonLayer)
	}
	spanX := runtime.mapExtent[2] - runtime.mapExtent[0]
	spanY := runtime.mapExtent[3] - runtime.mapExtent[1]
	if spanX <= 0 || spanY <= 0 {
		t.Fatalf("combined source has invalid extent: %v", runtime.mapExtent)
	}
	polygonWidth := (polygonBounds[2] - polygonBounds[0]) / spanX
	polygonHeight := (polygonBounds[3] - polygonBounds[1]) / spanY
	viewportZoom := 0.9 / math.Max(polygonWidth, polygonHeight)
	viewportCenter := render.Point{
		X: ((polygonBounds[0]+polygonBounds[2])/2 - runtime.mapExtent[0]) / spanX,
		Y: ((polygonBounds[1]+polygonBounds[3])/2 - runtime.mapExtent[1]) / spanY,
	}
	t.Logf("initial fitted polygon viewport layer=%s bounds=%v normalized-center=(%.4f,%.4f) zoom=%.3f", polygonLayer, polygonBounds, viewportCenter.X, viewportCenter.Y, viewportZoom)
	start := diagnosticEntryCount(t, native.DiagnosticLogJSON())
	runtime.refresh(context.Background(), render.Viewport{Center: viewportCenter, Zoom: viewportZoom})
	firstPublish := waitForRenderMessage(t, start, "first-publish generation=", 30*time.Second)
	t.Logf("first combined-source overview publication: %s", firstPublish)
	result := waitForRenderTerminalDiagnostic(t, start, 120*time.Second)
	vertices := -1
	for _, field := range strings.Fields(result) {
		if strings.HasPrefix(field, "vertices=") {
			if _, scanErr := fmt.Sscanf(strings.TrimPrefix(field, "vertices="), "%d", &vertices); scanErr != nil {
				t.Fatalf("parse rendered vertex count from %q: %v", result, scanErr)
			}
			break
		}
	}
	t.Logf("initial combined-source viewport result: %s", result)
	if vertices <= 0 {
		t.Fatalf("initial viewport produced no publishable geometry: %s", result)
	}

	layerNames := runtime.service.LayerNames()
	if len(layerNames) < 2 {
		t.Fatalf("combined source refresh loaded %d layers; want at least two", len(layerNames))
	}
	pointLayer := ""
	for _, name := range layerNames {
		if name != polygonLayer {
			pointLayer = name
			break
		}
	}
	if pointLayer == "" {
		t.Fatal("combined source refresh did not load a second layer")
	}
	applyLayerVisibility(runtime, `{"`+pointLayer+`":false}`)
	if runtime.visibility.IsVisible(pointLayer) || len(runtime.visibility.VisibleLayers()) != 1 {
		t.Fatalf("hide layer request did not leave exactly one visible layer: visible=%v", runtime.visibility.VisibleLayers())
	}
	runtime.advanceRenderGeneration()
	hideStart := diagnosticEntryCount(t, native.DiagnosticLogJSON())
	runtime.refresh(context.Background(), render.Viewport{Center: viewportCenter, Zoom: viewportZoom})
	if hidden := waitForRenderTerminalDiagnostic(t, hideStart, 30*time.Second); metricValue(t, hidden, "cache_hits") == 0 {
		t.Fatalf("remaining visible layer did not reuse initial viewport cache: %s", hidden)
	}
	applyLayerVisibility(runtime, `{"`+pointLayer+`":true}`)
	if !runtime.visibility.IsVisible(pointLayer) || len(runtime.visibility.VisibleLayers()) != 2 {
		t.Fatalf("show layer request did not restore both visible layers: visible=%v", runtime.visibility.VisibleLayers())
	}
	runtime.advanceRenderGeneration()
	showStart := diagnosticEntryCount(t, native.DiagnosticLogJSON())
	runtime.refresh(context.Background(), render.Viewport{Center: viewportCenter, Zoom: viewportZoom})
	shown := waitForRenderTerminalDiagnostic(t, showStart, 30*time.Second)
	if metricValue(t, shown, "cache_hits") == 0 {
		t.Fatalf("re-shown real SHP layer did not reuse retained viewport chunks: %s", shown)
	}
	t.Logf("real-source hide/show render result: %s", shown)
	runtime.mu.Lock()
	visibleFeatures := runtime.windowVisibleFeatureCount
	visiblePayloadBytes := runtime.windowVisiblePayloadBytes
	retainedHits := len(runtime.windowHits)
	retainedFeatures := len(runtime.features)
	retainedNames := len(runtime.windowFeatureNames)
	runtime.mu.Unlock()
	var memory goruntime.MemStats
	goruntime.ReadMemStats(&memory)
	currentRSS, _, rssAvailable := processMemoryBytes()
	t.Logf("real-source retained state: visible-features=%d payload-MiB=%d hit-chunks=%d features=%d names=%d heap-alloc-MiB=%d current-RSS-MiB=%d available=%t",
		visibleFeatures, visiblePayloadBytes>>20, retainedHits, retainedFeatures, retainedNames,
		memory.HeapAlloc>>20, currentRSS>>20, rssAvailable)
	if peakRSS, ok := benchmarkProcessMaxRSSBytes(); ok {
		t.Logf("real-source initial-viewport refresh process peak-RSS-MiB=%d", peakRSS/(1<<20))
	}
}

func TestWindowedReadOnlyFullExtentOverviewCompletesRealSource(t *testing.T) {
	paths := filepath.SplitList(os.Getenv("GOGIS_TEST_LARGE_VECTOR_SOURCES"))
	if len(paths) == 0 || paths[0] == "" {
		t.Skip("set GOGIS_TEST_LARGE_VECTOR_SOURCES to a platform path-list of real large vector sources")
	}
	previousPolicy := activeShapefileIndexPolicy
	configureShapefileIndexPolicy([]string{
		"--spatial-index-threshold=100000",
		"--spatial-index-location=cache",
	})
	defer func() { activeShapefileIndexPolicy = previousPolicy }()

	ctx := context.Background()
	var runtime *demoRuntime
	var autoReadOnly bool
	var featureCount int
	var viewport render.Viewport
	var err error
	if len(paths) > 1 {
		// Reproduce the user workflow: open the parcel layer, then append the
		// point layer while preserving the already-fitted parcel view.
		baseRuntime, large, count, loadErr := loadDataRuntimeFilesWithLargePolicy(
			ctx, paths[:1], nil, "", false)
		if loadErr != nil {
			t.Fatal(loadErr)
		}
		baseHasPolygon := false
		for _, geometryType := range baseRuntime.layerGeometryTypes {
			baseHasPolygon = baseHasPolygon || isPolygonOverviewLayer(geometryType)
		}
		if !baseHasPolygon {
			baseRuntime.closeAttributeSource()
			t.Fatalf("first source %q is not the parcel polygon layer", paths[0])
		}
		baseExtent := baseRuntime.mapExtent
		fitBounds := baseRuntime.mapFitExtent
		baseSpanX, baseSpanY := baseExtent[2]-baseExtent[0], baseExtent[3]-baseExtent[1]
		fractionX := (fitBounds[2] - fitBounds[0]) / baseSpanX
		fractionY := (fitBounds[3] - fitBounds[1]) / baseSpanY
		const viewportWidth, viewportHeight = 1280.0, 720.0
		oldCanvasWidth := math.Min(viewportWidth, viewportHeight*baseSpanX/baseSpanY)
		oldCanvasHeight := math.Min(viewportHeight, viewportWidth*baseSpanY/baseSpanX)
		baseZoom := 0.9 / math.Max(fractionX, fractionY)
		centerX := ((fitBounds[0]+fitBounds[2])/2 - baseExtent[0]) / baseSpanX
		centerY := ((fitBounds[1]+fitBounds[3])/2 - baseExtent[1]) / baseSpanY
		oldView := native.Viewport{
			PanX: (0.5 - centerX) * oldCanvasWidth * baseZoom,
			PanY: (centerY - 0.5) * oldCanvasHeight * baseZoom,
			Zoom: baseZoom, Width: oldCanvasWidth, Height: oldCanvasHeight,
			ViewportWidth: viewportWidth, ViewportHeight: viewportHeight,
		}
		baseRuntime.closeAttributeSource()
		sources := make([]vectorSourceSpec, 0, len(paths))
		for _, path := range paths {
			sources = append(sources, vectorSourceSpec{Path: path})
		}
		runtime, err = loadReadOnlyDataRuntimeWithBaseLayers(ctx, sources, "", nil, &baseExtent)
		if err == nil {
			runtime.workspaceView = preserveMapWorldView(oldView, baseExtent, runtime.mapExtent, "")
			preserved := runtime.workspaceView
			newSpanX := runtime.mapExtent[2] - runtime.mapExtent[0]
			newSpanY := runtime.mapExtent[3] - runtime.mapExtent[1]
			newCanvasWidth := math.Min(viewportWidth, viewportHeight*newSpanX/newSpanY)
			newCanvasHeight := math.Min(viewportHeight, viewportWidth*newSpanY/newSpanX)
			viewport = render.Viewport{
				Center:      render.Point{X: preserved.CenterX, Y: preserved.CenterY},
				Zoom:        preserved.Zoom,
				ScreenWidth: viewportWidth, ScreenHeight: viewportHeight,
				CanvasWidth: newCanvasWidth, CanvasHeight: newCanvasHeight,
			}
			preservedWorldCenterX := runtime.mapExtent[0] + preserved.CenterX*newSpanX
			preservedWorldCenterY := runtime.mapExtent[1] + preserved.CenterY*newSpanY
			if math.Abs(preservedWorldCenterX-(fitBounds[0]+fitBounds[2])/2) > 1e-6 ||
				math.Abs(preservedWorldCenterY-(fitBounds[1]+fitBounds[3])/2) > 1e-6 {
				t.Fatalf("adding outlier points shifted the fitted parcel center: got=(%g,%g) parcel-center=(%g,%g)",
					preservedWorldCenterX, preservedWorldCenterY,
					(fitBounds[0]+fitBounds[2])/2, (fitBounds[1]+fitBounds[3])/2)
			}
			oldUnitsPerPixel := baseSpanX / (oldCanvasWidth * baseZoom)
			wantZoom := newSpanX / (newCanvasWidth * oldUnitsPerPixel)
			if math.Abs(preserved.Zoom-wantZoom) > 1e-9 {
				t.Fatalf("adding outlier points changed the visible parcel scale: zoom=%g, want %g", preserved.Zoom, wantZoom)
			}
		}
		autoReadOnly, featureCount = large, count
	} else {
		runtime, autoReadOnly, featureCount, err = loadDataRuntimeFilesWithLargePolicy(
			ctx, paths, nil, "", false)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.closeAttributeSource()
	if !autoReadOnly || !runtime.viewportReadOnly || featureCount < largeDatasetReadOnlyThreshold {
		t.Fatalf("large-source policy: automatic-read-only=%t viewport=%t features=%d", autoReadOnly, runtime.viewportReadOnly, featureCount)
	}
	if len(paths) > 1 {
		polygonLayer, pointLayer := "", ""
		for name, geometryType := range runtime.layerGeometryTypes {
			if isPointGeometryType(geometryType) {
				pointLayer = name
			} else if strings.Contains(strings.ToUpper(geometryType), "POLYGON") {
				polygonLayer = name
			}
		}
		if polygonLayer == "" || pointLayer == "" {
			t.Fatalf("combined test did not load both polygon and point layers: %#v", runtime.layerGeometryTypes)
		}
		polygonBounds := runtime.layerBounds[polygonLayer]
		if runtime.mapFitExtent != polygonBounds {
			t.Fatalf("fit bounds %v include point outliers; want parcel bounds %v", runtime.mapFitExtent, polygonBounds)
		}
		if runtime.mapExtent[2] < 1_000_000 || runtime.mapExtent[1] > 100_000 {
			t.Fatalf("complete data canvas bounds %v dropped the source point outliers", runtime.mapExtent)
		}
	}
	fitBounds := runtime.mapFitExtent
	if len(paths) == 1 {
		spanX, spanY := runtime.mapExtent[2]-runtime.mapExtent[0], runtime.mapExtent[3]-runtime.mapExtent[1]
		fitFractionX := (fitBounds[2] - fitBounds[0]) / spanX
		fitFractionY := (fitBounds[3] - fitBounds[1]) / spanY
		viewport = render.Viewport{
			Center: render.Point{
				X: ((fitBounds[0]+fitBounds[2])/2 - runtime.mapExtent[0]) / spanX,
				Y: ((fitBounds[1]+fitBounds[3])/2 - runtime.mapExtent[1]) / spanY,
			},
			Zoom: 0.9 / math.Max(fitFractionX, fitFractionY),
		}
	}
	planner := runtime.planner
	viewportBucket := readOnlyWindowZoomBucket(viewport.Zoom)
	semanticLOD := readOnlyOverviewZoomBucket(viewportBucket, runtime.mapExtent, runtime.mapFitExtent)
	planner.ChunkSize = readOnlyWindowChunkSize(viewportBucket)
	t.Logf("sequential-append viewport: zoom=%g raw_bucket=%d semantic_lod=%d map_extent=%v fit_extent=%v chunk_size=%g",
		viewport.Zoom, viewportBucket, semanticLOD,
		runtime.mapExtent, runtime.mapFitExtent, planner.ChunkSize)
	var keys []render.ChunkKey
	for _, layerName := range runtime.service.LayerNames() {
		keys = append(keys, planner.VisibleKeys(viewport, layerName)...)
	}
	if len(keys) == 0 || len(keys) >= 1225 {
		t.Fatalf("parcel-area fit planned %d chunks at zoom %g from total extent %v / fit extent %v; expect non-empty and smaller than the outlier-diluted 1225-chunk full-world view",
			len(keys), viewport.Zoom, runtime.mapExtent, fitBounds)
	}
	started := time.Now()
	var totalVertices int64
	polygonMinX, polygonMinY := math.Inf(1), math.Inf(1)
	polygonMaxX, polygonMaxY := math.Inf(-1), math.Inf(-1)
	for index, key := range keys {
		chunk, err := runtime.builder(context.Background(), key)
		if err != nil {
			t.Fatalf("real-source overview chunk %d/%d (%v): %v", index+1, len(keys), key, err)
		}
		if len(chunk.Vertices) > render.MaxChunkVertices {
			t.Fatalf("overview chunk %v has %d vertices, limit is %d", key, len(chunk.Vertices), render.MaxChunkVertices)
		}
		totalVertices += int64(len(chunk.Vertices))
		if isPolygonOverviewLayer(runtime.layerGeometryTypes[key.Layer]) {
			for _, vertex := range chunk.Vertices {
				polygonMinX = math.Min(polygonMinX, float64(vertex.X))
				polygonMinY = math.Min(polygonMinY, float64(vertex.Y))
				polygonMaxX = math.Max(polygonMaxX, float64(vertex.X))
				polygonMaxY = math.Max(polygonMaxY, float64(vertex.Y))
			}
		}
	}
	if totalVertices == 0 {
		t.Fatal("real-source overview produced no perimeter vertices")
	}
	if totalVertices > render.MaxBatchVertices {
		t.Fatalf("real-source complete overview has %d vertices across %d chunks, viewport budget is %d", totalVertices, len(keys), render.MaxBatchVertices)
	}
	if !math.IsInf(polygonMinX, 1) {
		spanX := runtime.mapExtent[2] - runtime.mapExtent[0]
		spanY := runtime.mapExtent[3] - runtime.mapExtent[1]
		wantMinX := (runtime.mapFitExtent[0] - runtime.mapExtent[0]) / spanX
		wantMinY := (runtime.mapFitExtent[1] - runtime.mapExtent[1]) / spanY
		wantMaxX := (runtime.mapFitExtent[2] - runtime.mapExtent[0]) / spanX
		wantMaxY := (runtime.mapFitExtent[3] - runtime.mapExtent[1]) / spanY
		const perimeterExtentTolerance = 0.002 // allow display-only simplification at city scale
		if polygonMinX > wantMinX+perimeterExtentTolerance || polygonMinY > wantMinY+perimeterExtentTolerance ||
			polygonMaxX < wantMaxX-perimeterExtentTolerance || polygonMaxY < wantMaxY-perimeterExtentTolerance {
			t.Fatalf("overview polygon perimeter envelope [%g %g %g %g] does not cover fitted parcel bounds [%g %g %g %g]",
				polygonMinX, polygonMinY, polygonMaxX, polygonMaxY, wantMinX, wantMinY, wantMaxX, wantMaxY)
		}
	}
	t.Logf("real-source complete overview: chunks=%d vertices=%d elapsed=%s", len(keys), totalVertices, time.Since(started).Round(time.Millisecond))
	if len(paths) > 1 {
		t.Logf("real-source geometry types: %#v", runtime.layerGeometryTypes)
		forcedContext := context.WithValue(ctx, forceReadOnlyOverviewContextKey{}, true)
		started = time.Now()
		var forcedVertices int64
		loggedLayers := make(map[string]bool)
		for _, key := range keys {
			chunk, err := runtime.builder(forcedContext, key)
			if err != nil {
				t.Fatalf("forced generalized overview chunk %v: %v", key, err)
			}
			forcedVertices += int64(len(chunk.Vertices))
			if len(chunk.Vertices) > 0 && !loggedLayers[key.Layer] {
				lineVertices, pointVertices, fillVertices := 0, 0, 0
				for _, vertex := range chunk.Vertices {
					switch vertex.Kind {
					case render.VertexLine:
						lineVertices++
					case render.VertexPoint:
						pointVertices++
					case render.VertexFill:
						fillVertices++
					}
				}
				t.Logf("forced overview sample chunk %v geometry=%s vertices=%d lines=%d points=%d fills=%d",
					key, runtime.layerGeometryTypes[key.Layer], len(chunk.Vertices), lineVertices, pointVertices, fillVertices)
				loggedLayers[key.Layer] = true
			}
		}
		if forcedVertices == 0 || forcedVertices > render.MaxBatchVertices {
			t.Fatalf("forced generalized overview has %d vertices, want 1..%d", forcedVertices, render.MaxBatchVertices)
		}
		t.Logf("real-source generalized safety retry: chunks=%d vertices=%d elapsed=%s", len(keys), forcedVertices, time.Since(started).Round(time.Millisecond))
	}
}

func TestWindowedReadOnlyZoomedParcelRemainsVisibleRealSource(t *testing.T) {
	paths := filepath.SplitList(os.Getenv("GOGIS_TEST_LARGE_VECTOR_SOURCES"))
	if len(paths) < 2 || paths[0] == "" || paths[1] == "" {
		t.Skip("set GOGIS_TEST_LARGE_VECTOR_SOURCES to the parcel and survey-point SHPs")
	}
	runtime, err := loadReadOnlyDataRuntimeWithBaseLayers(context.Background(),
		[]vectorSourceSpec{{Path: paths[0]}, {Path: paths[1]}}, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.closeAttributeSource()
	data, fit := runtime.mapExtent, runtime.mapFitExtent
	centerX := ((fit[0]+fit[2])/2 - data[0]) / (data[2] - data[0])
	centerY := ((fit[1]+fit[3])/2 - data[1]) / (data[3] - data[1])
	for _, zoom := range []float64{194.44, 840} {
		viewport := render.Viewport{
			Center: render.Point{X: centerX, Y: centerY}, Zoom: zoom,
			ScreenWidth: 1600, ScreenHeight: 1000, CanvasWidth: 800, CanvasHeight: 1000,
		}
		planner := runtime.planner
		planner.ChunkSize = readOnlyWindowChunkSize(readOnlyWindowZoomBucket(viewport.Zoom))
		var polygonVertices, totalVertices, chunkCount int
		started := time.Now()
		for name, geometryType := range runtime.layerGeometryTypes {
			for _, key := range planner.VisibleKeys(viewport, name) {
				chunkCount++
				chunk, err := runtime.builder(context.Background(), key)
				if err != nil {
					t.Fatalf("zoomed chunk %v: %v", key, err)
				}
				totalVertices += len(chunk.Vertices)
				if isPolygonOverviewLayer(geometryType) {
					polygonVertices += len(chunk.Vertices)
				}
			}
		}
		if polygonVertices == 0 || totalVertices > render.MaxBatchVertices {
			t.Fatalf("zoom %.2f view vertices: parcel=%d total=%d (limit %d); parcel must remain visible after adding survey points",
				zoom, polygonVertices, totalVertices, render.MaxBatchVertices)
		}
		t.Logf("zoom %.2f Sejong chunks=%d parcel vertices=%d total=%d elapsed=%s",
			zoom, chunkCount, polygonVertices, totalVertices, time.Since(started).Round(time.Millisecond))
	}
}

func metricValue(t *testing.T, diagnostic, name string) int {
	t.Helper()
	for _, field := range strings.Fields(diagnostic) {
		if strings.HasPrefix(field, name+"=") {
			var value int
			if _, err := fmt.Sscanf(strings.TrimPrefix(field, name+"="), "%d", &value); err != nil {
				t.Fatalf("parse %s from %q: %v", name, diagnostic, err)
			}
			return value
		}
	}
	t.Fatalf("render diagnostic %q has no %s metric", diagnostic, name)
	return 0
}

func waitForRenderTerminalDiagnostic(t *testing.T, start int, timeout time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	if testDeadline, ok := t.Deadline(); ok && testDeadline.Before(deadline) {
		deadline = testDeadline.Add(-time.Second)
	}
	for time.Now().Before(deadline) {
		var entries []renderDiagnostic
		if err := json.Unmarshal([]byte(native.DiagnosticLogJSON()), &entries); err != nil {
			t.Fatalf("decode diagnostic log while waiting for initial render: %v", err)
		}
		for _, entry := range entries[min(start, len(entries)):] {
			if entry.Stream == "render" &&
				(strings.Contains(entry.Message, "ready generation=") || strings.Contains(entry.Message, "incomplete generation=")) {
				return entry.Message
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("timed out after %s waiting for initial viewport render diagnostic", timeout)
	return ""
}

func waitForRenderMessage(t *testing.T, start int, contains string, timeout time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	if testDeadline, ok := t.Deadline(); ok && testDeadline.Before(deadline) {
		deadline = testDeadline.Add(-time.Second)
	}
	for time.Now().Before(deadline) {
		var entries []renderDiagnostic
		if err := json.Unmarshal([]byte(native.DiagnosticLogJSON()), &entries); err != nil {
			t.Fatalf("decode diagnostic log while waiting for %q: %v", contains, err)
		}
		for _, entry := range entries[min(start, len(entries)):] {
			if entry.Stream == "render" && strings.Contains(entry.Message, contains) {
				return entry.Message
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("timed out after %s waiting for render diagnostic containing %q", timeout, contains)
	return ""
}

func TestWindowedReadOnlyRepeatedViewportMoves1M(t *testing.T) {
	if os.Getenv("GOGIS_TEST_REPEATED_VIEWPORT_1M") != "1" {
		t.Skip("set GOGIS_TEST_REPEATED_VIEWPORT_1M=1 to run the 1M-feature viewport stress test")
	}
	runWindowedReadOnlyRepeatedViewportStress(t, 1_000_000)
}

func TestWindowedReadOnlyRepeatedViewportMovesAboveIndexCap(t *testing.T) {
	if os.Getenv("GOGIS_TEST_REPEATED_VIEWPORT_ABOVE_INDEX_CAP") != "1" {
		t.Skip("set GOGIS_TEST_REPEATED_VIEWPORT_ABOVE_INDEX_CAP=1 to run the partial-index viewport stress test")
	}
	runWindowedReadOnlyRepeatedViewportStress(t, 1_000_001)
}

func runWindowedReadOnlyRepeatedViewportStress(t *testing.T, featureCount int) {
	t.Helper()
	path := desktopBenchmarkGeoJSONFixture(t, featureCount)
	runtime, err := loadReadOnlyDataRuntime(context.Background(), []vectorSourceSpec{{
		Path: path, Labels: core.LabelSettings{Enabled: true, LuaScript: `return feature.name`},
	}}, "")
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.closeAttributeSource()
	if !runtime.viewportReadOnly || len(runtime.features) != 0 {
		t.Fatalf("million-feature runtime viewport-backed=%t retained features=%d", runtime.viewportReadOnly, len(runtime.features))
	}

	const moves = 128
	var totalWindowHits int
	var totalWindowLabels int
	for move := 0; move < moves; move++ {
		key := render.ChunkKey{
			Layer: runtime.service.LayerNames()[0], ZoomBucket: 6,
			X: move, Y: (move * 37) % 256,
		}
		runtime.mu.Lock()
		runtime.retainVisibleWindowChunksLocked([]render.ChunkKey{key})
		runtime.mu.Unlock()
		if _, err := runtime.builder(context.Background(), key); err != nil {
			t.Fatalf("viewport move %d (%v): %v", move, key, err)
		}
		runtime.mu.Lock()
		if len(runtime.windowVisibleKeys) != 1 || len(runtime.windowFeatureCounts) > 1 ||
			runtime.windowVisibleFeatureCount > maxReadOnlyVisibleFeatures ||
			runtime.windowVisiblePayloadBytes > maxReadOnlyVisibleBytes {
			runtime.mu.Unlock()
			t.Fatalf("viewport move %d retained unbounded state: keys=%d chunks=%d features=%d bytes=%d",
				move, len(runtime.windowVisibleKeys), len(runtime.windowFeatureCounts),
				runtime.windowVisibleFeatureCount, runtime.windowVisiblePayloadBytes)
		}
		totalWindowHits += len(runtime.features)
		totalWindowLabels += len(runtime.windowLabels[key])
		runtime.mu.Unlock()
	}
	if totalWindowHits == 0 {
		t.Fatal("viewport stress test did not read any source features")
	}
	if totalWindowLabels == 0 {
		t.Fatal("viewport stress test did not evaluate configured Lua labels")
	}
	if peakRSS, ok := benchmarkProcessMaxRSSBytes(); ok {
		t.Logf("repeated viewport moves=%d source-features=%d total-window-hits=%d total-window-labels=%d peak-RSS-MiB=%d",
			moves, featureCount, totalWindowHits, totalWindowLabels, peakRSS/(1<<20))
	}
}

func largeReadOnlyFixture(tb testing.TB) string {
	return largeGeoJSONFixture(tb, previewMinimumFeatures)
}

func largeGeoJSONFixture(tb testing.TB, featureCount int) string {
	tb.Helper()
	path := filepath.Join(tb.TempDir(), "large.geojson")
	var fixture strings.Builder
	fixture.Grow(featureCount * 100)
	fixture.WriteString(`{"type":"FeatureCollection","features":[`)
	for index := 0; index < featureCount; index++ {
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
