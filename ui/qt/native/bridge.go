//go:build qt

package native

/*
#cgo CXXFLAGS: -std=c++17
#cgo pkg-config: Qt6Core Qt6Gui Qt6Qml Qt6Quick
#include <stdlib.h>
#include "mapcanvas.h"
*/
import "C"

import (
	"encoding/json"
	"fmt"
	"os"
	"unsafe"

	"gogis/internal/render"
)

func nativeVertexBatchAllowed(count int) bool {
	return count >= 0 && count <= render.MaxBatchVertices
}

// RegisterMapCanvas exposes the custom QSG-backed map item to QML.
func RegisterMapCanvas() {
	C.gogis_register_qml_types()
}

// BeginLoadTrace starts optional GOGIS_PERF=1 diagnostics. The C++ bridge
// timestamps vertex publication and scene-graph geometry updates from here.
func BeginLoadTrace() {
	C.gogis_trace_load_start()
}

// SetVertices copies normalized XY positions into the C++ scene-graph bridge.
// Vertex contains only numeric fields, so C++ can copy its stable layout
// synchronously. The C++ side owns the copy after the call returns and does not
// retain the Go pointer.
func SetVertices(vertices []render.Vertex) {
	SetVerticesStage(vertices, 0)
}

// SetVerticesStage labels a batch for optional load-to-scene-graph timing:
// 0=demo, 1=preview, 2=full dataset.
func SetVerticesStage(vertices []render.Vertex, stage int) {
	if len(vertices) == 0 {
		C.gogis_set_vertices(nil, 0)
		return
	}
	if !nativeVertexBatchAllowed(len(vertices)) {
		SetRenderStatus("Render error: source vertex safety limit exceeded")
		C.gogis_set_vertices_vertex_layout_stage(nil, 0, C.int(stage))
		return
	}
	if uint64(len(vertices)) > uint64(^uint32(0)>>1) {
		SetRenderStatus("Render skipped: vertex batch exceeds the native API limit")
		return
	}
	C.gogis_set_vertices_vertex_layout_stage(unsafe.Pointer(&vertices[0]), C.int(len(vertices)), C.int(stage))
}

func retainedNativeVertexBytes() uint64 {
	return uint64(C.gogis_retained_vertex_bytes())
}

// ViewportGeneration returns the generation raised by QML pan/zoom changes.
func ViewportGeneration() uint64 {
	return uint64(C.gogis_viewport_generation())
}

// RequestCanvasUpdate queues a repaint on Qt's GUI thread.
func RequestCanvasUpdate() {
	C.gogis_request_canvas_update()
}

// Viewport is the latest GUI-thread snapshot of the map item transform.
type Viewport struct {
	PanX           float64
	PanY           float64
	Zoom           float64
	Width          float64
	Height         float64
	ViewportWidth  float64
	ViewportHeight float64
}

// CurrentViewport returns a thread-safe snapshot suitable for render planning.
func CurrentViewport() Viewport {
	var panX, panY, zoom, width, height, viewportWidth, viewportHeight C.double
	C.gogis_canvas_viewport(&panX, &panY, &zoom, &width, &height, &viewportWidth, &viewportHeight)
	return Viewport{
		PanX:           float64(panX),
		PanY:           float64(panY),
		Zoom:           float64(zoom),
		Width:          float64(width),
		Height:         float64(height),
		ViewportWidth:  float64(viewportWidth),
		ViewportHeight: float64(viewportHeight),
	}
}

// CanvasVisible reports the QML visibility of the registered map canvas.
func CanvasVisible() bool {
	return C.gogis_canvas_visible() != 0
}

// ClickGeneration returns the latest click event observed by the Qt bridge.
func ClickGeneration() uint64 {
	return uint64(C.gogis_click_generation())
}

// CanvasClick is a click position in the MapCanvas parent (viewport) space.
type CanvasClick struct {
	X float64
	Y float64
}

// CurrentClick returns a thread-safe snapshot of the latest click position.
func CurrentClick() CanvasClick {
	var x, y C.double
	C.gogis_canvas_click(&x, &y)
	return CanvasClick{X: float64(x), Y: float64(y)}
}

// SetSelection publishes selection identity, editable value, and status to QML.
func SetSelection(layer, feature, value, status string) {
	cLayer := C.CString(layer)
	cFeature := C.CString(feature)
	cValue := C.CString(value)
	cStatus := C.CString(status)
	defer C.free(unsafe.Pointer(cLayer))
	defer C.free(unsafe.Pointer(cFeature))
	defer C.free(unsafe.Pointer(cValue))
	defer C.free(unsafe.Pointer(cStatus))
	C.gogis_set_selection(cLayer, cFeature, cValue, cStatus)
}

// LayerVisibilityGeneration returns the latest QML layer-state generation.
func LayerVisibilityGeneration() uint64 {
	return uint64(C.gogis_layer_visibility_generation())
}

// CurrentLayerVisibility returns the JSON payload published by QML.
func CurrentLayerVisibility() string {
	buffer := make([]C.char, 4096)
	C.gogis_layer_visibility((*C.char)(unsafe.Pointer(&buffer[0])), C.int(len(buffer)))
	return C.GoString((*C.char)(unsafe.Pointer(&buffer[0])))
}

// LayerSettingsGeneration returns the latest QML layer-property apply event.
func LayerSettingsGeneration() uint64 {
	return uint64(C.gogis_layer_settings_generation())
}

// CurrentLayerSettings returns the JSON settings submitted by the properties
// panel. The payload buffer allows multiline Lua snippets.
func CurrentLayerSettings() string {
	buffer := make([]C.char, 1<<20)
	C.gogis_layer_settings((*C.char)(unsafe.Pointer(&buffer[0])), C.int(len(buffer)))
	return C.GoString((*C.char)(unsafe.Pointer(&buffer[0])))
}

// EditGeneration returns the latest QML edit event generation.
func EditGeneration() uint64 {
	return uint64(C.gogis_edit_generation())
}

// EditEvent is a property editing action emitted by QML.
type EditEvent struct {
	Action string
	Value  string
}

// CurrentEdit returns a thread-safe snapshot of the latest edit event.
func CurrentEdit() EditEvent {
	action := make([]C.char, 128)
	value := make([]C.char, 4096)
	C.gogis_edit_event(
		(*C.char)(unsafe.Pointer(&action[0])), C.int(len(action)),
		(*C.char)(unsafe.Pointer(&value[0])), C.int(len(value)),
	)
	return EditEvent{
		Action: C.GoString((*C.char)(unsafe.Pointer(&action[0]))),
		Value:  C.GoString((*C.char)(unsafe.Pointer(&value[0]))),
	}
}

// SetAttributePayload publishes a JSON attribute table payload to QML.
func SetAttributePayload(payload string) {
	cPayload := C.CString(payload)
	defer C.free(unsafe.Pointer(cPayload))
	C.gogis_set_attribute_payload(cPayload)
}

// AttributePageGeneration returns the latest QML attribute page request.
func AttributePageGeneration() uint64 {
	return uint64(C.gogis_attribute_page_generation())
}

// CurrentAttributePage returns the zero-based page requested by QML.
func CurrentAttributePage() int {
	return int(C.gogis_attribute_page())
}

// SetLayerTreePayload publishes the current Go-side layer tree to QML.
func SetLayerTreePayload(payload string) {
	cPayload := C.CString(payload)
	defer C.free(unsafe.Pointer(cPayload))
	C.gogis_set_layer_tree_payload(cPayload)
}

// SetLayerLabelPayload publishes normalized vector labels for the viewport.
func SetLayerLabelPayload(payload string) {
	cPayload := C.CString(payload)
	defer C.free(unsafe.Pointer(cPayload))
	C.gogis_set_layer_label_payload(cPayload)
}

// SetVertexHandlePayload publishes normalized editable vertices for the
// currently selected feature.
func SetVertexHandlePayload(payload string) {
	cPayload := C.CString(payload)
	defer C.free(unsafe.Pointer(cPayload))
	C.gogis_set_vertex_handle_payload(cPayload)
}

// ActiveLayerGeneration returns the latest QML layer selection generation.
func ActiveLayerGeneration() uint64 {
	return uint64(C.gogis_active_layer_generation())
}

// CurrentActiveLayer returns the latest selected layer name.
func CurrentActiveLayer() string {
	buffer := make([]C.char, 4096)
	C.gogis_active_layer((*C.char)(unsafe.Pointer(&buffer[0])), C.int(len(buffer)))
	return C.GoString((*C.char)(unsafe.Pointer(&buffer[0])))
}

// SetRenderStatus publishes a short render progress message to QML.
func SetRenderStatus(status string) {
	if isDiagnosticStatus(status) {
		RecordDiagnostic("application", status)
		// Keep errors visible when the desktop app is launched from a terminal.
		// stdout is captured by the same bounded in-memory diagnostics pipeline.
		_, _ = fmt.Fprintln(os.Stdout, "GoGIS:", status)
	}
	cStatus := C.CString(status)
	defer C.free(unsafe.Pointer(cStatus))
	C.gogis_set_render_status(cStatus)
}

// SetDiagnosticLogPayload publishes the current bounded process log to QML.
func SetDiagnosticLogPayload(payload string) {
	cPayload := C.CString(payload)
	defer C.free(unsafe.Pointer(cPayload))
	C.gogis_set_diagnostic_log_payload(cPayload)
}

// SetMemoryStatus publishes process memory and Go heap diagnostics separately.
func SetMemoryStatus(processBytes, goHeapBytes uint64, processMemoryKind string, available bool) {
	kind, valid := C.int(0), C.int(0)
	switch processMemoryKind {
	case "Working set":
		kind = 1
	case "Peak RSS":
		kind = 2
	}
	if available {
		valid = 1
	}
	C.gogis_set_memory_status(C.ulonglong(processBytes), C.ulonglong(goHeapBytes), kind, valid)
}

// SetMapMetadata publishes the current full data extent in display CRS order.
func SetMapMetadata(crs string, extent [4]float64) {
	SetMapMetadataWithView(crs, extent, nil)
}

// MapViewState is normalized to the dataset extent, so it survives window resizing.
type MapViewState struct {
	CenterX     float64 `json:"centerX"`
	CenterY     float64 `json:"centerY"`
	Zoom        float64 `json:"zoom"`
	ActiveLayer string  `json:"activeLayer,omitempty"`
}

// SetMapMetadataWithView publishes the map extent and optional saved workspace view.
func SetMapMetadataWithView(crs string, extent [4]float64, view *MapViewState) {
	SetMapMetadataWithFitBounds(crs, extent, extent, view)
}

// SetMapMetadataWithFitBounds separates the complete data canvas bounds from
// the preferred extent used by initial/full-extent view fitting.
func SetMapMetadataWithFitBounds(crs string, extent, fitBounds [4]float64, view *MapViewState) {
	SetMapMetadataWithLayerPresence(crs, extent, fitBounds, view, true)
}

// SetMapMetadataWithLayerPresence also distinguishes an empty project from a
// layer whose CRS is unknown or whose bounds happen to be the unit square.
func SetMapMetadataWithLayerPresence(crs string, extent, fitBounds [4]float64, view *MapViewState, hasLayers bool) {
	payload, err := json.Marshal(struct {
		CRS       string        `json:"crs"`
		Bounds    [4]float64    `json:"bounds"`
		FitBounds [4]float64    `json:"fitBounds"`
		View      *MapViewState `json:"view,omitempty"`
		HasLayers bool          `json:"hasLayers"`
	}{CRS: crs, Bounds: extent, FitBounds: fitBounds, View: view, HasLayers: hasLayers})
	if err != nil {
		return
	}
	cPayload := C.CString(string(payload))
	defer C.free(unsafe.Pointer(cPayload))
	C.gogis_set_map_metadata(cPayload)
}

// MapMetadataGeneration is the latest metadata published to the GUI.
func MapMetadataGeneration() uint64 {
	return uint64(C.gogis_map_metadata_generation())
}

// AppliedMapMetadataGeneration is the latest generation QML has processed and
// the canvas has acknowledged. A lower value means the viewport snapshot may
// still use an older extent.
func AppliedMapMetadataGeneration() uint64 {
	return uint64(C.gogis_map_metadata_applied_generation())
}

// CancelGeneration returns the latest user cancellation request.
func CancelGeneration() uint64 {
	return uint64(C.gogis_cancel_generation())
}

// LoadGeneration returns the latest QML vector-file selection generation.
func LoadGeneration() uint64 {
	return uint64(C.gogis_load_generation())
}

// CurrentLoadRequests consumes every selection captured since the previous
// read, including multiple selections made within a single GUI frame.
func CurrentLoadRequests() ([][]string, error) {
	for {
		size := int(C.gogis_load_requests(nil, 0))
		if size > 16<<20 {
			return nil, fmt.Errorf("queued file selections exceed the 16 MiB bridge limit")
		}
		buffer := make([]C.char, size+1)
		if needed := int(C.gogis_load_requests((*C.char)(unsafe.Pointer(&buffer[0])), C.int(len(buffer)))); needed >= len(buffer) {
			continue // Another GUI frame appended a selection between the two calls.
		}
		var requests []struct {
			Paths []string `json:"paths"`
		}
		if err := json.Unmarshal([]byte(C.GoString((*C.char)(unsafe.Pointer(&buffer[0])))), &requests); err != nil {
			return nil, err
		}
		result := make([][]string, len(requests))
		for index, request := range requests {
			result[index] = request.Paths
		}
		return result, nil
	}
}

// SaveGeneration returns the latest QML GeoPackage save request generation.
func SaveGeneration() uint64 {
	return uint64(C.gogis_save_generation())
}

// CurrentSavePath returns the local destination path requested by QML.
func CurrentSavePath() string {
	buffer := make([]C.char, 4096)
	C.gogis_save_path((*C.char)(unsafe.Pointer(&buffer[0])), C.int(len(buffer)))
	return C.GoString((*C.char)(unsafe.Pointer(&buffer[0])))
}

// CurrentSaveProfile returns the export profile selected by the QML save dialog.
func CurrentSaveProfile() string {
	buffer := make([]C.char, 128)
	C.gogis_save_profile((*C.char)(unsafe.Pointer(&buffer[0])), C.int(len(buffer)))
	return C.GoString((*C.char)(unsafe.Pointer(&buffer[0])))
}
