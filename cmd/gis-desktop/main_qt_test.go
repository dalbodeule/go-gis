//go:build qt

package main

import (
	"context"
	"sync"
	"testing"
	"time"

	"gogis/internal/commands"
	"gogis/internal/core"
	"gogis/internal/render"
	"gogis/ui/qt/native"
)

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
