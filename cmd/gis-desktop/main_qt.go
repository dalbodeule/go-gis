//go:build qt

package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	goruntime "runtime"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/qml"
	"gogis/internal/commands"
	"gogis/internal/core"
	"gogis/internal/presentation"
	"gogis/internal/render"
	"gogis/internal/scripting"
	"gogis/internal/workspace"
	"gogis/ui/qt/native"
)

type forceReadOnlyOverviewContextKey struct{}

// Large viewport replacements should reveal newly rendered chunks as they
// arrive instead of leaving a zoomed, stale complete frame on screen until
// hundreds of chunk requests finish.
const maxPreservedCompleteFrameChunkKeys = 128

func shouldRetryViewportWithOverview(viewportReadOnly, alreadyForced bool, err string) bool {
	return viewportReadOnly && !alreadyForced && strings.Contains(err, "viewport render batch exceeds the")
}

// Main.qml is embedded so the prototype can be launched from a checkout
// without relying on a working directory or an installed resource bundle.
//
//go:embed qml/Main.qml
var mainQML []byte

var appVersion = "0.1.0-dev"

func main() {
	configureBundledGISResources()
	if err := startDesktopOutputCapture(); err != nil {
		fmt.Fprintf(os.Stderr, "GoGIS: unable to capture process output: %v\n", err)
	}
	language, qtArgs := desktopLanguageArgs(os.Args)
	qt.QCoreApplication_SetOrganizationName("GoGIS")
	qt.QCoreApplication_SetApplicationName("GoGIS")
	qt.NewQApplication(qtArgs)
	configureShapefileIndexPolicyWithDefaults(os.Args, desktopShapefileIndexThreshold(), "cache")
	// Keep the event loop responsive while GDAL opens and snapshots a large
	// dataset. Start from an empty project and replace it after QML is ready.
	runtime := loadEmptyProject()
	native.RegisterMapCanvas()
	engine := qml.NewQQmlApplicationEngine()
	rootContext := engine.RootContext()
	for name, value := range map[string]string{
		"appLanguage":       language,
		"appVersion":        appVersion,
		"appRuntime":        goruntime.Version(),
		"appBuildTarget":    goruntime.GOOS + "/" + goruntime.GOARCH,
		"appCRSCatalogJSON": desktopCRSCatalogJSON(),
	} {
		variant := qt.NewQVariant14(value)
		rootContext.SetContextProperty2(name, variant)
		variant.Delete()
	}
	engine.LoadData(mainQML)
	startViewportSync(runtime)
	startMemoryMonitor()
	startDiagnosticLogPublisher()
	startInitialDataLoad(runtime, os.Args)
	qt.QApplication_Exec()
}

func desktopShapefileIndexThreshold() int {
	settings := qt.NewQSettings7("GoGIS", "GoGIS")
	defer settings.Delete()
	return desktopShapefileIndexThresholdFromSettings(settings)
}

func desktopShapefileIndexThresholdFromSettings(settings *qt.QSettings) int {
	const defaultThreshold = 10_000
	key := qt.NewQAnyStringView3("preferences/shapefileIndexThreshold")
	defer key.Delete()
	if !settings.Contains(*key) {
		return defaultThreshold
	}
	value := settings.ValueWithKey(*key)
	threshold := value.ToInt()
	if threshold < 0 || threshold > 1_000_000 {
		return defaultThreshold
	}
	return threshold
}

// configureBundledGISResources makes PROJ/GDAL data discoverable when the app
// is launched from a macOS bundle or Windows portable package. Explicit caller
// configuration wins, which still permits alternate grid/data directories.
func configureBundledGISResources() {
	executable, err := os.Executable()
	if err != nil {
		return
	}
	for key, directory := range bundledGISResourceEnvironment(executable) {
		_ = os.Setenv(key, directory)
	}
}

func bundledGISResourceEnvironment(executable string) map[string]string {
	executable = filepath.Clean(executable)
	var resources string
	switch goruntime.GOOS {
	case "darwin":
		if !strings.Contains(filepath.ToSlash(executable), ".app/Contents/MacOS/") {
			return nil
		}
		contents := filepath.Dir(filepath.Dir(executable))
		resources = filepath.Join(contents, "Resources")
	case "windows":
		resources = filepath.Join(filepath.Dir(executable), "resources")
	default:
		return nil
	}
	result := make(map[string]string, 3)
	for key, directory := range map[string]string{
		"GDAL_DATA":        filepath.Join(resources, "gdal"),
		"GDAL_DRIVER_PATH": filepath.Join(resources, "gdalplugins"),
		"PROJ_DATA":        filepath.Join(resources, "proj"),
	} {
		if os.Getenv(key) != "" {
			continue
		}
		if info, err := os.Stat(directory); err == nil && info.IsDir() {
			result[key] = directory
		}
	}
	return result
}

func startDiagnosticLogPublisher() {
	go func() {
		lastPayload := ""
		publish := func() {
			payload := native.DiagnosticLogJSON()
			if payload != lastPayload {
				native.SetDiagnosticLogPayload(payload)
				lastPayload = payload
			}
		}
		publish()
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		for range ticker.C {
			publish()
		}
	}()
}

func startMemoryMonitor() {
	go func() {
		sample := func() {
			var memory goruntime.MemStats
			goruntime.ReadMemStats(&memory)
			processBytes, processKind, processAvailable := processMemoryBytes()
			native.SetMemoryStatus(processBytes, memory.HeapAlloc, processKind, processAvailable)
		}
		sample()
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for range ticker.C {
			sample()
		}
	}()
}

func desktopLanguageArgs(args []string) (string, []string) {
	language := "system"
	qtArgs := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		value := ""
		if strings.HasPrefix(arg, "--lang=") {
			value = strings.TrimSpace(strings.TrimPrefix(arg, "--lang="))
		} else if arg == "--lang" && i+1 < len(args) {
			i++
			value = strings.TrimSpace(args[i])
		} else if strings.HasPrefix(arg, "--spatial-index-threshold=") ||
			strings.HasPrefix(arg, "--spatial-index-location=") {
			continue
		} else if arg == "--spatial-index-threshold" || arg == "--spatial-index-location" {
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				i++
			}
			continue
		} else if arg == "--input" || arg == "--layer" || arg == "--source-crs" ||
			arg == "--target-crs" || arg == "--save" {
			// These are GoGIS startup options. Passing them through makes
			// QApplication parse them as Qt options before the data loader can
			// read os.Args.
			if i+1 < len(args) {
				i++
			}
			continue
		} else if arg == "--read-only" || arg == "--editable-large" {
			continue
		} else {
			qtArgs = append(qtArgs, arg)
			continue
		}
		switch strings.ToLower(value) {
		case "ko", "en":
			language = strings.ToLower(value)
		case "jp", "ja":
			language = "jp"
		default:
			language = "system"
		}
	}
	return language, qtArgs
}

type demoRuntime struct {
	scheduler                   *render.Scheduler
	batchStore                  *render.BatchStore
	publishedRevision           uint64
	hasCompleteFrame            bool
	completeFrameLayers         string
	planner                     render.ChunkPlanner
	visibility                  *render.LayerVisibility
	visibleLayers               map[string]bool
	layerStyles                 map[string]core.LayerStyle
	baseLayerStyles             map[string]core.LayerStyle
	layerGeometryTypes          map[string]string
	layerBounds                 map[string][4]float64
	layerFeatureCounts          map[string]int
	layerStyleMu                *sync.RWMutex
	mapLabels                   []render.LayerLabel
	builder                     render.ChunkBuilder
	readOnlyBaseBuilder         render.ChunkBuilder
	mu                          sync.Mutex
	cancel                      context.CancelFunc
	loadCancel                  context.CancelFunc
	loadGeneration              uint64
	pendingLoadBatches          [][]string
	forceOverviewNext           bool
	features                    []render.HitFeature
	viewportReadOnly            bool
	windowHits                  map[render.ChunkKey][]render.HitFeature
	windowFeatureCounts         map[render.ChunkKey]int
	windowFeatureIDs            map[render.ChunkKey][]uint64
	windowVisibleFeatureCount   int
	windowPayloadBytes          map[render.ChunkKey]int64
	windowVisiblePayloadBytes   int64
	windowVisibleKeys           map[render.ChunkKey]struct{}
	windowFeatureNames          map[uint64]string
	windowLabels                map[render.ChunkKey][]render.LayerLabel
	nextWindowFeatureID         uint64
	sources                     map[string]render.LayerSource
	sourcesMu                   *sync.RWMutex
	hitIndex                    render.HitIndex
	hitIndexReady               bool
	service                     *commands.ProjectService
	dataMode                    bool
	readOnly                    bool
	previewLoading              bool
	saveDestination             string
	readOnlySources             []vectorSourceSpec
	readOnlyBindings            map[string]readOnlyLayerBinding
	readOnlyBaseLayers          []core.Layer
	readOnlyBaseFeatures        []render.HitFeature
	readOnlyBaseLabels          []render.LayerLabel
	readOnlyDisplayCRS          string
	unavailableSources          map[string]unavailableSource
	mapExtent                   [4]float64
	mapFitExtent                [4]float64
	mapCRS                      string
	workspaceView               *workspace.ViewState
	persist                     func(context.Context, string) error
	attributePageReader         func(context.Context, string, int, int) (core.Layer, int, error)
	attributeFeatureReader      func(context.Context, string, uint64) (core.Feature, error)
	closeAttributeSource        func()
	attributeLayer              string
	attributePage               int
	attributeReady              bool
	attributeCache              map[attributePageKey]string
	attributeCacheOrder         []attributePageKey
	attributeCacheBytes         int64
	attributeGeneration         uint64
	attributeDispatchGeneration uint64
	attributeCancel             context.CancelFunc
	featureNameCacheLayer       string
	featureNameCacheID          uint64
	featureNameCacheValue       string
	featureNameCacheReady       bool
	selected                    render.HitResult
	hasSelect                   bool
	selectionGeneration         uint64
	selectionCancel             context.CancelFunc
	allowLargeEditable          bool
}

type vectorSourceSpec struct {
	Path             string
	LayerName        string
	Encoding         string
	SourceCRS        string
	FallbackCRS      string
	Name             string
	DisplayName      string
	Visible          *bool
	Style            core.LayerStyle
	Labels           core.LabelSettings
	DisplayRule      string
	AllowUnavailable bool
	InsertAt         int
	InsertAtSet      bool
}

type unavailableSource struct {
	Path      string
	LayerName string
	Encoding  string
	Reason    string
}

func loadDemoChunk() *demoRuntime {
	runtime := &demoRuntime{
		scheduler:  render.NewScheduler(),
		batchStore: render.NewBatchStore(),
		planner:    render.NewChunkPlanner(),
		visibility: render.NewLayerVisibility("roads", "buildings", "labels"),
		visibleLayers: map[string]bool{
			"roads": true, "buildings": true, "labels": true,
		},
	}
	runtime.builder = demoChunkBuilder(runtime.planner.ChunkSize)
	runtime.refresh(context.Background(), render.Viewport{Center: render.Point{X: 0.5, Y: 0.5}, Zoom: 1})
	runtime.service = newDemoService(runtime.features)
	runtime.publishLayerTree()
	runtime.publishAttributes("roads")
	return runtime
}

func loadEmptyProject() *demoRuntime {
	runtime := newEmptyProjectRuntime()
	runtime.publishLayerTree()
	runtime.publishAttributes("")
	return runtime
}

func newEmptyProjectRuntime() *demoRuntime {
	runtime := &demoRuntime{
		scheduler:          render.NewScheduler(),
		batchStore:         render.NewBatchStore(),
		planner:            render.NewChunkPlanner(),
		visibility:         render.NewLayerVisibility(),
		visibleLayers:      make(map[string]bool),
		layerStyles:        make(map[string]core.LayerStyle),
		baseLayerStyles:    make(map[string]core.LayerStyle),
		layerGeometryTypes: make(map[string]string),
		layerBounds:        make(map[string][4]float64),
		layerFeatureCounts: make(map[string]int),
		layerStyleMu:       &sync.RWMutex{},
		mapExtent:          [4]float64{0, 0, 1, 1},
		mapFitExtent:       [4]float64{0, 0, 1, 1},
		service:            commands.NewProjectService("Untitled", core.CRS{}),
		builder: func(context.Context, render.ChunkKey) (render.Chunk, error) {
			return render.Chunk{}, nil
		},
	}
	return runtime
}

type attributePayload struct {
	Columns  []string              `json:"columns"`
	Fields   []attributeFieldHint  `json:"fields"`
	Rows     []attributePayloadRow `json:"rows"`
	Page     int                   `json:"page"`
	PageSize int                   `json:"pageSize"`
	Total    int                   `json:"total"`
}

type attributeFieldHint struct {
	Name string         `json:"name"`
	Type core.FieldType `json:"type"`
}

type attributePayloadRow struct {
	FeatureID uint64         `json:"featureId"`
	Values    map[string]any `json:"values"`
}

type attributePageKey struct {
	layer string
	page  int
}

type layerTreePayloadRow struct {
	Name            string             `json:"name"`
	DisplayName     string             `json:"displayName"`
	SourcePath      string             `json:"sourcePath"`
	SourceLayerName string             `json:"sourceLayerName"`
	SourceEncoding  string             `json:"sourceEncoding"`
	SourceCRS       string             `json:"sourceCrs"`
	CRS             string             `json:"crs"`
	Visible         bool               `json:"visible"`
	SourceError     string             `json:"sourceError,omitempty"`
	Style           core.LayerStyle    `json:"style"`
	Labels          core.LabelSettings `json:"labels"`
	DisplayRule     string             `json:"displayRule,omitempty"`
	GeometryType    string             `json:"geometryType,omitempty"`
	Bounds          *[4]float64        `json:"bounds,omitempty"`
}

func (r *demoRuntime) publishLayerTree() {
	if r.service == nil {
		native.SetLayerTreePayload("[]")
		return
	}
	names := r.service.LayerNames()
	rows := make([]layerTreePayloadRow, len(names))
	for index, name := range names {
		visible := true
		if state, exists := r.visibleLayers[name]; exists {
			visible = state
		}
		layer, ok := r.service.LayerProperties(name)
		if !ok {
			continue
		}
		displayName := layer.DisplayName
		if displayName == "" {
			displayName = name
		}
		rows[index] = layerTreePayloadRow{
			Name: name, DisplayName: displayName, SourcePath: layer.SourcePath,
			SourceLayerName: layer.SourceLayerName, SourceEncoding: layer.SourceEncoding, SourceCRS: layer.SourceCRS,
			CRS: layer.CRS.AuthorityCode, Visible: visible, SourceError: r.unavailableSources[name].Reason, Style: layer.Style, Labels: layer.Labels, DisplayRule: layer.DisplayRule,
			GeometryType: r.layerGeometryTypes[name],
		}
		if bounds, exists := r.layerBounds[name]; exists {
			boundsCopy := bounds
			rows[index].Bounds = &boundsCopy
		}
	}
	payload, err := json.Marshal(rows)
	if err != nil {
		native.SetLayerTreePayload("[]")
		return
	}
	native.SetLayerTreePayload(string(payload))
}

func (r *demoRuntime) publishLayerLabels() {
	r.publishLayerLabelsWithStatus(true)
}

func (r *demoRuntime) publishLayerLabelsWithStatus(showLimitStatus bool) {
	r.mu.Lock()
	labels := append([]render.LayerLabel(nil), r.mapLabels...)
	sourceCount := len(labels)
	visibility := make(map[string]bool, len(r.visibleLayers))
	for layer, visible := range r.visibleLayers {
		visibility[layer] = visible
	}
	extent, crs := r.mapExtent, r.mapCRS
	r.mu.Unlock()
	viewport := native.CurrentViewport()
	denominator := labelScaleDenominator(viewport, extent, crs)
	visibleLabels := labels[:0]
	for _, label := range labels {
		if visible, exists := visibility[label.Layer]; exists && !visible {
			continue
		}
		if denominator > 0 && ((label.MinScale > 0 && denominator < label.MinScale) ||
			(label.MaxScale > 0 && denominator > label.MaxScale)) {
			continue
		}
		visibleLabels = append(visibleLabels, label)
	}
	labels, safetyOmitted := viewportLayerLabels(visibleLabels, viewport, maxLayerLabelCount)
	beforeDeclutter := len(labels)
	labels = declutterViewportLabelsAtScale(labels, viewport, denominator)
	limit := viewportLabelLimit(denominator)
	labels, scaleOmitted := sampleLayerLabels(labels, limit)
	payload, err := marshalLayerLabels(labels)
	if err != nil {
		native.SetLayerLabelPayload("[]")
		native.SetRenderStatus("Label display skipped: " + err.Error())
		return
	}
	native.SetLayerLabelPayload(payload)
	if showLimitStatus {
		native.RecordDiagnostic("labels", fmt.Sprintf("published source=%d viewport=%d scale=1:%.0f limit=%d overlap_omitted=%d budget_omitted=%d", sourceCount, len(labels), denominator, limit, beforeDeclutter-len(labels)-scaleOmitted, safetyOmitted+scaleOmitted))
	}
	if showLimitStatus && safetyOmitted+scaleOmitted > 0 {
		native.SetRenderStatus(fmt.Sprintf("Showing %d labels in the current view; zoom in for more", len(labels)))
	}
}

func labelScaleDenominator(view native.Viewport, extent [4]float64, crs string) float64 {
	if view.Width <= 0 || view.Zoom <= 0 || extent[2] <= extent[0] {
		return 0
	}
	metersPerUnit := 1.0
	if strings.EqualFold(crs, "EPSG:4326") {
		latitude := (extent[1] + extent[3]) / 2
		metersPerUnit = 111319.49 * math.Max(0.01, math.Cos(latitude*math.Pi/180))
	}
	return math.Round((extent[2] - extent[0]) / (view.Width * view.Zoom) * metersPerUnit * 96 / 0.0254)
}

// Screen-only density tiers. A larger denominator means a wider view with
// less room for individual parcel labels. No view may show over 200 labels,
// including when its approximate scale cannot be calculated.
func viewportLabelLimit(denominator float64) int {
	switch {
	case denominator >= 100_000:
		return 50
	case denominator >= 50_000:
		return 100
	case denominator >= 18_000:
		return 150
	default:
		return 200
	}
}

// Keep the screen-space label budget useful at dense scales. This lightweight
// grid is deliberately applied after viewport culling, so it does not retain
// geometry or create QML Text objects for labels hidden behind their neighbors.
func declutterViewportLabels(labels []render.LayerLabel, view native.Viewport) []render.LayerLabel {
	return declutterViewportLabelsAtScale(labels, view, 0)
}

func declutterViewportLabelsAtScale(labels []render.LayerLabel, view native.Viewport, denominator float64) []render.LayerLabel {
	if view.Width <= 0 || view.Height <= 0 || view.ViewportWidth <= 0 || view.ViewportHeight <= 0 || view.Zoom <= 0 {
		return labels
	}
	const cellSize = 48.0
	padding := 6.0
	switch {
	case denominator >= 100000:
		padding = 28
	case denominator >= 25000:
		padding = 20
	case denominator >= 5000:
		padding = 12
	}
	type box struct{ left, top, right, bottom float64 }
	occupied := make(map[[2]int][]box)
	selected := make([]render.LayerLabel, 0, len(labels))
	centerX := 0.5 - view.PanX/(view.Width*view.Zoom)
	centerY := 0.5 + view.PanY/(view.Height*view.Zoom)
	for _, label := range labels {
		x := view.ViewportWidth/2 + (label.X-centerX)*view.Width*view.Zoom + label.OffsetXMM*96/25.4
		y := view.ViewportHeight/2 - (label.Y-centerY)*view.Height*view.Zoom + label.OffsetYMM*96/25.4
		if math.IsNaN(x) || math.IsInf(x, 0) || math.IsNaN(y) || math.IsInf(y, 0) {
			continue
		}
		height := math.Max(8, label.HeightMM*96/25.4)
		width := 0.0
		for _, letter := range label.Text {
			if letter < 128 {
				width += height * 0.6
			} else {
				width += height
			}
		}
		width = math.Max(width, height)
		if label.Rotation != 0 {
			// A conservative square also covers labels rotated by the QML delegate.
			width, height = math.Hypot(width, height), math.Hypot(width, height)
		}
		// A maliciously long label must not span an unbounded number of cells.
		width = math.Min(width, view.ViewportWidth*2)
		height = math.Min(height, view.ViewportHeight*2)
		candidate := box{x - width/2 - padding, y - height/2 - padding, x + width/2 + padding, y + height/2 + padding}
		minX, maxX := int(math.Floor(candidate.left/cellSize)), int(math.Floor(candidate.right/cellSize))
		minY, maxY := int(math.Floor(candidate.top/cellSize)), int(math.Floor(candidate.bottom/cellSize))
		overlaps := false
		for cx := minX; cx <= maxX && !overlaps; cx++ {
			for cy := minY; cy <= maxY && !overlaps; cy++ {
				for _, other := range occupied[[2]int{cx, cy}] {
					if candidate.left < other.right && other.left < candidate.right && candidate.top < other.bottom && other.top < candidate.bottom {
						overlaps = true
						break
					}
				}
			}
		}
		if overlaps {
			continue
		}
		selected = append(selected, label)
		for cx := minX; cx <= maxX; cx++ {
			for cy := minY; cy <= maxY; cy++ {
				occupied[[2]int{cx, cy}] = append(occupied[[2]int{cx, cy}], candidate)
			}
		}
	}
	return selected
}

// viewportLayerLabels bounds QML object creation to labels near the current
// viewport and samples dense views instead of dropping the entire label layer.
func viewportLayerLabels(labels []render.LayerLabel, viewport native.Viewport, limit int) ([]render.LayerLabel, int) {
	if limit <= 0 {
		return nil, len(labels)
	}
	if viewport.Width > 0 && viewport.Height > 0 && viewport.Zoom > 0 && viewport.ViewportWidth > 0 && viewport.ViewportHeight > 0 {
		centerX := 0.5 - viewport.PanX/(viewport.Width*viewport.Zoom)
		centerY := 0.5 + viewport.PanY/(viewport.Height*viewport.Zoom)
		halfWidth := 0.55 * viewport.ViewportWidth / (viewport.Width * viewport.Zoom)
		halfHeight := 0.55 * viewport.ViewportHeight / (viewport.Height * viewport.Zoom)
		visible := labels[:0]
		for _, label := range labels {
			if label.X >= centerX-halfWidth && label.X <= centerX+halfWidth && label.Y >= centerY-halfHeight && label.Y <= centerY+halfHeight {
				visible = append(visible, label)
			}
		}
		labels = visible
	}
	// Window chunks finish out of order. Stable spatial order prevents their
	// map iteration order from changing which label wins a crowded screen.
	sort.Slice(labels, func(i, j int) bool {
		if labels[i].Y != labels[j].Y {
			return labels[i].Y < labels[j].Y
		}
		if labels[i].X != labels[j].X {
			return labels[i].X < labels[j].X
		}
		if labels[i].Layer != labels[j].Layer {
			return labels[i].Layer < labels[j].Layer
		}
		if labels[i].FeatureID != labels[j].FeatureID {
			return labels[i].FeatureID < labels[j].FeatureID
		}
		return labels[i].Text < labels[j].Text
	})
	return sampleLayerLabels(labels, limit)
}

func sampleLayerLabels(labels []render.LayerLabel, limit int) ([]render.LayerLabel, int) {
	if limit <= 0 {
		return nil, len(labels)
	}
	if len(labels) <= limit {
		return labels, 0
	}
	// Evenly sample in source order so large layers retain spatial coverage.
	selected := make([]render.LayerLabel, 0, limit)
	for index := 0; index < limit; index++ {
		selected = append(selected, labels[index*len(labels)/limit])
	}
	return selected, len(labels) - limit
}

const maxLayerLabelPayloadBytes = 8 << 20
const maxLayerLabelCount = 20_000
const maxLayerLabelJSONFixedBytes = 288

func addJSONEscapedStringUpperBound(estimate *int64, value string, limit int64) bool {
	for len(value) > 0 {
		r, size := utf8.DecodeRuneInString(value)
		value = value[size:]
		encodedSize := int64(size)
		switch {
		case r == utf8.RuneError && size == 1:
			encodedSize = 6 // encoding/json replaces invalid UTF-8 with "\\ufffd".
		case r == '"' || r == '\\' || r == '\b' || r == '\f' || r == '\n' || r == '\r' || r == '\t':
			encodedSize = 2
		case r < 0x20 || r == '<' || r == '>' || r == '&' || r == '\u2028' || r == '\u2029':
			encodedSize = 6
		}
		if *estimate > limit-encodedSize {
			return false
		}
		*estimate += encodedSize
	}
	return true
}

func marshalLayerLabels(labels []render.LayerLabel) (string, error) {
	if labels == nil {
		labels = []render.LayerLabel{}
	}
	if len(labels) > maxLayerLabelCount {
		return "", fmt.Errorf("label count exceeds the %d-label safety limit", maxLayerLabelCount)
	}
	// Account for JSON escaping and fixed fields before json.Marshal allocates
	// the serialized copy. Invalid UTF-8, HTML-sensitive bytes, and controls
	// can expand substantially in encoding/json.
	estimated := int64(2)
	for _, label := range labels {
		// This covers all field names, punctuation, the uint64 ID, six finite
		// float64 values, and the array comma for this label.
		if estimated > int64(maxLayerLabelPayloadBytes-maxLayerLabelJSONFixedBytes) {
			return "", fmt.Errorf("label payload exceeds the %d MiB safety limit", maxLayerLabelPayloadBytes>>20)
		}
		estimated += maxLayerLabelJSONFixedBytes
		if !addJSONEscapedStringUpperBound(&estimated, label.Layer, maxLayerLabelPayloadBytes) ||
			!addJSONEscapedStringUpperBound(&estimated, label.Text, maxLayerLabelPayloadBytes) {
			return "", fmt.Errorf("label payload exceeds the %d MiB safety limit", maxLayerLabelPayloadBytes>>20)
		}
	}
	payload, err := json.Marshal(labels)
	if err != nil {
		return "", err
	}
	if len(payload) > maxLayerLabelPayloadBytes {
		return "", fmt.Errorf("label payload exceeds the %d MiB safety limit", maxLayerLabelPayloadBytes>>20)
	}
	return string(payload), nil
}

func (r *demoRuntime) publishAttributes(layerName string) {
	r.publishAttributesPage(layerName, 0)
}

const attributePageSize = 200

func attributePageOffset(page int) (int, bool) {
	if page < 0 || page > int(^uint(0)>>1)/attributePageSize {
		return 0, false
	}
	return page * attributePageSize, true
}

func (r *demoRuntime) publishAttributesPage(layerName string, page int) {
	r.mu.Lock()
	dispatch := r.nextAttributeDispatchLocked()
	r.mu.Unlock()
	r.publishAttributesPageDispatched(layerName, page, dispatch)
}

func (r *demoRuntime) publishAttributesPageAsync(layerName string, page int) {
	r.mu.Lock()
	dispatch := r.nextAttributeDispatchLocked()
	r.mu.Unlock()
	go r.publishAttributesPageDispatched(layerName, page, dispatch)
}

func (r *demoRuntime) nextAttributeDispatchLocked() uint64 {
	r.attributeDispatchGeneration++
	r.attributeGeneration++
	if r.attributeCancel != nil {
		r.attributeCancel()
		r.attributeCancel = nil
	}
	return r.attributeDispatchGeneration
}

func (r *demoRuntime) publishAttributesPageDispatched(layerName string, page int, dispatch uint64) {
	if page < 0 {
		page = 0
	}
	pageOffset, validOffset := attributePageOffset(page)
	if !validOffset {
		return
	}
	r.mu.Lock()
	if dispatch != r.attributeDispatchGeneration {
		r.mu.Unlock()
		return
	}
	if r.attributeReady && r.attributeLayer == layerName && r.attributePage == page {
		r.mu.Unlock()
		return
	}
	r.attributeGeneration++
	generation := r.attributeGeneration
	if r.attributeCancel != nil {
		r.attributeCancel()
		r.attributeCancel = nil
	}
	key := attributePageKey{layer: layerName, page: page}
	if payload, ok := r.attributeCache[key]; ok {
		native.SetAttributePayload(payload)
		r.attributeLayer = layerName
		r.attributePage = page
		r.attributeReady = true
		r.mu.Unlock()
		return
	}
	if r.service == nil {
		native.SetAttributePayload("[]")
		r.attributeLayer = layerName
		r.attributePage = page
		r.attributeReady = true
		r.mu.Unlock()
		return
	}
	service := r.service
	reader := r.attributePageReader
	requestContext, cancel := context.WithCancel(context.Background())
	r.attributeCancel = cancel
	r.mu.Unlock()
	defer cancel()
	payloadModel := attributePayload{}
	var layer core.Layer
	var total int
	var ok bool
	var pageErr error
	if reader != nil {
		layer, total, pageErr = reader(requestContext, layerName, pageOffset, attributePageSize)
		ok = pageErr == nil
	} else {
		layer, total, ok = service.LayerAttributePageOwned(layerName, pageOffset, attributePageSize)
	}
	if ok {
		table := presentation.AttributeTableOwned(layer)
		payloadModel.Columns = make([]string, len(table.Columns))
		payloadModel.Fields = make([]attributeFieldHint, len(table.Columns))
		for i, column := range table.Columns {
			payloadModel.Columns[i] = column.Name
			payloadModel.Fields[i] = attributeFieldHint{Name: column.Name, Type: column.Type}
		}
		payloadModel.Rows = make([]attributePayloadRow, len(table.Rows))
		for i, row := range table.Rows {
			payloadModel.Rows[i] = attributePayloadRow{FeatureID: row.FeatureID, Values: row.Values}
		}
		payloadModel.Page = page
		payloadModel.PageSize = attributePageSize
		payloadModel.Total = total
	}
	payloadTooLarge := estimateAttributePayloadJSONBytes(payloadModel) > maxAttributePayloadJSONBytes
	var payload []byte
	var marshalErr error
	if !payloadTooLarge {
		payload, marshalErr = json.Marshal(payloadModel)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if generation != r.attributeGeneration || r.service != service {
		return
	}
	r.attributeCancel = nil
	if pageErr != nil {
		if requestContext.Err() == nil {
			native.SetRenderStatus("Attribute page failed: " + pageErr.Error())
		}
		return
	}
	if payloadTooLarge {
		native.SetAttributePayload("[]")
		native.SetRenderStatus(fmt.Sprintf("Attribute page exceeds the %d MiB display payload limit", maxAttributePayloadJSONBytes>>20))
		r.attributeLayer = layerName
		r.attributePage = page
		r.attributeReady = true
		return
	}
	if marshalErr != nil {
		native.SetAttributePayload("[]")
		r.attributeLayer = layerName
		r.attributePage = page
		r.attributeReady = true
		return
	}
	native.SetAttributePayload(string(payload))
	r.cacheAttributePayload(key, string(payload))
	r.attributeLayer = layerName
	r.attributePage = page
	r.attributeReady = true
}

func (r *demoRuntime) republishAttributesAsync(layerName string) {
	r.mu.Lock()
	r.attributeReady = false
	r.attributeCache = nil
	r.attributeCacheOrder = nil
	r.attributeCacheBytes = 0
	dispatch := r.nextAttributeDispatchLocked()
	r.mu.Unlock()
	go r.publishAttributesPageDispatched(layerName, 0, dispatch)
}

const attributePayloadCacheLimit = 8
const attributePayloadCacheByteLimit = 32 << 20
const maxAttributePayloadJSONBytes = 16 << 20
const maxHiddenLayerCacheVertices = 1 << 20
const maxAttributePayloadJSONNodes = 1 << 20

func (r *demoRuntime) cacheAttributePayload(key attributePageKey, payload string) {
	payloadBytes := int64(len(payload))
	if payloadBytes > attributePayloadCacheByteLimit {
		return
	}
	if r.attributeCache == nil {
		r.attributeCache = make(map[attributePageKey]string)
	}
	if _, exists := r.attributeCache[key]; exists {
		return
	}
	r.attributeCache[key] = payload
	r.attributeCacheOrder = append(r.attributeCacheOrder, key)
	r.attributeCacheBytes += payloadBytes
	for len(r.attributeCacheOrder) > attributePayloadCacheLimit || r.attributeCacheBytes > attributePayloadCacheByteLimit {
		oldest := r.attributeCacheOrder[0]
		oldestPayload := r.attributeCache[oldest]
		delete(r.attributeCache, oldest)
		r.attributeCacheBytes -= int64(len(oldestPayload))
		r.attributeCacheOrder = r.attributeCacheOrder[1:]
	}
}

// estimateAttributePayloadJSONBytes conservatively bounds the encoded JSON
// size before json.Marshal allocates its output and the Qt bridge copies it.
func estimateAttributePayloadJSONBytes(payload attributePayload) int64 {
	limit := int64(maxAttributePayloadJSONBytes)
	total := int64(0)
	add := func(size int64) bool {
		if size < 0 || total > limit-size {
			total = limit + 1
			return false
		}
		total += size
		return total <= limit
	}
	var writeString func(string) bool
	writeString = func(value string) bool {
		if !add(2) { // JSON quotes
			return false
		}
		for len(value) > 0 {
			runeValue, size := utf8.DecodeRuneInString(value)
			if runeValue == utf8.RuneError && size == 1 {
				if !add(3) { // encoding/json replaces invalid UTF-8 with U+FFFD
					return false
				}
				value = value[1:]
				continue
			}
			encodedSize := int64(size)
			switch {
			case runeValue < 0x20, runeValue == '<', runeValue == '>', runeValue == '&', runeValue == '\u2028', runeValue == '\u2029':
				encodedSize = 6
			case runeValue == '"' || runeValue == '\\':
				encodedSize = 2
			}
			if !add(encodedSize) {
				return false
			}
			value = value[size:]
		}
		return true
	}
	nodes := 0
	var writeValue func(any, int) bool
	writeValue = func(value any, depth int) bool {
		nodes++
		if nodes > maxAttributePayloadJSONNodes || depth > 64 {
			return false
		}
		switch value := value.(type) {
		case nil:
			return add(4)
		case bool:
			if value {
				return add(4)
			}
			return add(5)
		case string:
			return writeString(value)
		case []byte:
			encodedLength := ((int64(len(value)) + 2) / 3) * 4
			return add(encodedLength + 2)
		case json.Number:
			return add(int64(len(value)))
		case int:
			return add(24)
		case int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
			return add(32)
		case []string:
			if !add(2) {
				return false
			}
			for index, item := range value {
				if index > 0 && !add(1) {
					return false
				}
				nodes++
				if nodes > maxAttributePayloadJSONNodes || !writeString(item) {
					return false
				}
			}
			return true
		case []any:
			if !add(2) {
				return false
			}
			for index, item := range value {
				if index > 0 && !add(1) {
					return false
				}
				if !writeValue(item, depth+1) {
					return false
				}
			}
			return true
		case map[string]any:
			if !add(2) {
				return false
			}
			index := 0
			for key, item := range value {
				if index > 0 && !add(1) {
					return false
				}
				if !writeString(key) || !add(1) || !writeValue(item, depth+1) {
					return false
				}
				index++
			}
			return true
		default:
			return false
		}
	}

	if !add(256) { // top-level object keys, page metadata, and punctuation
		return limit + 1
	}
	for index, column := range payload.Columns {
		if index > 0 && !add(1) {
			return limit + 1
		}
		if !writeString(column) {
			return limit + 1
		}
	}
	for index, field := range payload.Fields {
		if index > 0 && !add(1) {
			return limit + 1
		}
		if !writeString(field.Name) || !writeString(string(field.Type)) || !add(32) {
			return limit + 1
		}
	}
	for index, row := range payload.Rows {
		if index > 0 && !add(1) {
			return limit + 1
		}
		if !add(64) { // row object, feature ID, and field names
			return limit + 1
		}
		if !add(2) { // values object
			return limit + 1
		}
		valueIndex := 0
		for key, value := range row.Values {
			if (valueIndex > 0 && !add(1)) || !writeString(key) || !add(1) || !writeValue(value, 0) {
				return limit + 1
			}
			valueIndex++
		}
	}
	return total
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
	r.mu.Lock()
	scheduler := r.scheduler
	batchStore := r.batchStore
	builder := r.builder
	planner := r.planner
	visibility := r.visibility
	viewportReadOnly := r.viewportReadOnly
	mapExtentForKeys := r.mapExtent
	layerBoundsForKeys := make(map[string][4]float64, len(r.layerBounds))
	if viewportReadOnly {
		for name, bounds := range r.layerBounds {
			layerBoundsForKeys[name] = bounds
		}
	}
	forceOverview := r.forceOverviewNext
	r.forceOverviewNext = false
	visibleLayerNames := visibility.VisibleLayers()
	layerSignature := strings.Join(visibleLayerNames, "\x00")
	keepCompleteFrame := r.hasCompleteFrame && r.completeFrameLayers == layerSignature && viewportReadOnly
	maxVisibleLayerFeatureCount := 0
	for _, layerName := range visibleLayerNames {
		maxVisibleLayerFeatureCount = max(maxVisibleLayerFeatureCount, r.layerFeatureCounts[layerName])
	}
	layerVisibility := make(map[string]bool, len(r.visibleLayers))
	for layer, visible := range r.visibleLayers {
		layerVisibility[layer] = visible
	}
	renderStage := 0
	if r.dataMode {
		renderStage = 2
		if r.previewLoading {
			renderStage = 1
		}
	}
	r.mu.Unlock()
	overviewStride := 1
	if viewportReadOnly {
		zoomBucket := readOnlyWindowZoomBucket(viewport.Zoom)
		planner.ChunkSize = readOnlyWindowChunkSize(zoomBucket)
		overviewStride = readOnlyOverviewStrideForFeatureCount(zoomBucket, maxVisibleLayerFeatureCount)
		if forceOverview {
			overviewStride = max(2, overviewStride) // Show the approximate-overview status during a budget retry.
		}
	}
	keyBuffer := scheduler.AcquireChunkKeyBuffer(0)
	keys := keyBuffer.Keys
	chunkPlanExceeded := false
	prunedReadOnlyKeys := 0
	for _, layer := range visibleLayerNames {
		var withinLimit bool
		keys, withinLimit = planner.VisibleKeysIntoLimit(keys, viewport, layer, render.MaxViewportChunkKeys)
		if !withinLimit {
			chunkPlanExceeded = true
			break
		}
	}
	if viewportReadOnly && !chunkPlanExceeded {
		planned := len(keys)
		keys = filterReadOnlyKeysByLayerBounds(keys, mapExtentForKeys, planner.ChunkSize, layerBoundsForKeys)
		prunedReadOnlyKeys = planned - len(keys)
	}
	keyBuffer.Keys = keys
	if chunkPlanExceeded {
		keys = nil
	}
	keys = visibility.FilterChunkKeysInPlace(keys)
	prioritizeViewportChunks(keys, viewport, planner.ChunkSize)
	if keepCompleteFrame && len(keys) > maxPreservedCompleteFrameChunkKeys {
		keepCompleteFrame = false
		native.RecordDiagnostic("render", fmt.Sprintf(
			"progressive-replacement chunks=%d previous_complete_frame=true threshold=%d",
			len(keys), maxPreservedCompleteFrameChunkKeys,
		))
	}
	hiddenKeys := make([]render.ChunkKey, 0)
	if !chunkPlanExceeded && len(keys) < render.MaxViewportChunkKeys {
		for layer, visible := range layerVisibility {
			if visible {
				continue
			}
			var withinLimit bool
			hiddenKeys, withinLimit = planner.VisibleKeysIntoLimit(hiddenKeys, viewport, layer,
				render.MaxViewportChunkKeys-len(keys))
			if !withinLimit {
				// Drop the optional hidden-layer cache as a whole if its key plan
				// would exceed the viewport cap; visible rendering takes priority.
				hiddenKeys = nil
				break
			}
		}
		if viewportReadOnly {
			hiddenKeys = filterReadOnlyKeysByLayerBounds(hiddenKeys, mapExtentForKeys, planner.ChunkSize, layerBoundsForKeys)
		}
	}
	// Preserve current-view chunks for visible layers and a small cache for
	// hidden layers. This makes a single-layer toggle responsive without
	// retaining the full hidden geometry payload.
	scheduler.RetainViewportChunks(keys, hiddenKeys, maxHiddenLayerCacheVertices)
	generation := scheduler.Generation()
	batchStore.BeginGenerationWithVisible(generation, keys)
	r.mu.Lock()
	if r.scheduler != scheduler {
		r.mu.Unlock()
		scheduler.ReleaseChunkKeyBuffer(keyBuffer)
		return
	}
	previousCancel := r.cancel
	r.cancel = nil
	if !r.dataMode {
		r.features = demoFeatures(keys, planner.ChunkSize)
		r.hitIndex = render.HitIndex{}
		r.hitIndexReady = false
	} else if r.viewportReadOnly {
		r.retainVisibleWindowChunksLocked(keys)
	}
	r.mu.Unlock()
	if previousCancel != nil {
		previousCancel()
	}
	if viewportReadOnly && !keepCompleteFrame {
		// Discard labels from old windows as soon as the new viewport is planned.
		r.publishLayerLabelsWithStatus(false)
	}
	if len(keys) == 0 {
		r.mu.Lock()
		if r.scheduler != scheduler {
			r.mu.Unlock()
			scheduler.ReleaseChunkKeyBuffer(keyBuffer)
			return
		}
		if chunkPlanExceeded {
			native.SetRenderStatus("Render stopped: viewport exceeds the chunk-key safety limit")
		} else if len(layerVisibility) > 0 {
			native.SetRenderStatus("No visible layers")
		}
		batchStore.Clear()
		native.SetVertices(nil)
		r.hasCompleteFrame = false
		r.completeFrameLayers = ""
		native.RequestCanvasUpdate()
		r.publishedRevision = batchStore.Revision()
		native.SetSelection("", "", "", "No feature selected")
		r.mu.Unlock()
		r.publishLayerLabelsWithStatus(false)
		scheduler.ReleaseChunkKeyBuffer(keyBuffer)
		return
	}

	requestContext, cancel := context.WithCancel(ctx)
	if forceOverview {
		requestContext = context.WithValue(requestContext, forceReadOnlyOverviewContextKey{}, true)
	}
	requestGeneration := generation
	r.mu.Lock()
	if r.scheduler != scheduler {
		r.mu.Unlock()
		cancel()
		scheduler.ReleaseChunkKeyBuffer(keyBuffer)
		return
	}
	r.cancel = cancel
	r.mu.Unlock()
	renderStartedAt := time.Now()
	renderStatsBefore := scheduler.Stats()
	lastProgress := time.Time{}
	setProgress := func(progress presentation.RenderProgress, force bool) {
		now := time.Now()
		if !force && !lastProgress.IsZero() && now.Sub(lastProgress) < 16*time.Millisecond {
			return
		}
		r.mu.Lock()
		current := r.scheduler == scheduler && scheduler.Generation() == requestGeneration
		preview := r.previewLoading
		loading := r.loadCancel != nil
		if !current || (loading && !preview) {
			r.mu.Unlock()
			return
		}
		if preview {
			native.SetRenderStatus("Preview displayed; loading full data")
		} else if overviewStride > 1 && (progress.Phase == "Loading" || progress.Phase == "Ready") {
			if progress.Phase == "Ready" {
				native.SetRenderStatus("Approximate overview ready; zoom in for exact geometry and selection")
			} else {
				native.SetRenderStatus(fmt.Sprintf("Loading approximate overview (%d/%d); zoom in for exact geometry and selection", progress.Completed, progress.Total))
			}
		} else {
			native.SetRenderStatus(progress.Message())
		}
		r.mu.Unlock()
		lastProgress = now
	}
	setStatusIfCurrent := func(status string) {
		r.mu.Lock()
		defer r.mu.Unlock()
		if r.scheduler == scheduler && scheduler.Generation() == requestGeneration {
			native.SetRenderStatus(status)
		}
	}
	setProgress(presentation.RenderProgress{Phase: "Loading", Total: len(keys), Cancellable: true}, true)
	zoomBucket := readOnlyWindowZoomBucket(viewport.Zoom)
	r.mu.Lock()
	mapExtent, fitExtent := r.mapExtent, r.mapFitExtent
	r.mu.Unlock()
	lodBucket := readOnlyOverviewZoomBucket(zoomBucket, mapExtent, fitExtent)
	windowWorkerLimit := scheduler.MaxWorkers()
	if viewportReadOnly && lodBucket > 1 && !forceOverview {
		windowWorkerLimit = min(windowWorkerLimit, maxReadOnlyDetailWorkers)
	}
	native.RecordDiagnostic("render", fmt.Sprintf(
		"request generation=%d layers=%d chunks=%d pruned=%d zoom=%g lod=%d workers=%d window_limit=%d forced_overview=%t center=(%.6f,%.6f) screen=%.0fx%.0f canvas=%.0fx%.0f extent=[%.3f,%.3f,%.3f,%.3f] fit=[%.3f,%.3f,%.3f,%.3f]",
		requestGeneration, len(visibleLayerNames), len(keys), prunedReadOnlyKeys, viewport.Zoom, lodBucket,
		scheduler.MaxWorkers(), windowWorkerLimit, forceOverview,
		viewport.Center.X, viewport.Center.Y, viewport.ScreenWidth, viewport.ScreenHeight,
		viewport.CanvasWidth, viewport.CanvasHeight, mapExtent[0], mapExtent[1], mapExtent[2], mapExtent[3],
		fitExtent[0], fitExtent[1], fitExtent[2], fitExtent[3]))

	go func(requestKeys []render.ChunkKey, keyBuffer *render.ChunkKeyBuffer, scheduler *render.Scheduler) {
		defer scheduler.ReleaseChunkKeyBuffer(keyBuffer)
		var activeBuildsMu sync.Mutex
		activeBuilds := make(map[render.ChunkKey]time.Time)
		trackedBuilder := func(buildContext context.Context, key render.ChunkKey) (render.Chunk, error) {
			activeBuildsMu.Lock()
			activeBuilds[key] = time.Now()
			activeBuildsMu.Unlock()
			defer func() {
				activeBuildsMu.Lock()
				delete(activeBuilds, key)
				activeBuildsMu.Unlock()
			}()
			return builder(buildContext, key)
		}
		requestFinished := make(chan struct{})
		defer close(requestFinished)
		results := scheduler.RequestUnique(requestContext, requestKeys, trackedBuilder)
		go func() {
			ticker := time.NewTicker(5 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-requestFinished:
					return
				case <-requestContext.Done():
					return
				case <-ticker.C:
					activeBuildsMu.Lock()
					entries := make([]string, 0, len(activeBuilds))
					for key, started := range activeBuilds {
						entries = append(entries, fmt.Sprintf("%s:%d,%d:%dms", key.Layer, key.X, key.Y,
							time.Since(started).Milliseconds()))
					}
					activeBuildsMu.Unlock()
					if len(entries) > 0 {
						sort.Strings(entries)
					}
					stats := scheduler.Stats()
					native.RecordDiagnostic("render", fmt.Sprintf(
						"in-flight generation=%d workers=%d/%d tiles=%v",
						requestGeneration, stats.ActiveWorkers, scheduler.MaxWorkers(), entries))
				}
			}
		}()
		completed := 0
		renderedVertices := 0
		nonemptyChunks := 0
		emptyChunkKeys := make([]render.ChunkKey, 0)
		fillVerticesByLayer := make(map[string]int, len(visibleLayerNames))
		zeroAlphaFillVertices := 0
		centerVertices := 0
		validVertices := 0
		var occupiedCells [16 * 16]bool
		vertexMinX, vertexMinY := float32(math.Inf(1)), float32(math.Inf(1))
		vertexMaxX, vertexMaxY := float32(math.Inf(-1)), float32(math.Inf(-1))
		renderErr := ""
		lastPublish := time.Now()
		lastDiagnosticProgress := time.Now()
		// Publishing flattens and copies the complete visible batch. Limit that
		// repeated work during bulk loads while keeping partial results responsive.
		const progressivePublishInterval = 50 * time.Millisecond
		const maxProgressiveReadOnlyVertices = 30_000
		const maxProgressiveReadOnlyPublishes = 8
		firstPublish := false
		progressivePublishes := 0
		lastLabelPublish := time.Time{}
		publishLabelsIfDue := func() {
			if !viewportReadOnly || requestContext.Err() != nil || (!lastLabelPublish.IsZero() && time.Since(lastLabelPublish) < 150*time.Millisecond) {
				return
			}
			r.mu.Lock()
			current := r.scheduler == scheduler && scheduler.Generation() == requestGeneration
			hasLabels := len(r.mapLabels) > 0
			r.mu.Unlock()
			if current && hasLabels {
				r.publishLayerLabelsWithStatus(false)
				lastLabelPublish = time.Now()
			}
		}
		var publishScratch []render.Vertex
		dirty := true // BeginGeneration may have removed now-hidden chunks.
		publishBatch := func(final bool) {
			if !dirty || requestContext.Err() != nil || (keepCompleteFrame && !final) {
				return
			}
			// A citywide layer can grow to millions of source vertices. Repeatedly
			// flattening, copying, and rebuilding every intermediate frame makes
			// the render thread fall behind the worker; publish the final batch once.
			if viewportReadOnly && !final &&
				(renderedVertices > maxProgressiveReadOnlyVertices || progressivePublishes >= maxProgressiveReadOnlyPublishes) {
				return
			}
			r.mu.Lock()
			if r.scheduler != scheduler {
				r.mu.Unlock()
				return
			}
			if batchStore.Revision() == r.publishedRevision {
				dirty = false
				r.mu.Unlock()
				return
			}
			currentGeneration, revision, vertices := batchStore.CurrentIntoVersion(publishScratch)
			publishScratch = vertices
			if currentGeneration == requestGeneration && requestContext.Err() == nil {
				native.SetVerticesStage(publishScratch, renderStage)
				native.RequestCanvasUpdate()
				if !firstPublish && len(publishScratch) > 0 {
					native.RecordDiagnostic("render", fmt.Sprintf("first-publish generation=%d elapsed_ms=%d vertices=%d",
						requestGeneration, time.Since(renderStartedAt).Milliseconds(), len(publishScratch)))
					firstPublish = true
				}
				r.publishedRevision = revision
				dirty = false
				lastPublish = time.Now()
				if viewportReadOnly && !final {
					progressivePublishes++
				}
			}
			r.mu.Unlock()
		}
		for result := range results {
			completed++
			if completed%25 == 0 || time.Since(lastDiagnosticProgress) >= 5*time.Second {
				native.RecordDiagnostic("render", fmt.Sprintf(
					"progress generation=%d chunks=%d/%d last=%s:%d,%d elapsed_ms=%d",
					requestGeneration, completed, len(requestKeys), result.Key.Layer, result.Key.X, result.Key.Y,
					time.Since(renderStartedAt).Milliseconds(),
				))
				lastDiagnosticProgress = time.Now()
			}
			setProgress(presentation.RenderProgress{Phase: "Loading", Completed: completed, Total: len(requestKeys), Cancellable: true}, completed == len(requestKeys))
			applied, applyErr := batchStore.ApplyImmutableChecked(result)
			if !applied {
				if applyErr != nil && renderErr == "" {
					// Keep draining so all workers release their request slots.
					renderErr = applyErr.Error()
				}
				continue
			}
			renderedVertices += len(result.Chunk.Vertices)
			if len(result.Chunk.Vertices) > 0 {
				nonemptyChunks++
			} else {
				emptyChunkKeys = append(emptyChunkKeys, result.Key)
			}
			chunkFillVertices := 0
			for _, vertex := range result.Chunk.Vertices {
				if vertex.Kind == render.VertexFill {
					chunkFillVertices++
					if vertex.Color&0xff == 0 {
						zeroAlphaFillVertices++
					}
				}
				if math.IsNaN(float64(vertex.X)) || math.IsInf(float64(vertex.X), 0) ||
					math.IsNaN(float64(vertex.Y)) || math.IsInf(float64(vertex.Y), 0) {
					continue
				}
				vertexMinX = min(vertexMinX, vertex.X)
				vertexMinY = min(vertexMinY, vertex.Y)
				vertexMaxX = max(vertexMaxX, vertex.X)
				vertexMaxY = max(vertexMaxY, vertex.Y)
				validVertices++
				if vertex.X >= 0.45 && vertex.X <= 0.55 && vertex.Y >= 0.45 && vertex.Y <= 0.55 {
					centerVertices++
				}
				if vertex.X >= 0 && vertex.X <= 1 && vertex.Y >= 0 && vertex.Y <= 1 {
					xCell := min(15, int(vertex.X*16))
					yCell := min(15, int(vertex.Y*16))
					occupiedCells[yCell*16+xCell] = true
				}
			}
			if chunkFillVertices > 0 {
				fillVerticesByLayer[result.Key.Layer] += chunkFillVertices
			}
			dirty = true
			if time.Since(lastPublish) >= progressivePublishInterval {
				publishBatch(false)
			}
			// A retained complete frame suppresses geometry publication until the
			// replacement is ready, but its new labels must not wait for that frame.
			publishLabelsIfDue()
		}
		// Publish the current generation's successful chunks when the request
		// terminates with an error or missing results. Keeping the previous
		// complete frame in that case makes stale cached tiles look like a
		// successful render of the new viewport.
		publishBatch(requestContext.Err() == nil)
		if requestContext.Err() == nil && (renderErr != "" || completed != len(requestKeys)) {
			r.mu.Lock()
			if r.scheduler == scheduler && scheduler.Generation() == requestGeneration {
				r.hasCompleteFrame = false
				r.completeFrameLayers = ""
			}
			r.mu.Unlock()
		}
		if requestContext.Err() != nil {
			setProgress(presentation.RenderProgress{Phase: "Render cancelled"}, true)
		} else if renderErr != "" {
			if shouldRetryViewportWithOverview(viewportReadOnly, forceOverview, renderErr) {
				r.mu.Lock()
				canRetry := r.scheduler == scheduler && scheduler.Generation() == requestGeneration
				if canRetry {
					r.forceOverviewNext = true
				}
				r.mu.Unlock()
				if canRetry {
					for _, layerName := range visibleLayerNames {
						scheduler.InvalidateLayer(layerName)
					}
					r.mu.Lock()
					canRetry = r.scheduler == scheduler && scheduler.Generation() == requestGeneration && r.forceOverviewNext
					r.mu.Unlock()
					if canRetry {
						native.RecordDiagnostic("render", fmt.Sprintf("retry generation=%d using generalized polygon boundaries after viewport batch budget", requestGeneration))
						r.refresh(context.Background(), viewport)
						return
					}
				}
			}
			stats := scheduler.Stats()
			native.RecordDiagnostic("render", fmt.Sprintf(
				"incomplete generation=%d chunks=%d vertices=%d elapsed_ms=%d cache_hits=%d chunks_built=%d error=%s",
				requestGeneration, completed, renderedVertices, time.Since(renderStartedAt).Milliseconds(),
				stats.CacheHits-renderStatsBefore.CacheHits, stats.ChunksBuilt-renderStatsBefore.ChunksBuilt,
				renderErr,
			))
			setStatusIfCurrent("Render incomplete; zoom in and try again: " + renderErr)
		} else if completed != len(requestKeys) {
			// A closed result stream is not proof that every requested chunk was
			// delivered (for example, cancellation or a scheduler regression can
			// end it early). Never label a partial viewport as ready.
			missing := len(requestKeys) - completed
			native.RecordDiagnostic("render", fmt.Sprintf(
				"incomplete generation=%d chunks=%d/%d vertices=%d elapsed_ms=%d missing_results=%d",
				requestGeneration, completed, len(requestKeys), renderedVertices,
				time.Since(renderStartedAt).Milliseconds(), missing,
			))
			setStatusIfCurrent(fmt.Sprintf("Render incomplete; zoom in and try again: %d chunks were not returned", missing))
		} else {
			setProgress(presentation.RenderProgress{Phase: "Ready", Completed: completed, Total: len(requestKeys)}, true)
			if nonemptyChunks < completed {
				setStatusIfCurrent(fmt.Sprintf("Render ready: %d/%d chunks contain geometry", nonemptyChunks, completed))
			}
			r.mu.Lock()
			if r.scheduler == scheduler && scheduler.Generation() == requestGeneration {
				r.hasCompleteFrame = true
				r.completeFrameLayers = layerSignature
			}
			r.mu.Unlock()
			stats := scheduler.Stats()
			occupiedCount := 0
			for _, occupied := range occupiedCells {
				if occupied {
					occupiedCount++
				}
			}
			sort.Slice(emptyChunkKeys, func(left, right int) bool {
				if emptyChunkKeys[left].Layer != emptyChunkKeys[right].Layer {
					return emptyChunkKeys[left].Layer < emptyChunkKeys[right].Layer
				}
				if emptyChunkKeys[left].Y != emptyChunkKeys[right].Y {
					return emptyChunkKeys[left].Y < emptyChunkKeys[right].Y
				}
				return emptyChunkKeys[left].X < emptyChunkKeys[right].X
			})
			emptySampleCount := min(16, len(emptyChunkKeys))
			emptySample := make([]string, emptySampleCount)
			for index, key := range emptyChunkKeys[:emptySampleCount] {
				emptySample[index] = fmt.Sprintf("%s:%d,%d", key.Layer, key.X, key.Y)
			}
			fillSummary, _ := json.Marshal(fillVerticesByLayer)
			native.RecordDiagnostic("render", fmt.Sprintf(
				"ready generation=%d chunks=%d nonempty=%d empty=%d empty_sample=%v vertices=%d fill_vertices_by_layer=%s zero_alpha_fills=%d vertex_bounds=[%.6f,%.6f,%.6f,%.6f] center_vertices=%d/%d occupied_cells=%d/256 elapsed_ms=%d cache_hits=%d chunks_built=%d",
				requestGeneration, completed, nonemptyChunks, completed-nonemptyChunks, emptySample, renderedVertices, string(fillSummary), zeroAlphaFillVertices,
				vertexMinX, vertexMinY, vertexMaxX, vertexMaxY, centerVertices, validVertices, occupiedCount,
				time.Since(renderStartedAt).Milliseconds(),
				stats.CacheHits-renderStatsBefore.CacheHits, stats.ChunksBuilt-renderStatsBefore.ChunksBuilt,
			))
		}
		// Keep the final map-render status visible; label-limit notices must not
		// replace a ready/incomplete chunk summary in the shared status channel.
		r.publishLayerLabelsWithStatus(false)
	}(keys, keyBuffer, scheduler)
}

// prioritizeViewportChunks makes progressive rendering useful: workers start
// with the chunk nearest the view center while preserving project layer order
// (and stable ordering for equally distant chunks).
func prioritizeViewportChunks(keys []render.ChunkKey, viewport render.Viewport, chunkSize float64) {
	if len(keys) < 2 || chunkSize <= 0 {
		return
	}
	for start := 0; start < len(keys); {
		end := start + 1
		for end < len(keys) && keys[end].Layer == keys[start].Layer {
			end++
		}
		group := keys[start:end]
		sort.SliceStable(group, func(left, right int) bool {
			leftX := (float64(group[left].X)+0.5)*chunkSize - viewport.Center.X
			leftY := (float64(group[left].Y)+0.5)*chunkSize - viewport.Center.Y
			rightX := (float64(group[right].X)+0.5)*chunkSize - viewport.Center.X
			rightY := (float64(group[right].Y)+0.5)*chunkSize - viewport.Center.Y
			return leftX*leftX+leftY*leftY < rightX*rightX+rightY*rightY
		})
		start = end
	}
}

// retainVisibleWindowChunksLocked drops cached feature geometry and metadata
// once its chunk leaves the viewport. The caller must hold r.mu.
func (r *demoRuntime) retainVisibleWindowChunksLocked(keys []render.ChunkKey) {
	r.windowVisibleKeys = make(map[render.ChunkKey]struct{}, len(keys))
	for _, key := range keys {
		r.windowVisibleKeys[key] = struct{}{}
	}
	for key := range r.windowPayloadBytes {
		if _, keep := r.windowVisibleKeys[key]; !keep {
			r.removeWindowChunkLocked(key)
		}
	}
	r.rebuildWindowFeaturesLocked()
}

// rebuildWindowFeaturesLocked keeps hit-test and label data bounded to the
// same viewport chunks as rendered geometry. The caller must hold r.mu.
func (r *demoRuntime) rebuildWindowFeaturesLocked() {
	features := append([]render.HitFeature(nil), r.readOnlyBaseFeatures...)
	labels := append([]render.LayerLabel(nil), r.readOnlyBaseLabels...)
	type labelKey struct {
		layer string
		text  string
		x, y  float64
	}
	seenLabels := make(map[labelKey]struct{})
	for key := range r.windowVisibleKeys {
		features = append(features, r.windowHits[key]...)
		for _, label := range r.windowLabels[key] {
			key := labelKey{layer: label.Layer, text: label.Text, x: label.X, y: label.Y}
			if _, seen := seenLabels[key]; seen {
				continue
			}
			seenLabels[key] = struct{}{}
			labels = append(labels, label)
		}
	}
	r.features = features
	r.mapLabels = labels
	r.hitIndex = render.HitIndex{}
	r.hitIndexReady = false
}

func (r *demoRuntime) removeWindowChunkLocked(key render.ChunkKey) {
	for _, id := range r.windowFeatureIDs[key] {
		delete(r.windowFeatureNames, id)
	}
	r.windowVisibleFeatureCount -= r.windowFeatureCounts[key]
	r.windowVisiblePayloadBytes -= r.windowPayloadBytes[key]
	delete(r.windowFeatureCounts, key)
	delete(r.windowPayloadBytes, key)
	delete(r.windowHits, key)
	delete(r.windowFeatureIDs, key)
	delete(r.windowLabels, key)
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
	// refresh replaces the feature slice instead of mutating it in place, so
	// the snapshot remains valid after releasing the runtime mutex. Avoid a
	// full feature-array copy on every click.
	features := r.features
	if !r.hitIndexReady {
		r.hitIndex = render.NewHitIndex(features, 0.01)
		r.hitIndexReady = true
	}
	hitIndex := r.hitIndex
	visibleLayers := r.visibleLayers
	serviceSnapshot := r.service
	readOnlySnapshot := r.readOnly
	r.mu.Unlock()
	result, ok := hitIndex.HitTestScreenVisible(render.Point{X: click.X, Y: click.Y}, worldViewport, width, height, 8, visibleLayers)
	if !ok {
		r.mu.Lock()
		if r.service != serviceSnapshot {
			r.mu.Unlock()
			return
		}
		r.hasSelect = false
		r.selectionGeneration++
		r.nextAttributeDispatchLocked()
		r.attributeReady = false
		if r.selectionCancel != nil {
			r.selectionCancel()
			r.selectionCancel = nil
		}
		r.mu.Unlock()
		native.SetSelection("", "", "", "No feature selected")
		native.SetAttributePayload("[]")
		native.SetVertexHandlePayload("[]")
		return
	}
	ensureFeature(serviceSnapshot, readOnlySnapshot, result, features)
	label := fmt.Sprintf("%s segment #%d", result.Layer, result.FeatureID)
	r.mu.Lock()
	if r.service != serviceSnapshot {
		r.mu.Unlock()
		return
	}
	r.selected = result
	r.hasSelect = true
	r.selectionGeneration++
	generation := r.selectionGeneration
	if r.selectionCancel != nil {
		r.selectionCancel()
		r.selectionCancel = nil
	}
	readOnly := r.readOnly
	reader := r.attributeFeatureReader
	service := r.service
	cachedName := ""
	cached := readOnly && r.featureNameCacheReady && r.featureNameCacheLayer == result.Layer && r.featureNameCacheID == result.FeatureID
	if cached {
		cachedName = r.featureNameCacheValue
	}
	var lookupContext context.Context
	if readOnly && reader != nil && !cached {
		var cancel context.CancelFunc
		lookupContext, cancel = context.WithCancel(context.Background())
		r.selectionCancel = cancel
	}
	if readOnly {
		native.SetSelection(result.Layer, label, cachedName, "Selected for inspection")
	}
	r.mu.Unlock()
	r.publishVertexHandles(result, features, readOnly)
	r.publishAttributesPageAsync(result.Layer, 0)
	if readOnly {
		if lookupContext != nil {
			go r.finishReadOnlySelectionName(lookupContext, generation, service, reader, result, label)
		}
		return
	}
	name := r.featureName(result.Layer, result.FeatureID)
	r.mu.Lock()
	if generation == r.selectionGeneration && r.service == serviceSnapshot && r.hasSelect {
		native.SetSelection(result.Layer, label, name, "Selected for inspection")
	}
	r.mu.Unlock()
}

func (r *demoRuntime) publishVertexHandles(selected render.HitResult, features []render.HitFeature, readOnly bool) {
	r.mu.Lock()
	service := r.service
	r.mu.Unlock()
	if readOnly || service == nil || !service.LayerEditable(selected.Layer) {
		native.SetVertexHandlePayload("[]")
		return
	}
	type handle struct {
		X           float64 `json:"x"`
		Y           float64 `json:"y"`
		VertexIndex int     `json:"vertexIndex"`
	}
	const maxVertexHandles = 10000
	handles := make([]handle, 0)
	for _, feature := range features {
		if feature.Layer != selected.Layer || feature.FeatureID != selected.FeatureID {
			continue
		}
		appendPart := func(points []render.Point) {
			for _, point := range points {
				if len(handles) >= maxVertexHandles {
					return
				}
				handles = append(handles, handle{X: point.X, Y: point.Y, VertexIndex: len(handles)})
			}
		}
		if len(feature.Parts) == 0 {
			appendPart(feature.Vertices)
		} else {
			for _, part := range feature.Parts {
				appendPart(part)
			}
		}
		break
	}
	payload, err := json.Marshal(handles)
	if err != nil {
		native.SetVertexHandlePayload("[]")
		return
	}
	native.SetVertexHandlePayload(string(payload))
}

func (r *demoRuntime) finishReadOnlySelectionName(ctx context.Context, generation uint64, service *commands.ProjectService, reader func(context.Context, string, uint64) (core.Feature, error), selected render.HitResult, label string) {
	feature, err := reader(ctx, selected.Layer, selected.FeatureID)
	if ctx.Err() != nil {
		return
	}
	name, _ := feature.Properties["name"].(string)
	r.mu.Lock()
	defer r.mu.Unlock()
	if generation != r.selectionGeneration || r.service != service || !r.hasSelect || r.selected.Layer != selected.Layer || r.selected.FeatureID != selected.FeatureID {
		return
	}
	r.selectionCancel = nil
	if err != nil {
		native.SetSelection(selected.Layer, label, "", "Selected; name lookup failed")
		return
	}
	r.featureNameCacheLayer = selected.Layer
	r.featureNameCacheID = selected.FeatureID
	r.featureNameCacheValue = name
	r.featureNameCacheReady = true
	native.SetSelection(selected.Layer, label, name, "Selected for inspection")
}

func ensureFeature(service *commands.ProjectService, readOnly bool, result render.HitResult, features []render.HitFeature) {
	if service == nil || readOnly {
		return
	}
	if service.HasFeature(result.Layer, result.FeatureID) {
		return
	}
	for _, feature := range features {
		if feature.Layer != result.Layer || feature.FeatureID != result.FeatureID || len(feature.Vertices) < 2 {
			continue
		}
		wkt := fmt.Sprintf("LINESTRING (%g %g, %g %g)", feature.Vertices[0].X, feature.Vertices[0].Y, feature.Vertices[1].X, feature.Vertices[1].Y)
		if err := service.BeginEdit(); err != nil {
			return
		}
		if err := service.AddFeature(result.Layer, core.Feature{ID: result.FeatureID, Geometry: core.WKTGeometry{WKT: wkt}, Properties: map[string]any{"name": fmt.Sprintf("%s feature #%d", result.Layer, result.FeatureID)}}); err != nil {
			_ = service.Rollback()
			return
		}
		_ = service.Commit()
		return
	}
}

func (r *demoRuntime) edit(event native.EditEvent) {
	r.mu.Lock()
	selected, ok := r.selected, r.hasSelect
	readOnly := r.readOnly
	r.mu.Unlock()
	if !ok {
		return
	}
	if readOnly {
		r.mu.Lock()
		name := ""
		if r.featureNameCacheReady && r.featureNameCacheLayer == selected.Layer && r.featureNameCacheID == selected.FeatureID {
			name = r.featureNameCacheValue
		}
		r.mu.Unlock()
		native.SetSelection(selected.Layer, fmt.Sprintf("%s segment #%d", selected.Layer, selected.FeatureID), name, "Read-only mode")
		return
	}
	if event.Action == "moveVertex" {
		if err := r.moveSelectedVertex(selected, event.Value); err != nil {
			native.SetRenderStatus("Vertex edit failed: " + err.Error())
			return
		}
		native.SetRenderStatus("Vertex moved")
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
	r.republishAttributesAsync(selected.Layer)
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

func (r *demoRuntime) moveSelectedVertex(selected render.HitResult, value string) error {
	var request struct {
		VertexIndex int     `json:"vertexIndex"`
		X           float64 `json:"x"`
		Y           float64 `json:"y"`
	}
	if err := json.Unmarshal([]byte(value), &request); err != nil {
		return fmt.Errorf("invalid vertex edit: %w", err)
	}
	r.mu.Lock()
	service := r.service
	readOnly := r.readOnly
	r.mu.Unlock()
	if service == nil || readOnly || !service.LayerEditable(selected.Layer) {
		return fmt.Errorf("layer %q is not editable", selected.Layer)
	}
	geometry, ok := service.FeatureGeometry(selected.Layer, selected.FeatureID)
	if !ok {
		return fmt.Errorf("selected feature geometry is unavailable")
	}
	wkb, ok := geometry.(core.WKBGeometry)
	if !ok {
		return fmt.Errorf("vertex editing requires WKB geometry, got %T", geometry)
	}
	moved, err := core.MoveWKBVertex(wkb.WKB, request.VertexIndex, request.X, request.Y)
	if err != nil {
		return err
	}
	if err := service.BeginEdit(); err != nil {
		return err
	}
	if err := service.SetFeatureGeometry(selected.Layer, selected.FeatureID, core.WKBGeometry{WKB: moved}); err != nil {
		_ = service.Rollback()
		return err
	}
	if err := service.Commit(); err != nil {
		return err
	}
	if err := r.rebuildEditedLayer(selected.Layer); err != nil {
		return fmt.Errorf("geometry changed in memory but render refresh failed: %w", err)
	}
	if r.persist != nil {
		if err := r.persist(context.Background(), selected.Layer); err != nil {
			return fmt.Errorf("geometry changed in memory but save failed: %w", err)
		}
	}
	return nil
}

func (r *demoRuntime) featureName(layerName string, featureID uint64) string {
	r.mu.Lock()
	service := r.service
	readOnly := r.readOnly
	reader := r.attributeFeatureReader
	cached := readOnly && r.featureNameCacheReady && r.featureNameCacheLayer == layerName && r.featureNameCacheID == featureID
	cachedName := r.featureNameCacheValue
	r.mu.Unlock()
	if service != nil {
		if value, ok := service.FeatureProperty(layerName, featureID, "name"); ok {
			if name, ok := value.(string); ok {
				return name
			}
		}
	}
	if cached {
		return cachedName
	}
	if reader != nil {
		if feature, err := reader(context.Background(), layerName, featureID); err == nil {
			name, _ := feature.Properties["name"].(string)
			if readOnly {
				r.mu.Lock()
				if r.service == service && r.readOnly {
					r.featureNameCacheLayer = layerName
					r.featureNameCacheID = featureID
					r.featureNameCacheValue = name
					r.featureNameCacheReady = true
				}
				r.mu.Unlock()
			}
			return name
		}
	}
	return ""
}

func (r *demoRuntime) cancelCurrentRender() {
	r.mu.Lock()
	renderCancel := r.cancel
	loadCancel := r.loadCancel
	loading := loadCancel != nil
	if loading {
		r.loadGeneration++
		r.loadCancel = nil
		r.pendingLoadBatches = nil
		r.previewLoading = false
	}
	r.mu.Unlock()
	if renderCancel != nil {
		renderCancel()
	}
	if loadCancel != nil {
		loadCancel()
	}
	if loading {
		native.SetRenderStatus("Loading cancelled")
	} else if renderCancel != nil {
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
	lastLayerSettingsGeneration := native.LayerSettingsGeneration()
	lastClickGeneration := native.ClickGeneration()
	lastEditGeneration := native.EditGeneration()
	lastCancelGeneration := native.CancelGeneration()
	lastActiveLayerGeneration := native.ActiveLayerGeneration()
	lastAttributePageGeneration := native.AttributePageGeneration()
	lastLoadGeneration := native.LoadGeneration()
	lastSaveGeneration := native.SaveGeneration()
	go func() {
		var viewportChangedAt time.Time
		pendingViewportRefresh := false
		for range time.NewTicker(16 * time.Millisecond).C {
			saveGeneration := native.SaveGeneration()
			if saveGeneration != lastSaveGeneration {
				lastSaveGeneration = saveGeneration
				options, err := native.CurrentSaveOptions()
				if err != nil {
					native.SetRenderStatus("Save failed: " + err.Error())
				} else {
					runtime.saveDatasetWithOptions(native.CurrentSavePath(), native.CurrentSaveProfile(), options)
				}
			}
			loadGeneration := native.LoadGeneration()
			if loadGeneration != lastLoadGeneration {
				requests, err := native.CurrentLoadRequests()
				if err != nil {
					native.SetRenderStatus("Open failed: invalid file selection")
				} else {
					lastLoadGeneration = loadGeneration
					for _, paths := range requests {
						if len(paths) == 0 {
							native.SetRenderStatus("Open cancelled")
						} else {
							runtime.startDataLoadPaths(paths)
						}
					}
				}
			}
			qtGeneration := native.ViewportGeneration()
			layerVisibilityGeneration := native.LayerVisibilityGeneration()
			layerSettingsGeneration := native.LayerSettingsGeneration()
			if layerSettingsGeneration != lastLayerSettingsGeneration {
				lastLayerSettingsGeneration = layerSettingsGeneration
				payload := native.CurrentLayerSettings()
				var action struct {
					Operation string `json:"operation"`
				}
				_ = json.Unmarshal([]byte(payload), &action)
				if action.Operation == "remove" {
					native.SetRenderStatus("Removing layer")
				}
				if err := applyLayerSettings(runtime, payload); err != nil {
					if action.Operation == "remove" {
						native.SetRenderStatus("Remove layer failed: " + err.Error())
					} else {
						native.SetRenderStatus("Layer settings failed: " + err.Error())
					}
					runtime.publishLayerTree()
				} else if action.Operation != "remove" {
					native.SetRenderStatus("Layer settings applied")
				}
			}
			viewport := native.CurrentViewport()
			if qtGeneration != lastQtGeneration {
				lastQtGeneration = qtGeneration
				viewportChangedAt = time.Now()
				pendingViewportRefresh = true
			}
			visibilityChanged := false
			if layerVisibilityGeneration != lastLayerVisibilityGeneration {
				lastLayerVisibilityGeneration = layerVisibilityGeneration
				visibilityChanged = applyLayerVisibility(runtime, native.CurrentLayerVisibility())
			}
			// Reuse the already rendered map while a wheel/drag gesture is
			// active. Rebuilding each 16 ms cancels expensive GDAL windows
			// before parcel tiles can ever finish.
			if visibilityChanged || (pendingViewportRefresh && time.Since(viewportChangedAt) >= 120*time.Millisecond) {
				pendingViewportRefresh = false
				runtime.advanceRenderGeneration()
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
					Zoom:        zoom,
					ScreenWidth: viewport.ViewportWidth, ScreenHeight: viewport.ViewportHeight,
					CanvasWidth: width, CanvasHeight: height,
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
				pendingViewportRefresh = false
				runtime.cancelCurrentRender()
			}
			activeLayerGeneration := native.ActiveLayerGeneration()
			if activeLayerGeneration != lastActiveLayerGeneration {
				lastActiveLayerGeneration = activeLayerGeneration
				if layer := native.CurrentActiveLayer(); layer != "" {
					runtime.publishAttributesPageAsync(layer, 0)
				}
			}
			attributePageGeneration := native.AttributePageGeneration()
			if attributePageGeneration != lastAttributePageGeneration {
				lastAttributePageGeneration = attributePageGeneration
				if layer := native.CurrentActiveLayer(); layer != "" {
					runtime.publishAttributesPageAsync(layer, native.CurrentAttributePage())
				}
			}
		}
	}()
}

type layerSettingsRequest struct {
	Operation       string             `json:"operation,omitempty"`
	ProjectCRS      string             `json:"projectCrs,omitempty"`
	Name            string             `json:"name"`
	DisplayName     string             `json:"displayName"`
	SourcePath      string             `json:"sourcePath"`
	SourceLayerName string             `json:"sourceLayerName"`
	SourceEncoding  string             `json:"sourceEncoding"`
	SourceCRS       string             `json:"sourceCrs"`
	Visible         bool               `json:"visible"`
	Style           core.LayerStyle    `json:"style"`
	Labels          core.LabelSettings `json:"labels"`
	DisplayRule     string             `json:"displayRule,omitempty"`
}

func applyLayerSettings(runtime *demoRuntime, payload string) error {
	var request layerSettingsRequest
	if err := json.Unmarshal([]byte(payload), &request); err != nil {
		return fmt.Errorf("invalid settings payload: %w", err)
	}
	if request.Operation == "remove" {
		if request.Name == "" {
			return fmt.Errorf("layer identity is missing")
		}
		return runtime.startRemoveLayer(request.Name)
	}
	if request.Operation == "project-crs" {
		return runtime.startProjectCRSChange(strings.TrimSpace(request.ProjectCRS))
	}
	if request.Name == "" {
		return fmt.Errorf("layer identity is missing")
	}
	if request.Operation != "" {
		return fmt.Errorf("unknown layer operation %q", request.Operation)
	}
	request.DisplayName = strings.TrimSpace(request.DisplayName)
	if request.DisplayName == "" {
		return fmt.Errorf("display name must not be empty")
	}
	if strings.TrimSpace(request.SourcePath) == "" || strings.TrimSpace(request.SourceLayerName) == "" {
		return fmt.Errorf("source path and source layer name are required")
	}
	if err := request.Style.Validate(); err != nil {
		return err
	}
	if err := request.Labels.Validate(); err != nil {
		return err
	}
	if err := scripting.ValidateLabelComposerScripts(request.Labels.LuaScript, request.Labels.Rule); err != nil {
		return fmt.Errorf("invalid Lua label settings: %w", err)
	}
	if strings.TrimSpace(request.DisplayRule) != "" {
		program, err := scripting.CompileLabelProgram(request.DisplayRule)
		if err != nil {
			return fmt.Errorf("invalid Lua feature display rule: %w", err)
		}
		program.Close()
	}
	runtime.mu.Lock()
	service := runtime.service
	readOnly := runtime.readOnly
	displayCRS := runtime.readOnlyDisplayCRS
	saveDestination := runtime.saveDestination
	runtime.mu.Unlock()
	if service == nil {
		return fmt.Errorf("no project is loaded")
	}
	if displayCRS == "" {
		_, projectCRS := service.ProjectInfo()
		displayCRS = projectCRS.AuthorityCode
	}
	current, ok := service.LayerProperties(request.Name)
	if !ok {
		return fmt.Errorf("layer %q not found", request.Name)
	}
	if request.SourcePath != current.SourcePath || request.SourceLayerName != current.SourceLayerName ||
		request.SourceEncoding != current.SourceEncoding || request.SourceCRS != current.SourceCRS {
		return runtime.reloadLayerWithSettings(request, readOnly, displayCRS, saveDestination)
	}
	if request.DisplayName != "" && request.DisplayName != current.DisplayName {
		if err := service.RenameLayer(request.Name, request.DisplayName); err != nil {
			return err
		}
	}
	updated := current
	updated.DisplayName = request.DisplayName
	updated.SourceEncoding = request.SourceEncoding
	updated.Visible = request.Visible
	updated.Style = request.Style
	updated.Labels = request.Labels
	updated.DisplayRule = request.DisplayRule
	if err := service.UpdateLayerSettings(request.Name, updated); err != nil {
		return err
	}
	runtime.mu.Lock()
	if runtime.visibleLayers == nil {
		runtime.visibleLayers = make(map[string]bool)
	}
	runtime.visibleLayers[request.Name] = request.Visible
	if runtime.visibility != nil {
		runtime.visibility.Set(request.Name, request.Visible)
	}
	styleChanged := current.Style != updated.Style
	labelsChanged := current.Labels != updated.Labels
	displayRuleChanged := current.DisplayRule != updated.DisplayRule
	scheduler := runtime.scheduler
	styleMu := runtime.layerStyleMu
	_, hasReadOnlyWindow := runtime.readOnlyBindings[request.Name]
	runtime.mu.Unlock()
	if styleMu == nil {
		styleMu = &sync.RWMutex{}
		runtime.mu.Lock()
		runtime.layerStyleMu = styleMu
		runtime.mu.Unlock()
	}
	styleMu.Lock()
	if runtime.layerStyles == nil {
		runtime.layerStyles = make(map[string]core.LayerStyle)
	}
	runtime.layerStyles[request.Name] = updated.Style
	styleMu.Unlock()
	presentationChanged := labelsChanged || displayRuleChanged
	if (styleChanged || presentationChanged) && hasReadOnlyWindow {
		runtime.mu.Lock()
		binding, exists := runtime.readOnlyBindings[request.Name]
		if exists {
			binding.layer.Style = updated.Style
			binding.layer.Labels = updated.Labels
			binding.layer.DisplayRule = updated.DisplayRule
			runtime.readOnlyBindings[request.Name] = binding
			for index := range runtime.readOnlySources {
				source := &runtime.readOnlySources[index]
				if source.Name == request.Name || (source.Name == "" && source.Path == current.SourcePath &&
					(source.LayerName == "" || source.LayerName == current.SourceLayerName)) {
					source.Style = updated.Style
					source.Labels = updated.Labels
					source.DisplayRule = updated.DisplayRule
				}
			}
			if presentationChanged {
				for key := range runtime.windowVisibleKeys {
					if key.Layer == request.Name {
						runtime.removeWindowChunkLocked(key)
					}
				}
				runtime.rebuildWindowFeaturesLocked()
			}
		}
		runtime.mu.Unlock()
	}
	if (styleChanged || presentationChanged) && scheduler != nil {
		scheduler.InvalidateLayer(request.Name)
	}
	if presentationChanged && !hasReadOnlyWindow {
		if err := runtime.rebuildEditedLayer(request.Name); err != nil {
			_ = service.UpdateLayerSettings(request.Name, current)
			styleMu.Lock()
			runtime.layerStyles[request.Name] = current.Style
			styleMu.Unlock()
			return fmt.Errorf("rebuild layer presentation: %w", err)
		}
	} else if styleChanged || presentationChanged {
		runtime.refreshCurrentViewport()
	}
	runtime.publishLayerTree()
	return nil
}

func (r *demoRuntime) advanceRenderGeneration() {
	r.mu.Lock()
	r.scheduler.AdvanceGeneration()
	r.mu.Unlock()
}

func (r *demoRuntime) refreshCurrentViewport() {
	viewport := native.CurrentViewport()
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
	r.advanceRenderGeneration()
	r.refresh(context.Background(), render.Viewport{
		Center:      render.Point{X: 0.5 - viewport.PanX/(width*zoom), Y: 0.5 + viewport.PanY/(height*zoom)},
		Zoom:        zoom,
		ScreenWidth: viewport.ViewportWidth, ScreenHeight: viewport.ViewportHeight,
		CanvasWidth: width, CanvasHeight: height,
	})
}

func applyLayerVisibility(runtime *demoRuntime, payload string) bool {
	var visibility map[string]bool
	if err := json.Unmarshal([]byte(payload), &visibility); err != nil {
		return false
	}
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	var visibleSnapshot map[string]bool
	for layer, visible := range visibility {
		if runtime.visibility.IsVisible(layer) != visible {
			if runtime.visibility.Set(layer, visible) {
				if runtime.service != nil {
					if settings, ok := runtime.service.LayerProperties(layer); ok {
						settings.Visible = visible
						_ = runtime.service.UpdateLayerSettings(layer, settings)
					}
				}
				if visibleSnapshot == nil {
					visibleSnapshot = make(map[string]bool, len(runtime.visibleLayers))
					for name, state := range runtime.visibleLayers {
						visibleSnapshot[name] = state
					}
				}
				visibleSnapshot[layer] = visible
			}
		}
	}
	if visibleSnapshot != nil {
		runtime.visibleLayers = visibleSnapshot
	}
	return visibleSnapshot != nil
}
