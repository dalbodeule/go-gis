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
	"unsafe"

	"gogis/internal/render"
)

// RegisterMapCanvas exposes the custom QSG-backed map item to QML.
func RegisterMapCanvas() {
	C.gogis_register_qml_types()
}

// SetVertices copies normalized XY positions into the C++ scene-graph bridge.
// Vertex contains only numeric fields, so C++ can read its stable 12-byte
// layout synchronously and copy only XY. The C++ side owns the copy after the
// call returns and does not retain the Go pointer.
func SetVertices(vertices []render.Vertex) {
	if len(vertices) == 0 {
		C.gogis_set_vertices(nil, 0)
		return
	}
	C.gogis_set_vertices_vertex_layout(unsafe.Pointer(&vertices[0]), C.int(len(vertices)))
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

// CancelGeneration returns the latest user cancellation request.
func CancelGeneration() uint64 {
	return uint64(C.gogis_cancel_generation())
}

// LoadGeneration returns the latest QML file-open request generation.
func LoadGeneration() uint64 {
	return uint64(C.gogis_load_generation())
}

// CurrentLoadPath returns the latest file path requested by QML.
func CurrentLoadPath() string {
	buffer := make([]C.char, 16384)
	C.gogis_load_path((*C.char)(unsafe.Pointer(&buffer[0])), C.int(len(buffer)))
	return C.GoString((*C.char)(unsafe.Pointer(&buffer[0])))
}
