//go:build qt && native

package main

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"gogis/drivers/gdal"
	geosdriver "gogis/drivers/geos"
	"gogis/drivers/proj"
	"gogis/internal/commands"
	"gogis/internal/core"
	"gogis/internal/render"
	"gogis/internal/scripting"
	"gogis/internal/workspace"
	"gogis/ui/qt/native"
)

type readOnlyLayerBinding struct {
	session    *gdal.AttributeSession
	sourceName string
}

func loadRuntime(args []string) *demoRuntime {
	input, layerName, sourceCRS, targetCRS := desktopInputArgs(args)
	savePath := desktopSavePath(args)
	if input == "" {
		return loadEmptyProject()
	}
	var runtime *demoRuntime
	var err error
	if isWorkspacePath(input) {
		runtime, err = loadWorkspaceRuntime(context.Background(), input, desktopReadOnly(args) && savePath == "", savePath)
	} else {
		runtime, err = loadDataRuntimeMode(input, layerName, sourceCRS, targetCRS, savePath, desktopReadOnly(args) && savePath == "")
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "GoGIS: unable to load %q: %v\n", input, err)
		return loadEmptyProject()
	}
	runtime.refresh(context.Background(), runtimeInitialViewport(runtime))
	runtime.publishMapMetadata()
	runtime.publishLayerTree()
	if names := runtime.service.LayerNames(); len(names) > 0 {
		runtime.publishAttributes(names[0])
	}
	return runtime
}

func startInitialDataLoad(runtime *demoRuntime, args []string) {
	runtime.allowLargeEditable = desktopAllowLargeEditable(args)
	input, layerName, sourceCRS, targetCRS := desktopInputArgs(args)
	if input == "" {
		return
	}
	if isWorkspacePath(input) {
		runtime.startWorkspaceLoad(input)
		return
	}
	savePath := desktopSavePath(args)
	readOnly := desktopReadOnly(args) && savePath == ""
	runtime.startDataLoad(input, layerName, sourceCRS, targetCRS, savePath, readOnly)
}

func isWorkspacePath(path string) bool {
	return strings.EqualFold(filepath.Ext(path), ".gogis")
}

func normalizedSourcePath(path string) string {
	absolute, err := filepath.Abs(path)
	if err != nil {
		absolute = path
	}
	if resolved, resolveErr := filepath.EvalSymlinks(absolute); resolveErr == nil {
		absolute = resolved
	}
	return filepath.Clean(absolute)
}

func uniqueSourcePaths(paths []string) []string {
	seen := make(map[string]struct{}, len(paths))
	unique := make([]string, 0, len(paths))
	for _, path := range paths {
		if strings.TrimSpace(path) == "" {
			continue
		}
		key := normalizedSourcePath(path)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		unique = append(unique, path)
	}
	return unique
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
	native.BeginLoadTrace()
	native.SetRenderStatus("Loading " + input)
	go func() {
		loadAsReadOnly := readOnly
		largeReadOnly := false
		featureCount := 0
		if !loadAsReadOnly && savePath == "" && !r.allowLargeEditable {
			native.SetRenderStatus("Loading: checking feature count")
			var inspectErr error
			featureCount, largeReadOnly, inspectErr = inspectSourceFeatureCount(loadContext, []vectorSourceSpec{{Path: input}})
			if inspectErr != nil {
				if loadContext.Err() != nil {
					return
				}
				largeReadOnly = true
			}
			loadAsReadOnly = shouldOpenLargeDatasetReadOnly(featureCount, largeReadOnly, r.allowLargeEditable)
			largeReadOnly = loadAsReadOnly
		}
		var onPreview func(*demoRuntime)
		if loadAsReadOnly && savePath == "" && os.Getenv("GOGIS_DISABLE_PREVIEW") != "1" {
			onPreview = func(preview *demoRuntime) {
				r.replaceWithPreview(preview, generation)
			}
		}
		next, err := loadDataRuntimeModeContextWithPreview(loadContext, input, layerName, sourceCRS, targetCRS, savePath, loadAsReadOnly, onPreview)
		r.mu.Lock()
		current := generation == r.loadGeneration
		if current && err != nil {
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
		if largeReadOnly {
			native.SetRenderStatus(largeDatasetReadOnlyStatus(featureCount))
		}
	}()
}

func largeDatasetReadOnlyStatus(featureCount int) string {
	if featureCount < largeDatasetReadOnlyThreshold {
		return "Dataset size unavailable; opened read-only to limit memory; use --editable-large to edit"
	}
	return fmt.Sprintf("Dataset has at least %d features; opened read-only to limit memory. Use --editable-large to edit", featureCount)
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
	return loadDataRuntimeModeContextWithEncoding(ctx, input, layerName, sourceCRS, targetCRS, savePath, "", readOnly, onPreview)
}

func loadDataRuntimeModeContextWithEncoding(ctx context.Context, input, layerName, sourceCRS, targetCRS, savePath, encoding string, readOnly bool, onPreview func(*demoRuntime)) (*demoRuntime, error) {
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
		attributeSession, err = gdal.OpenAttributeSession(input, encoding)
		if err == nil {
			if onPreview != nil && savePath == "" {
				tryReadOnlyPreview(ctx, attributeSession, layerName, sourceCRS, targetCRS, input, encoding, onPreview)
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
			layers, err = (gdal.Reader{Encoding: encoding}).OpenAll(ctx, input)
		} else {
			var selected core.Layer
			selected, err = (gdal.Reader{Encoding: encoding}).Open(ctx, input, layerName)
			if err == nil {
				layers = []core.Layer{selected}
			}
		}
	}
	if err != nil {
		return nil, fmt.Errorf("open dataset: %w", err)
	}
	for index := range layers {
		layers[index].SourcePath = input
		layers[index].SourceLayerName = layers[index].Name
		layers[index].SourceEncoding = encoding
		layers[index].SourceCRS = layers[index].CRS.AuthorityCode
		if sourceCRS != "" {
			layers[index].SourceCRS = sourceCRS
		}
		layers[index].Visible = true
		layers[index] = layers[index].WithDefaultPresentation()
	}
	runtime, err := buildDataRuntime(ctx, layers, input, sourceCRS, targetCRS, savePath, encoding, readOnly, attributeSession, nil)
	if err != nil {
		return nil, err
	}
	if readOnly {
		runtime.readOnlySources = []vectorSourceSpec{{Path: input, LayerName: layerName, SourceCRS: sourceCRS, Encoding: encoding}}
		runtime.readOnlyDisplayCRS = runtime.mapCRS
	}
	keepAttributeSession = attributeSession != nil
	return runtime, nil
}

const previewFeatureLimit = 2000
const previewMinimumFeatures = 50000
const largeDatasetReadOnlyThreshold = 100000

func shouldOpenLargeDatasetReadOnly(featureCount int, unknownCount, allowEditable bool) bool {
	return !allowEditable && (unknownCount || featureCount >= largeDatasetReadOnlyThreshold)
}

func inspectSourceFeatureCount(ctx context.Context, sources []vectorSourceSpec) (int, bool, error) {
	total := 0
	unknownCount := false
	for _, source := range sources {
		if err := ctx.Err(); err != nil {
			return total, false, err
		}
		session, err := gdal.OpenAttributeSession(source.Path, source.Encoding)
		if err != nil {
			if source.AllowUnavailable {
				continue
			}
			return total, false, err
		}
		overviews, inspectErr := session.Inspect(ctx)
		closeErr := session.Close()
		if inspectErr != nil {
			if source.AllowUnavailable {
				continue
			}
			return total, false, inspectErr
		}
		if closeErr != nil {
			if source.AllowUnavailable {
				continue
			}
			return total, false, closeErr
		}
		for _, overview := range overviews {
			if source.LayerName != "" && overview.Name != source.LayerName {
				continue
			}
			if overview.FeatureCount < 0 {
				unknownCount = true
				continue
			}
			if overview.FeatureCount >= largeDatasetReadOnlyThreshold-total {
				return largeDatasetReadOnlyThreshold, true, nil
			}
			total += overview.FeatureCount
		}
	}
	return total, unknownCount, nil
}

func desktopAllowLargeEditable(args []string) bool {
	for _, arg := range args {
		if arg == "--editable-large" {
			return true
		}
	}
	return false
}

// A preview is only safe when metadata bounds and the eventual render CRS
// describe the same coordinate space. Mixed-CRS layers need the full transform
// before normalization, so they take the existing full-load path.
func previewBounds(overviews []gdal.LayerOverview, layerName, sourceCRS, targetCRS string) ([4]float64, bool) {
	var bounds [4]float64
	var crs string
	selected := 0
	featureCount := 0
	previewCount := 0
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
		previewCount += min(layer.FeatureCount, previewFeatureLimit)
		selected++
	}
	return bounds, selected > 0 && featureCount >= previewMinimumFeatures && previewCount*2 <= featureCount
}

func tryReadOnlyPreview(ctx context.Context, session *gdal.AttributeSession, layerName, sourceCRS, targetCRS, input, encoding string, onPreview func(*demoRuntime)) {
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
	preview, err := buildDataRuntime(ctx, layers, input, sourceCRS, targetCRS, "", encoding, true, nil, &bounds)
	if err != nil || ctx.Err() != nil {
		return
	}
	// The retained session remains owned by the loader until the full snapshot
	// replaces the preview. No attribute lookups are issued against it yet.
	preview.attributePageReader = nil
	preview.attributeFeatureReader = nil
	preview.readOnlySources = []vectorSourceSpec{{Path: input, LayerName: layerName, SourceCRS: sourceCRS, Encoding: encoding}}
	preview.readOnlyDisplayCRS = preview.mapCRS
	onPreview(preview)
}

func loadReadOnlyDataRuntime(ctx context.Context, sources []vectorSourceSpec, displayCRS string) (*demoRuntime, error) {
	if len(sources) == 0 {
		return nil, fmt.Errorf("at least one vector source is required")
	}
	sessions := make([]*gdal.AttributeSession, 0, len(sources))
	closeSessions := func() {
		for _, session := range sessions {
			_ = session.Close()
		}
	}
	loaded := false
	defer func() {
		if !loaded {
			closeSessions()
		}
	}()
	layers := make([]core.Layer, 0, len(sources))
	bindings := make(map[string]readOnlyLayerBinding)
	usedNames := make(map[string]bool)
	unavailable := make(map[string]unavailableSource)
	keepUnavailable := func(source vectorSourceSpec, sourceErr error) {
		layer := unavailableWorkspaceLayer(source)
		layers = append(layers, layer)
		usedNames[strings.ToLower(layer.Name)] = true
		unavailable[layer.Name] = unavailableSource{
			Path: source.Path, LayerName: source.LayerName, Encoding: source.Encoding, Reason: sourceErr.Error(),
		}
	}
	for _, source := range sources {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		session, err := gdal.OpenAttributeSession(source.Path, source.Encoding)
		if err != nil {
			if source.AllowUnavailable && ctx.Err() == nil {
				keepUnavailable(source, fmt.Errorf("open %q: %w", source.Path, err))
				continue
			}
			return nil, fmt.Errorf("open %q: %w", source.Path, err)
		}
		sessions = append(sessions, session)
		var opened []core.Layer
		if source.Labels.Enabled {
			reader := gdal.Reader{Encoding: source.Encoding}
			if source.LayerName == "" {
				opened, err = reader.OpenAll(ctx, source.Path)
			} else {
				var layer core.Layer
				layer, err = reader.Open(ctx, source.Path, source.LayerName)
				if err == nil {
					opened = []core.Layer{layer}
				}
			}
		} else if source.LayerName == "" {
			opened, err = session.OpenAllGeometryOnly(ctx)
		} else {
			var layer core.Layer
			layer, err = session.OpenGeometryOnly(ctx, source.LayerName)
			if err == nil {
				opened = []core.Layer{layer}
			}
		}
		if err != nil {
			if source.AllowUnavailable && ctx.Err() == nil {
				keepUnavailable(source, fmt.Errorf("read geometry from %q: %w", source.Path, err))
				continue
			}
			return nil, fmt.Errorf("read geometry from %q: %w", source.Path, err)
		}
		if len(opened) == 0 {
			err = fmt.Errorf("%q contains no vector layers", source.Path)
			if source.AllowUnavailable {
				keepUnavailable(source, err)
				continue
			}
			return nil, err
		}
		for _, layer := range opened {
			sourceLayerName := layer.Name
			layer.SourcePath = source.Path
			layer.SourceLayerName = sourceLayerName
			layer.SourceEncoding = source.Encoding
			layer.SourceCRS = layer.CRS.AuthorityCode
			if source.SourceCRS != "" {
				layer.SourceCRS = source.SourceCRS
				layer.CRS = core.CRS{AuthorityCode: source.SourceCRS}
			}
			if source.Name != "" {
				layer.Name = source.Name
			} else {
				layer.Name = uniqueImportedLayerName(layer.Name, source.Path, usedNames)
			}
			layer.DisplayName = source.DisplayName
			layer.Visible = source.Visible == nil || *source.Visible
			if source.Style != (core.LayerStyle{}) {
				layer.Style = source.Style
			}
			if source.Labels != (core.LabelSettings{}) {
				layer.Labels = source.Labels
			}
			layer = layer.WithDefaultPresentation()
			bindings[layer.Name] = readOnlyLayerBinding{session: session, sourceName: sourceLayerName}
			layers = append(layers, layer)
		}
	}
	runtime, err := buildDataRuntime(ctx, layers, "", "", displayCRS, "", "", true, nil, nil)
	if err != nil {
		return nil, err
	}
	runtime.attributePageReader = func(ctx context.Context, layerName string, offset, limit int) (core.Layer, int, error) {
		if _, missing := unavailable[layerName]; missing {
			layer, ok := runtime.service.LayerProperties(layerName)
			if !ok {
				return core.Layer{}, 0, fmt.Errorf("layer %q not found", layerName)
			}
			return layer, 0, nil
		}
		binding, ok := bindings[layerName]
		if !ok {
			return core.Layer{}, 0, fmt.Errorf("layer %q not found", layerName)
		}
		return binding.session.OpenAttributePage(ctx, binding.sourceName, offset, limit)
	}
	runtime.attributeFeatureReader = func(ctx context.Context, layerName string, featureID uint64) (core.Feature, error) {
		binding, ok := bindings[layerName]
		if !ok {
			return core.Feature{}, fmt.Errorf("layer %q not found", layerName)
		}
		return binding.session.OpenFeature(ctx, binding.sourceName, featureID)
	}
	runtime.closeAttributeSource = closeSessions
	runtime.readOnlySources = append([]vectorSourceSpec(nil), sources...)
	runtime.readOnlyDisplayCRS = runtime.mapCRS
	runtime.unavailableSources = unavailable
	loaded = true
	return runtime, nil
}

func unavailableWorkspaceLayer(source vectorSourceSpec) core.Layer {
	name := source.Name
	if name == "" {
		name = strings.TrimSuffix(filepath.Base(source.Path), filepath.Ext(source.Path))
	}
	crs := source.SourceCRS
	if crs == "" {
		crs = source.FallbackCRS
	}
	visible := source.Visible == nil || *source.Visible
	return core.Layer{
		Name: name, DisplayName: source.DisplayName, SourcePath: source.Path,
		SourceLayerName: source.LayerName, SourceEncoding: source.Encoding, SourceCRS: source.SourceCRS,
		CRS: core.CRS{AuthorityCode: crs}, Visible: visible, Style: source.Style, Labels: source.Labels,
	}.WithDefaultPresentation()
}

func uniqueImportedLayerName(original, sourcePath string, used map[string]bool) string {
	name := original
	if used[strings.ToLower(name)] {
		stem := strings.TrimSuffix(filepath.Base(sourcePath), filepath.Ext(sourcePath))
		name = stem + "_" + original
	}
	candidate := name
	for suffix := 2; used[strings.ToLower(candidate)]; suffix++ {
		candidate = fmt.Sprintf("%s_%d", name, suffix)
	}
	used[strings.ToLower(candidate)] = true
	return candidate
}

func buildDataRuntime(ctx context.Context, layers []core.Layer, input, sourceCRS, targetCRS, savePath, sourceEncoding string, readOnly bool, attributeSession *gdal.AttributeSession, bounds *[4]float64) (*demoRuntime, error) {
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
	if err := prepareLayerLabels(ctx, layers); err != nil {
		return nil, err
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
	if err := attachPolygonFillGeometry(ctx, layers, sources); err != nil {
		return nil, fmt.Errorf("prepare polygon fills: %w", err)
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
	for _, layer := range layers {
		if !layer.Visible {
			visibleLayers[layer.Name] = false
		}
	}
	planner := render.NewChunkPlanner()
	if len(layers) > 0 {
		planner.ChunkSize = sources[layers[0].Name].ChunkSize
	}
	mapExtent := [4]float64{0, 0, 1, 1}
	mapCRS := ""
	if len(layers) > 0 {
		mapExtent = sources[layers[0].Name].Extent
		mapCRS = layers[0].CRS.AuthorityCode
	}
	serviceLayers := layers
	if readOnly {
		// Geometry and WKB are already owned by render sources. Keeping a second
		// full layer snapshot in ProjectService would double the large-import
		// memory footprint even though read-only attribute data is fetched from
		// GDAL pages on demand.
		serviceLayers = layerMetadataOnly(layers)
	}
	loadedService, err := newLoadedProjectService(serviceLayers)
	if err != nil {
		return nil, fmt.Errorf("create project service: %w", err)
	}
	layerStyleMu := &sync.RWMutex{}
	mapLabels := make([]render.LayerLabel, 0)
	for _, name := range layerNames {
		mapLabels = append(mapLabels, sources[name].Labels...)
	}
	runtime := &demoRuntime{
		scheduler:          render.NewScheduler(),
		batchStore:         render.NewBatchStore(),
		planner:            planner,
		visibility:         render.NewLayerVisibility(layerNames...),
		visibleLayers:      visibleLayers,
		layerStyles:        make(map[string]core.LayerStyle, len(layers)),
		baseLayerStyles:    make(map[string]core.LayerStyle, len(layers)),
		layerGeometryTypes: make(map[string]string, len(layers)),
		layerStyleMu:       layerStyleMu,
		mapLabels:          mapLabels,
		features:           features,
		sources:            sources,
		sourcesMu:          &sync.RWMutex{},
		service:            loadedService,
		dataMode:           true,
		readOnly:           readOnly,
		mapExtent:          mapExtent,
		mapCRS:             mapCRS,
		saveDestination:    savePath,
	}
	for _, layer := range layers {
		style := layer.Style
		if style == (core.LayerStyle{}) {
			style = core.DefaultLayerStyle()
		}
		geometryType := ""
		if len(layer.Features) > 0 && layer.Features[0].Geometry != nil {
			geometryType = layer.Features[0].Geometry.GeometryType()
		}
		runtime.layerStyles[layer.Name] = style
		runtime.baseLayerStyles[layer.Name] = style
		runtime.layerGeometryTypes[layer.Name] = geometryType
	}
	runtime.builder = func(ctx context.Context, key render.ChunkKey) (render.Chunk, error) {
		runtime.sourcesMu.RLock()
		source, ok := sources[key.Layer]
		runtime.sourcesMu.RUnlock()
		if !ok {
			return render.Chunk{}, fmt.Errorf("render source for layer %q is missing", key.Layer)
		}
		chunk, err := source.Builder(ctx, key)
		if err != nil {
			return render.Chunk{}, err
		}
		layerStyleMu.RLock()
		style := runtime.layerStyles[key.Layer]
		baseStyle := runtime.baseLayerStyles[key.Layer]
		geometryType := runtime.layerGeometryTypes[key.Layer]
		layerStyleMu.RUnlock()
		if style != baseStyle {
			chunk.Vertices = append([]render.Vertex(nil), chunk.Vertices...)
			for index := range chunk.Vertices {
				vertex := &chunk.Vertices[index]
				switch vertex.Kind {
				case render.VertexFill:
					vertex.Color = render.ColorForPolygonFill(style)
				case render.VertexPoint:
					vertex.Color = render.ColorForGeometry(style, "POINT")
					chunk.Vertices[index].SizeMM = float32(style.PointSizeMM)
				default:
					vertex.Color = render.ColorForGeometry(style, geometryType)
					chunk.Vertices[index].SizeMM = float32(style.LineWidthMM)
				}
			}
		}
		return chunk, nil
	}
	if savePath != "" {
		configureRuntimePersistence(runtime, savePath)
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
			return (gdal.Reader{Encoding: sourceEncoding}).OpenAttributePage(ctx, input, layerName, offset, limit)
		}
	}
	if attributeSession == nil {
		runtime.attributeFeatureReader = func(ctx context.Context, layerName string, featureID uint64) (core.Feature, error) {
			return (gdal.Reader{Encoding: sourceEncoding}).OpenFeature(ctx, input, layerName, featureID)
		}
	}
	return runtime, nil
}

func (r *demoRuntime) rebuildEditedLayer(layerName string) error {
	r.mu.Lock()
	service := r.service
	bounds := r.mapExtent
	r.mu.Unlock()
	if service == nil || r.sourcesMu == nil {
		return fmt.Errorf("render sources are unavailable")
	}
	layer, ok := service.ProjectLayerRenderSnapshot(layerName)
	if !ok {
		return fmt.Errorf("layer %q was not found", layerName)
	}
	if err := prepareLayerLabels(context.Background(), []core.Layer{layer}); err != nil {
		return err
	}
	newSources, newFeatures, err := render.NewLayerSourcesWithExtent([]core.Layer{layer}, bounds)
	if err != nil {
		return err
	}
	if err := attachPolygonFillGeometry(context.Background(), []core.Layer{layer}, newSources); err != nil {
		return err
	}
	source := newSources[layerName]
	r.sourcesMu.Lock()
	r.sources[layerName] = source
	r.sourcesMu.Unlock()
	r.mu.Lock()
	features := make([]render.HitFeature, 0, len(r.features)-len(source.Features)+len(newFeatures))
	for _, feature := range r.features {
		if feature.Layer != layerName {
			features = append(features, feature)
		}
	}
	features = append(features, newFeatures...)
	r.features = features
	r.hitIndexReady = false
	labels := make([]render.LayerLabel, 0)
	r.sourcesMu.RLock()
	for _, current := range r.sources {
		labels = append(labels, current.Labels...)
	}
	r.sourcesMu.RUnlock()
	r.mapLabels = labels
	selected := r.selected
	r.mu.Unlock()
	r.publishLayerLabels()
	r.refreshCurrentViewport()
	r.publishVertexHandles(selected, newFeatures, false)
	return nil
}

func attachPolygonFillGeometry(ctx context.Context, layers []core.Layer, sources map[string]render.LayerSource) error {
	for _, layer := range layers {
		isPolygonLayer := false
		for _, feature := range layer.Features {
			if feature.Geometry != nil && strings.Contains(strings.ToUpper(feature.Geometry.GeometryType()), "POLYGON") {
				isPolygonLayer = true
				break
			}
		}
		if !isPolygonLayer {
			continue
		}
		source, ok := sources[layer.Name]
		if !ok {
			return fmt.Errorf("render source for polygon layer %q is missing", layer.Name)
		}
		bounds := source.Extent
		spanX, spanY := bounds[2]-bounds[0], bounds[3]-bounds[1]
		if spanX <= 0 || spanY <= 0 {
			continue
		}
		fillByCell := make(map[[2]int][]render.Vertex)
		operator := geosdriver.NewOperator()
		for _, feature := range layer.Features {
			if err := ctx.Err(); err != nil {
				return err
			}
			if feature.Geometry == nil || !strings.Contains(strings.ToUpper(feature.Geometry.GeometryType()), "POLYGON") {
				continue
			}
			triangles, err := operator.ConstrainedTriangles(ctx, feature.Geometry)
			if err != nil {
				return fmt.Errorf("layer %q feature %d: %w", layer.Name, feature.ID, err)
			}
			for _, triangle := range triangles {
				var normalized [3]render.Point
				minX, minY := math.Inf(1), math.Inf(1)
				maxX, maxY := math.Inf(-1), math.Inf(-1)
				for index, point := range triangle {
					normalized[index] = render.Point{X: (point[0] - bounds[0]) / spanX, Y: (point[1] - bounds[1]) / spanY}
					minX, minY = math.Min(minX, normalized[index].X), math.Min(minY, normalized[index].Y)
					maxX, maxY = math.Max(maxX, normalized[index].X), math.Max(maxY, normalized[index].Y)
				}
				const chunkSize = 0.25
				firstX, lastX := max(0, int(math.Floor(minX/chunkSize))), min(3, int(math.Floor(maxX/chunkSize)))
				firstY, lastY := max(0, int(math.Floor(minY/chunkSize))), min(3, int(math.Floor(maxY/chunkSize)))
				for cellY := firstY; cellY <= lastY; cellY++ {
					for cellX := firstX; cellX <= lastX; cellX++ {
						clipped := render.ClipTriangleToRect(normalized,
							float64(cellX)*chunkSize, float64(cellY)*chunkSize,
							float64(cellX+1)*chunkSize, float64(cellY+1)*chunkSize)
						for _, part := range clipped {
							vertices := fillByCell[[2]int{cellX, cellY}]
							for _, point := range part {
								vertices = append(vertices, render.Vertex{X: float32(point.X), Y: float32(point.Y),
									Color: render.ColorForPolygonFill(layer.Style), Kind: render.VertexFill})
							}
							fillByCell[[2]int{cellX, cellY}] = vertices
						}
					}
				}
			}
		}
		baseBuilder := source.Builder
		source.Builder = func(buildContext context.Context, key render.ChunkKey) (render.Chunk, error) {
			chunk, err := baseBuilder(buildContext, key)
			if err != nil {
				return render.Chunk{}, err
			}
			fill := fillByCell[[2]int{key.X, key.Y}]
			if len(fill) == 0 {
				return chunk, nil
			}
			vertices := make([]render.Vertex, 0, len(fill)+len(chunk.Vertices))
			vertices = append(vertices, fill...)
			vertices = append(vertices, chunk.Vertices...)
			chunk.Vertices = vertices
			return chunk, nil
		}
		sources[layer.Name] = source
	}
	return nil
}

func prepareLayerLabels(ctx context.Context, layers []core.Layer) error {
	for layerIndex := range layers {
		settings := layers[layerIndex].Labels
		if !settings.Enabled || len(layers[layerIndex].Features) == 0 {
			continue
		}
		var textProgram, ruleProgram *scripting.LabelProgram
		var err error
		if strings.TrimSpace(settings.LuaScript) != "" {
			textProgram, err = scripting.CompileLabelProgram(settings.LuaScript)
			if err != nil {
				return fmt.Errorf("layer %q label script: %w", layers[layerIndex].Name, err)
			}
			defer textProgram.Close()
		}
		if strings.TrimSpace(settings.Rule) != "" {
			ruleProgram, err = scripting.CompileLabelProgram(settings.Rule)
			if err != nil {
				if textProgram != nil {
					textProgram.Close()
				}
				return fmt.Errorf("layer %q label rule: %w", layers[layerIndex].Name, err)
			}
			defer ruleProgram.Close()
		}
		var geometryOperator *geosdriver.Operator
		for featureIndex := range layers[layerIndex].Features {
			if err := ctx.Err(); err != nil {
				return err
			}
			feature := &layers[layerIndex].Features[featureIndex]
			if ruleProgram != nil {
				visible, evalErr := ruleProgram.EvaluateRule(ctx, feature.Properties)
				if evalErr != nil {
					return fmt.Errorf("layer %q feature %d label rule: %w", layers[layerIndex].Name, feature.ID, evalErr)
				}
				if !visible {
					feature.Label = nil
					continue
				}
			}
			var text string
			if textProgram != nil {
				text, err = textProgram.EvaluateText(ctx, feature.Properties)
			} else {
				text, err = core.EvaluateLabelTemplate(settings.Expression, feature.Properties)
			}
			if err != nil {
				return fmt.Errorf("layer %q feature %d label text: %w", layers[layerIndex].Name, feature.ID, err)
			}
			if text == "" {
				feature.Label = nil
				continue
			}
			label := core.Label{Text: text, Height: settings.HeightMM}
			if feature.Geometry != nil && strings.Contains(strings.ToUpper(feature.Geometry.GeometryType()), "POLYGON") {
				if geometryOperator == nil {
					geometryOperator = geosdriver.NewOperator()
				}
				anchor, found, anchorErr := geometryOperator.PointOnSurface(ctx, feature.Geometry)
				if anchorErr != nil {
					return fmt.Errorf("layer %q feature %d label anchor: %w", layers[layerIndex].Name, feature.ID, anchorErr)
				}
				if found {
					label.X, label.Y, label.AnchorSet = anchor[0], anchor[1], true
				}
			}
			rotation := 0.0
			if settings.RotationField != "" {
				value, exists := feature.Properties[settings.RotationField]
				if !exists {
					return fmt.Errorf("layer %q feature %d rotation field %q is missing", layers[layerIndex].Name, feature.ID, settings.RotationField)
				}
				if value != nil && strings.TrimSpace(fmt.Sprint(value)) != "" {
					parsedRotation, parseErr := strconv.ParseFloat(strings.TrimSpace(fmt.Sprint(value)), 64)
					if parseErr != nil || math.IsNaN(parsedRotation) || math.IsInf(parsedRotation, 0) {
						return fmt.Errorf("layer %q feature %d rotation field %q must contain a finite number", layers[layerIndex].Name, feature.ID, settings.RotationField)
					}
					rotation = parsedRotation
				}
			}
			label.Rotation = rotation
			feature.Label = &label
		}
	}
	return nil
}

func configureRuntimePersistence(runtime *demoRuntime, destination string) {
	writer := gdal.Writer{}
	service := runtime.service
	runtime.saveDestination = destination
	runtime.persist = func(ctx context.Context, layerName string) error {
		if strings.EqualFold(filepath.Ext(destination), ".gpkg") {
			return service.SaveAllLayers(ctx, writer, destination)
		}
		if len(service.LayerNames()) > 1 {
			return fmt.Errorf("cannot persist multiple layers to Shapefile; save the project as GeoPackage")
		}
		return service.SaveLayer(ctx, writer, destination, layerName)
	}
}

func (r *demoRuntime) publishMapMetadata() {
	r.mu.Lock()
	crs, extent, savedView := r.mapCRS, r.mapExtent, r.workspaceView
	r.mu.Unlock()
	var view *native.MapViewState
	if savedView != nil {
		view = &native.MapViewState{
			CenterX: savedView.CenterX, CenterY: savedView.CenterY,
			Zoom: savedView.Zoom, ActiveLayer: savedView.ActiveLayer,
		}
	}
	native.SetMapMetadataWithView(crs, extent, view)
}

func loadDataRuntimeFiles(ctx context.Context, paths []string, baseLayers []core.Layer, saveDestination string) (*demoRuntime, error) {
	sources := make([]vectorSourceSpec, len(paths))
	for index, path := range paths {
		sources[index] = vectorSourceSpec{Path: path}
	}
	return loadDataRuntimeSources(ctx, sources, baseLayers, saveDestination)
}

func loadDataRuntimeFilesWithLargePolicy(ctx context.Context, paths []string, baseLayers []core.Layer, saveDestination string, allowLargeEditable bool) (*demoRuntime, bool, int, error) {
	featureCount := 0
	if len(baseLayers) == 0 && saveDestination == "" && !allowLargeEditable {
		sources := make([]vectorSourceSpec, len(paths))
		for index, path := range paths {
			sources[index] = vectorSourceSpec{Path: path}
		}
		count, unknownCount, inspectErr := inspectSourceFeatureCount(ctx, sources)
		featureCount = count
		if inspectErr != nil {
			if ctx.Err() != nil {
				return nil, false, featureCount, ctx.Err()
			}
			unknownCount = true
		}
		if shouldOpenLargeDatasetReadOnly(featureCount, unknownCount, allowLargeEditable) {
			runtime, err := loadReadOnlyDataRuntime(ctx, sources, "")
			return runtime, true, featureCount, err
		}
	}
	runtime, err := loadDataRuntimeFiles(ctx, paths, baseLayers, saveDestination)
	return runtime, false, featureCount, err
}

func loadDataRuntimeSources(ctx context.Context, sources []vectorSourceSpec, baseLayers []core.Layer, saveDestination string) (*demoRuntime, error) {
	return loadDataRuntimeSourcesWithCRS(ctx, sources, baseLayers, saveDestination, "")
}

func loadDataRuntimeSourcesWithCRS(ctx context.Context, sources []vectorSourceSpec, baseLayers []core.Layer, saveDestination, targetCRS string) (*demoRuntime, error) {
	if len(sources) == 0 {
		return nil, fmt.Errorf("at least one vector file is required")
	}
	layers := make([]core.Layer, 0, len(baseLayers)+len(sources))
	usedNames := make(map[string]bool, len(baseLayers)+len(sources))
	unavailable := make(map[string]unavailableSource)
	for _, layer := range baseLayers {
		layers = append(layers, layer)
		usedNames[strings.ToLower(layer.Name)] = true
	}
	appendLayer := func(layer core.Layer, source vectorSourceSpec, offset int) {
		if !source.InsertAtSet {
			layers = append(layers, layer)
			return
		}
		index := source.InsertAt + offset
		if index < 0 {
			index = 0
		}
		if index > len(layers) {
			index = len(layers)
		}
		layers = append(layers, core.Layer{})
		copy(layers[index+1:], layers[index:])
		layers[index] = layer
	}
	for _, source := range sources {
		path := source.Path
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		reader := gdal.Reader{Encoding: source.Encoding}
		var opened []core.Layer
		var err error
		if source.LayerName != "" {
			var layer core.Layer
			layer, err = reader.Open(ctx, path, source.LayerName)
			if err == nil {
				opened = []core.Layer{layer}
			}
		} else {
			opened, err = reader.OpenAll(ctx, path)
		}
		if err != nil {
			if source.AllowUnavailable && ctx.Err() == nil {
				layer := unavailableWorkspaceLayer(source)
				appendLayer(layer, source, 0)
				usedNames[strings.ToLower(layer.Name)] = true
				unavailable[layer.Name] = unavailableSource{
					Path: source.Path, LayerName: source.LayerName, Encoding: source.Encoding,
					Reason: fmt.Errorf("open %q: %w", path, err).Error(),
				}
				continue
			}
			return nil, fmt.Errorf("open %q: %w", path, err)
		}
		if len(opened) == 0 {
			err = fmt.Errorf("%q contains no vector layers", path)
			if source.AllowUnavailable {
				layer := unavailableWorkspaceLayer(source)
				appendLayer(layer, source, 0)
				usedNames[strings.ToLower(layer.Name)] = true
				unavailable[layer.Name] = unavailableSource{
					Path: source.Path, LayerName: source.LayerName, Encoding: source.Encoding, Reason: err.Error(),
				}
				continue
			}
			return nil, err
		}
		for layerIndex, layer := range opened {
			sourceLayerName := layer.Name
			if source.LayerName != "" {
				sourceLayerName = source.LayerName
			}
			layer.SourcePath = path
			layer.SourceLayerName = sourceLayerName
			layer.SourceEncoding = source.Encoding
			layer.SourceCRS = source.SourceCRS
			if layer.SourceCRS == "" {
				layer.SourceCRS = layer.CRS.AuthorityCode
			}
			if source.Name != "" {
				layer.Name = source.Name
			} else {
				layer.Name = uniqueImportedLayerName(layer.Name, path, usedNames)
			}
			layer.DisplayName = source.DisplayName
			layer.Visible = source.Visible == nil || *source.Visible
			if source.Style != (core.LayerStyle{}) {
				layer.Style = source.Style
			}
			if source.Labels != (core.LabelSettings{}) {
				layer.Labels = source.Labels
			}
			layer = layer.WithDefaultPresentation()
			appendLayer(layer, source, layerIndex)
		}
	}
	runtime, err := buildDataRuntime(ctx, layers, "", "", targetCRS, "", "", false, nil, nil)
	if err != nil {
		return nil, err
	}
	runtime.unavailableSources = unavailable
	runtime.attributePageReader = func(_ context.Context, layerName string, offset, limit int) (core.Layer, int, error) {
		page, total, ok := runtime.service.LayerAttributePageOwned(layerName, offset, limit)
		if !ok {
			return core.Layer{}, 0, fmt.Errorf("layer %q not found", layerName)
		}
		return page, total, nil
	}
	if saveDestination != "" {
		configureRuntimePersistence(runtime, saveDestination)
	}
	return runtime, nil
}

func layerMetadataOnly(layers []core.Layer) []core.Layer {
	metadata := make([]core.Layer, len(layers))
	for index, layer := range layers {
		metadata[index] = core.Layer{
			Name: layer.Name, DisplayName: layer.DisplayName, SourcePath: layer.SourcePath, SourceLayerName: layer.SourceLayerName,
			SourceEncoding: layer.SourceEncoding, SourceCRS: layer.SourceCRS, CRS: layer.CRS,
			Visible: layer.Visible, Style: layer.Style, Labels: layer.Labels,
		}
	}
	return metadata
}

func (r *demoRuntime) saveDataset(destination string) {
	if destination == "" {
		native.SetRenderStatus("Save cancelled")
		return
	}
	if strings.EqualFold(filepath.Ext(destination), ".gogis") {
		r.saveWorkspace(destination)
		return
	}
	if !strings.EqualFold(filepath.Ext(destination), ".gpkg") {
		native.SetRenderStatus("Save failed: choose a .gpkg destination")
		return
	}
	r.mu.Lock()
	service, readOnly := r.service, r.readOnly
	r.mu.Unlock()
	if readOnly {
		native.SetRenderStatus("Save failed: read-only dataset is not editable")
		return
	}
	if service == nil {
		native.SetRenderStatus("Save failed: no project is loaded")
		return
	}
	native.SetRenderStatus("Saving " + destination)
	writer := gdal.Writer{}
	if err := service.SaveAllLayers(context.Background(), writer, destination); err != nil {
		native.SetRenderStatus("Save failed: " + err.Error())
		return
	}
	r.mu.Lock()
	r.persist = func(ctx context.Context, layerName string) error {
		if strings.EqualFold(filepath.Ext(destination), ".gpkg") {
			return service.SaveAllLayers(ctx, writer, destination)
		}
		return service.SaveLayer(ctx, writer, destination, layerName)
	}
	r.saveDestination = destination
	r.mu.Unlock()
	native.SetRenderStatus("Saved " + destination)
}

func (r *demoRuntime) saveWorkspace(destination string) {
	r.mu.Lock()
	service := r.service
	r.mu.Unlock()
	if service == nil {
		native.SetRenderStatus("Workspace save failed: no project is loaded")
		return
	}
	name, crs := service.ProjectInfo()
	project := core.Project{Name: name, CRS: crs, Layers: service.ProjectLayerProperties()}
	doc := workspace.FromProject(project)
	view := workspaceViewFromViewport(native.CurrentViewport(), native.CurrentActiveLayer())
	doc.View = &view
	if err := workspace.Save(destination, doc); err != nil {
		native.SetRenderStatus("Workspace save failed: " + err.Error())
		return
	}
	native.SetRenderStatus("Workspace saved " + destination)
}

func workspaceViewFromViewport(view native.Viewport, activeLayer string) workspace.ViewState {
	zoom := view.Zoom
	if math.IsNaN(zoom) || math.IsInf(zoom, 0) || zoom <= 0 {
		zoom = 1
	}
	centerX, centerY := 0.5, 0.5
	if view.Width > 0 && view.Height > 0 && !math.IsNaN(view.PanX) && !math.IsInf(view.PanX, 0) &&
		!math.IsNaN(view.PanY) && !math.IsInf(view.PanY, 0) {
		centerX = 0.5 - view.PanX/(view.Width*zoom)
		centerY = 0.5 + view.PanY/(view.Height*zoom)
	}
	return workspace.ViewState{CenterX: centerX, CenterY: centerY, Zoom: zoom, ActiveLayer: activeLayer}
}

func (r *demoRuntime) startDataLoadPaths(paths []string) {
	if len(paths) == 1 && isWorkspacePath(paths[0]) {
		r.startWorkspaceLoad(paths[0])
		return
	}
	paths = uniqueSourcePaths(paths)
	if len(paths) == 0 {
		native.SetRenderStatus("No vector files selected")
		return
	}
	r.mu.Lock()
	if r.loadCancel != nil {
		r.mu.Unlock()
		native.SetRenderStatus("A file load is already in progress")
		return
	}
	loadContext, cancel := context.WithCancel(context.Background())
	r.loadCancel = cancel
	r.loadGeneration++
	generation := r.loadGeneration
	service := r.service
	appendLayers := r.dataMode && service != nil
	readOnly := r.readOnly
	saveDestination := r.saveDestination
	readOnlySources := append([]vectorSourceSpec(nil), r.readOnlySources...)
	displayCRS := r.readOnlyDisplayCRS
	previousVisibility := make(map[string]bool)
	if appendLayers {
		previousVisibility = make(map[string]bool, len(r.visibleLayers))
		for name, visible := range r.visibleLayers {
			previousVisibility[name] = visible
		}
	}
	r.mu.Unlock()
	native.SetRenderStatus(fmt.Sprintf("Loading %d vector file(s)", len(paths)))
	var baseLayers []core.Layer
	if appendLayers && !readOnly {
		knownSources := make(map[string]struct{})
		for _, layer := range service.ProjectLayerProperties() {
			if layer.SourcePath != "" {
				knownSources[normalizedSourcePath(layer.SourcePath)] = struct{}{}
			}
		}
		filtered := paths[:0]
		for _, path := range paths {
			if _, exists := knownSources[normalizedSourcePath(path)]; !exists {
				filtered = append(filtered, path)
			}
		}
		paths = filtered
		if len(paths) == 0 {
			cancel()
			r.mu.Lock()
			if generation == r.loadGeneration {
				r.loadCancel = nil
			}
			r.mu.Unlock()
			native.SetRenderStatus("Selected source is already loaded")
			return
		}
		// Clone feature data only after filtering duplicate paths. Project() is
		// intentionally detached for callers that will rebuild the runtime.
		baseLayers = service.Project().Layers
	}
	if appendLayers && readOnly && len(readOnlySources) == 0 {
		cancel()
		r.mu.Lock()
		if generation == r.loadGeneration {
			r.loadCancel = nil
		}
		r.mu.Unlock()
		native.SetRenderStatus("Cannot add files until the read-only source is ready")
		return
	}
	if readOnly {
		knownSources := make(map[string]struct{}, len(readOnlySources))
		for _, source := range readOnlySources {
			knownSources[normalizedSourcePath(source.Path)] = struct{}{}
		}
		filtered := paths[:0]
		for _, path := range paths {
			if _, exists := knownSources[normalizedSourcePath(path)]; !exists {
				filtered = append(filtered, path)
			}
		}
		paths = filtered
		if len(paths) == 0 {
			cancel()
			r.mu.Lock()
			if generation == r.loadGeneration {
				r.loadCancel = nil
			}
			r.mu.Unlock()
			native.SetRenderStatus("Selected source is already loaded")
			return
		}
		for _, path := range paths {
			readOnlySources = append(readOnlySources, vectorSourceSpec{Path: path})
		}
	}
	native.BeginLoadTrace()
	go func() {
		var next *demoRuntime
		var err error
		largeReadOnly := false
		featureCount := 0
		if readOnly {
			next, err = loadReadOnlyDataRuntime(loadContext, readOnlySources, displayCRS)
		} else {
			if !appendLayers && saveDestination == "" && !r.allowLargeEditable {
				native.SetRenderStatus("Loading: checking feature count")
			}
			next, largeReadOnly, featureCount, err = loadDataRuntimeFilesWithLargePolicy(
				loadContext, paths, baseLayers, saveDestination, r.allowLargeEditable)
		}
		r.mu.Lock()
		current := generation == r.loadGeneration
		if current && err != nil {
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
		if appendLayers {
			r.mu.Lock()
			oldExtent := r.mapExtent
			r.mu.Unlock()
			oldView := native.CurrentViewport()
			activeLayer := native.CurrentActiveLayer()
			next.workspaceView = preserveMapWorldView(oldView, oldExtent, next.mapExtent, activeLayer)
		}
		for name, visible := range previousVisibility {
			if _, exists := next.visibleLayers[name]; exists {
				next.visibleLayers[name] = visible
				next.visibility.Set(name, visible)
			}
		}
		r.replaceWithLoaded(next, generation)
		if largeReadOnly {
			native.SetRenderStatus(largeDatasetReadOnlyStatus(featureCount))
		}
	}()
}

func preserveMapWorldView(view native.Viewport, oldExtent, newExtent [4]float64, activeLayer string) *workspace.ViewState {
	oldView := workspaceViewFromViewport(view, activeLayer)
	oldSpanX, oldSpanY := oldExtent[2]-oldExtent[0], oldExtent[3]-oldExtent[1]
	newSpanX, newSpanY := newExtent[2]-newExtent[0], newExtent[3]-newExtent[1]
	if oldSpanX <= 0 || oldSpanY <= 0 || newSpanX <= 0 || newSpanY <= 0 {
		return &oldView
	}
	worldX := oldExtent[0] + oldView.CenterX*oldSpanX
	worldY := oldExtent[1] + oldView.CenterY*oldSpanY
	oldView.CenterX = (worldX - newExtent[0]) / newSpanX
	oldView.CenterY = (worldY - newExtent[1]) / newSpanY
	return &oldView
}

func loadWorkspaceRuntime(ctx context.Context, path string, readOnly bool, saveDestination string) (*demoRuntime, error) {
	runtime, _, _, err := loadWorkspaceRuntimeWithLargePolicy(ctx, path, readOnly, saveDestination, false)
	return runtime, err
}

func loadWorkspaceRuntimeWithLargePolicy(ctx context.Context, path string, readOnly bool, saveDestination string, allowLargeEditable bool) (*demoRuntime, bool, int, error) {
	doc, err := workspace.Load(path)
	if err != nil {
		return nil, false, 0, err
	}
	project, err := doc.Project()
	if err != nil {
		return nil, false, 0, err
	}
	sources := make([]vectorSourceSpec, len(project.Layers))
	for index, layer := range project.Layers {
		visible := layer.Visible
		fallbackCRS := layer.CRS.AuthorityCode
		if fallbackCRS == "" {
			fallbackCRS = project.CRS.AuthorityCode
		}
		sources[index] = vectorSourceSpec{
			Path: layer.SourcePath, LayerName: layer.SourceLayerName, SourceCRS: layer.SourceCRS,
			FallbackCRS: fallbackCRS, AllowUnavailable: true,
			Encoding: layer.SourceEncoding, Name: layer.Name, DisplayName: layer.DisplayName,
			Visible: &visible, Style: layer.Style, Labels: layer.Labels,
		}
	}
	largeReadOnly := false
	featureCount := 0
	if !readOnly && saveDestination == "" && !allowLargeEditable {
		count, unknownCount, inspectErr := inspectSourceFeatureCount(ctx, sources)
		featureCount = count
		if inspectErr != nil {
			if ctx.Err() != nil {
				return nil, false, featureCount, ctx.Err()
			}
			unknownCount = true
		}
		largeReadOnly = shouldOpenLargeDatasetReadOnly(featureCount, unknownCount, allowLargeEditable)
		readOnly = largeReadOnly
	}
	var runtime *demoRuntime
	if readOnly {
		runtime, err = loadReadOnlyDataRuntime(ctx, sources, project.CRS.AuthorityCode)
	} else {
		runtime, err = loadDataRuntimeSourcesWithCRS(ctx, sources, nil, saveDestination, project.CRS.AuthorityCode)
	}
	if err != nil {
		return nil, largeReadOnly, featureCount, fmt.Errorf("load workspace sources: %w", err)
	}
	if runtime.service != nil {
		runtime.service.SetProjectInfo(project.Name, project.CRS)
	}
	runtime.saveDestination = saveDestination
	if doc.View != nil {
		view := *doc.View
		runtime.workspaceView = &view
	}
	runtime.readOnlyDisplayCRS = project.CRS.AuthorityCode
	return runtime, largeReadOnly, featureCount, nil
}

func (r *demoRuntime) startWorkspaceLoad(path string) {
	r.mu.Lock()
	if r.loadCancel != nil {
		r.mu.Unlock()
		native.SetRenderStatus("A file load is already in progress")
		return
	}
	readOnly := r.readOnly
	saveDestination := r.saveDestination
	ctx, cancel := context.WithCancel(context.Background())
	r.loadCancel = cancel
	r.loadGeneration++
	generation := r.loadGeneration
	r.mu.Unlock()
	native.SetRenderStatus("Loading workspace " + filepath.Base(path))
	go func() {
		next, largeReadOnly, featureCount, err := loadWorkspaceRuntimeWithLargePolicy(
			ctx, path, readOnly, saveDestination, r.allowLargeEditable)
		r.mu.Lock()
		current := generation == r.loadGeneration
		if current && err != nil {
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
			native.SetRenderStatus("Workspace load failed: " + err.Error())
			return
		}
		warningNames := make([]string, 0, len(next.unavailableSources))
		for name := range next.unavailableSources {
			warningNames = append(warningNames, name)
		}
		sort.Strings(warningNames)
		r.replaceWithLoaded(next, generation)
		if len(warningNames) > 0 {
			native.SetRenderStatus("Workspace loaded; relink unavailable layers: " + strings.Join(warningNames, ", "))
		} else if largeReadOnly {
			native.SetRenderStatus(largeDatasetReadOnlyStatus(featureCount))
		} else {
			native.SetRenderStatus("Workspace loaded " + filepath.Base(path))
		}
	}()
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

func runtimeInitialViewport(runtime *demoRuntime) render.Viewport {
	viewport := render.Viewport{Center: render.Point{X: 0.5, Y: 0.5}, Zoom: 1}
	if runtime != nil && runtime.workspaceView != nil {
		viewport.Center = render.Point{X: runtime.workspaceView.CenterX, Y: runtime.workspaceView.CenterY}
		viewport.Zoom = runtime.workspaceView.Zoom
	}
	return viewport
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
	r.publishedRevision = 0
	r.planner = next.planner
	r.visibility = next.visibility
	r.visibleLayers = next.visibleLayers
	r.layerStyles = next.layerStyles
	r.baseLayerStyles = next.baseLayerStyles
	r.layerGeometryTypes = next.layerGeometryTypes
	r.layerStyleMu = next.layerStyleMu
	r.mapLabels = next.mapLabels
	r.builder = next.builder
	r.features = next.features
	r.hitIndex = next.hitIndex
	r.hitIndexReady = next.hitIndexReady
	r.service = next.service
	r.dataMode = next.dataMode
	r.readOnly = next.readOnly
	r.previewLoading = preview
	r.mapExtent = next.mapExtent
	r.mapCRS = next.mapCRS
	r.workspaceView = next.workspaceView
	r.saveDestination = next.saveDestination
	r.readOnlySources = next.readOnlySources
	r.readOnlyDisplayCRS = next.readOnlyDisplayCRS
	r.unavailableSources = next.unavailableSources
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
	r.publishMapMetadata()
	r.refresh(context.Background(), runtimeInitialViewport(next))
	r.publishLayerTree()
	r.publishLayerLabels()
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
		if len(layer.Features) == 0 {
			aligned[index] = layer
			continue
		}
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

func newLoadedProjectService(layers []core.Layer) (*commands.ProjectService, error) {
	crs := core.CRS{}
	if len(layers) > 0 {
		crs = layers[0].CRS
	}
	return commands.NewProjectServiceWithLayers("loaded", crs, layers)
}
