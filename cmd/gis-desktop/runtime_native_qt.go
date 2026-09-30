//go:build qt && native

package main

import (
	"context"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"

	"gogis/drivers/gdal"
	"gogis/drivers/proj"
	"gogis/internal/commands"
	"gogis/internal/core"
	"gogis/internal/render"
	"gogis/ui/qt/native"
)

func loadRuntime(args []string) *demoRuntime {
	input, layerName, sourceCRS, targetCRS := desktopInputArgs(args)
	savePath := desktopSavePath(args)
	if input == "" {
		return loadDemoChunk()
	}
	runtime, err := loadDataRuntimeMode(input, layerName, sourceCRS, targetCRS, savePath, desktopReadOnly(args) && savePath == "")
	if err != nil {
		fmt.Fprintf(os.Stderr, "GoGIS: unable to load %q: %v\n", input, err)
		return loadDemoChunk()
	}
	runtime.refresh(context.Background(), render.Viewport{Center: render.Point{X: 0.5, Y: 0.5}, Zoom: 1})
	runtime.publishLayerTree()
	if names := runtime.service.LayerNames(); len(names) > 0 {
		runtime.publishAttributes(names[0])
	}
	return runtime
}

func startInitialDataLoad(runtime *demoRuntime, args []string) {
	input, layerName, sourceCRS, targetCRS := desktopInputArgs(args)
	if input == "" {
		return
	}
	savePath := desktopSavePath(args)
	readOnly := desktopReadOnly(args) && savePath == ""
	runtime.startDataLoad(input, layerName, sourceCRS, targetCRS, savePath, readOnly)
}

func (r *demoRuntime) startDataLoad(input, layerName, sourceCRS, targetCRS, savePath string, readOnly bool) {
	loadContext, cancel := context.WithCancel(context.Background())
	r.mu.Lock()
	previousCancel := r.loadCancel
	r.loadCancel = cancel
	r.loadGeneration++
	generation := r.loadGeneration
	r.mu.Unlock()
	if previousCancel != nil {
		previousCancel()
	}
	native.SetRenderStatus("Loading " + input)
	go func() {
		next, err := loadDataRuntimeModeContextWithPreview(loadContext, input, layerName, sourceCRS, targetCRS, savePath, readOnly, func(preview *demoRuntime) {
			r.replaceWithPreview(preview, generation)
		})
		r.mu.Lock()
		current := generation == r.loadGeneration
		if current {
			r.loadCancel = nil
		}
		r.mu.Unlock()
		if !current {
			if next != nil && next.closeAttributeSource != nil {
				next.closeAttributeSource()
			}
			return
		}
		if err != nil {
			native.SetRenderStatus("Open failed: " + err.Error())
			return
		}
		r.replaceWithLoaded(next, generation)
	}()
}

func loadDataRuntime(input, layerName, sourceCRS, targetCRS, savePath string) (*demoRuntime, error) {
	return loadDataRuntimeMode(input, layerName, sourceCRS, targetCRS, savePath, false)
}

func loadDataRuntimeMode(input, layerName, sourceCRS, targetCRS, savePath string, readOnly bool) (*demoRuntime, error) {
	return loadDataRuntimeModeContext(context.Background(), input, layerName, sourceCRS, targetCRS, savePath, readOnly)
}

func loadDataRuntimeModeContext(ctx context.Context, input, layerName, sourceCRS, targetCRS, savePath string, readOnly bool) (*demoRuntime, error) {
	return loadDataRuntimeModeContextWithPreview(ctx, input, layerName, sourceCRS, targetCRS, savePath, readOnly, nil)
}

func loadDataRuntimeModeContextWithPreview(ctx context.Context, input, layerName, sourceCRS, targetCRS, savePath string, readOnly bool, onPreview func(*demoRuntime)) (*demoRuntime, error) {
	var layers []core.Layer
	var err error
	var attributeSession *gdal.AttributeSession
	keepAttributeSession := false
	defer func() {
		if attributeSession != nil && !keepAttributeSession {
			_ = attributeSession.Close()
		}
	}()
	if readOnly {
		attributeSession, err = gdal.OpenAttributeSession(input)
		if err == nil {
			if onPreview != nil && savePath == "" {
				tryReadOnlyPreview(ctx, attributeSession, layerName, sourceCRS, targetCRS, input, onPreview)
			}
			if layerName == "" {
				layers, err = attributeSession.OpenAllGeometryOnly(ctx)
			} else {
				var selected core.Layer
				selected, err = attributeSession.OpenGeometryOnly(ctx, layerName)
				if err == nil {
					layers = []core.Layer{selected}
				}
			}
		}
	} else {
		if layerName == "" {
			layers, err = (gdal.Reader{}).OpenAll(ctx, input)
		} else {
			var selected core.Layer
			selected, err = (gdal.Reader{}).Open(ctx, input, layerName)
			if err == nil {
				layers = []core.Layer{selected}
			}
		}
	}
	if err != nil {
		return nil, fmt.Errorf("open dataset: %w", err)
	}
	runtime, err := buildDataRuntime(ctx, layers, input, sourceCRS, targetCRS, savePath, readOnly, attributeSession, nil)
	if err != nil {
		return nil, err
	}
	keepAttributeSession = attributeSession != nil
	return runtime, nil
}

const previewFeatureLimit = 2000
const previewMinimumFeatures = 50000

// A preview is only safe when metadata bounds and the eventual render CRS
// describe the same coordinate space. Mixed-CRS layers need the full transform
// before normalization, so they take the existing full-load path.
func previewBounds(overviews []gdal.LayerOverview, layerName, sourceCRS, targetCRS string) ([4]float64, bool) {
	var bounds [4]float64
	var crs string
	selected := 0
	featureCount := 0
	for _, layer := range overviews {
		if layerName != "" && layer.Name != layerName {
			continue
		}
		if !layer.HasBounds || layer.FeatureCount < 0 {
			return bounds, false
		}
		layerCRS := layer.CRS.AuthorityCode
		if sourceCRS != "" {
			layerCRS = sourceCRS
		}
		if targetCRS != "" && !strings.EqualFold(targetCRS, layerCRS) {
			return bounds, false
		}
		if selected > 0 && !strings.EqualFold(crs, layerCRS) {
			return bounds, false
		}
		if selected == 0 {
			bounds = layer.Bounds
			crs = layerCRS
		} else {
			bounds[0] = math.Min(bounds[0], layer.Bounds[0])
			bounds[1] = math.Min(bounds[1], layer.Bounds[1])
			bounds[2] = math.Max(bounds[2], layer.Bounds[2])
			bounds[3] = math.Max(bounds[3], layer.Bounds[3])
		}
		featureCount += layer.FeatureCount
		selected++
	}
	return bounds, selected > 0 && featureCount >= previewMinimumFeatures
}

func tryReadOnlyPreview(ctx context.Context, session *gdal.AttributeSession, layerName, sourceCRS, targetCRS, input string, onPreview func(*demoRuntime)) {
	overviews, err := session.Inspect(ctx)
	if err != nil {
		return
	}
	bounds, ok := previewBounds(overviews, layerName, sourceCRS, targetCRS)
	if !ok {
		return
	}
	var layers []core.Layer
	if layerName == "" {
		layers, err = session.OpenAllGeometryPrefix(ctx, previewFeatureLimit)
	} else {
		var selected core.Layer
		selected, err = session.OpenGeometryPrefix(ctx, layerName, previewFeatureLimit)
		if err == nil {
			layers = []core.Layer{selected}
		}
	}
	if err != nil || ctx.Err() != nil {
		return
	}
	preview, err := buildDataRuntime(ctx, layers, input, sourceCRS, targetCRS, "", true, nil, &bounds)
	if err != nil || ctx.Err() != nil {
		return
	}
	// The retained session remains owned by the loader until the full snapshot
	// replaces the preview. No attribute lookups are issued against it yet.
	preview.attributePageReader = nil
	preview.attributeFeatureReader = nil
	onPreview(preview)
}

func buildDataRuntime(ctx context.Context, layers []core.Layer, input, sourceCRS, targetCRS, savePath string, readOnly bool, attributeSession *gdal.AttributeSession, bounds *[4]float64) (*demoRuntime, error) {
	var err error
	if sourceCRS != "" {
		for index := range layers {
			layers[index].CRS = core.CRS{AuthorityCode: sourceCRS}
		}
	}
	layers, err = alignLayerCRS(ctx, layers, targetCRS)
	if err != nil {
		return nil, fmt.Errorf("align CRS: %w", err)
	}
	var sources map[string]render.LayerSource
	var features []render.HitFeature
	var sourceErr error
	if bounds != nil {
		sources, features, sourceErr = render.NewLayerSourcesWithExtent(layers, *bounds)
	} else {
		sources, features, sourceErr = render.NewLayerSourcesWithFeatures(layers)
	}
	if sourceErr != nil {
		return nil, fmt.Errorf("prepare layers: %w", sourceErr)
	}
	layerNames := make([]string, 0, len(layers))
	for _, layer := range layers {
		layerNames = append(layerNames, layer.Name)
	}
	sort.Strings(layerNames)
	visibleLayers := make(map[string]bool, len(layerNames))
	for _, name := range layerNames {
		visibleLayers[name] = true
	}
	planner := render.NewChunkPlanner()
	if len(layers) > 0 {
		planner.ChunkSize = sources[layers[0].Name].ChunkSize
	}
	serviceLayers := layers
	if readOnly {
		// Geometry and WKB are already owned by render sources. Keeping a second
		// full layer snapshot in ProjectService would double the large-import
		// memory footprint even though read-only attribute data is fetched from
		// GDAL pages on demand.
		serviceLayers = layerMetadataOnly(layers)
	}
	loadedService := newLoadedProjectService(serviceLayers)
	runtime := &demoRuntime{
		scheduler:     render.NewScheduler(),
		batchStore:    render.NewBatchStore(),
		planner:       planner,
		visibility:    render.NewLayerVisibility(layerNames...),
		visibleLayers: visibleLayers,
		features:      features,
		service:       loadedService,
		dataMode:      true,
		readOnly:      readOnly,
	}
	runtime.builder = func(ctx context.Context, key render.ChunkKey) (render.Chunk, error) {
		source, ok := sources[key.Layer]
		if !ok {
			return render.Chunk{}, fmt.Errorf("render source for layer %q is missing", key.Layer)
		}
		return source.Builder(ctx, key)
	}
	if savePath != "" {
		writer := gdal.Writer{}
		runtime.persist = func(ctx context.Context, layerName string) error {
			return runtime.service.SaveLayer(ctx, writer, savePath, layerName)
		}
	}
	if attributeSession != nil {
		runtime.attributePageReader = func(ctx context.Context, layerName string, offset, limit int) (core.Layer, int, error) {
			return attributeSession.OpenAttributePage(ctx, layerName, offset, limit)
		}
		runtime.attributeFeatureReader = func(ctx context.Context, layerName string, featureID uint64) (core.Feature, error) {
			return attributeSession.OpenFeature(ctx, layerName, featureID)
		}
		runtime.closeAttributeSource = func() {
			_ = attributeSession.Close()
		}
	} else {
		runtime.attributePageReader = func(ctx context.Context, layerName string, offset, limit int) (core.Layer, int, error) {
			return (gdal.Reader{}).OpenAttributePage(ctx, input, layerName, offset, limit)
		}
	}
	if attributeSession == nil {
		runtime.attributeFeatureReader = func(ctx context.Context, layerName string, featureID uint64) (core.Feature, error) {
			return (gdal.Reader{}).OpenFeature(ctx, input, layerName, featureID)
		}
	}
	return runtime, nil
}

func layerMetadataOnly(layers []core.Layer) []core.Layer {
	metadata := make([]core.Layer, len(layers))
	for index, layer := range layers {
		metadata[index] = core.Layer{Name: layer.Name, CRS: layer.CRS}
	}
	return metadata
}

func (r *demoRuntime) replaceWith(next *demoRuntime) {
	r.replaceWithLoaded(next, 0)
}

func (r *demoRuntime) replaceWithLoaded(next *demoRuntime, expectedLoadGeneration uint64) {
	r.replaceWithLoadedMode(next, expectedLoadGeneration, false)
}

func (r *demoRuntime) replaceWithPreview(next *demoRuntime, expectedLoadGeneration uint64) {
	r.replaceWithLoadedMode(next, expectedLoadGeneration, true)
}

func (r *demoRuntime) replaceWithLoadedMode(next *demoRuntime, expectedLoadGeneration uint64, preview bool) {
	r.mu.Lock()
	if expectedLoadGeneration != 0 && expectedLoadGeneration != r.loadGeneration {
		r.mu.Unlock()
		if next != nil && next.closeAttributeSource != nil {
			next.closeAttributeSource()
		}
		return
	}
	previousRenderCancel := r.cancel
	r.cancel = nil
	previousAttributeCloser := r.closeAttributeSource
	r.scheduler = next.scheduler
	r.batchStore = next.batchStore
	r.planner = next.planner
	r.visibility = next.visibility
	r.visibleLayers = next.visibleLayers
	r.builder = next.builder
	r.features = next.features
	r.hitIndex = next.hitIndex
	r.hitIndexReady = next.hitIndexReady
	r.service = next.service
	r.dataMode = next.dataMode
	r.readOnly = next.readOnly
	r.previewLoading = preview
	r.persist = next.persist
	r.attributePageReader = next.attributePageReader
	r.attributeFeatureReader = next.attributeFeatureReader
	r.closeAttributeSource = next.closeAttributeSource
	r.attributeGeneration++
	r.attributeDispatchGeneration++
	if r.attributeCancel != nil {
		r.attributeCancel()
		r.attributeCancel = nil
	}
	r.attributeLayer = ""
	r.attributePage = 0
	r.attributeReady = false
	r.attributeCache = nil
	r.attributeCacheOrder = nil
	r.featureNameCacheLayer = ""
	r.featureNameCacheID = 0
	r.featureNameCacheValue = ""
	r.featureNameCacheReady = false
	r.selectionGeneration++
	if r.selectionCancel != nil {
		r.selectionCancel()
		r.selectionCancel = nil
	}
	if !preview {
		r.loadCancel = nil
	}
	r.hasSelect = false
	r.mu.Unlock()
	if previousRenderCancel != nil {
		previousRenderCancel()
	}
	if previousAttributeCloser != nil {
		previousAttributeCloser()
	}
	native.SetSelection("", "", "", "Loaded")
	r.refresh(context.Background(), render.Viewport{Center: render.Point{X: 0.5, Y: 0.5}, Zoom: 1})
	r.publishLayerTree()
	if preview {
		native.SetAttributePayload("[]")
		return
	}
	if names := next.service.LayerNames(); len(names) > 0 {
		r.publishAttributes(names[0])
	}
}

func alignLayerCRS(ctx context.Context, layers []core.Layer, targetAuthority string) ([]core.Layer, error) {
	target := core.CRS{AuthorityCode: targetAuthority}
	if target.AuthorityCode == "" {
		for _, layer := range layers {
			if layer.CRS.AuthorityCode != "" {
				target = layer.CRS
				break
			}
		}
	}
	if target.AuthorityCode == "" {
		return layers, nil
	}
	transformer := proj.Transformer{}
	aligned := make([]core.Layer, len(layers))
	grouped := make(map[string][]int)
	for index, layer := range layers {
		if layer.CRS.AuthorityCode == "" {
			return nil, fmt.Errorf("layer %q has no CRS while target is %s", layer.Name, target.AuthorityCode)
		}
		if strings.EqualFold(layer.CRS.AuthorityCode, target.AuthorityCode) {
			aligned[index] = layer
			continue
		}
		key := strings.ToUpper(strings.TrimSpace(layer.CRS.AuthorityCode))
		grouped[key] = append(grouped[key], index)
	}
	for _, indices := range grouped {
		group := make([]core.Layer, len(indices))
		for groupIndex, layerIndex := range indices {
			group[groupIndex] = layers[layerIndex]
		}
		converted, err := transformer.TransformLayers(ctx, group[0].CRS, target, group)
		if err != nil {
			return nil, err
		}
		for groupIndex, layerIndex := range indices {
			aligned[layerIndex] = converted[groupIndex]
		}
	}
	return aligned, nil
}

func desktopSavePath(args []string) string {
	for index := 1; index < len(args); index++ {
		if args[index] == "--save" && index+1 < len(args) {
			return args[index+1]
		}
	}
	return ""
}

func desktopReadOnly(args []string) bool {
	for index := 1; index < len(args); index++ {
		if args[index] == "--read-only" {
			return true
		}
	}
	return false
}

func desktopInputArgs(args []string) (string, string, string, string) {
	input, layer, sourceCRS, targetCRS := "", "", "", ""
	for index := 1; index < len(args); index++ {
		switch args[index] {
		case "--input":
			if index+1 < len(args) {
				input = args[index+1]
				index++
			}
		case "--layer":
			if index+1 < len(args) {
				layer = args[index+1]
				index++
			}
		case "--source-crs":
			if index+1 < len(args) {
				sourceCRS = args[index+1]
				index++
			}
		case "--target-crs":
			if index+1 < len(args) {
				targetCRS = args[index+1]
				index++
			}
		}
	}
	return input, layer, sourceCRS, targetCRS
}

func newLoadedProjectService(layers []core.Layer) *commands.ProjectService {
	if len(layers) == 0 {
		return commands.NewProjectService("loaded", core.CRS{})
	}
	service := commands.NewProjectService("loaded", layers[0].CRS)
	if err := service.BeginEdit(); err != nil {
		return service
	}
	for _, layer := range layers {
		if err := service.AddLayer(layer); err != nil {
			_ = service.Rollback()
			return service
		}
	}
	if err := service.Commit(); err != nil {
		_ = service.Rollback()
	}
	return service
}
