//go:build qt

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"gogis/internal/commands"
	"gogis/internal/core"
	"gogis/internal/render"
	"gogis/ui/qt/native"
)

func TestMarshalLayerLabelsNormalizesNilSlice(t *testing.T) {
	payload, err := marshalLayerLabels(nil)
	if err != nil {
		t.Fatal(err)
	}
	if payload != "[]" {
		t.Fatalf("nil label payload = %q, want []", payload)
	}
}

func TestViewportLayerLabelsCullOffscreenAndSampleDenseViews(t *testing.T) {
	labels := []render.LayerLabel{
		{Layer: "parcels", X: 0.5, Y: 0.5},
		{Layer: "parcels", X: 0.54, Y: 0.53},
		{Layer: "parcels", X: 0.8, Y: 0.5},
	}
	view := native.Viewport{Width: 1000, Height: 1000, ViewportWidth: 100, ViewportHeight: 100, Zoom: 1}
	visible, omitted := viewportLayerLabels(labels, view, 10)
	if len(visible) != 2 || omitted != 0 {
		t.Fatalf("visible labels=%d omitted=%d; want 2 and 0", len(visible), omitted)
	}
	labels = make([]render.LayerLabel, 100)
	for index := range labels {
		labels[index] = render.LayerLabel{FeatureID: uint64(index), X: .5, Y: .5}
	}
	sampled, omitted := viewportLayerLabels(labels, native.Viewport{}, 10)
	if len(sampled) != 10 || omitted != 90 || sampled[0].FeatureID != 0 || sampled[9].FeatureID != 90 {
		t.Fatalf("sampled labels=%d omitted=%d first/last=%d/%d", len(sampled), omitted, sampled[0].FeatureID, sampled[len(sampled)-1].FeatureID)
	}
}

func TestViewportLabelLimitAtScaleBoundaries(t *testing.T) {
	for _, test := range []struct {
		denominator float64
		want        int
	}{
		{0, 200},
		{999, 200},
		{1_000, 200},
		{5_000, 200},
		{17_999, 200},
		{18_000, 150},
		{49_999, 150},
		{50_000, 100},
		{100_000, 50},
		{math.NaN(), 200},
	} {
		if got := viewportLabelLimit(test.denominator); got != test.want {
			t.Errorf("scale 1:%.0f limit = %d, want %d", test.denominator, got, test.want)
		}
	}
}

func TestViewportScaleBudgetAppliesAfterCollisionRemoval(t *testing.T) {
	view := native.Viewport{Width: 1000, Height: 1000, ViewportWidth: 1000, ViewportHeight: 1000, Zoom: 1}
	labels := make([]render.LayerLabel, 0, 800)
	for index := 0; index < 400; index++ {
		label := render.LayerLabel{FeatureID: uint64(index + 1), Text: "x", X: 0.05 + float64(index%20)*0.045, Y: 0.05 + float64(index/20)*0.045, HeightMM: 2.5}
		labels = append(labels, label, label)
	}
	visible, safetyOmitted := viewportLayerLabels(labels, view, maxLayerLabelCount)
	decluttered := declutterViewportLabelsAtScale(visible, view, 18_000)
	selected, scaleOmitted := sampleLayerLabels(decluttered, viewportLabelLimit(18_000))
	if safetyOmitted != 0 || len(decluttered) != 400 || len(selected) != 150 || scaleOmitted != 250 {
		t.Fatalf("scale budget after collisions: safety=%d nonoverlapping=%d selected=%d scale-omitted=%d", safetyOmitted, len(decluttered), len(selected), scaleOmitted)
	}
}

func TestDeclutterViewportLabelsKeepsSeparatedLabels(t *testing.T) {
	view := native.Viewport{Width: 1000, Height: 1000, ViewportWidth: 1000, ViewportHeight: 1000, Zoom: 1}
	labels := []render.LayerLabel{
		{Text: "parcel 1", X: 0.5, Y: 0.5, HeightMM: 2.5},
		{Text: "parcel 2", X: 0.502, Y: 0.5, HeightMM: 2.5},
		{Text: "parcel 3", X: 0.8, Y: 0.5, HeightMM: 2.5},
	}
	got := declutterViewportLabels(labels, view)
	if len(got) != 2 || got[0].Text != "parcel 1" || got[1].Text != "parcel 3" {
		t.Fatalf("decluttered labels = %+v", got)
	}
	if got := declutterViewportLabels(labels, native.Viewport{}); len(got) != len(labels) {
		t.Fatalf("invalid viewport should not discard labels: %+v", got)
	}
}

func TestDeclutterViewportLabelsUsesMoreSpacingAtWideScale(t *testing.T) {
	view := native.Viewport{Width: 1000, Height: 1000, ViewportWidth: 1000, ViewportHeight: 1000, Zoom: 1}
	labels := []render.LayerLabel{
		{Text: "1", X: 0.5, Y: 0.5, HeightMM: 2.5},
		{Text: "2", X: 0.54, Y: 0.5, HeightMM: 2.5},
	}
	if got := declutterViewportLabelsAtScale(labels, view, 1000); len(got) != 2 {
		t.Fatalf("close-scale labels = %+v, want both", got)
	}
	if got := declutterViewportLabelsAtScale(labels, view, 100000); len(got) != 1 {
		t.Fatalf("wide-scale labels = %+v, want one", got)
	}
}

func TestLabelScaleDenominatorMatchesProjectedViewport(t *testing.T) {
	view := native.Viewport{Width: 1000, Zoom: 2}
	got := labelScaleDenominator(view, [4]float64{0, 0, 1000, 1000}, "EPSG:5186")
	if got != 1890 {
		t.Fatalf("scale denominator = %v, want 1890", got)
	}
}

func TestViewportBatchBudgetRetriesOnceWithGeneralizedOverview(t *testing.T) {
	batchError := "viewport render batch exceeds the 2500000-vertex safety limit"
	for _, test := range []struct {
		name, err        string
		readOnly, forced bool
		want             bool
	}{
		{name: "large read-only viewport", err: batchError, readOnly: true, want: true},
		{name: "already retried", err: batchError, readOnly: true, forced: true},
		{name: "editable project", err: batchError},
		{name: "different render failure", err: "GEOS simplify failed", readOnly: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := shouldRetryViewportWithOverview(test.readOnly, test.forced, test.err); got != test.want {
				t.Fatalf("shouldRetryViewportWithOverview() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestPrioritizeViewportChunksStartsAtCenterAndPreservesLayerOrder(t *testing.T) {
	keys := []render.ChunkKey{
		{Layer: "parcels", X: 0, Y: 0},
		{Layer: "parcels", X: 5, Y: 5},
		{Layer: "parcels", X: 9, Y: 9},
		{Layer: "points", X: 0, Y: 0},
		{Layer: "points", X: 5, Y: 5},
	}
	prioritizeViewportChunks(keys, render.Viewport{Center: render.Point{X: 5.5, Y: 5.5}}, 1)
	if keys[0].Layer != "parcels" || keys[0].X != 5 || keys[0].Y != 5 {
		t.Fatalf("first layer did not start at viewport center: %+v", keys)
	}
	if keys[3].Layer != "points" || keys[3].X != 5 || keys[3].Y != 5 {
		t.Fatalf("second layer did not preserve group order/start at center: %+v", keys)
	}
}

func TestProcessMemoryStatusIsAvailableAndNonzero(t *testing.T) {
	bytes, kind, ok := processMemoryBytes()
	if !ok || bytes == 0 || kind == "" {
		t.Fatalf("process memory sample = (%d, %q, %t), want nonzero supported measurement", bytes, kind, ok)
	}
	wantKind := map[string]string{"darwin": "RSS", "linux": "RSS", "windows": "Working set"}[runtime.GOOS]
	if wantKind != "" && kind != wantKind {
		t.Fatalf("process memory metric kind = %q, want %q on %s", kind, wantKind, runtime.GOOS)
	}
}

func TestAttributePayloadCacheRespectsByteAndEntryLimits(t *testing.T) {
	runtime := &demoRuntime{}
	first := attributePageKey{layer: "roads", page: 0}
	second := attributePageKey{layer: "roads", page: 1}
	runtime.cacheAttributePayload(first, strings.Repeat("a", 20<<20))
	runtime.cacheAttributePayload(second, strings.Repeat("b", 20<<20))
	if _, exists := runtime.attributeCache[first]; exists {
		t.Fatal("oldest payload was retained after total byte limit was exceeded")
	}
	if _, exists := runtime.attributeCache[second]; !exists || runtime.attributeCacheBytes != 20<<20 {
		t.Fatalf("cache after byte eviction has %d bytes and pages %v", runtime.attributeCacheBytes, runtime.attributeCacheOrder)
	}
	oversized := attributePageKey{layer: "roads", page: 2}
	runtime.cacheAttributePayload(oversized, strings.Repeat("x", attributePayloadCacheByteLimit+1))
	if _, exists := runtime.attributeCache[oversized]; exists || runtime.attributeCacheBytes > attributePayloadCacheByteLimit {
		t.Fatalf("oversized payload entered cache; retained bytes=%d", runtime.attributeCacheBytes)
	}
	for page := 3; page < 3+attributePayloadCacheLimit; page++ {
		runtime.cacheAttributePayload(attributePageKey{layer: "roads", page: page}, "[]")
	}
	if len(runtime.attributeCache) != attributePayloadCacheLimit || runtime.attributeCacheBytes > attributePayloadCacheByteLimit {
		t.Fatalf("cache limits not enforced: entries=%d bytes=%d", len(runtime.attributeCache), runtime.attributeCacheBytes)
	}
}

func TestEstimateAttributePayloadJSONBytesBoundsEscapedAndNestedValues(t *testing.T) {
	payload := attributePayload{
		Columns: []string{"name"},
		Fields:  []attributeFieldHint{{Name: "name", Type: core.FieldTypeText}},
		Rows: []attributePayloadRow{{FeatureID: 1, Values: map[string]any{
			"name": "한글 <road>", "nested": []any{map[string]any{"ok": true, "count": float64(2)}},
			"bytes": []byte{0, 1, 2}, "number": json.Number("1.234e+20"), "integer": int64(-42),
			"list": []string{"a", "b"},
		}}},
	}
	estimated := estimateAttributePayloadJSONBytes(payload)
	if estimated > maxAttributePayloadJSONBytes {
		t.Fatalf("ordinary nested attribute payload estimate = %d; limit=%d", estimated, maxAttributePayloadJSONBytes)
	}
	encoded, err := json.Marshal(payload)
	if err != nil || int64(len(encoded)) > estimated {
		t.Fatalf("nested payload estimate=%d actual=%d marshal error=%v", estimated, len(encoded), err)
	}

	payload.Rows[0].Values["controls"] = strings.Repeat("\x01", 3<<20)
	estimated = estimateAttributePayloadJSONBytes(payload)
	if estimated <= maxAttributePayloadJSONBytes {
		t.Fatalf("escaped control-character payload estimate = %d; want > %d", estimated, maxAttributePayloadJSONBytes)
	}
	encoded, err = json.Marshal(payload)
	if err != nil || int64(len(encoded)) <= maxAttributePayloadJSONBytes {
		t.Fatalf("escaped payload estimate=%d actual=%d marshal error=%v", estimated, len(encoded), err)
	}
}

func TestMarshalLayerLabelsBoundsEscapedPayloadBeforeEncoding(t *testing.T) {
	label := render.LayerLabel{
		FeatureID: ^uint64(0), X: -math.MaxFloat64, Y: math.MaxFloat64,
		Rotation: -math.MaxFloat64, HeightMM: math.MaxFloat64,
		MinScale: -math.MaxFloat64, MaxScale: math.MaxFloat64,
	}
	encoded, err := json.Marshal([]render.LayerLabel{label})
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded)-2 > maxLayerLabelJSONFixedBytes {
		t.Fatalf("fixed JSON estimate %d is smaller than maximal fields %d", maxLayerLabelJSONFixedBytes, len(encoded)-2)
	}
	if _, err := marshalLayerLabels([]render.LayerLabel{{Text: strings.Repeat("<", maxLayerLabelPayloadBytes/6+1)}}); err == nil || !strings.Contains(err.Error(), "safety limit") {
		t.Fatalf("expanded label payload error = %v", err)
	}
	if _, err := marshalLayerLabels(make([]render.LayerLabel, maxLayerLabelCount+1)); err == nil || !strings.Contains(err.Error(), "safety limit") {
		t.Fatalf("label count error = %v", err)
	}
	if _, err := marshalLayerLabels([]render.LayerLabel{{Text: "필지 & 도로"}}); err != nil {
		t.Fatalf("ordinary UTF-8 label rejected: %v", err)
	}
}

func TestRetainVisibleWindowChunksReleasesOffscreenGeometry(t *testing.T) {
	oldKey := render.ChunkKey{Layer: "parcels", ZoomBucket: 2, X: 1, Y: 1}
	keepKey := render.ChunkKey{Layer: "parcels", ZoomBucket: 2, X: 2, Y: 1}
	futureKey := render.ChunkKey{Layer: "parcels", ZoomBucket: 2, X: 3, Y: 1}
	runtime := &demoRuntime{
		windowVisibleKeys:         map[render.ChunkKey]struct{}{oldKey: {}, keepKey: {}},
		windowFeatureCounts:       map[render.ChunkKey]int{oldKey: 1, keepKey: 2},
		windowPayloadBytes:        map[render.ChunkKey]int64{oldKey: 10, keepKey: 20},
		windowVisibleFeatureCount: 3,
		windowVisiblePayloadBytes: 30,
		windowFeatureIDs:          map[render.ChunkKey][]uint64{oldKey: {101}, keepKey: {202, 203}},
		windowFeatureNames:        map[uint64]string{101: "offscreen", 202: "visible", 203: "visible 2"},
		windowHits: map[render.ChunkKey][]render.HitFeature{
			oldKey:  {{Layer: "parcels", FeatureID: 101, Vertices: []render.Point{{X: 0.1, Y: 0.1}}}},
			keepKey: {{Layer: "parcels", FeatureID: 202, Vertices: []render.Point{{X: 0.2, Y: 0.1}}}},
		},
		windowLabels: map[render.ChunkKey][]render.LayerLabel{
			oldKey:  {{Layer: "parcels", FeatureID: 101, Text: "offscreen"}},
			keepKey: {{Layer: "parcels", FeatureID: 202, Text: "visible"}},
		},
	}

	runtime.mu.Lock()
	runtime.retainVisibleWindowChunksLocked([]render.ChunkKey{keepKey, futureKey})
	runtime.mu.Unlock()

	if runtime.windowVisibleFeatureCount != 2 || runtime.windowVisiblePayloadBytes != 20 {
		t.Fatalf("retained budgets = %d features/%d bytes, want 2/20", runtime.windowVisibleFeatureCount, runtime.windowVisiblePayloadBytes)
	}
	if _, ok := runtime.windowFeatureNames[101]; ok || len(runtime.windowHits[oldKey]) != 0 || len(runtime.windowLabels[oldKey]) != 0 {
		t.Fatal("offscreen hit geometry, labels, or name remained cached")
	}
	if len(runtime.features) != 1 || runtime.features[0].FeatureID != 202 || len(runtime.mapLabels) != 1 || runtime.mapLabels[0].Text != "visible" {
		t.Fatalf("visible snapshot = %#v labels=%#v", runtime.features, runtime.mapLabels)
	}
	if _, ok := runtime.windowVisibleKeys[futureKey]; !ok {
		t.Fatal("new viewport key was not recorded")
	}
}

func TestRetainVisibleWindowChunksReleasesOffscreenOverviewLabels(t *testing.T) {
	key := render.ChunkKey{Layer: "parcels", ZoomBucket: -7, X: 1, Y: 1}
	runtime := &demoRuntime{
		windowVisibleKeys:         map[render.ChunkKey]struct{}{key: {}},
		windowPayloadBytes:        map[render.ChunkKey]int64{key: 80},
		windowVisiblePayloadBytes: 80,
		windowLabels:              map[render.ChunkKey][]render.LayerLabel{key: {{Text: "parcel"}}},
	}
	runtime.mu.Lock()
	runtime.retainVisibleWindowChunksLocked(nil)
	runtime.mu.Unlock()
	if runtime.windowVisiblePayloadBytes != 0 || len(runtime.windowLabels) != 0 || len(runtime.mapLabels) != 0 {
		t.Fatalf("offscreen overview labels retained: bytes=%d labels=%v", runtime.windowVisiblePayloadBytes, runtime.windowLabels)
	}
}

func TestAttributePageOffsetRejectsIntegerOverflow(t *testing.T) {
	if offset, ok := attributePageOffset(3); !ok || offset != 3*attributePageSize {
		t.Fatalf("ordinary page offset = %d, valid=%t", offset, ok)
	}
	if _, ok := attributePageOffset(int(^uint(0)>>1)/attributePageSize + 1); ok {
		t.Fatal("overflowing page offset accepted")
	}
}

func TestDesktopLanguageArgs(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "default", args: []string{"gis-desktop", "--verbose"}, want: "en"},
		{name: "Korean equals form", args: []string{"gis-desktop", "--lang=ko"}, want: "ko"},
		{name: "English separate form", args: []string{"gis-desktop", "--lang", "en"}, want: "en"},
		{name: "Japanese alias", args: []string{"gis-desktop", "--lang=ja"}, want: "jp"},
		{name: "unknown falls back", args: []string{"gis-desktop", "--lang=fr"}, want: "en"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, qtArgs := desktopLanguageArgs(test.args)
			if got != test.want {
				t.Fatalf("language = %q, want %q", got, test.want)
			}
			for _, arg := range qtArgs {
				if arg == "--lang" || strings.HasPrefix(arg, "--lang=") {
					t.Fatalf("language option leaked into Qt arguments: %q", arg)
				}
			}
		})
	}
}

func TestLoadEmptyProjectStartsWithoutLayersOrFeatures(t *testing.T) {
	runtime := loadEmptyProject()
	if runtime.service == nil {
		t.Fatal("empty project has no project service")
	}
	if names := runtime.service.LayerNames(); len(names) != 0 {
		t.Fatalf("empty project layers = %v, want none", names)
	}
	if len(runtime.features) != 0 {
		t.Fatalf("empty project render features = %d, want none", len(runtime.features))
	}
}

func TestReadOnlySelectionNameLookupDoesNotBlockClick(t *testing.T) {
	runtime := &demoRuntime{
		service:       commands.NewProjectService("test", core.CRS{}),
		readOnly:      true,
		visibleLayers: map[string]bool{"roads": true},
		features: []render.HitFeature{{
			Layer: "roads", FeatureID: 1,
			Vertices: []render.Point{{X: 0.45, Y: 0.5}, {X: 0.55, Y: 0.5}},
		}},
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	runtime.attributeFeatureReader = func(context.Context, string, uint64) (core.Feature, error) {
		close(entered)
		<-release
		return core.Feature{Properties: map[string]any{"name": "selected road"}}, nil
	}
	finished := make(chan struct{})
	go func() {
		runtime.selectAt(native.CanvasClick{X: 50, Y: 50}, native.Viewport{Width: 100, Height: 100, Zoom: 1})
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(time.Second):
		close(release)
		t.Fatal("click waited for the feature name lookup")
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		close(release)
		t.Fatal("feature name lookup did not start")
	}
	close(release)
	deadline := time.After(time.Second)
	for {
		runtime.mu.Lock()
		ready := runtime.featureNameCacheReady && runtime.featureNameCacheValue == "selected road"
		runtime.mu.Unlock()
		if ready {
			break
		}
		select {
		case <-deadline:
			t.Fatal("feature name was not published")
		case <-time.After(time.Millisecond):
		}
	}
}

func TestReadOnlySelectionDiscardsLateNameAfterClearingSelection(t *testing.T) {
	runtime := &demoRuntime{
		service:       commands.NewProjectService("test", core.CRS{}),
		readOnly:      true,
		visibleLayers: map[string]bool{"roads": true},
		features: []render.HitFeature{{
			Layer: "roads", FeatureID: 1,
			Vertices: []render.Point{{X: 0.45, Y: 0.5}, {X: 0.55, Y: 0.5}},
		}},
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	runtime.attributeFeatureReader = func(context.Context, string, uint64) (core.Feature, error) {
		close(entered)
		<-release
		return core.Feature{Properties: map[string]any{"name": "stale road"}}, nil
	}
	viewport := native.Viewport{Width: 100, Height: 100, Zoom: 1}
	runtime.selectAt(native.CanvasClick{X: 50, Y: 50}, viewport)
	select {
	case <-entered:
	case <-time.After(time.Second):
		close(release)
		t.Fatal("feature name lookup did not start")
	}
	runtime.selectAt(native.CanvasClick{X: 0, Y: 0}, viewport)
	close(release)
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if runtime.hasSelect || runtime.featureNameCacheReady {
		t.Fatal("cleared selection retained a stale feature name")
	}
}

func TestAttributePageLateResultDoesNotReplaceNewerPage(t *testing.T) {
	runtime := &demoRuntime{service: commands.NewProjectService("test", core.CRS{})}
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	runtime.attributePageReader = func(_ context.Context, layer string, offset, limit int) (core.Layer, int, error) {
		if offset == 0 {
			once.Do(func() { close(entered) })
			<-release // Simulate a native request that cannot stop immediately.
		}
		return core.Layer{Name: layer}, 400, nil
	}
	runtime.mu.Lock()
	firstDispatch := runtime.nextAttributeDispatchLocked()
	runtime.mu.Unlock()
	finished := make(chan struct{})
	go func() {
		runtime.publishAttributesPageDispatched("roads", 0, firstDispatch)
		close(finished)
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("first page did not start")
	}
	runtime.publishAttributesPage("roads", 1)
	close(release)
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("canceled page did not finish")
	}
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if !runtime.attributeReady || runtime.attributeLayer != "roads" || runtime.attributePage != 1 {
		t.Fatalf("published stale page: ready=%t layer=%q page=%d", runtime.attributeReady, runtime.attributeLayer, runtime.attributePage)
	}
}

func TestAttributePageAsyncDispatchKeepsViewportLoopFree(t *testing.T) {
	runtime := &demoRuntime{service: commands.NewProjectService("test", core.CRS{})}
	entered := make(chan struct{})
	release := make(chan struct{})
	runtime.attributePageReader = func(_ context.Context, layer string, offset, limit int) (core.Layer, int, error) {
		if offset == 0 {
			close(entered)
			<-release
		}
		return core.Layer{Name: layer}, 400, nil
	}
	runtime.publishAttributesPageAsync("roads", 0)
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("first asynchronous page did not start")
	}
	runtime.publishAttributesPageAsync("roads", 1)
	deadline := time.After(time.Second)
	for {
		runtime.mu.Lock()
		ready := runtime.attributeReady && runtime.attributePage == 1
		runtime.mu.Unlock()
		if ready {
			break
		}
		select {
		case <-deadline:
			t.Fatal("new page waited for the blocked old page")
		case <-time.After(time.Millisecond):
		}
	}
	close(release)
}

func TestRefreshCancelsBuilderWhenAllLayersHidden(t *testing.T) {
	started := make(chan struct{})
	stopped := make(chan struct{})
	var startOnce, stopOnce sync.Once
	visibility := render.NewLayerVisibility("roads")
	runtime := &demoRuntime{
		scheduler:  render.NewScheduler(),
		batchStore: render.NewBatchStore(),
		planner:    render.NewChunkPlanner(),
		visibility: visibility,
		builder: func(ctx context.Context, _ render.ChunkKey) (render.Chunk, error) {
			startOnce.Do(func() { close(started) })
			<-ctx.Done()
			stopOnce.Do(func() { close(stopped) })
			return render.Chunk{}, ctx.Err()
		},
	}
	viewport := render.Viewport{Center: render.Point{X: 0.5, Y: 0.5}, Zoom: 1}
	runtime.refresh(context.Background(), viewport)
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("render builder did not start")
	}
	visibility.Set("roads", false)
	runtime.scheduler.AdvanceGeneration()
	runtime.refresh(context.Background(), viewport)
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("render builder was not canceled after hiding all layers")
	}
}

func TestRefreshPreservesChunkCacheWhileAllLayersAreHidden(t *testing.T) {
	viewport := render.Viewport{Center: render.Point{X: 0.5, Y: 0.5}, Zoom: 1}
	planner := render.NewChunkPlanner()
	planner.ChunkSize = 0.25
	planner.Margin = 0
	planner.Domain = [4]float64{0, 0, 0, 0}
	planner.HasDomain = true
	keys := planner.VisibleKeys(viewport, "roads")
	if len(keys) == 0 {
		t.Fatal("test viewport produced no chunk keys")
	}
	scheduler := render.NewScheduler()
	results := scheduler.RequestUnique(context.Background(), keys[:1], func(_ context.Context, key render.ChunkKey) (render.Chunk, error) {
		return render.Chunk{Key: key, Vertices: []render.Vertex{{X: 0.25, Y: 0.25}, {X: 0.75, Y: 0.75}}}, nil
	})
	for range results {
	}
	if _, cached := scheduler.Cached(keys[0]); !cached {
		t.Fatal("setup failed to cache visible chunk")
	}

	visibility := render.NewLayerVisibility("roads")
	visibility.Set("roads", false)
	runtime := &demoRuntime{
		scheduler: scheduler, batchStore: render.NewBatchStore(), planner: planner,
		visibility: visibility, visibleLayers: map[string]bool{"roads": false},
	}
	runtime.refresh(context.Background(), viewport)
	if _, cached := scheduler.Cached(keys[0]); !cached {
		t.Fatal("hiding every layer discarded the last bounded viewport cache")
	}

	visibility.Set("roads", true)
	runtime.visibleLayers["roads"] = true
	runtime.refresh(context.Background(), viewport)
	if _, cached := scheduler.Cached(keys[0]); !cached {
		t.Fatal("restoring a layer discarded its reusable chunk")
	}
}

func TestRefreshPreservesHiddenLayerChunksWithinBudget(t *testing.T) {
	viewport := render.Viewport{Center: render.Point{X: 0.5, Y: 0.5}, Zoom: 1}
	planner := render.NewChunkPlanner()
	planner.ChunkSize = 0.25
	planner.Margin = 0
	planner.Domain = [4]float64{0, 0, 0, 0}
	planner.HasDomain = true
	roads := render.ChunkKey{Layer: "roads", X: 0, Y: 0}
	buildings := render.ChunkKey{Layer: "buildings", X: 0, Y: 0}
	scheduler := render.NewScheduler()
	results := scheduler.RequestUnique(context.Background(), []render.ChunkKey{roads, buildings}, func(_ context.Context, key render.ChunkKey) (render.Chunk, error) {
		return render.Chunk{Key: key, Vertices: []render.Vertex{{X: 0.1, Y: 0.1}, {X: 0.2, Y: 0.2}}}, nil
	})
	for range results {
	}

	visibility := render.NewLayerVisibility("roads", "buildings")
	visibility.Set("roads", false)
	runtime := &demoRuntime{
		scheduler: scheduler, batchStore: render.NewBatchStore(), planner: planner,
		visibility: visibility, visibleLayers: map[string]bool{"roads": false, "buildings": true},
		builder: func(_ context.Context, key render.ChunkKey) (render.Chunk, error) {
			return render.Chunk{Key: key}, nil
		},
	}
	runtime.refresh(context.Background(), viewport)
	if _, cached := scheduler.Cached(buildings); !cached {
		t.Fatal("currently visible layer chunk was not retained")
	}
	if _, cached := scheduler.Cached(roads); !cached {
		t.Fatal("hidden layer chunk was not retained for a visibility toggle")
	}

	visibility.Set("roads", true)
	runtime.visibleLayers["roads"] = true
	runtime.refresh(context.Background(), viewport)
	if _, cached := scheduler.Cached(roads); !cached {
		t.Fatal("re-shown layer chunk was not reused")
	}
}

func TestEmptyProjectRefreshDoesNotReportNoVisibleLayers(t *testing.T) {
	before := strings.Count(native.DiagnosticLogJSON(), "No visible layers")
	runtime := loadEmptyProject()
	runtime.refresh(context.Background(), render.Viewport{Center: render.Point{X: 0.5, Y: 0.5}, Zoom: 1})
	after := strings.Count(native.DiagnosticLogJSON(), "No visible layers")
	if after != before {
		t.Fatalf("empty project refresh added a misleading no-visible-layers diagnostic: before=%d after=%d", before, after)
	}
}

func TestRefreshVisibleLayerPublishesVerticesAndReusesCacheAfterToggle(t *testing.T) {
	runtime := &demoRuntime{
		scheduler:     render.NewScheduler(),
		batchStore:    render.NewBatchStore(),
		planner:       render.NewChunkPlanner(),
		visibility:    render.NewLayerVisibility("roads"),
		visibleLayers: map[string]bool{"roads": true},
		dataMode:      true,
		builder: func(_ context.Context, key render.ChunkKey) (render.Chunk, error) {
			return render.Chunk{Key: key, Vertices: []render.Vertex{{X: 0.25, Y: 0.75, Kind: render.VertexLine}}}, nil
		},
	}
	viewport := render.Viewport{Center: render.Point{X: 0.5, Y: 0.5}, Zoom: 1}
	runtime.advanceRenderGeneration()
	firstDiagnosticCount := diagnosticEntryCount(t, native.DiagnosticLogJSON())
	runtime.refresh(context.Background(), viewport)
	firstReady := waitForRenderDiagnostic(t, firstDiagnosticCount, "ready generation=1")
	if strings.Contains(firstReady, "vertices=0") {
		t.Fatalf("initial visible-layer render produced no vertices: %s", firstReady)
	}

	applyLayerVisibility(runtime, `{"roads":false}`)
	runtime.advanceRenderGeneration()
	runtime.refresh(context.Background(), viewport)

	applyLayerVisibility(runtime, `{"roads":true}`)
	runtime.advanceRenderGeneration()
	secondDiagnosticCount := diagnosticEntryCount(t, native.DiagnosticLogJSON())
	runtime.refresh(context.Background(), viewport)
	secondReady := waitForRenderDiagnostic(t, secondDiagnosticCount, "ready generation=3")
	if strings.Contains(secondReady, "vertices=0") {
		t.Fatalf("visible-layer restoration did not publish cached geometry: %s", secondReady)
	}
	var cacheHits int
	if _, err := fmt.Sscanf(strings.SplitN(secondReady, "cache_hits=", 2)[1], "%d", &cacheHits); err != nil {
		t.Fatalf("parse cache hit count from %q: %v", secondReady, err)
	}
	if cacheHits == 0 {
		t.Fatalf("restored layer rebuilt its chunks instead of using the retained cache: %s", secondReady)
	}
}

type renderDiagnostic struct {
	Stream  string `json:"stream"`
	Message string `json:"message"`
}

func diagnosticEntryCount(t *testing.T, payload string) int {
	t.Helper()
	var entries []renderDiagnostic
	if err := json.Unmarshal([]byte(payload), &entries); err != nil {
		t.Fatalf("decode diagnostic log: %v", err)
	}
	return len(entries)
}

func waitForRenderDiagnostic(t *testing.T, start int, generation string) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var entries []renderDiagnostic
		if err := json.Unmarshal([]byte(native.DiagnosticLogJSON()), &entries); err != nil {
			t.Fatalf("decode diagnostic log while waiting: %v", err)
		}
		for _, entry := range entries[min(start, len(entries)):] {
			if entry.Stream == "render" && strings.Contains(entry.Message, generation) &&
				strings.Contains(entry.Message, "vertices=") && strings.Contains(entry.Message, "cache_hits=") {
				return entry.Message
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for render diagnostic for %s", generation)
	return ""
}

func TestRefreshReadsViewportModeUnderRuntimeLock(t *testing.T) {
	runtime := &demoRuntime{
		scheduler:  render.NewScheduler(),
		batchStore: render.NewBatchStore(),
		planner:    render.NewChunkPlanner(),
		visibility: render.NewLayerVisibility(),
	}
	viewport := render.Viewport{Center: render.Point{X: 0.5, Y: 0.5}, Zoom: 1}
	stop := make(chan struct{})
	started := make(chan struct{})
	mutatorDone := make(chan struct{})
	go func() {
		defer close(mutatorDone)
		for {
			select {
			case <-stop:
				return
			default:
			}
			runtime.mu.Lock()
			runtime.viewportReadOnly = !runtime.viewportReadOnly
			runtime.mu.Unlock()
			select {
			case <-started:
			default:
				close(started)
			}
			time.Sleep(time.Microsecond)
		}
	}()
	<-started
	for i := 0; i < 250; i++ {
		runtime.refresh(context.Background(), viewport)
	}
	close(stop)
	<-mutatorDone
}

func TestApplyLayerVisibilityKeepsHitTestSnapshotImmutable(t *testing.T) {
	runtime := &demoRuntime{
		visibility:    render.NewLayerVisibility("roads", "buildings"),
		visibleLayers: map[string]bool{"roads": true, "buildings": true},
	}
	previous := runtime.visibleLayers
	applyLayerVisibility(runtime, `{"roads":false,"buildings":true}`)
	if !previous["roads"] || runtime.visibleLayers["roads"] || !runtime.visibleLayers["buildings"] {
		t.Fatalf("visibility snapshots = old %v, new %v", previous, runtime.visibleLayers)
	}
}

func TestAdvanceRenderGenerationUsesSwappedScheduler(t *testing.T) {
	oldScheduler := render.NewScheduler()
	newScheduler := render.NewScheduler()
	runtime := &demoRuntime{scheduler: oldScheduler}
	runtime.mu.Lock()
	runtime.scheduler = newScheduler
	runtime.mu.Unlock()
	runtime.advanceRenderGeneration()
	if oldScheduler.Generation() != 0 || newScheduler.Generation() != 1 {
		t.Fatalf("scheduler generations = old %d, new %d", oldScheduler.Generation(), newScheduler.Generation())
	}
}

func TestViewportStateAccessDuringPreviewSwap(t *testing.T) {
	runtime := &demoRuntime{
		scheduler:     render.NewScheduler(),
		visibility:    render.NewLayerVisibility("roads"),
		visibleLayers: map[string]bool{"roads": true},
	}
	var workers sync.WaitGroup
	workers.Add(2)
	go func() {
		defer workers.Done()
		for index := 0; index < 1000; index++ {
			runtime.advanceRenderGeneration()
			applyLayerVisibility(runtime, `{"roads":false}`)
			applyLayerVisibility(runtime, `{"roads":true}`)
		}
	}()
	go func() {
		defer workers.Done()
		for index := 0; index < 1000; index++ {
			runtime.mu.Lock()
			runtime.scheduler = render.NewScheduler()
			runtime.visibility = render.NewLayerVisibility("roads")
			runtime.visibleLayers = map[string]bool{"roads": true}
			runtime.mu.Unlock()
		}
	}()
	workers.Wait()
}
