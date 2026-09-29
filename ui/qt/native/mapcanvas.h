#pragma once

#ifdef __cplusplus
extern "C" {
#endif

// Registers the custom scene-graph item as GoGIS.MapCanvas 1.0.
void gogis_register_qml_types(void);

// Copies normalized XY vertices into the scene-graph bridge. The caller owns
// the input memory after this function returns.
void gogis_set_vertices(const float* xy, int vertex_count);

// Returns the monotonically increasing generation produced by MapCanvas
// transform changes (pan/zoom).
unsigned long long gogis_viewport_generation(void);

// Schedules a scene-graph update on Qt's GUI thread.
void gogis_request_canvas_update(void);

// Reads the latest GUI-thread viewport snapshot without touching Qt objects
// from the caller's goroutine.
void gogis_canvas_viewport(double* pan_x, double* pan_y, double* zoom,
                           double* width, double* height);

// Returns whether the registered MapCanvas is currently visible in QML.
int gogis_canvas_visible(void);

// Returns the latest QML click generation observed on the GUI thread.
unsigned long long gogis_click_generation(void);

// Reads the latest click position in MapCanvas parent coordinates.
void gogis_canvas_click(double* x, double* y);

// Publishes selection state for the QML properties on MapCanvas.
void gogis_set_selection(const char* layer, const char* feature,
                         const char* status);

// Reads the latest JSON layer visibility payload observed on the GUI thread.
unsigned long long gogis_layer_visibility_generation(void);
void gogis_layer_visibility(char* buffer, int buffer_length);

// Reads the latest QML edit action and value observed on the GUI thread.
unsigned long long gogis_edit_generation(void);
void gogis_edit_event(char* action, int action_length, char* value, int value_length);

// Publishes a JSON attribute-row payload for the QML table.
void gogis_set_attribute_payload(const char* payload);

// Publishes the current Go-side render request status to QML.
void gogis_set_render_status(const char* status);

// Returns the latest user-requested render cancellation generation.
unsigned long long gogis_cancel_generation(void);

#ifdef __cplusplus
}
#endif
