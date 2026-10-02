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
	"time"

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
	layer      core.Layer
	sourceCRS  string
}

type contextSemaphore struct {
	permits chan struct{}
}

func newContextSemaphore(limit int) *contextSemaphore {
	if limit < 1 {
		limit = 1
	}
	return &contextSemaphore{permits: make(chan struct{}, limit)}
}

func (semaphore *contextSemaphore) acquire(ctx context.Context) (func(), error) {
	select {
	case semaphore.permits <- struct{}{}:
		if err := ctx.Err(); err != nil {
			<-semaphore.permits
			return nil, err
		}
		var once sync.Once
		return func() { once.Do(func() { <-semaphore.permits }) }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Runtime replacement may leave canceled GEOS/native work winding down while
// the new runtime starts. Share this gate across runtimes so their per-runtime
// worker limits cannot multiply the large temporary window allocations.
var readOnlyWindowBuildSemaphore = newContextSemaphore(2)

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
		var inspectionSession *gdal.AttributeSession
		if !loadAsReadOnly && savePath == "" && !r.allowLargeEditable {
			native.SetRenderStatus("Loading: checking feature count")
			var inspectErr error
			featureCount, largeReadOnly, inspectionSession, inspectErr = inspectSingleSourceFeatureCount(loadContext, vectorSourceSpec{Path: input})
			if inspectErr != nil {
				if loadContext.Err() != nil {
					if inspectionSession != nil {
						_ = inspectionSession.Close()
					}
					return
				}
				largeReadOnly = true
			}
			loadAsReadOnly = shouldOpenLargeDatasetReadOnly(featureCount, largeReadOnly, r.allowLargeEditable)
			largeReadOnly = loadAsReadOnly
		}
		if !loadAsReadOnly && inspectionSession != nil {
			_ = inspectionSession.Close()
			inspectionSession = nil
		}
		var onPreview func(*demoRuntime)
		if loadAsReadOnly && savePath == "" && os.Getenv("GOGIS_DISABLE_PREVIEW") != "1" {
			onPreview = func(preview *demoRuntime) {
				r.replaceWithPreview(preview, generation)
			}
		}
		next, err := loadDataRuntimeModeContextWithPreviewAndSession(loadContext, input, layerName, sourceCRS, targetCRS, savePath, loadAsReadOnly, onPreview, inspectionSession)
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
		return "Dataset size unavailable; opened read-only to limit memory"
	}
	return fmt.Sprintf("Dataset has at least %d features; opened read-only to limit memory", featureCount)
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
	return loadDataRuntimeModeContextWithPreviewAndSession(ctx, input, layerName, sourceCRS, targetCRS, savePath, readOnly, onPreview, nil)
}

func loadDataRuntimeModeContextWithPreviewAndSession(ctx context.Context, input, layerName, sourceCRS, targetCRS, savePath string, readOnly bool, onPreview func(*demoRuntime), initialSession *gdal.AttributeSession) (*demoRuntime, error) {
	return loadDataRuntimeModeContextWithEncodingAndSession(ctx, input, layerName, sourceCRS, targetCRS, savePath, "", readOnly, onPreview, initialSession)
}

func loadDataRuntimeModeContextWithEncoding(ctx context.Context, input, layerName, sourceCRS, targetCRS, savePath, encoding string, readOnly bool, onPreview func(*demoRuntime)) (*demoRuntime, error) {
	return loadDataRuntimeModeContextWithEncodingAndSession(ctx, input, layerName, sourceCRS, targetCRS, savePath, encoding, readOnly, onPreview, nil)
}

func loadDataRuntimeModeContextWithEncodingAndSession(ctx context.Context, input, layerName, sourceCRS, targetCRS, savePath, encoding string, readOnly bool, onPreview func(*demoRuntime), initialSession *gdal.AttributeSession) (_ *demoRuntime, resultErr error) {
	defer func() {
		if initialSession != nil {
			_ = initialSession.Close()
		}
	}()
	if readOnly && savePath == "" {
		// Reuse the inspection/preview dataset for metadata and subsequent
		// viewport queries. Reopening a large GeoJSON source repeats GDAL's
		// source scan and can raise peak memory during load.
		if initialSession == nil {
			var sessionErr error
			initialSession, sessionErr = gdal.OpenAttributeSession(input, encoding)
			if sessionErr != nil {
				initialSession = nil
			}
		}
		if initialSession != nil {
			if onPreview != nil {
				tryReadOnlyPreview(ctx, initialSession, layerName, sourceCRS, targetCRS, input, encoding, onPreview)
			}
			sessionForWindowedLoad := initialSession
			initialSession = nil // ownership transfers to the windowed loader
			windowed, ok, err := tryLoadWindowedReadOnlyRuntimeWithSession(ctx, []vectorSourceSpec{{
				Path: input, LayerName: layerName, SourceCRS: sourceCRS, Encoding: encoding,
			}}, targetCRS, sessionForWindowedLoad)
			if err != nil || ok {
				return windowed, err
			}
		}
	}
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

// inspectSingleSourceFeatureCount retains the opened dataset session when its
// feature count indicates a read-only load, avoiding another native dataset
// open between the safety check and the windowed runtime.
func inspectSingleSourceFeatureCount(ctx context.Context, source vectorSourceSpec) (int, bool, *gdal.AttributeSession, error) {
	if err := ctx.Err(); err != nil {
		return 0, false, nil, err
	}
	session, err := gdal.OpenAttributeSession(source.Path, source.Encoding)
	if err != nil {
		return 0, false, nil, err
	}
	overviews, err := session.Inspect(ctx)
	if err != nil {
		_ = session.Close()
		return 0, false, nil, err
	}
	total := 0
	unknownCount := false
	for _, overview := range overviews {
		if source.LayerName != "" && source.LayerName != overview.Name {
			continue
		}
		if overview.FeatureCount < 0 {
			unknownCount = true
			continue
		}
		if overview.FeatureCount >= largeDatasetReadOnlyThreshold-total {
			return largeDatasetReadOnlyThreshold, true, session, nil
		}
		total += overview.FeatureCount
	}
	if shouldOpenLargeDatasetReadOnly(total, unknownCount, false) {
		return total, unknownCount, session, nil
	}
	if err := session.Close(); err != nil {
		return total, unknownCount, nil, err
	}
	return total, unknownCount, nil, nil
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
	return loadReadOnlyDataRuntimeWithBaseLayers(ctx, sources, displayCRS, nil, nil)
}

func loadReadOnlyDataRuntimeWithBaseLayers(ctx context.Context, sources []vectorSourceSpec, displayCRS string, baseLayers []core.Layer, baseExtent *[4]float64) (*demoRuntime, error) {
	if len(sources) == 0 {
		return nil, fmt.Errorf("at least one vector source is required")
	}
	if runtime, ok, err := tryLoadWindowedReadOnlyRuntimeWithBaseLayers(ctx, sources, displayCRS, baseLayers, baseExtent); err != nil || ok {
		return runtime, err
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
	layers := append([]core.Layer(nil), baseLayers...)
	bindings := make(map[string]readOnlyLayerBinding)
	usedNames := make(map[string]bool, len(baseLayers)+len(sources))
	for _, layer := range baseLayers {
		usedNames[strings.ToLower(layer.Name)] = true
	}
	unavailable := make(map[string]unavailableSource)
	materializedFeatures, materializedBytes := 0, int64(0)
	var err error
	for _, layer := range baseLayers {
		materializedFeatures, materializedBytes, err = accumulateMaterializedRuntimeLayerUsage(
			materializedFeatures, materializedBytes, layer, maxDesktopMaterializedFeatures, maxDesktopMaterializedBytes)
		if err != nil {
			return nil, fmt.Errorf("read-only base project: %w", err)
		}
	}
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
			materializedFeatures, materializedBytes, err = accumulateMaterializedRuntimeLayerUsage(
				materializedFeatures, materializedBytes, layer, maxDesktopMaterializedFeatures, maxDesktopMaterializedBytes)
			if err != nil {
				return nil, fmt.Errorf("read-only materialized fallback for %q: %w", source.Path, err)
			}
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
			bindings[layer.Name] = readOnlyLayerBinding{session: session, sourceName: sourceLayerName, layer: layer, sourceCRS: layer.SourceCRS}
			layers = append(layers, layer)
		}
	}
	runtime, err := buildDataRuntime(ctx, layers, "", "", displayCRS, "", "", true, nil, nil)
	if err != nil {
		return nil, err
	}
	runtime.attributePageReader = func(ctx context.Context, layerName string, offset, limit int) (core.Layer, int, error) {
		for _, baseLayer := range baseLayers {
			if baseLayer.Name == layerName {
				return inMemoryLayerAttributePage(baseLayer, offset, limit)
			}
		}
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
		for _, baseLayer := range baseLayers {
			if baseLayer.Name == layerName {
				for _, feature := range baseLayer.Features {
					if feature.ID == featureID {
						return feature, nil
					}
				}
				return core.Feature{}, fmt.Errorf("feature %d not found in layer %q", featureID, layerName)
			}
		}
		binding, ok := bindings[layerName]
		if !ok {
			return core.Feature{}, fmt.Errorf("layer %q not found", layerName)
		}
		return binding.session.OpenFeature(ctx, binding.sourceName, featureID)
	}
	runtime.closeAttributeSource = closeSessions
	runtime.readOnlySources = append([]vectorSourceSpec(nil), sources...)
	runtime.readOnlyBaseLayers = baseLayers
	runtime.readOnlyDisplayCRS = runtime.mapCRS
	runtime.unavailableSources = unavailable
	loaded = true
	return runtime, nil
}

func inMemoryLayerAttributePage(layer core.Layer, offset, limit int) (core.Layer, int, error) {
	if offset < 0 || limit <= 0 || limit > 1_000 {
		return core.Layer{}, 0, fmt.Errorf("invalid attribute page: offset=%d limit=%d", offset, limit)
	}
	total := len(layer.Features)
	if offset >= total {
		layer.Features = nil
		return layer, total, nil
	}
	end := offset + min(limit, total-offset)
	layer.Features = layer.Features[offset:end]
	return layer, total, nil
}

// tryLoadWindowedReadOnlyRuntime keeps only layer metadata resident and queries
// feature geometry for the requested render chunks. It is intentionally used
// only when every participating layer has a known CRS and extent. Different
// source CRSs are transformed into one display CRS while only each requested
// source window is resident.
func tryLoadWindowedReadOnlyRuntime(ctx context.Context, sources []vectorSourceSpec, displayCRS string) (*demoRuntime, bool, error) {
	return tryLoadWindowedReadOnlyRuntimeWithSession(ctx, sources, displayCRS, nil)
}

// tryLoadWindowedReadOnlyRuntimeWithSession optionally takes ownership of an
// already-open dataset session. This lets the large-file preview and the
// steady-state window renderer share one GDAL dataset without exposing GDAL
// handles outside the read-only runtime lifecycle.
func tryLoadWindowedReadOnlyRuntimeWithSession(ctx context.Context, sources []vectorSourceSpec, displayCRS string, initialSession *gdal.AttributeSession) (*demoRuntime, bool, error) {
	return tryLoadWindowedReadOnlyRuntimeWithBaseLayersAndSession(ctx, sources, displayCRS, nil, nil, initialSession)
}

func tryLoadWindowedReadOnlyRuntimeWithBaseLayers(ctx context.Context, sources []vectorSourceSpec, displayCRS string, baseLayers []core.Layer, baseExtent *[4]float64) (*demoRuntime, bool, error) {
	return tryLoadWindowedReadOnlyRuntimeWithBaseLayersAndSession(ctx, sources, displayCRS, baseLayers, baseExtent, nil)
}

func tryLoadWindowedReadOnlyRuntimeWithBaseLayersAndSession(ctx context.Context, sources []vectorSourceSpec, displayCRS string, baseLayers []core.Layer, baseExtent *[4]float64, initialSession *gdal.AttributeSession) (*demoRuntime, bool, error) {
	if len(sources) == 0 {
		if initialSession != nil {
			_ = initialSession.Close()
		}
		return nil, false, nil
	}
	sessions := make([]*gdal.AttributeSession, 0, len(sources))
	defer func() {
		if initialSession != nil {
			_ = initialSession.Close()
		}
	}()
	closeSessions := func() {
		for _, session := range sessions {
			_ = session.Close()
		}
	}
	keep := false
	defer func() {
		if !keep {
			closeSessions()
		}
	}()
	baseFeatureCount, basePayloadBytes := 0, int64(0)
	var budgetErr error
	for _, layer := range baseLayers {
		baseFeatureCount, basePayloadBytes, budgetErr = accumulateMaterializedRuntimeLayerUsage(
			baseFeatureCount, basePayloadBytes, layer, maxDesktopMaterializedFeatures, maxDesktopMaterializedBytes)
		if budgetErr != nil {
			return nil, true, fmt.Errorf("read-only base project: %w", budgetErr)
		}
	}
	layers := append([]core.Layer(nil), baseLayers...)
	bindings := make(map[string]readOnlyLayerBinding)
	unavailable := make(map[string]unavailableSource)
	usedNames := make(map[string]bool, len(baseLayers))
	var extent [4]float64
	hasExtent := false
	targetCRS := strings.TrimSpace(displayCRS)
	if baseExtent != nil {
		extent, hasExtent = *baseExtent, true
	}
	for _, layer := range baseLayers {
		usedNames[strings.ToLower(layer.Name)] = true
		if targetCRS == "" {
			targetCRS = layer.CRS.AuthorityCode
		}
	}
	remainingGeoJSONIndexFeatures := maxDesktopGeoJSONIndexFeatures
	addUnavailable := func(source vectorSourceSpec, reason error) {
		layer := unavailableWorkspaceLayer(source)
		if source.Name != "" {
			layer.Name = source.Name
		}
		if source.DisplayName != "" {
			layer.DisplayName = source.DisplayName
		}
		layers = append(layers, layer.WithDefaultPresentation())
		usedNames[strings.ToLower(layer.Name)] = true
		unavailable[layer.Name] = unavailableSource{
			Path: source.Path, LayerName: source.LayerName, Encoding: source.Encoding, Reason: reason.Error(),
		}
	}
	for sourceIndex, source := range sources {
		if err := ctx.Err(); err != nil {
			return nil, true, err
		}
		if source.AllowUnavailable {
			if _, statErr := os.Stat(source.Path); statErr != nil {
				addUnavailable(source, fmt.Errorf("source %q is unavailable: %w", source.Path, statErr))
				continue
			}
		}
		var session *gdal.AttributeSession
		var err error
		if sourceIndex == 0 && initialSession != nil {
			session, initialSession = initialSession, nil
		} else {
			session, err = gdal.OpenAttributeSession(source.Path, source.Encoding)
		}
		if err != nil {
			if source.AllowUnavailable {
				addUnavailable(source, fmt.Errorf("open %q: %w", source.Path, err))
				continue
			}
			return nil, true, fmt.Errorf("open %q: %w", source.Path, err)
		}
		sessions = append(sessions, session)
		indexLimit := min(remainingGeoJSONIndexFeatures, 1_000_000)
		streamIndexApplied, err := session.SetGeoJSONStreamIndexFeatureLimit(indexLimit)
		if err != nil {
			return nil, true, fmt.Errorf("configure source index for %q: %w", source.Path, err)
		}
		overviews, err := session.Inspect(ctx)
		if err != nil {
			if source.AllowUnavailable {
				_ = session.Close()
				sessions = sessions[:len(sessions)-1]
				addUnavailable(source, fmt.Errorf("inspect %q: %w", source.Path, err))
				continue
			}
			return nil, true, fmt.Errorf("inspect %q: %w", source.Path, err)
		}
		if streamIndexApplied && len(overviews) == 1 && overviews[0].FeatureCount >= 0 && overviews[0].FeatureCount <= indexLimit {
			remainingGeoJSONIndexFeatures -= overviews[0].FeatureCount
		}
		selected := overviews[:0]
		for _, overview := range overviews {
			if source.LayerName == "" || strings.EqualFold(source.LayerName, overview.Name) {
				selected = append(selected, overview)
			}
		}
		if len(selected) == 0 {
			if source.AllowUnavailable {
				_ = session.Close()
				sessions = sessions[:len(sessions)-1]
				addUnavailable(source, fmt.Errorf("layer %q not found in %q", source.LayerName, source.Path))
				continue
			}
			return nil, true, fmt.Errorf("layer %q not found in %q", source.LayerName, source.Path)
		}
		for _, overview := range selected {
			sourceCRS := overview.CRS.AuthorityCode
			if source.SourceCRS != "" {
				sourceCRS = source.SourceCRS
			} else if sourceCRS == "" {
				sourceCRS = source.FallbackCRS
			}
			if metadataErr := validateReadOnlyWindowMetadata(overview.Name, sourceCRS, overview.HasBounds); metadataErr != nil {
				return nil, true, metadataErr
			}
			if targetCRS == "" {
				targetCRS = sourceCRS
			}
			targetBounds := overview.Bounds
			if !strings.EqualFold(sourceCRS, targetCRS) {
				targetBounds, err = (proj.Transformer{}).TransformBounds(ctx,
					core.CRS{AuthorityCode: sourceCRS}, core.CRS{AuthorityCode: targetCRS}, overview.Bounds)
				if err != nil {
					return nil, true, fmt.Errorf("transform extent for %q from %s to %s: %w", overview.Name, sourceCRS, targetCRS, err)
				}
			}
			if !hasExtent {
				extent = targetBounds
				hasExtent = true
			} else {
				extent[0] = math.Min(extent[0], targetBounds[0])
				extent[1] = math.Min(extent[1], targetBounds[1])
				extent[2] = math.Max(extent[2], targetBounds[2])
				extent[3] = math.Max(extent[3], targetBounds[3])
			}
			page, _, err := session.OpenAttributePage(ctx, overview.Name, 0, 1)
			if err != nil {
				return nil, true, fmt.Errorf("read schema for %q: %w", overview.Name, err)
			}
			layer := core.Layer{
				Name: overview.Name, Fields: page.Fields, SourcePath: source.Path,
				SourceLayerName: overview.Name, SourceEncoding: source.Encoding,
				SourceCRS: sourceCRS, CRS: core.CRS{AuthorityCode: targetCRS},
				Visible:     source.Visible == nil || *source.Visible,
				DisplayName: source.DisplayName, Style: source.Style, Labels: source.Labels,
			}
			if source.Name != "" {
				layer.Name = source.Name
			} else {
				layer.Name = uniqueImportedLayerName(layer.Name, source.Path, usedNames)
			}
			if source.Style == (core.LayerStyle{}) {
				layer.Style = core.DefaultLayerStyle()
			}
			if source.Labels == (core.LabelSettings{}) {
				layer.Labels = core.DefaultLabelSettings()
			}
			layer = layer.WithDefaultPresentation()
			layers = append(layers, layer)
			bindings[layer.Name] = readOnlyLayerBinding{session: session, sourceName: overview.Name, layer: layer, sourceCRS: sourceCRS}
		}
	}
	if !hasExtent {
		return nil, false, nil
	}
	for index := range layers {
		if layers[index].CRS.AuthorityCode == "" {
			layers[index].CRS = core.CRS{AuthorityCode: targetCRS}
		}
	}
	runtime, err := buildDataRuntime(ctx, layers, "", "", targetCRS, "", "", true, nil, &extent)
	if err != nil {
		return nil, true, err
	}
	runtime.viewportReadOnly = true
	// Each window can spend its entire decoded-payload budget and run GEOS
	// triangulation. Limit concurrency so per-window temporary allocations do
	// not multiply across the scheduler's normal worker pool.
	runtime.scheduler = render.NewSchedulerWithMaxWorkers(2)
	runtime.windowHits = make(map[render.ChunkKey][]render.HitFeature)
	runtime.windowFeatureCounts = make(map[render.ChunkKey]int)
	runtime.windowFeatureIDs = make(map[render.ChunkKey][]uint64)
	runtime.windowPayloadBytes = make(map[render.ChunkKey]int64)
	runtime.windowVisibleKeys = make(map[render.ChunkKey]struct{})
	runtime.windowFeatureNames = make(map[uint64]string)
	runtime.windowLabels = make(map[render.ChunkKey][]render.LayerLabel)
	runtime.unavailableSources = unavailable
	runtime.readOnlyBaseLayers = append([]core.Layer(nil), baseLayers...)
	runtime.readOnlyBaseFeatures = append([]render.HitFeature(nil), runtime.features...)
	runtime.readOnlyBaseLabels = append([]render.LayerLabel(nil), runtime.mapLabels...)
	runtime.windowVisibleFeatureCount = len(runtime.readOnlyBaseFeatures)
	for _, layer := range baseLayers {
		runtime.windowVisiblePayloadBytes += estimateReadOnlyWindowPayloadBytes(layer)
		for _, feature := range layer.Features {
			if feature.ID > runtime.nextWindowFeatureID {
				runtime.nextWindowFeatureID = feature.ID
			}
		}
	}
	for _, label := range runtime.readOnlyBaseLabels {
		runtime.windowVisiblePayloadBytes += int64(len(label.Text) + 64)
	}
	runtime.attributePageReader = func(ctx context.Context, layerName string, offset, limit int) (core.Layer, int, error) {
		for _, layer := range baseLayers {
			if layer.Name == layerName {
				return inMemoryLayerAttributePage(layer, offset, limit)
			}
		}
		binding, ok := bindings[layerName]
		if !ok {
			return core.Layer{}, 0, fmt.Errorf("layer %q not found", layerName)
		}
		return binding.session.OpenAttributePage(ctx, binding.sourceName, offset, limit)
	}
	runtime.attributeFeatureReader = func(_ context.Context, layerName string, featureID uint64) (core.Feature, error) {
		for _, layer := range baseLayers {
			if layer.Name == layerName {
				for _, feature := range layer.Features {
					if feature.ID == featureID {
						return feature, nil
					}
				}
				return core.Feature{}, fmt.Errorf("feature %d not found in layer %q", featureID, layerName)
			}
		}
		runtime.mu.Lock()
		name, ok := runtime.windowFeatureNames[featureID]
		runtime.mu.Unlock()
		if !ok {
			return core.Feature{}, fmt.Errorf("feature %d is outside the current render window", featureID)
		}
		return core.Feature{ID: featureID, Properties: map[string]any{"name": name}}, nil
	}
	baseBuilder := runtime.builder
	runtime.builder = func(ctx context.Context, key render.ChunkKey) (render.Chunk, error) {
		if _, isWindowLayer := bindings[key.Layer]; !isWindowLayer {
			if _, missing := unavailable[key.Layer]; !missing && baseBuilder != nil {
				return baseBuilder(ctx, key)
			}
		}
		binding, ok := bindings[key.Layer]
		if !ok {
			if _, missing := unavailable[key.Layer]; missing {
				return render.Chunk{Key: key}, nil
			}
			return render.Chunk{}, fmt.Errorf("render source for layer %q is missing", key.Layer)
		}
		releaseWindowSlot, err := readOnlyWindowBuildSemaphore.acquire(ctx)
		if err != nil {
			return render.Chunk{}, err
		}
		defer releaseWindowSlot()
		chunkSize := readOnlyWindowChunkSize(key.ZoomBucket)
		queryBounds, ok := renderChunkBounds(runtime.mapExtent, chunkSize, key)
		if !ok {
			return render.Chunk{Key: key}, nil
		}
		if !strings.EqualFold(binding.sourceCRS, binding.layer.CRS.AuthorityCode) {
			queryBounds, err = (proj.Transformer{}).TransformBounds(ctx,
				binding.layer.CRS, core.CRS{AuthorityCode: binding.sourceCRS}, queryBounds)
			if err != nil {
				return render.Chunk{}, fmt.Errorf("transform query bounds for %s: %w", key.Layer, err)
			}
		}
		// Properties are needed for label expressions and for the selected
		// feature's display name. The layer snapshot is chunk-scoped and dropped
		// after vertex generation; the runtime retains only the small name map.
		window, err := binding.session.OpenWindowWithLimits(ctx, binding.sourceName, queryBounds, true,
			maxReadOnlyWindowFeatures, maxReadOnlyWindowBytes)
		if err != nil {
			return render.Chunk{}, fmt.Errorf("query %s window: %w", key.Layer, err)
		}
		if err := validateReadOnlyWindowPolygonVertexBudget(window, maxReadOnlyWindowPolygonVertices); err != nil {
			return render.Chunk{}, err
		}
		window.Name, window.CRS = binding.layer.Name, core.CRS{AuthorityCode: binding.sourceCRS}
		window.Fields, window.Style, window.Labels = binding.layer.Fields, binding.layer.Style, binding.layer.Labels
		if !strings.EqualFold(binding.sourceCRS, binding.layer.CRS.AuthorityCode) {
			window, err = (proj.Transformer{}).Transform(ctx,
				core.CRS{AuthorityCode: binding.sourceCRS}, binding.layer.CRS, window)
			if err != nil {
				return render.Chunk{}, fmt.Errorf("transform %s window: %w", key.Layer, err)
			}
		}
		if len(window.Features) == 0 {
			runtime.mu.Lock()
			runtime.removeWindowChunkLocked(key)
			runtime.rebuildWindowFeaturesLocked()
			runtime.mu.Unlock()
			return render.Chunk{Key: key}, nil
		}
		payloadBytes := estimateReadOnlyWindowPayloadBytes(window)
		if payloadBytes > maxReadOnlyWindowBytes {
			return render.Chunk{}, fmt.Errorf("spatial window exceeds the %d MiB geometry/property budget", maxReadOnlyWindowBytes>>20)
		}
		window = window.WithDefaultPresentation()
		featureNames := make(map[uint64]string, len(window.Features))
		runtime.mu.Lock()
		for index := range window.Features {
			runtime.nextWindowFeatureID++
			window.Features[index].ID = runtime.nextWindowFeatureID
			name := fmt.Sprint(window.Features[index].Properties["name"])
			if name == "<nil>" || name == "" {
				name = fmt.Sprintf("%s feature #%d", key.Layer, runtime.nextWindowFeatureID)
			}
			featureNames[runtime.nextWindowFeatureID] = name
		}
		runtime.mu.Unlock()
		if err := prepareLayerLabels(ctx, []core.Layer{window}); err != nil {
			return render.Chunk{}, err
		}
		newSources, hits, err := render.NewLayerSourcesWithExtentAndChunkSizeForChunk([]core.Layer{window}, runtime.mapExtent, chunkSize, key)
		if err != nil {
			return render.Chunk{}, err
		}
		for _, label := range newSources[key.Layer].Labels {
			if payloadBytes > maxReadOnlyWindowBytes-int64(len(label.Text)+64) {
				return render.Chunk{}, fmt.Errorf("spatial window labels exceed the %d MiB payload budget", maxReadOnlyWindowBytes>>20)
			}
			payloadBytes += int64(len(label.Text) + 64)
		}
		if err := attachPolygonFillGeometryForChunk(ctx, []core.Layer{window}, newSources, &key); err != nil {
			return render.Chunk{}, err
		}
		runtime.mu.Lock()
		for index := range window.Features {
			if index < len(hits) {
				hits[index].FeatureID = window.Features[index].ID
			}
		}
		if _, visible := runtime.windowVisibleKeys[key]; visible {
			runtime.removeWindowChunkLocked(key)
			if runtime.windowVisibleFeatureCount+len(window.Features) > maxReadOnlyVisibleFeatures {
				runtime.mu.Unlock()
				return render.Chunk{}, fmt.Errorf("visible read-only data exceeds the %d-feature safety limit", maxReadOnlyVisibleFeatures)
			}
			if runtime.windowVisiblePayloadBytes+payloadBytes > maxReadOnlyVisibleBytes {
				runtime.mu.Unlock()
				return render.Chunk{}, fmt.Errorf("visible read-only data exceeds the %d MiB geometry/property budget", maxReadOnlyVisibleBytes>>20)
			}
			runtime.windowVisibleFeatureCount += len(window.Features)
			runtime.windowVisiblePayloadBytes += payloadBytes
			runtime.windowFeatureCounts[key] = len(window.Features)
			runtime.windowPayloadBytes[key] = payloadBytes
			for id, name := range featureNames {
				runtime.windowFeatureNames[id] = name
			}
			ids := make([]uint64, 0, len(featureNames))
			for id := range featureNames {
				ids = append(ids, id)
			}
			runtime.windowFeatureIDs[key] = ids
			if len(hits) > 0 {
				runtime.windowHits[key] = hits
			}
			if len(newSources[key.Layer].Labels) > 0 {
				runtime.windowLabels[key] = newSources[key.Layer].Labels
			}
			runtime.rebuildWindowFeaturesLocked()
		}
		runtime.mu.Unlock()
		return newSources[key.Layer].Builder(ctx, key)
	}
	runtime.closeAttributeSource = closeSessions
	runtime.readOnlySources = append([]vectorSourceSpec(nil), sources...)
	runtime.readOnlyDisplayCRS = targetCRS
	keep = true
	return runtime, true, nil
}

func validateReadOnlyWindowMetadata(layerName, sourceCRS string, hasBounds bool) error {
	if strings.TrimSpace(sourceCRS) == "" || !hasBounds {
		return fmt.Errorf("layer %q cannot be safely opened read-only: viewport rendering requires a declared source CRS and valid extent; refusing a full-geometry fallback (specify --source-crs or repair the dataset metadata)", layerName)
	}
	return nil
}

const (
	// Bounds the transient Go geometry/property snapshot for a single chunk.
	maxReadOnlyWindowFeatures = 20_000
	// Bounds polygon coordinate complexity before GEOS triangulation in a
	// window-backed read-only render.
	maxReadOnlyWindowPolygonVertices = 250_000
	// Bounds retained hit-test geometry across the active viewport. When a view
	// exceeds this cap the affected chunk fails visibly and users can zoom in.
	maxReadOnlyVisibleFeatures = 100_000
	maxReadOnlyWindowBytes     = 32 << 20
	maxReadOnlyVisibleBytes    = 128 << 20
	// Bounds all retained polygon fill meshes in a materialized desktop project
	// (8M Go render.Vertex values, about 160 MiB before allocator overhead).
	maxDesktopPolygonFillVertices  = 8 * 1024 * 1024
	maxDesktopGeoJSONIndexFeatures = 2_000_000
	maxDesktopMaterializedFeatures = 1_000_000
	maxDesktopMaterializedBytes    = 256 << 20
)

func readOnlyWindowChunkSize(zoomBucket int) float64 {
	if zoomBucket <= 0 {
		return 0.25
	}
	if zoomBucket > 10 {
		zoomBucket = 10
	}
	return math.Ldexp(0.25, -zoomBucket)
}

func readOnlyWindowZoomBucket(zoom float64) int {
	if math.IsNaN(zoom) || math.IsInf(zoom, 0) || zoom <= 1 {
		return 0
	}
	bucket := int(math.Floor(math.Log2(zoom)))
	if bucket > 10 {
		return 10
	}
	return bucket
}

func estimateMaterializedRuntimeLayerBytes(layer core.Layer) int64 {
	const saturation = int64(maxDesktopMaterializedBytes) + 1
	total := int64(0)
	add := func(size int64) {
		if size < 0 || total > saturation-size {
			total = saturation
			return
		}
		total += size
	}
	var addValue func(any, int)
	addValue = func(value any, depth int) {
		if total >= saturation {
			return
		}
		if depth >= 64 {
			add(saturation)
			return
		}
		switch value := value.(type) {
		case nil:
			add(8)
		case string:
			add(int64(len(value)) + 24)
		case []byte:
			add(int64(len(value)) + 24)
		case []string:
			add(24 + int64(len(value))*24)
			for _, item := range value {
				add(int64(len(item)) + 24)
			}
		case []any:
			add(24 + int64(len(value))*16)
			for _, item := range value {
				addValue(item, depth+1)
			}
		case map[string]any:
			add(128 + int64(len(value))*16)
			for key, item := range value {
				add(int64(len(key)) + 24)
				addValue(item, depth+1)
			}
		default:
			add(16)
		}
	}
	add(int64(len(layer.Name)) + int64(len(layer.Fields))*64 + 128)
	for _, feature := range layer.Features {
		add(128)
		switch geometry := feature.Geometry.(type) {
		case core.WKBGeometry:
			add(int64(len(geometry.WKB)) + 32)
		case core.WKTGeometry:
			add(int64(len(geometry.WKT)) + 32)
		case nil:
		default:
			add(saturation)
		}
		for name, value := range feature.Properties {
			add(int64(len(name)) + 48)
			addValue(value, 0)
		}
		if feature.Label != nil {
			add(int64(len(feature.Label.Text)) + 32)
		}
	}
	return total
}

func accumulateMaterializedRuntimeLayerUsage(currentFeatures int, currentBytes int64, layer core.Layer, maxFeatures int, maxBytes int64) (int, int64, error) {
	if currentFeatures < 0 || currentBytes < 0 || maxFeatures < 0 || maxBytes < 0 {
		return currentFeatures, currentBytes, fmt.Errorf("materialized project budgets must not be negative")
	}
	if len(layer.Features) > maxFeatures-currentFeatures {
		return currentFeatures, currentBytes, fmt.Errorf("materialized project exceeds the %d-feature safety limit; use read-only viewport loading or remove sources", maxFeatures)
	}
	layerBytes := estimateMaterializedRuntimeLayerBytes(layer)
	if layerBytes > maxBytes-currentBytes {
		return currentFeatures, currentBytes, fmt.Errorf("materialized project exceeds the %d MiB payload safety limit; use read-only viewport loading or remove sources", maxBytes>>20)
	}
	return currentFeatures + len(layer.Features), currentBytes + layerBytes, nil
}

func estimateReadOnlyWindowPayloadBytes(layer core.Layer) int64 {
	const saturation = int64(maxReadOnlyWindowBytes) + 1
	var total int64
	add := func(size int) bool {
		if size < 0 || total > saturation-int64(size) {
			total = saturation
			return false
		}
		total += int64(size)
		return total <= maxReadOnlyWindowBytes
	}
	for _, feature := range layer.Features {
		switch geometry := feature.Geometry.(type) {
		case core.WKBGeometry:
			if !add(len(geometry.WKB)) {
				return saturation
			}
		case core.WKTGeometry:
			if !add(len(geometry.WKT)) {
				return saturation
			}
		case nil:
		default:
			return saturation
		}
		for name, value := range feature.Properties {
			if !add(len(name)) || !add(16) {
				return saturation
			}
			if !addReadOnlyPropertyEstimate(value, add) {
				return saturation
			}
		}
	}
	return total
}

func addReadOnlyPropertyEstimate(value any, add func(int) bool) bool {
	const maxPropertyNodes = 1 << 20
	values := []any{value}
	visited := 0
	for len(values) > 0 {
		visited++
		if visited > maxPropertyNodes {
			return add(maxReadOnlyWindowBytes + 1)
		}
		last := len(values) - 1
		current := values[last]
		values = values[:last]
		switch current := current.(type) {
		case nil:
			if !add(8) {
				return false
			}
		case string:
			if !add(len(current)) || !add(24) {
				return false
			}
		case []byte:
			if !add(len(current)) || !add(24) {
				return false
			}
		case []string:
			if !add(24 + len(current)*24) {
				return false
			}
			for _, item := range current {
				if !add(len(item) + 24) {
					return false
				}
			}
		case []any:
			if !add(24 + len(current)*16) {
				return false
			}
			values = append(values, current...)
		case map[string]any:
			if !add(128 + len(current)*16) {
				return false
			}
			for key, item := range current {
				if !add(len(key) + 24) {
					return false
				}
				values = append(values, item)
			}
		default:
			if !add(24) {
				return false
			}
		}
	}
	return true
}

func renderChunkBounds(extent [4]float64, size float64, key render.ChunkKey) ([4]float64, bool) {
	if size <= 0 || extent[0] >= extent[2] || extent[1] >= extent[3] {
		return [4]float64{}, false
	}
	x0, y0 := math.Max(0, float64(key.X)*size), math.Max(0, float64(key.Y)*size)
	x1, y1 := math.Min(1, float64(key.X+1)*size), math.Min(1, float64(key.Y+1)*size)
	if x0 >= x1 || y0 >= y1 {
		return [4]float64{}, false
	}
	spanX, spanY := extent[2]-extent[0], extent[3]-extent[1]
	return [4]float64{extent[0] + x0*spanX, extent[1] + y0*spanY, extent[0] + x1*spanX, extent[1] + y1*spanY}, true
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
	// LayerSource normalizes data-backed feature coordinates to the unit square.
	// Bound viewport planning to that domain so extreme zoom-out never allocates
	// keys for an arbitrarily large empty world.
	planner.Domain = [4]float64{0, 0, 1, 1}
	planner.HasDomain = true
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
	updatedSources := make(map[string]render.LayerSource)
	r.sourcesMu.RLock()
	for name, source := range r.sources {
		updatedSources[name] = source
	}
	r.sourcesMu.RUnlock()
	updatedSources[layerName] = newSources[layerName]
	if err := attachPolygonFillGeometry(context.Background(), []core.Layer{layer}, updatedSources); err != nil {
		return err
	}
	source := updatedSources[layerName]
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
	return attachPolygonFillGeometryWithLimit(ctx, layers, sources, nil, maxDesktopPolygonFillVertices)
}

func attachPolygonFillGeometryForChunk(ctx context.Context, layers []core.Layer, sources map[string]render.LayerSource, target *render.ChunkKey) error {
	return attachPolygonFillGeometryWithLimit(ctx, layers, sources, target, maxDesktopPolygonFillVertices)
}

func attachPolygonFillGeometryWithLimit(ctx context.Context, layers []core.Layer, sources map[string]render.LayerSource, target *render.ChunkKey, maxProjectVertices int) error {
	if maxProjectVertices < 0 {
		return fmt.Errorf("polygon fill project vertex budget must not be negative")
	}
	building := make(map[string]bool, len(layers))
	for _, layer := range layers {
		building[layer.Name] = true
	}
	existingCapacity := 0
	for name, source := range sources {
		if building[name] {
			continue
		}
		if source.PolygonFillCapacity < 0 || source.PolygonFillCapacity > maxProjectVertices-existingCapacity {
			return fmt.Errorf("existing polygon fill geometry exceeds the %d-vertex project safety limit", maxProjectVertices)
		}
		existingCapacity += source.PolygonFillCapacity
	}
	remainingCapacity := maxProjectVertices - existingCapacity
	generatedCapacity := 0
	generatedVertices := 0
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
		if target != nil {
			if err := validateReadOnlyPolygonVertexBudget(layer, source, maxReadOnlyWindowPolygonVertices); err != nil {
				return err
			}
		}
		fillByCell := make(map[[2]int][]render.Vertex)
		fillColor := render.ColorForPolygonFill(layer.Style)
		fillCapacities := estimatePolygonFillCapacities(layer, source, target)
		estimatedVertices := 0
		for _, capacity := range fillCapacities {
			if capacity > render.MaxChunkVertices {
				return fmt.Errorf("polygon fill for layer %q exceeds the %d-vertex chunk limit", layer.Name, render.MaxChunkVertices)
			}
			if capacity < 0 || capacity > remainingCapacity-generatedCapacity-estimatedVertices {
				return fmt.Errorf("polygon fill for layer %q exceeds the %d-vertex project safety limit", layer.Name, maxProjectVertices)
			}
			estimatedVertices += capacity
		}
		for cell, capacity := range fillCapacities {
			if capacity > 0 {
				fillByCell[cell] = make([]render.Vertex, 0, capacity)
			}
		}
		generatedCapacity += estimatedVertices
		capacityBeforeLayer := generatedCapacity - estimatedVertices
		verticesBeforeLayer := generatedVertices
		operator := geosdriver.NewOperator()
		fillVertexLimitExceeded := false
		for featureIndex, feature := range layer.Features {
			if err := ctx.Err(); err != nil {
				return err
			}
			if feature.Geometry == nil || !strings.Contains(strings.ToUpper(feature.Geometry.GeometryType()), "POLYGON") {
				continue
			}
			if strings.EqualFold(feature.Geometry.GeometryType(), "POLYGON") && featureIndex < len(source.Features) {
				parts := source.Features[featureIndex].Parts
				var exterior []render.Point
				if len(parts) == 0 {
					exterior = source.Features[featureIndex].Vertices
				} else if len(parts) == 1 {
					exterior = parts[0]
				}
				if len(exterior) > 0 && appendConvexPolygonTriangles(exterior, func(triangle [3]render.Point) {
					if !fillVertexLimitExceeded && !appendPolygonFillTriangle(
						fillByCell, triangle, fillColor, source.ChunkSize, target, render.MaxChunkVertices,
						&generatedVertices, remainingCapacity,
						&generatedCapacity, remainingCapacity) {
						fillVertexLimitExceeded = true
					}
				}) {
					if fillVertexLimitExceeded {
						return fmt.Errorf("polygon fill for layer %q exceeds the %d-vertex chunk limit", layer.Name, render.MaxChunkVertices)
					}
					continue
				}
			}
			triangles, err := operator.ConstrainedTriangles(ctx, feature.Geometry)
			if err != nil {
				return fmt.Errorf("layer %q feature %d: %w", layer.Name, feature.ID, err)
			}
			for _, triangle := range triangles {
				var normalized [3]render.Point
				for index, point := range triangle {
					normalized[index] = render.Point{X: (point[0] - bounds[0]) / spanX, Y: (point[1] - bounds[1]) / spanY}
				}
				if !appendPolygonFillTriangle(fillByCell, normalized, fillColor, source.ChunkSize, target,
					render.MaxChunkVertices, &generatedVertices, remainingCapacity,
					&generatedCapacity, remainingCapacity) {
					fillVertexLimitExceeded = true
					break
				}
			}
			if fillVertexLimitExceeded {
				return fmt.Errorf("polygon fill for layer %q exceeds the %d-vertex chunk limit", layer.Name, render.MaxChunkVertices)
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
			if len(chunk.Vertices) > render.MaxChunkVertices || len(fill) > render.MaxChunkVertices-len(chunk.Vertices) {
				return render.Chunk{}, fmt.Errorf("render chunk exceeds the %d-vertex safety limit", render.MaxChunkVertices)
			}
			vertices := make([]render.Vertex, 0, len(fill)+len(chunk.Vertices))
			vertices = append(vertices, fill...)
			vertices = append(vertices, chunk.Vertices...)
			chunk.Vertices = vertices
			return chunk, nil
		}
		source.PolygonFillVertices = generatedVertices - verticesBeforeLayer
		source.PolygonFillCapacity = generatedCapacity - capacityBeforeLayer
		sources[layer.Name] = source
	}
	return nil
}

func estimatePolygonFillCapacities(layer core.Layer, source render.LayerSource, target *render.ChunkKey) map[[2]int]int {
	capacities := make(map[[2]int]int)
	chunkSize := source.ChunkSize
	if chunkSize <= 0 || chunkSize > 1 || math.IsNaN(chunkSize) || math.IsInf(chunkSize, 0) {
		return capacities
	}
	maxCell := int(math.Ceil(1/chunkSize)) - 1
	for featureIndex, feature := range layer.Features {
		if feature.Geometry == nil || featureIndex >= len(source.Features) ||
			!strings.Contains(strings.ToUpper(feature.Geometry.GeometryType()), "POLYGON") {
			continue
		}
		points := source.Features[featureIndex].Vertices
		if len(points) == 0 || len(points) > int(^uint(0)>>1)/3 {
			continue
		}
		minX, minY := math.Inf(1), math.Inf(1)
		maxX, maxY := math.Inf(-1), math.Inf(-1)
		for _, point := range points {
			minX, minY = math.Min(minX, point.X), math.Min(minY, point.Y)
			maxX, maxY = math.Max(maxX, point.X), math.Max(maxY, point.Y)
		}
		firstX, lastX := max(0, int(math.Floor(minX/chunkSize))), min(maxCell, int(math.Floor(maxX/chunkSize)))
		firstY, lastY := max(0, int(math.Floor(minY/chunkSize))), min(maxCell, int(math.Floor(maxY/chunkSize)))
		if target != nil {
			firstX, lastX = max(firstX, target.X), min(lastX, target.X)
			firstY, lastY = max(firstY, target.Y), min(lastY, target.Y)
		}
		if firstX > lastX || firstY > lastY {
			continue
		}
		cellCount := (lastX - firstX + 1) * (lastY - firstY + 1)
		triangleEstimate := len(points)
		if strings.EqualFold(feature.Geometry.GeometryType(), "POLYGON") {
			ringCount := len(source.Features[featureIndex].Parts)
			if ringCount == 0 {
				ringCount = 1
			}
			triangleEstimate = max(1, len(points)+ringCount-4)
		}
		if triangleEstimate > int(^uint(0)>>1)/3 {
			continue
		}
		estimate := triangleEstimate * 3
		perCell, remainder := estimate/cellCount, estimate%cellCount
		for cellY := firstY; cellY <= lastY; cellY++ {
			for cellX := firstX; cellX <= lastX; cellX++ {
				capacity := perCell
				if remainder > 0 {
					capacity++
					remainder--
				}
				capacities[[2]int{cellX, cellY}] += capacity
			}
		}
	}
	return capacities
}

func validateReadOnlyPolygonVertexBudget(layer core.Layer, source render.LayerSource, maxVertices int) error {
	if maxVertices <= 0 {
		return nil
	}
	vertices := 0
	for index, feature := range layer.Features {
		if feature.Geometry == nil || index >= len(source.Features) ||
			!strings.Contains(strings.ToUpper(feature.Geometry.GeometryType()), "POLYGON") {
			continue
		}
		count := len(source.Features[index].Vertices)
		if count > maxVertices-vertices {
			return fmt.Errorf("read-only polygon window exceeds the %d-vertex triangulation safety limit", maxVertices)
		}
		vertices += count
	}
	return nil
}

// validateReadOnlyWindowPolygonVertexBudget checks polygon WKB directly,
// before label placement or fill triangulation can invoke GEOS. PointCount is
// a non-allocating structural scan, so the guard itself does not materialize
// decoded coordinate arrays.
func validateReadOnlyWindowPolygonVertexBudget(layer core.Layer, maxVertices int) error {
	if maxVertices <= 0 {
		return nil
	}
	vertices := 0
	for _, feature := range layer.Features {
		if feature.Geometry == nil || !strings.Contains(strings.ToUpper(feature.Geometry.GeometryType()), "POLYGON") {
			continue
		}
		geometry, ok := feature.Geometry.(core.WKBGeometry)
		if !ok {
			return fmt.Errorf("read-only polygon feature %d has unsupported geometry encoding %T", feature.ID, feature.Geometry)
		}
		count, err := geometry.PointCount()
		if err != nil {
			return fmt.Errorf("read-only polygon feature %d has invalid WKB: %w", feature.ID, err)
		}
		if count > maxVertices-vertices {
			return fmt.Errorf("read-only polygon window exceeds the %d-vertex triangulation safety limit", maxVertices)
		}
		vertices += count
	}
	return nil
}

func appendConvexPolygonTriangles(ring []render.Point, emit func([3]render.Point)) bool {
	count := len(ring)
	if count > 1 && ring[0] == ring[count-1] {
		count--
	}
	if count < 3 {
		return false
	}
	sign := 0.0
	for index := 0; index < count; index++ {
		a, b, c := ring[index], ring[(index+1)%count], ring[(index+2)%count]
		cross := (b.X-a.X)*(c.Y-b.Y) - (b.Y-a.Y)*(c.X-b.X)
		if math.Abs(cross) <= 1e-14 {
			continue
		}
		if sign == 0 {
			sign = cross
		} else if sign*cross < 0 {
			return false
		}
	}
	if sign == 0 {
		return false
	}
	for index := 1; index+1 < count; index++ {
		triangle := [3]render.Point{ring[0], ring[index], ring[index+1]}
		area2 := (triangle[1].X-triangle[0].X)*(triangle[2].Y-triangle[0].Y) -
			(triangle[2].X-triangle[0].X)*(triangle[1].Y-triangle[0].Y)
		if math.Abs(area2) > 1e-14 {
			emit(triangle)
		}
	}
	return true
}

func appendPolygonFillTriangle(fillByCell map[[2]int][]render.Vertex, triangle [3]render.Point, color uint32, chunkSize float64, target *render.ChunkKey, maxVertices int, totalVertices *int, maxTotalVertices int, totalCapacity *int, maxTotalCapacity int) bool {
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	for _, point := range triangle {
		minX, minY = math.Min(minX, point.X), math.Min(minY, point.Y)
		maxX, maxY = math.Max(maxX, point.X), math.Max(maxY, point.Y)
	}
	if chunkSize <= 0 || chunkSize > 1 || math.IsNaN(chunkSize) || math.IsInf(chunkSize, 0) {
		return true
	}
	maxCell := int(math.Ceil(1/chunkSize)) - 1
	firstX, lastX := max(0, int(math.Floor(minX/chunkSize))), min(maxCell, int(math.Floor(maxX/chunkSize)))
	firstY, lastY := max(0, int(math.Floor(minY/chunkSize))), min(maxCell, int(math.Floor(maxY/chunkSize)))
	if target != nil {
		firstX, lastX = max(firstX, target.X), min(lastX, target.X)
		firstY, lastY = max(firstY, target.Y), min(lastY, target.Y)
	}
	for cellY := firstY; cellY <= lastY; cellY++ {
		for cellX := firstX; cellX <= lastX; cellX++ {
			minCellX, minCellY := float64(cellX)*chunkSize, float64(cellY)*chunkSize
			maxCellX, maxCellY := float64(cellX+1)*chunkSize, float64(cellY+1)*chunkSize
			cellKey := [2]int{cellX, cellY}
			vertices := fillByCell[cellKey]
			fullyInside := true
			for _, point := range triangle {
				if point.X < minCellX || point.X > maxCellX || point.Y < minCellY || point.Y > maxCellY {
					fullyInside = false
					break
				}
			}
			if fullyInside {
				if totalVertices == nil || *totalVertices > maxTotalVertices || 3 > maxTotalVertices-*totalVertices {
					return false
				}
				var ok bool
				vertices, ok = appendPolygonFillPointsWithinProjectBudget(
					vertices, triangle[:], color, maxVertices, totalCapacity, maxTotalCapacity)
				if !ok {
					return false
				}
				fillByCell[cellKey] = vertices
				*totalVertices += 3
				continue
			}
			for _, part := range render.ClipTriangleToRect(triangle, minCellX, minCellY, maxCellX, maxCellY) {
				if totalVertices == nil || *totalVertices > maxTotalVertices || len(part) > maxTotalVertices-*totalVertices {
					return false
				}
				var ok bool
				vertices, ok = appendPolygonFillPointsWithinProjectBudget(
					vertices, part[:], color, maxVertices, totalCapacity, maxTotalCapacity)
				if !ok {
					return false
				}
				*totalVertices += len(part)
			}
			fillByCell[cellKey] = vertices
		}
	}
	return true
}

func appendPolygonFillPointsWithinProjectBudget(current []render.Vertex, points []render.Point, color uint32, maxChunkCapacity int, totalCapacity *int, maxTotalCapacity int) ([]render.Vertex, bool) {
	if totalCapacity == nil || *totalCapacity < cap(current) || *totalCapacity > maxTotalCapacity {
		return current, false
	}
	if len(current) > maxChunkCapacity || len(points) > maxChunkCapacity-len(current) {
		return current, false
	}
	needed := len(current) + len(points)
	if needed > cap(current) {
		available := maxTotalCapacity - (*totalCapacity - cap(current))
		newCapacity := cap(current) * 2
		if newCapacity < needed {
			newCapacity = needed
		}
		if newCapacity > maxChunkCapacity {
			newCapacity = maxChunkCapacity
		}
		if newCapacity > available {
			newCapacity = available
		}
		if newCapacity < needed {
			return current, false
		}
		grown := make([]render.Vertex, len(current), newCapacity)
		copy(grown, current)
		*totalCapacity += newCapacity - cap(current)
		current = grown
	}
	for _, point := range points {
		current = append(current, render.Vertex{X: float32(point.X), Y: float32(point.Y), Color: color, Kind: render.VertexFill})
	}
	return current, true
}

func appendPolygonFillPoints(current []render.Vertex, points []render.Point, color uint32, limit int) ([]render.Vertex, bool) {
	if limit < 0 || len(current) > limit || len(points) > limit-len(current) {
		return current, false
	}
	needed := len(current) + len(points)
	if needed > cap(current) {
		newCapacity := cap(current) * 2
		if newCapacity < needed {
			newCapacity = needed
		}
		if newCapacity > limit {
			newCapacity = limit
		}
		grown := make([]render.Vertex, len(current), newCapacity)
		copy(grown, current)
		current = grown
	}
	for _, point := range points {
		current = append(current, render.Vertex{X: float32(point.X), Y: float32(point.Y), Color: color, Kind: render.VertexFill})
	}
	return current, true
}

const maxLabelScriptExecutionDuration = 10 * time.Minute
const maxPreparedLabelTextBytes = 128 << 20
const maxPreparedLabelTextPerFeature = 64 << 10

func prepareLayerLabels(ctx context.Context, layers []core.Layer) error {
	return prepareLayerLabelsWithMaximumDuration(ctx, layers, maxLabelScriptExecutionDuration)
}

func prepareLayerLabelsWithMaximumDuration(ctx context.Context, layers []core.Layer, maximum time.Duration) error {
	if ctx == nil {
		ctx = context.Background()
	}
	hasLua := false
	for _, layer := range layers {
		if layer.Labels.Enabled && (strings.TrimSpace(layer.Labels.LuaScript) != "" || strings.TrimSpace(layer.Labels.Rule) != "") {
			hasLua = true
			break
		}
	}
	if hasLua {
		if maximum <= 0 {
			return fmt.Errorf("label Lua execution limit must be positive")
		}
		if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > maximum {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, maximum)
			defer cancel()
		}
	}
	return prepareLayerLabelsCore(ctx, layers)
}

func prepareLayerLabelsCore(ctx context.Context, layers []core.Layer) error {
	return prepareLayerLabelsCoreWithBudget(ctx, layers, maxPreparedLabelTextBytes, maxPreparedLabelTextPerFeature)
}

func prepareLayerLabelsCoreWithBudget(ctx context.Context, layers []core.Layer, maxTotalBytes, maxFeatureBytes int64) error {
	if maxTotalBytes < 0 || maxFeatureBytes < 0 {
		return fmt.Errorf("label text budgets must not be negative")
	}
	var totalLabelBytes int64
	for _, layer := range layers {
		for _, feature := range layer.Features {
			if feature.Label == nil {
				continue
			}
			if int64(len(feature.Label.Text)) > maxTotalBytes-totalLabelBytes {
				return fmt.Errorf("existing label text exceeds the %d-byte project safety limit", maxTotalBytes)
			}
			totalLabelBytes += int64(len(feature.Label.Text))
		}
	}
	for layerIndex := range layers {
		settings := layers[layerIndex].Labels
		if !settings.Enabled || len(layers[layerIndex].Features) == 0 {
			continue
		}
		textScript := strings.TrimSpace(settings.LuaScript)
		ruleScript := strings.TrimSpace(settings.Rule)
		var composer *scripting.LabelComposerProgram
		var err error
		if textScript != "" || ruleScript != "" {
			if textScript == "" {
				textScript = "return nil"
			}
			composer, err = scripting.CompileLabelComposerProgram(textScript, ruleScript)
			if err != nil {
				return fmt.Errorf("layer %q label composer: %w", layers[layerIndex].Name, err)
			}
			defer composer.Close()
		}
		var geometryOperator *geosdriver.Operator
		for featureIndex := range layers[layerIndex].Features {
			if err := ctx.Err(); err != nil {
				return err
			}
			feature := &layers[layerIndex].Features[featureIndex]
			if feature.Label != nil {
				totalLabelBytes -= int64(len(feature.Label.Text))
				feature.Label = nil
			}
			var text string
			if composer != nil {
				var visible bool
				text, visible, err = composer.Evaluate(ctx, feature.Properties)
				if err != nil {
					return fmt.Errorf("layer %q feature %d label expression: %w", layers[layerIndex].Name, feature.ID, err)
				}
				if !visible {
					feature.Label = nil
					continue
				}
				if strings.TrimSpace(settings.LuaScript) == "" {
					text, err = core.EvaluateLabelTemplate(settings.Expression, feature.Properties)
				}
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
			textBytes := int64(len(text))
			if textBytes > maxFeatureBytes {
				return fmt.Errorf("layer %q feature %d label exceeds the %d-byte safety limit", layers[layerIndex].Name, feature.ID, maxFeatureBytes)
			}
			if textBytes > maxTotalBytes-totalLabelBytes {
				return fmt.Errorf("prepared label text exceeds the %d-byte project safety limit", maxTotalBytes)
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
			totalLabelBytes += textBytes
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
	return loadDataRuntimeFilesWithLargePolicyAndBaseExtent(ctx, paths, baseLayers, saveDestination, allowLargeEditable, nil)
}

func loadDataRuntimeFilesWithLargePolicyAndBaseExtent(ctx context.Context, paths []string, baseLayers []core.Layer, saveDestination string, allowLargeEditable bool, baseExtent *[4]float64) (*demoRuntime, bool, int, error) {
	featureCount := 0
	if saveDestination == "" && !allowLargeEditable {
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
			if len(baseLayers) > 0 {
				if baseExtent == nil {
					return nil, false, featureCount, fmt.Errorf("cannot transition existing layers to read-only loading without a valid current map extent")
				}
				runtime, err := loadReadOnlyDataRuntimeWithBaseLayers(ctx, sources, "", baseLayers, baseExtent)
				return runtime, true, featureCount, err
			}
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
	materializedFeatures, materializedBytes := 0, int64(0)
	for _, layer := range baseLayers {
		var budgetErr error
		materializedFeatures, materializedBytes, budgetErr = accumulateMaterializedRuntimeLayerUsage(
			materializedFeatures, materializedBytes, layer, maxDesktopMaterializedFeatures, maxDesktopMaterializedBytes)
		if budgetErr != nil {
			return nil, fmt.Errorf("base project: %w", budgetErr)
		}
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
			materializedFeatures, materializedBytes, err = accumulateMaterializedRuntimeLayerUsage(
				materializedFeatures, materializedBytes, layer, maxDesktopMaterializedFeatures, maxDesktopMaterializedBytes)
			if err != nil {
				return nil, fmt.Errorf("read %q: %w", path, err)
			}
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
	if displayCRS == "" {
		displayCRS = r.mapCRS
	}
	baseExtent := r.mapExtent
	readOnlyBaseLayers := append([]core.Layer(nil), r.readOnlyBaseLayers...)
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
		// Snapshot only layer/feature headers after filtering duplicate paths.
		// Geometry and property payloads are immutable and remain copy-on-write
		// owned by the ProjectService, avoiding another full dataset clone.
		baseLayers = service.ProjectRenderSnapshot().Layers
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
			next, err = loadReadOnlyDataRuntimeWithBaseLayers(loadContext, readOnlySources, displayCRS, readOnlyBaseLayers, &baseExtent)
		} else {
			if !appendLayers && saveDestination == "" && !r.allowLargeEditable {
				native.SetRenderStatus("Loading: checking feature count")
			}
			next, largeReadOnly, featureCount, err = loadDataRuntimeFilesWithLargePolicyAndBaseExtent(
				loadContext, paths, baseLayers, saveDestination, r.allowLargeEditable, &baseExtent)
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

// adoptLoadedSpatialStateLocked transfers source ownership and window-backed
// render state when a fully prepared runtime replaces the current project.
// The caller must hold r.mu; these maps can retain substantial geometry/hit
// data for large read-only projects.
func (r *demoRuntime) adoptLoadedSpatialStateLocked(next *demoRuntime) {
	r.sources = next.sources
	r.sourcesMu = next.sourcesMu
	r.viewportReadOnly = next.viewportReadOnly
	r.windowHits = next.windowHits
	r.windowFeatureCounts = next.windowFeatureCounts
	r.windowFeatureIDs = next.windowFeatureIDs
	r.windowVisibleFeatureCount = next.windowVisibleFeatureCount
	r.windowPayloadBytes = next.windowPayloadBytes
	r.windowVisiblePayloadBytes = next.windowVisiblePayloadBytes
	r.windowVisibleKeys = next.windowVisibleKeys
	r.windowFeatureNames = next.windowFeatureNames
	r.windowLabels = next.windowLabels
	r.nextWindowFeatureID = next.nextWindowFeatureID
	r.readOnlyBaseLayers = next.readOnlyBaseLayers
	r.readOnlyBaseFeatures = next.readOnlyBaseFeatures
	r.readOnlyBaseLabels = next.readOnlyBaseLabels
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
	r.adoptLoadedSpatialStateLocked(next)
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
	r.attributeCacheBytes = 0
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
