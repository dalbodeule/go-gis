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
	"unsafe"

	"gogis/internal/render"
)

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
	if uint64(len(vertices)) > uint64(^uint32(0)>>1) {
		SetRenderStatus("Render skipped: vertex batch exceeds the native API limit")
		return
	}
	C.gogis_set_vertices_vertex_layout_stage(unsafe.Pointer(&vertices[0]), C.int(len(vertices)), C.int(stage))
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
	PanX   float64
	PanY   float64
	Zoom   float64
	Width  float64
	Height float64
}

// CurrentViewport returns a thread-safe snapshot suitable for render planning.
func CurrentViewport() Viewport {
	var panX, panY, zoom, width, height C.double
	C.gogis_canvas_viewport(&panX, &panY, &zoom, &width, &height)
	return Viewport{
		PanX:   float64(panX),
		PanY:   float64(panY),
		Zoom:   float64(zoom),
		Width:  float64(width),
		Height: float64(height),
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
	cStatus := C.CString(status)
	defer C.free(unsafe.Pointer(cStatus))
	C.gogis_set_render_status(cStatus)
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
	payload, err := json.Marshal(struct {
		CRS    string        `json:"crs"`
		Bounds [4]float64    `json:"bounds"`
		View   *MapViewState `json:"view,omitempty"`
	}{CRS: crs, Bounds: extent, View: view})
	if err != nil {
		return
	}
	cPayload := C.CString(string(payload))
	defer C.free(unsafe.Pointer(cPayload))
	C.gogis_set_map_metadata(cPayload)
}

// CancelGeneration returns the latest user cancellation request.
func CancelGeneration() uint64 {
	return uint64(C.gogis_cancel_generation())
}

// LoadGeneration returns the latest QML vector-file selection generation.
func LoadGeneration() uint64 {
	return uint64(C.gogis_load_generation())
}

// CurrentLoadPaths decodes the local paths selected in QML.
func CurrentLoadPaths() ([]string, error) {
	buffer := make([]C.char, 262144)
	C.gogis_load_path((*C.char)(unsafe.Pointer(&buffer[0])), C.int(len(buffer)))
	var paths []string
	err := json.Unmarshal([]byte(C.GoString((*C.char)(unsafe.Pointer(&buffer[0])))), &paths)
	return paths, err
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
