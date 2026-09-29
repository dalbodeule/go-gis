//go:build qt

package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/qml"
	"gogis/internal/commands"
	"gogis/internal/core"
	"gogis/internal/presentation"
	"gogis/internal/render"
	"gogis/ui/qt/native"
)

// Main.qml is embedded so the prototype can be launched from a checkout
// without relying on a working directory or an installed resource bundle.
//
//go:embed qml/Main.qml
var mainQML []byte

func main() {
	qt.NewQApplication(os.Args)
	runtime := loadRuntime(os.Args)
	native.RegisterMapCanvas()
	engine := qml.NewQQmlApplicationEngine()
	engine.LoadData(mainQML)
	startViewportSync(runtime)
	qt.QApplication_Exec()
}

type demoRuntime struct {
	scheduler  *render.Scheduler
	batchStore *render.BatchStore
	planner    render.ChunkPlanner
	visibility *render.LayerVisibility
	builder    render.ChunkBuilder
	mu         sync.Mutex
	cancel     context.CancelFunc
	features   []render.HitFeature
	hitIndex   render.HitIndex
	service    *commands.ProjectService
	dataMode   bool
	persist    func(context.Context, string) error
	selected   render.HitResult
	hasSelect  bool
}

func loadDemoChunk() *demoRuntime {
	runtime := &demoRuntime{
		scheduler:  render.NewScheduler(),
		batchStore: render.NewBatchStore(),
		planner:    render.NewChunkPlanner(),
		visibility: render.NewLayerVisibility("roads", "buildings", "labels"),
	}
	runtime.builder = demoChunkBuilder(runtime.planner.ChunkSize)
	runtime.refresh(context.Background(), render.Viewport{Center: render.Point{X: 0.5, Y: 0.5}, Zoom: 1})
	runtime.service = newDemoService(runtime.features)
	runtime.publishLayerTree()
	runtime.publishAttributes("roads")
	return runtime
}

type attributePayload struct {
	Columns []string              `json:"columns"`
	Rows    []attributePayloadRow `json:"rows"`
}

type attributePayloadRow struct {
	FeatureID uint64         `json:"featureId"`
	Values    map[string]any `json:"values"`
}

type layerTreePayloadRow struct {
	Name string `json:"name"`
}

func (r *demoRuntime) publishLayerTree() {
	if r.service == nil {
		native.SetLayerTreePayload("[]")
		return
	}
	tree := presentation.LayerTree(r.service.Project())
	rows := make([]layerTreePayloadRow, len(tree))
	for index, item := range tree {
		rows[index] = layerTreePayloadRow{Name: item.Name}
	}
	payload, err := json.Marshal(rows)
	if err != nil {
		native.SetLayerTreePayload("[]")
		return
	}
	native.SetLayerTreePayload(string(payload))
}

func (r *demoRuntime) publishAttributes(layerName string) {
	if r.service == nil {
		native.SetAttributePayload("[]")
		return
	}
	payloadModel := attributePayload{}
	for _, layer := range r.service.Project().Layers {
		if layer.Name != layerName {
			continue
		}
		table := presentation.AttributeTable(layer)
		payloadModel.Columns = make([]string, len(table.Columns))
		for i, column := range table.Columns {
			payloadModel.Columns[i] = column.Name
		}
		payloadModel.Rows = make([]attributePayloadRow, len(table.Rows))
		for i, row := range table.Rows {
			payloadModel.Rows[i] = attributePayloadRow{FeatureID: row.FeatureID, Values: row.Values}
		}
		break
	}
	payload, err := json.Marshal(payloadModel)
	if err != nil {
		native.SetAttributePayload("[]")
		return
	}
	native.SetAttributePayload(string(payload))
}

func newDemoService(features []render.HitFeature) *commands.ProjectService {
	service := commands.NewProjectService("demo", core.CRS{AuthorityCode: "EPSG:4326"})
	byLayer := map[string][]core.Feature{}
	for _, feature := range features {
		if len(feature.Vertices) < 2 {
			continue
		}
		wkt := fmt.Sprintf("LINESTRING (%g %g, %g %g)", feature.Vertices[0].X, feature.Vertices[0].Y, feature.Vertices[1].X, feature.Vertices[1].Y)
		byLayer[feature.Layer] = append(byLayer[feature.Layer], core.Feature{
			ID: feature.FeatureID, Geometry: core.WKTGeometry{WKT: wkt},
			Properties: map[string]any{"name": fmt.Sprintf("%s feature #%d", feature.Layer, feature.FeatureID)},
		})
	}
	if err := service.BeginEdit(); err != nil {
		return service
	}
	for _, layer := range []string{"roads", "buildings", "labels"} {
		_ = service.AddLayer(core.Layer{Name: layer, Editable: true, Fields: []core.Field{{Name: "name", Type: core.FieldTypeText}}, Features: byLayer[layer]})
	}
	_ = service.Commit()
	return service
}

func (r *demoRuntime) refresh(ctx context.Context, viewport render.Viewport) {
	var keys []render.ChunkKey
	if r.dataMode {
		for _, layer := range r.visibility.VisibleLayers() {
			keys = append(keys, r.planner.VisibleKeys(viewport, layer)...)
		}
	} else {
		for _, layer := range r.visibility.VisibleLayers() {
			keys = append(keys, r.planner.VisibleKeys(viewport, layer)...)
		}
	}
	keys = r.visibility.FilterChunkKeys(keys)
	generation := r.scheduler.Generation()
	r.batchStore.BeginGeneration(generation, keys...)
	r.mu.Lock()
	if !r.dataMode {
		r.features = demoFeatures(keys, r.planner.ChunkSize)
		r.hitIndex = render.NewHitIndex(r.features, 0.01)
	}
	r.mu.Unlock()
	if len(keys) == 0 {
		native.SetRenderStatus("No visible layers")
		r.batchStore.Clear()
		native.SetVertices(nil)
		native.RequestCanvasUpdate()
		native.SetSelection("", "", "", "No feature selected")
		return
	}

	requestContext, cancel := context.WithCancel(ctx)
	requestGeneration := generation
	r.mu.Lock()
	previousCancel := r.cancel
	r.cancel = cancel
	r.mu.Unlock()
	if previousCancel != nil {
		previousCancel()
	}
	setProgress := func(progress presentation.RenderProgress) {
		if r.scheduler.Generation() == requestGeneration {
			native.SetRenderStatus(progress.Message())
		}
	}
	setProgress(presentation.RenderProgress{Phase: "Loading", Total: len(keys), Cancellable: true})

	go func() {
		results := r.scheduler.Request(requestContext, keys, r.builder)
		completed := 0
		for result := range results {
			completed++
			setProgress(presentation.RenderProgress{Phase: "Loading", Completed: completed, Total: len(keys), Cancellable: true})
			if !r.batchStore.Apply(result) {
				continue
			}
			if _, vertices := r.batchStore.Current(); len(vertices) > 0 {
				native.SetVertices(vertices)
				native.RequestCanvasUpdate()
			}
		}
		if requestContext.Err() != nil {
			setProgress(presentation.RenderProgress{Phase: "Render cancelled"})
		} else {
			setProgress(presentation.RenderProgress{Phase: "Ready", Completed: completed, Total: len(keys)})
		}
	}()
}

func demoFeatures(keys []render.ChunkKey, chunkSize float64) []render.HitFeature {
	features := make([]render.HitFeature, 0, len(keys)*2)
	for _, key := range keys {
		x := float64(key.X) * chunkSize
		y := float64(key.Y) * chunkSize
		baseID := uint64(uint32(key.X))<<32 | uint64(uint32(key.Y))
		features = append(features,
			render.HitFeature{Layer: key.Layer, FeatureID: baseID*2 + 1, Vertices: []render.Point{{X: x, Y: y}, {X: x + chunkSize*0.45, Y: y + chunkSize*0.30}}},
			render.HitFeature{Layer: key.Layer, FeatureID: baseID*2 + 2, Vertices: []render.Point{{X: x + chunkSize*0.45, Y: y + chunkSize*0.30}, {X: x + chunkSize*0.90, Y: y + chunkSize*0.15}}},
		)
	}
	return features
}

func (r *demoRuntime) selectAt(click native.CanvasClick, viewport native.Viewport) {
	width, height := viewport.Width, viewport.Height
	if width <= 0 || height <= 0 || viewport.Zoom <= 0 {
		return
	}
	worldViewport := render.Viewport{
		Center: render.Point{
			X: 0.5 - viewport.PanX/(width*viewport.Zoom),
			Y: 0.5 + viewport.PanY/(height*viewport.Zoom),
		},
		Zoom: viewport.Zoom,
	}
	r.mu.Lock()
	features := append([]render.HitFeature(nil), r.features...)
	r.mu.Unlock()
	r.mu.Lock()
	hitIndex := r.hitIndex
	r.mu.Unlock()
	visible := make(map[string]bool)
	for _, layer := range r.visibility.VisibleLayers() {
		visible[layer] = true
	}
	result, ok := hitIndex.HitTestScreenVisible(render.Point{X: click.X, Y: click.Y}, worldViewport, width, height, 8, visible)
	if !ok {
		r.mu.Lock()
		r.hasSelect = false
		r.mu.Unlock()
		native.SetSelection("", "", "", "No feature selected")
		native.SetAttributePayload("[]")
		return
	}
	r.ensureFeature(result, features)
	r.mu.Lock()
	r.selected = result
	r.hasSelect = true
	r.mu.Unlock()
	r.publishAttributes(result.Layer)
	native.SetSelection(result.Layer, fmt.Sprintf("%s segment #%d", result.Layer, result.FeatureID), r.featureName(result.Layer, result.FeatureID), "Selected for inspection")
}

func (r *demoRuntime) ensureFeature(result render.HitResult, features []render.HitFeature) {
	if r.service == nil {
		return
	}
	for _, layer := range r.service.Project().Layers {
		if layer.Name != result.Layer {
			continue
		}
		for _, feature := range layer.Features {
			if feature.ID == result.FeatureID {
				return
			}
		}
	}
	for _, feature := range features {
		if feature.Layer != result.Layer || feature.FeatureID != result.FeatureID || len(feature.Vertices) < 2 {
			continue
		}
		wkt := fmt.Sprintf("LINESTRING (%g %g, %g %g)", feature.Vertices[0].X, feature.Vertices[0].Y, feature.Vertices[1].X, feature.Vertices[1].Y)
		if err := r.service.BeginEdit(); err != nil {
			return
		}
		if err := r.service.AddFeature(result.Layer, core.Feature{ID: result.FeatureID, Geometry: core.WKTGeometry{WKT: wkt}, Properties: map[string]any{"name": fmt.Sprintf("%s feature #%d", result.Layer, result.FeatureID)}}); err != nil {
			_ = r.service.Rollback()
			return
		}
		_ = r.service.Commit()
		return
	}
}

func (r *demoRuntime) edit(event native.EditEvent) {
	r.mu.Lock()
	selected, ok := r.selected, r.hasSelect
	r.mu.Unlock()
	if !ok {
		return
	}
	if event.Action == "rollback" {
		native.SetSelection(selected.Layer, fmt.Sprintf("%s segment #%d", selected.Layer, selected.FeatureID), r.featureName(selected.Layer, selected.FeatureID), "Edit cancelled")
		return
	}
	if event.Action != "commit" || r.service == nil {
		return
	}
	if err := r.service.BeginEdit(); err != nil {
		native.SetSelection(selected.Layer, fmt.Sprintf("%s segment #%d", selected.Layer, selected.FeatureID), r.featureName(selected.Layer, selected.FeatureID), "Edit failed")
		return
	}
	if err := r.service.SetFeatureProperty(selected.Layer, selected.FeatureID, "name", event.Value); err != nil {
		_ = r.service.Rollback()
		native.SetSelection(selected.Layer, fmt.Sprintf("%s segment #%d", selected.Layer, selected.FeatureID), r.featureName(selected.Layer, selected.FeatureID), "Edit failed")
		return
	}
	if err := r.service.Commit(); err != nil {
		native.SetSelection(selected.Layer, fmt.Sprintf("%s segment #%d", selected.Layer, selected.FeatureID), r.featureName(selected.Layer, selected.FeatureID), "Edit failed")
		return
	}
	r.publishAttributes(selected.Layer)
	if r.persist != nil {
		if err := r.persist(context.Background(), selected.Layer); err != nil {
			native.SetSelection(selected.Layer, fmt.Sprintf("%s segment #%d", selected.Layer, selected.FeatureID), event.Value, fmt.Sprintf("Saved in memory; disk save failed: %v", err))
			return
		}
		native.SetSelection(selected.Layer, fmt.Sprintf("%s segment #%d", selected.Layer, selected.FeatureID), event.Value, "Property saved to disk")
		return
	}
	native.SetSelection(selected.Layer, fmt.Sprintf("%s segment #%d", selected.Layer, selected.FeatureID), event.Value, "Property saved in memory")
}

func (r *demoRuntime) featureName(layerName string, featureID uint64) string {
	if r.service != nil {
		for _, layer := range r.service.Project().Layers {
			if layer.Name != layerName {
				continue
			}
			for _, feature := range layer.Features {
				if feature.ID == featureID {
					if name, ok := feature.Properties["name"].(string); ok {
						return name
					}
				}
			}
		}
	}
	return ""
}

func (r *demoRuntime) cancelCurrentRender() {
	r.mu.Lock()
	cancel := r.cancel
	r.mu.Unlock()
	if cancel != nil {
		cancel()
		native.SetRenderStatus("Render cancelled")
	}
}

func demoChunkBuilder(chunkSize float64) render.ChunkBuilder {
	return func(_ context.Context, key render.ChunkKey) (render.Chunk, error) {
		x := float32(float64(key.X) * chunkSize)
		y := float32(float64(key.Y) * chunkSize)
		size := float32(chunkSize)
		return render.Chunk{Vertices: []render.Vertex{
			{X: x, Y: y}, {X: x + size*0.45, Y: y + size*0.30},
			{X: x + size*0.45, Y: y + size*0.30}, {X: x + size*0.90, Y: y + size*0.15},
		}}, nil
	}
}

func startViewportSync(runtime *demoRuntime) {
	lastQtGeneration := native.ViewportGeneration()
	lastLayerVisibilityGeneration := native.LayerVisibilityGeneration()
	lastClickGeneration := native.ClickGeneration()
	lastEditGeneration := native.EditGeneration()
	lastCancelGeneration := native.CancelGeneration()
	lastActiveLayerGeneration := native.ActiveLayerGeneration()
	lastLoadGeneration := native.LoadGeneration()
	go func() {
		for range time.NewTicker(16 * time.Millisecond).C {
			loadGeneration := native.LoadGeneration()
			if loadGeneration != lastLoadGeneration {
				lastLoadGeneration = loadGeneration
				path := native.CurrentLoadPath()
				if path == "" {
					native.SetRenderStatus("Open cancelled")
				} else if next, err := loadDataRuntime(path, "", "", "", ""); err != nil {
					native.SetRenderStatus("Open failed: " + err.Error())
				} else {
					runtime.replaceWith(next)
				}
			}
			qtGeneration := native.ViewportGeneration()
			layerVisibilityGeneration := native.LayerVisibilityGeneration()
			viewport := native.CurrentViewport()
			if qtGeneration != lastQtGeneration || layerVisibilityGeneration != lastLayerVisibilityGeneration {
				lastQtGeneration = qtGeneration
				if layerVisibilityGeneration != lastLayerVisibilityGeneration {
					lastLayerVisibilityGeneration = layerVisibilityGeneration
					applyLayerVisibility(runtime, native.CurrentLayerVisibility())
				}
				runtime.scheduler.AdvanceGeneration()
				width, height := viewport.Width, viewport.Height
				if width <= 0 {
					width = 1
				}
				if height <= 0 {
					height = 1
				}
				zoom := viewport.Zoom
				if zoom <= 0 {
					zoom = 1
				}
				runtime.refresh(context.Background(), render.Viewport{
					Center: render.Point{
						X: 0.5 - viewport.PanX/(width*zoom),
						Y: 0.5 + viewport.PanY/(height*zoom),
					},
					Zoom: zoom,
				})
			}
			clickGeneration := native.ClickGeneration()
			if clickGeneration != lastClickGeneration {
				lastClickGeneration = clickGeneration
				runtime.selectAt(native.CurrentClick(), viewport)
			}
			editGeneration := native.EditGeneration()
			if editGeneration != lastEditGeneration {
				lastEditGeneration = editGeneration
				runtime.edit(native.CurrentEdit())
			}
			cancelGeneration := native.CancelGeneration()
			if cancelGeneration != lastCancelGeneration {
				lastCancelGeneration = cancelGeneration
				runtime.cancelCurrentRender()
			}
			activeLayerGeneration := native.ActiveLayerGeneration()
			if activeLayerGeneration != lastActiveLayerGeneration {
				lastActiveLayerGeneration = activeLayerGeneration
				if layer := native.CurrentActiveLayer(); layer != "" {
					runtime.publishAttributes(layer)
				}
			}
		}
	}()
}

func applyLayerVisibility(runtime *demoRuntime, payload string) {
	var visibility map[string]bool
	if err := json.Unmarshal([]byte(payload), &visibility); err != nil {
		return
	}
	for layer, visible := range visibility {
		if runtime.visibility.IsVisible(layer) != visible {
			runtime.visibility.Set(layer, visible)
		}
	}
}
