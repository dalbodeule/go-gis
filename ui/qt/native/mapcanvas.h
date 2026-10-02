#pragma once

#ifdef __cplusplus
extern "C" {
#endif

// Registers the custom scene-graph item as GoGIS.MapCanvas 1.0.
void gogis_register_qml_types(void);

// Copies normalized XY vertices into the scene-graph bridge. The caller owns
// the input memory after this function returns.
void gogis_set_vertices(const float* xy, int vertex_count);

// Copies the XY fields from Go's numeric render.Vertex layout synchronously.
// The bridge does not retain the caller's memory after this function returns.
void gogis_set_vertices_vertex_layout(const void* vertices, int vertex_count);

// The staged variant labels demo (0), preview (1), and full data (2) batches
// in optional performance diagnostics.
void gogis_set_vertices_vertex_layout_stage(const void* vertices, int vertex_count,
                                            int stage);
unsigned long long gogis_retained_vertex_bytes(void);

// Starts optional GOGIS_PERF=1 load-to-scene-graph timing.
void gogis_trace_load_start(void);

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
                         const char* value, const char* status);

// Reads the latest JSON layer visibility payload observed on the GUI thread.
unsigned long long gogis_layer_visibility_generation(void);
void gogis_layer_visibility(char* buffer, int buffer_length);
unsigned long long gogis_layer_settings_generation(void);
void gogis_layer_settings(char* buffer, int buffer_length);

// Reads the latest QML edit action and value observed on the GUI thread.
unsigned long long gogis_edit_generation(void);
void gogis_edit_event(char* action, int action_length, char* value, int value_length);

// Publishes a JSON attribute-row payload for the QML table.
void gogis_set_attribute_payload(const char* payload);

// Reads the latest QML attribute-table page request.
unsigned long long gogis_attribute_page_generation(void);
int gogis_attribute_page(void);

// Publishes a JSON layer-tree payload for the QML layer model.
void gogis_set_layer_tree_payload(const char* payload);
void gogis_set_layer_label_payload(const char* payload);
void gogis_set_vertex_handle_payload(const char* payload);

// Reads the latest QML-selected layer name.
unsigned long long gogis_active_layer_generation(void);
void gogis_active_layer(char* buffer, int buffer_length);

// Publishes the current Go-side render request status to QML.
void gogis_set_render_status(const char* status);
void gogis_set_diagnostic_log_payload(const char* payload);
void gogis_set_memory_status(unsigned long long process_bytes,
                             unsigned long long go_heap_bytes,
                             int process_memory_kind,
                             int available);

// Publishes the current map coordinate bounds and display CRS to QML.
void gogis_set_map_metadata(const char* payload);

// Returns the latest user-requested render cancellation generation.
unsigned long long gogis_cancel_generation(void);

// Reads the JSON array of local file paths emitted by the QML file picker.
unsigned long long gogis_load_generation(void);
void gogis_load_path(char* buffer, int buffer_length);

// Reads a GeoPackage save request emitted by QML.
unsigned long long gogis_save_generation(void);
void gogis_save_path(char* buffer, int buffer_length);

#ifdef __cplusplus
}
#endif
