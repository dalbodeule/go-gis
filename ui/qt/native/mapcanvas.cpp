#include "mapcanvas.h"

#include <QColor>
#include <QJsonArray>
#include <QJsonDocument>
#include <QJsonObject>
#include <QQuickItem>
#include <QSGGeometryNode>
#include <QSGVertexColorMaterial>
#include <QMetaObject>
#include <QTimer>
#include <QVariant>
#include <QtQml/qqml.h>
#include <algorithm>
#include <atomic>
#include <chrono>
#include <cmath>
#include <cstddef>
#include <cstdio>
#include <cstdlib>
#include <cstdint>
#include <cstring>
#include <limits>
#include <memory>
#include <mutex>
#include <new>
#include <string>
#include <vector>
#ifdef _WIN32
#ifndef NOMINMAX
#define NOMINMAX
#endif
#include <windows.h>
#else
#include <unistd.h>
#endif

namespace {

std::mutex g_vertices_mutex;
struct GoGISVertex {
    float x;
    float y;
    std::uint32_t color;
    float size_mm;
    std::uint32_t kind;
};
static_assert(sizeof(GoGISVertex) == 20, "unexpected Go vertex layout");
static_assert(offsetof(GoGISVertex, color) == 8 && offsetof(GoGISVertex, size_mm) == 12 &&
                  offsetof(GoGISVertex, kind) == 16,
              "Go render.Vertex field offsets changed");
std::vector<GoGISVertex> g_vertices;
constexpr size_t kMaxSourceVertexCount = 12 * 1024 * 1024;
constexpr size_t kMaxSceneGraphVertices = 36 * 1024 * 1024;
// Keep every unindexed draw comfortably below the 16-bit vertex range used
// by some scene-graph batching paths. Each batch ends on a triangle boundary.
constexpr size_t kSceneGraphBatchVertices = 60'000;
constexpr size_t kRetainedVertexCapacityFloor = 64 * 1024;
int g_vertices_stage = 0;
std::atomic<unsigned long long> g_vertices_generation{0};
std::atomic<long long> g_perf_load_started_ns{0};
std::atomic<unsigned long long> g_viewport_generation{0};
std::atomic<double> g_pan_x{0};
std::atomic<double> g_pan_y{0};
std::atomic<double> g_zoom{1};
std::atomic<double> g_width{1};
std::atomic<double> g_height{1};
std::atomic<double> g_viewport_width{1};
std::atomic<double> g_viewport_height{1};
std::atomic<double> g_logical_pixels_per_mm{96.0 / 25.4};
std::atomic<bool> g_visible{true};
std::atomic<unsigned long long> g_click_generation{0};
std::atomic<double> g_click_x{0};
std::atomic<double> g_click_y{0};
std::mutex g_layer_visibility_mutex;
std::string g_layer_visibility_payload;
std::atomic<unsigned long long> g_layer_visibility_generation{0};
std::mutex g_layer_settings_mutex;
std::string g_layer_settings_payload;
std::atomic<unsigned long long> g_layer_settings_generation{0};
std::mutex g_edit_mutex;
std::string g_edit_action;
std::string g_edit_value;
std::atomic<unsigned long long> g_edit_generation{0};
std::mutex g_attribute_mutex;
std::string g_attribute_payload;
std::atomic<unsigned long long> g_attribute_generation{0};
std::atomic<unsigned long long> g_attribute_page_generation{0};
std::atomic<int> g_attribute_page{0};
std::mutex g_layer_tree_mutex;
std::string g_layer_tree_payload;
std::atomic<unsigned long long> g_layer_tree_generation{0};
std::mutex g_layer_label_mutex;
std::string g_layer_label_payload = "[]";
std::atomic<unsigned long long> g_layer_label_generation{0};
std::mutex g_vertex_handle_mutex;
std::string g_vertex_handle_payload = "[]";
std::atomic<unsigned long long> g_vertex_handle_generation{0};
std::mutex g_active_layer_mutex;
std::string g_active_layer;
std::atomic<unsigned long long> g_active_layer_generation{0};
std::mutex g_render_status_mutex;
std::string g_render_status;
std::atomic<unsigned long long> g_render_status_generation{0};
std::mutex g_diagnostic_log_mutex;
std::string g_diagnostic_log_payload = "[]";
std::atomic<unsigned long long> g_diagnostic_log_generation{0};
std::atomic<unsigned long long> g_process_memory_bytes{0};
std::atomic<unsigned long long> g_go_heap_bytes{0};
std::atomic<int> g_process_memory_kind{0};
std::atomic<int> g_memory_status_available{0};
std::atomic<unsigned long long> g_memory_status_generation{0};
std::mutex g_map_metadata_mutex;
std::string g_map_metadata_payload;
std::atomic<unsigned long long> g_map_metadata_generation{0};
std::atomic<unsigned long long> g_map_metadata_applied_generation{0};
std::atomic<unsigned long long> g_cancel_generation{0};
std::mutex g_load_mutex;
QJsonArray g_load_requests;
std::atomic<unsigned long long> g_load_generation{0};
std::mutex g_save_mutex;
std::string g_save_path;
std::string g_save_profile;
std::atomic<unsigned long long> g_save_generation{0};
std::mutex g_selection_mutex;
std::string g_selection_layer;
std::string g_selection_feature;
std::string g_selection_value;
std::string g_selection_status;
std::atomic<unsigned long long> g_selection_generation{0};
std::mutex g_canvas_mutex;
QQuickItem* g_canvas = nullptr;

void set_render_status_safely(const char* status) noexcept {
    try {
        {
            std::lock_guard<std::mutex> lock(g_render_status_mutex);
            g_render_status = status != nullptr ? status : "";
        }
        g_render_status_generation.fetch_add(1, std::memory_order_relaxed);
    } catch (...) {
        // Reporting a rendering allocation failure must not terminate the UI.
    }
    if (status != nullptr) {
#ifdef _WIN32
        const HANDLE output_handle = GetStdHandle(STD_OUTPUT_HANDLE);
        if (output_handle != nullptr && output_handle != INVALID_HANDLE_VALUE) {
            DWORD written = 0;
            WriteFile(output_handle, status, static_cast<DWORD>(std::strlen(status)), &written, nullptr);
            static constexpr char newline = '\n';
            WriteFile(output_handle, &newline, 1, &written, nullptr);
        }
        const HANDLE error_handle = GetStdHandle(STD_ERROR_HANDLE);
        if (error_handle != nullptr && error_handle != INVALID_HANDLE_VALUE) {
            DWORD written = 0;
            WriteFile(error_handle, status, static_cast<DWORD>(std::strlen(status)), &written, nullptr);
            static constexpr char newline = '\n';
            WriteFile(error_handle, &newline, 1, &written, nullptr);
        }
#else
        (void)::write(STDOUT_FILENO, status, std::strlen(status));
        static constexpr char newline = '\n';
        (void)::write(STDOUT_FILENO, &newline, 1);
        (void)::write(STDERR_FILENO, status, std::strlen(status));
        (void)::write(STDERR_FILENO, &newline, 1);
#endif
    }
}

bool resize_vertex_buffer(size_t count) noexcept {
    if (count == 0) {
        std::vector<GoGISVertex>().swap(g_vertices);
        return true;
    }

    // Keep small fluctuations cheap, but do not retain a previous viewport's
    // peak-sized native allocation after the new batch is at least 4x smaller.
    if (g_vertices.capacity() > kRetainedVertexCapacityFloor &&
        count <= g_vertices.capacity() / 4) {
        try {
            std::vector<GoGISVertex> reduced;
            reduced.resize(count);
            g_vertices.swap(reduced);
            return true;
        } catch (...) {
            // Retain the old allocation and fall back to resize; the payload
            // remains bounded by kMaxSourceVertexCount either way.
        }
    }
    try {
        g_vertices.resize(count);
        return true;
    } catch (const std::bad_alloc&) {
        set_render_status_safely("Render error: insufficient memory for vertex batch");
    } catch (...) {
        set_render_status_safely("Render error: native vertex allocation failed");
    }
    return false;
}

long long steady_nanoseconds() {
    return std::chrono::duration_cast<std::chrono::nanoseconds>(
               std::chrono::steady_clock::now().time_since_epoch())
        .count();
}

bool perf_trace_enabled() {
    static const bool enabled = []() {
        const char* value = std::getenv("GOGIS_PERF");
        return value != nullptr && std::strcmp(value, "1") == 0;
    }();
    return enabled;
}

const char* stage_name(int stage) {
    switch (stage) {
    case 1:
        return "preview";
    case 2:
        return "full";
    default:
        return "demo";
    }
}

void trace_load_event(const char* event, int stage, size_t vertex_count) {
    if (!perf_trace_enabled()) {
        return;
    }
    const auto start = g_perf_load_started_ns.load(std::memory_order_relaxed);
    if (start == 0) {
        return;
    }
    const double elapsed_ms = static_cast<double>(steady_nanoseconds() - start) / 1'000'000.0;
    std::fprintf(stderr, "GoGIS perf: %s stage=%s %.1fms vertices=%zu\n",
                 event, stage_name(stage), elapsed_ms, vertex_count);
}

void update_viewport_snapshot(const QQuickItem* item) {
    const QVariant pan_x = item->property("viewportPanX");
    const QVariant pan_y = item->property("viewportPanY");
    g_pan_x.store(pan_x.isValid() ? pan_x.toDouble() : item->x(), std::memory_order_relaxed);
    g_pan_y.store(pan_y.isValid() ? pan_y.toDouble() : item->y(), std::memory_order_relaxed);
    g_zoom.store(item->scale(), std::memory_order_relaxed);
    g_width.store(item->width(), std::memory_order_relaxed);
    g_height.store(item->height(), std::memory_order_relaxed);
    const auto* viewport = item->parentItem();
    g_viewport_width.store(viewport != nullptr ? viewport->width() : item->width(), std::memory_order_relaxed);
    g_viewport_height.store(viewport != nullptr ? viewport->height() : item->height(), std::memory_order_relaxed);
    g_visible.store(item->isVisible(), std::memory_order_relaxed);
}

} // namespace

class GoGISMapCanvas : public QQuickItem {
public:
    explicit GoGISMapCanvas(QQuickItem* parent = nullptr)
        : QQuickItem(parent),
          geometry_(QSGGeometry::defaultAttributes_ColoredPoint2D(), 0),
          material_() {
        // Keep the bounded child meshes as separate GPU draws. Qt's default
        // renderer can otherwise merge compatible QSGGeometryNodes again.
        material_.setFlag(QSGMaterial::NoBatching);
        setFlag(ItemHasContents, true);
        update_viewport_snapshot(this);

        // QML adds clickX/clickY/clickGeneration to the registered item. Read
        // those dynamic properties on the GUI thread so Go never touches a
        // QObject from its render polling goroutine.
        click_timer_.setInterval(16);
        QObject::connect(&click_timer_, &QTimer::timeout, this, [this]() {
            const auto view_generation = property("viewGeneration").toULongLong();
            if (view_generation != view_generation_) {
                update_viewport_snapshot(this);
                view_generation_ = view_generation;
                g_viewport_generation.fetch_add(1, std::memory_order_relaxed);
            }
            // A viewport can grow along its non-limiting axis without changing
            // the extent-sized canvas geometry. Capture that resize too, or
            // render planning continues to use the old screen dimensions.
            const auto* viewport = parentItem();
            const double viewport_width = viewport != nullptr ? viewport->width() : width();
            const double viewport_height = viewport != nullptr ? viewport->height() : height();
            if (viewport_width != g_viewport_width.load(std::memory_order_relaxed) ||
                viewport_height != g_viewport_height.load(std::memory_order_relaxed)) {
                update_viewport_snapshot(this);
                g_viewport_generation.fetch_add(1, std::memory_order_relaxed);
            }
            const double logical_pixels_per_mm = property("logicalPixelsPerMm").toDouble();
            if (std::isfinite(logical_pixels_per_mm) && logical_pixels_per_mm > 0.0) {
                g_logical_pixels_per_mm.store(logical_pixels_per_mm, std::memory_order_relaxed);
            }

            const QVariant generation = property("clickGeneration");
            const auto next_generation = generation.toULongLong();
            if (next_generation != g_click_generation.load(std::memory_order_relaxed)) {
                g_click_x.store(property("clickX").toDouble(), std::memory_order_relaxed);
                g_click_y.store(property("clickY").toDouble(), std::memory_order_relaxed);
                g_click_generation.store(next_generation, std::memory_order_relaxed);
            }

            const auto layer_generation = property("layerVisibilityGeneration").toULongLong();
            if (layer_generation != g_layer_visibility_generation.load(std::memory_order_relaxed)) {
                std::lock_guard<std::mutex> lock(g_layer_visibility_mutex);
                g_layer_visibility_payload = property("layerVisibilityPayload").toString().toStdString();
                g_layer_visibility_generation.store(layer_generation, std::memory_order_relaxed);
            }

            const auto settings_generation = property("layerSettingsGeneration").toULongLong();
            if (settings_generation != g_layer_settings_generation.load(std::memory_order_relaxed)) {
                std::lock_guard<std::mutex> lock(g_layer_settings_mutex);
                g_layer_settings_payload = property("layerSettingsPayload").toString().toStdString();
                g_layer_settings_generation.store(settings_generation, std::memory_order_relaxed);
            }

            const auto edit_generation = property("editGeneration").toULongLong();
            if (edit_generation != g_edit_generation.load(std::memory_order_relaxed)) {
                std::lock_guard<std::mutex> lock(g_edit_mutex);
                g_edit_action = property("editAction").toString().toStdString();
                g_edit_value = property("editValue").toString().toStdString();
                g_edit_generation.store(edit_generation, std::memory_order_relaxed);
            }

            const auto selection_generation = g_selection_generation.load(std::memory_order_relaxed);
            if (selection_generation != selection_generation_) {
                std::lock_guard<std::mutex> lock(g_selection_mutex);
                setProperty("selectionLayer", QString::fromStdString(g_selection_layer));
                setProperty("selectionFeature", QString::fromStdString(g_selection_feature));
                setProperty("selectionValue", QString::fromStdString(g_selection_value));
                setProperty("selectionStatus", QString::fromStdString(g_selection_status));
                selection_generation_ = selection_generation;
            }

            const auto attribute_generation = g_attribute_generation.load(std::memory_order_relaxed);
            if (attribute_generation != attribute_generation_) {
                std::lock_guard<std::mutex> lock(g_attribute_mutex);
                setProperty("attributePayload", QString::fromStdString(g_attribute_payload));
                attribute_generation_ = attribute_generation;
            }

            const auto attribute_page_generation = property("attributePageGeneration").toULongLong();
            if (attribute_page_generation != g_attribute_page_generation.load(std::memory_order_relaxed)) {
                g_attribute_page.store(property("attributePage").toInt(), std::memory_order_relaxed);
                g_attribute_page_generation.store(attribute_page_generation, std::memory_order_relaxed);
            }

            const auto layer_tree_generation = g_layer_tree_generation.load(std::memory_order_relaxed);
            if (layer_tree_generation != layer_tree_generation_) {
                std::lock_guard<std::mutex> lock(g_layer_tree_mutex);
                setProperty("layerTreePayload", QString::fromStdString(g_layer_tree_payload));
                layer_tree_generation_ = layer_tree_generation;
            }

            const auto label_generation = g_layer_label_generation.load(std::memory_order_relaxed);
            if (label_generation != layer_label_generation_) {
                std::lock_guard<std::mutex> lock(g_layer_label_mutex);
                setProperty("layerLabelPayload", QString::fromStdString(g_layer_label_payload));
                setProperty("layerLabelGeneration", QVariant::fromValue<qulonglong>(label_generation));
                layer_label_generation_ = label_generation;
            }

            const auto vertex_handle_generation = g_vertex_handle_generation.load(std::memory_order_relaxed);
            if (vertex_handle_generation != vertex_handle_generation_) {
                std::lock_guard<std::mutex> lock(g_vertex_handle_mutex);
                setProperty("vertexHandlePayload", QString::fromStdString(g_vertex_handle_payload));
                setProperty("vertexHandleGeneration", QVariant::fromValue<qulonglong>(vertex_handle_generation));
                vertex_handle_generation_ = vertex_handle_generation;
            }

            const auto render_status_generation = g_render_status_generation.load(std::memory_order_relaxed);
            if (render_status_generation != render_status_generation_) {
                std::lock_guard<std::mutex> lock(g_render_status_mutex);
                setProperty("renderStatus", QString::fromStdString(g_render_status));
                render_status_generation_ = render_status_generation;
            }

            const auto diagnostic_log_generation = g_diagnostic_log_generation.load(std::memory_order_relaxed);
            if (diagnostic_log_generation != diagnostic_log_generation_) {
                std::lock_guard<std::mutex> lock(g_diagnostic_log_mutex);
                setProperty("diagnosticLogPayload", QString::fromStdString(g_diagnostic_log_payload));
                diagnostic_log_generation_ = diagnostic_log_generation;
            }

            const auto memory_status_generation = g_memory_status_generation.load(std::memory_order_acquire);
            if (memory_status_generation != memory_status_generation_) {
                setProperty("processMemoryBytes", QVariant::fromValue<qulonglong>(g_process_memory_bytes.load(std::memory_order_relaxed)));
                setProperty("goHeapBytes", QVariant::fromValue<qulonglong>(g_go_heap_bytes.load(std::memory_order_relaxed)));
                setProperty("processMemoryKind", g_process_memory_kind.load(std::memory_order_relaxed));
                setProperty("memoryStatusAvailable", g_memory_status_available.load(std::memory_order_relaxed) != 0);
                memory_status_generation_ = memory_status_generation;
            }

            const auto map_metadata_generation = g_map_metadata_generation.load(std::memory_order_relaxed);
            if (map_metadata_generation != map_metadata_generation_) {
                std::lock_guard<std::mutex> lock(g_map_metadata_mutex);
                setProperty("mapMetadataPayload", QString::fromStdString(g_map_metadata_payload));
                setProperty("mapMetadataGeneration", QVariant::fromValue<qulonglong>(map_metadata_generation));
                map_metadata_generation_ = map_metadata_generation;
            }
            const auto applied_metadata_generation = property("mapMetadataAppliedGeneration").toULongLong();
            if (applied_metadata_generation != g_map_metadata_applied_generation.load(std::memory_order_relaxed)) {
                update_viewport_snapshot(this);
                g_map_metadata_applied_generation.store(applied_metadata_generation, std::memory_order_relaxed);
            }

            const auto cancel_generation = property("cancelGeneration").toULongLong();
            if (cancel_generation != g_cancel_generation.load(std::memory_order_relaxed)) {
                g_cancel_generation.store(cancel_generation, std::memory_order_relaxed);
            }

            const auto load_generation = property("loadGeneration").toULongLong();
            if (load_generation != g_load_generation.load(std::memory_order_relaxed)) {
                const auto journal = QJsonDocument::fromJson(property("loadRequestJournal").toString().toUtf8()).array();
                const auto previous = g_load_generation.load(std::memory_order_relaxed);
                {
                    std::lock_guard<std::mutex> lock(g_load_mutex);
                    for (const auto& entry : journal) {
                        const auto request = entry.toObject();
                        const auto generation = request.value("generation").toInteger();
                        if (generation > static_cast<qint64>(previous) && generation <= static_cast<qint64>(load_generation)) {
                            g_load_requests.append(request);
                        }
                    }
                    g_load_generation.store(load_generation, std::memory_order_relaxed);
                }
                setProperty("loadCapturedGeneration", QVariant::fromValue<qulonglong>(load_generation));
            }

            const auto save_generation = property("saveGeneration").toULongLong();
            if (save_generation != g_save_generation.load(std::memory_order_relaxed)) {
                std::lock_guard<std::mutex> lock(g_save_mutex);
                g_save_path = property("savePath").toString().toStdString();
                g_save_profile = property("saveProfile").toString().toStdString();
                g_save_generation.store(save_generation, std::memory_order_relaxed);
            }

            const auto active_layer_generation = property("activeLayerGeneration").toULongLong();
            if (active_layer_generation != g_active_layer_generation.load(std::memory_order_relaxed)) {
                std::lock_guard<std::mutex> lock(g_active_layer_mutex);
                g_active_layer = property("activeLayer").toString().toStdString();
                g_active_layer_generation.store(active_layer_generation, std::memory_order_relaxed);
            }
        });
        click_timer_.start();

        std::lock_guard<std::mutex> lock(g_canvas_mutex);
        g_canvas = this;
    }

    ~GoGISMapCanvas() override {
        std::lock_guard<std::mutex> lock(g_canvas_mutex);
        if (g_canvas == this) {
            g_canvas = nullptr;
        }
    }

protected:
    QSGNode* updatePaintNode(QSGNode* oldNode, UpdatePaintNodeData*) override {
        auto* node = oldNode;
        const bool new_node = node == nullptr;
        if (node == nullptr) {
            try {
                auto created_node = std::make_unique<QSGNode>();
                node = created_node.release();
            } catch (const std::bad_alloc&) {
                set_render_status_safely("Render error: insufficient memory for scene-graph node");
                return nullptr;
            } catch (...) {
                set_render_status_safely("Render error: scene-graph node creation failed");
                return nullptr;
            }
        }

        const float width = static_cast<float>(this->width());
        const float height = static_cast<float>(this->height());
        bool geometry_changed = false;
        {
            // Copy only when the source batch or canvas dimensions changed.
            // Repaint requests can arrive for unrelated QML updates; avoiding
            // a full geometry rewrite keeps large static layers cheap.
            std::lock_guard<std::mutex> lock(g_vertices_mutex);
            const auto source_generation = g_vertices_generation.load(std::memory_order_relaxed);
            const float item_scale = std::max(0.0001f, static_cast<float>(this->scale()));
            const float logical_pixels_per_mm = static_cast<float>(
                g_logical_pixels_per_mm.load(std::memory_order_relaxed));
            if (new_node || source_generation != rendered_generation_ || width != rendered_width_ ||
                height != rendered_height_ || item_scale != rendered_scale_ ||
                logical_pixels_per_mm != rendered_pixels_per_mm_) {
                const size_t source_vertex_count = g_vertices.size();
                size_t output_vertex_count = 0;
                bool can_render_geometry = true;
                const size_t max_qt_vertex_count = std::min(
                    kMaxSceneGraphVertices,
                    static_cast<size_t>(std::numeric_limits<int>::max()));
                for (size_t cursor = 0; cursor < source_vertex_count;) {
                    const size_t increment =
                        g_vertices[cursor].kind == 2 && source_vertex_count - cursor >= 3 ? 3 : 6;
                    if (output_vertex_count > max_qt_vertex_count - increment) {
                        can_render_geometry = false;
                        break;
                    }
                    if (g_vertices[cursor].kind == 2 && source_vertex_count - cursor >= 3) {
                        output_vertex_count += 3;
                        cursor += 3;
                    } else {
                        output_vertex_count += 6;
                        cursor += std::min<size_t>(2, source_vertex_count - cursor);
                    }
                }
                if (!can_render_geometry) {
                    set_render_status_safely(
                        "Render error: scene-graph vertex safety limit exceeded; zoom in");
                    try {
                        geometry_.allocate(0);
                    } catch (...) {
                    }
                    rendered_vertex_count_ = 0;
                } else if (output_vertex_count != rendered_vertex_count_) {
                    try {
                        geometry_.allocate(static_cast<int>(output_vertex_count));
                        rendered_vertex_count_ = output_vertex_count;
                    } catch (const std::bad_alloc&) {
                        can_render_geometry = false;
                        set_render_status_safely(
                            "Render error: insufficient memory for map geometry");
                    } catch (...) {
                        can_render_geometry = false;
                        set_render_status_safely("Render error: Qt geometry allocation failed");
                    }
                    if (!can_render_geometry) {
                        try {
                            geometry_.allocate(0);
                        } catch (...) {
                        }
                        rendered_vertex_count_ = 0;
                    }
                }
                auto* vertices = can_render_geometry ? geometry_.vertexDataAsColoredPoint2D() : nullptr;
                if (can_render_geometry && output_vertex_count > 0 && vertices == nullptr) {
                    can_render_geometry = false;
                    set_render_status_safely(
                        "Render error: Qt scene-graph vertex buffer allocation failed");
                    try {
                        geometry_.allocate(0);
                    } catch (...) {
                    }
                    rendered_vertex_count_ = 0;
                }
                if (can_render_geometry) {
                    size_t output = 0;
                    bool vertex_write_out_of_bounds = false;
                    auto set_vertex = [vertices, output_vertex_count, &vertex_write_out_of_bounds](
                                          size_t index, float x, float y, std::uint32_t color) {
                        if (vertices == nullptr || index >= output_vertex_count) {
                            vertex_write_out_of_bounds = true;
                            return;
                        }
                        const auto alpha = static_cast<unsigned char>(color & 0xff);
                        const auto red = static_cast<unsigned char>((color >> 24) & 0xff);
                        const auto green = static_cast<unsigned char>((color >> 16) & 0xff);
                        const auto blue = static_cast<unsigned char>((color >> 8) & 0xff);
                        vertices[index].set(x, y,
                                            static_cast<unsigned char>((red * alpha + 127) / 255),
                                            static_cast<unsigned char>((green * alpha + 127) / 255),
                                            static_cast<unsigned char>((blue * alpha + 127) / 255),
                                            alpha);
                    };
                    for (size_t i = 0; i < source_vertex_count;) {
                        const auto& first = g_vertices[i];
                        if (first.kind == 2 && source_vertex_count - i >= 3) {
                            for (size_t triangle_vertex = 0; triangle_vertex < 3; ++triangle_vertex) {
                                const auto& source = g_vertices[i + triangle_vertex];
                                const auto color = source.color == 0 ? 0x2b6cb0ff : source.color;
                                set_vertex(output++, source.x * width, (1.0f - source.y) * height, color);
                            }
                            i += 3;
                            continue;
                        }
                        if (source_vertex_count - i < 2) break;
                        const auto& second = g_vertices[i + 1];
                        const auto color = first.color == 0 ? 0x2b6cb0ff : first.color;
                        const float x1 = first.x * width;
                        const float y1 = (1.0f - first.y) * height;
                        const float x2 = second.x * width;
                        const float y2 = (1.0f - second.y) * height;
                        // Apply the minimum in screen pixels, then undo the
                        // item's zoom. A 0.5 local-pixel floor grows to 100
                        // screen pixels at 200x zoom and obscures the map.
                        const float size = std::max(0.5f, first.size_mm * logical_pixels_per_mm) / item_scale;
                        if (first.kind == 1) {
                            const float half = size * 0.5f;
                            set_vertex(output++, x1 - half, y1 - half, color);
                            set_vertex(output++, x1 + half, y1 - half, color);
                            set_vertex(output++, x1 - half, y1 + half, color);
                            set_vertex(output++, x1 - half, y1 + half, color);
                            set_vertex(output++, x1 + half, y1 - half, color);
                            set_vertex(output++, x1 + half, y1 + half, color);
                            i += 2;
                            continue;
                        }
                        const float dx = x2 - x1;
                        const float dy = y2 - y1;
                        const float length = std::sqrt(dx * dx + dy * dy);
                        if (length <= 0.0001f) {
                            for (int duplicate = 0; duplicate < 6; ++duplicate) {
                                set_vertex(output++, x1, y1, color);
                            }
                            i += 2;
                            continue;
                        }
                        const float nx = -dy / length * size * 0.5f;
                        const float ny = dx / length * size * 0.5f;
                        set_vertex(output++, x1 + nx, y1 + ny, color);
                        set_vertex(output++, x1 - nx, y1 - ny, color);
                        set_vertex(output++, x2 + nx, y2 + ny, color);
                        set_vertex(output++, x2 + nx, y2 + ny, color);
                        set_vertex(output++, x1 - nx, y1 - ny, color);
                        set_vertex(output++, x2 - nx, y2 - ny, color);
                        i += 2;
                    }
                    if (vertex_write_out_of_bounds || output != output_vertex_count) {
                        can_render_geometry = false;
                        set_render_status_safely(
                            "Render error: scene-graph vertex count changed during conversion");
                        try {
                            geometry_.allocate(0);
                        } catch (...) {
                        }
                        rendered_vertex_count_ = 0;
                    }
                }
                rendered_generation_ = source_generation;
                rendered_stage_ = g_vertices_stage;
                rendered_width_ = width;
                rendered_height_ = height;
                rendered_scale_ = item_scale;
                rendered_pixels_per_mm_ = logical_pixels_per_mm;
                geometry_changed = true;
            }
        }
        if (geometry_changed) {
            while (auto* child = node->firstChild()) {
                node->removeChildNode(child);
                delete child;
            }
            bool batched = true;
            size_t batch_count = 0;
            try {
                const auto* source = geometry_.vertexDataAsColoredPoint2D();
                for (size_t start = 0; start < rendered_vertex_count_;) {
                    const size_t count = std::min(kSceneGraphBatchVertices, rendered_vertex_count_ - start);
                    auto batch_node = std::make_unique<QSGGeometryNode>();
                    auto batch_geometry = std::make_unique<QSGGeometry>(
                        QSGGeometry::defaultAttributes_ColoredPoint2D(), static_cast<int>(count));
                    std::memcpy(batch_geometry->vertexDataAsColoredPoint2D(), source + start,
                                count * sizeof(QSGGeometry::ColoredPoint2D));
                    batch_geometry->setDrawingMode(QSGGeometry::DrawTriangles);
                    batch_node->setGeometry(batch_geometry.release());
                    batch_node->setFlag(QSGNode::OwnsGeometry, true);
                    batch_node->setMaterial(&material_);
                    batch_node->setFlag(QSGNode::OwnsMaterial, false);
                    batch_node->markDirty(QSGNode::DirtyGeometry);
                    node->appendChildNode(batch_node.release());
                    ++batch_count;
                    start += count;
                }
            } catch (const std::bad_alloc&) {
                batched = false;
                set_render_status_safely("Render error: insufficient memory for scene-graph batches");
            } catch (...) {
                batched = false;
                set_render_status_safely("Render error: scene-graph batching failed");
            }
            if (!batched) {
                while (auto* child = node->firstChild()) {
                    node->removeChildNode(child);
                    delete child;
                }
            } else if (rendered_vertex_count_ > 0) {
                trace_load_event("scenegraph", rendered_stage_, rendered_vertex_count_);
                if (perf_trace_enabled()) {
                    std::fprintf(stderr,
                                 "GoGIS perf: scenegraph-batches stage=%s nodes=%zu canvas=%.0fx%.0f scale=%.3f\n",
                                 stage_name(rendered_stage_), batch_count, rendered_width_, rendered_height_,
                                 rendered_scale_);
                }
            }
        }
        return node;
    }

    void itemChange(ItemChange change, const ItemChangeData& value) override {
        QQuickItem::itemChange(change, value);
        if (change == ItemScaleHasChanged || change == ItemTransformHasChanged ||
            change == ItemVisibleHasChanged) {
            update_viewport_snapshot(this);
            g_viewport_generation.fetch_add(1, std::memory_order_relaxed);
            if (change == ItemScaleHasChanged) {
                update();
            }
        }
    }

    void geometryChange(const QRectF& newGeometry, const QRectF& oldGeometry) override {
        QQuickItem::geometryChange(newGeometry, oldGeometry);
        update_viewport_snapshot(this);
        if (newGeometry.size() != oldGeometry.size()) {
            g_viewport_generation.fetch_add(1, std::memory_order_relaxed);
        }
    }

private:
    QSGGeometry geometry_;
    QSGVertexColorMaterial material_;
    QTimer click_timer_;
    unsigned long long view_generation_ = 0;
    unsigned long long selection_generation_ = 0;
    unsigned long long attribute_generation_ = 0;
    unsigned long long layer_tree_generation_ = 0;
    unsigned long long layer_label_generation_ = 0;
    unsigned long long vertex_handle_generation_ = 0;
    unsigned long long render_status_generation_ = 0;
    unsigned long long diagnostic_log_generation_ = 0;
    unsigned long long memory_status_generation_ = 0;
    unsigned long long map_metadata_generation_ = 0;
    unsigned long long rendered_generation_ = 0;
    size_t rendered_vertex_count_ = 0;
    int rendered_stage_ = 0;
    float rendered_width_ = -1.0f;
    float rendered_height_ = -1.0f;
    float rendered_scale_ = -1.0f;
    float rendered_pixels_per_mm_ = -1.0f;
};

extern "C" void gogis_register_qml_types(void) {
    qmlRegisterType<GoGISMapCanvas>("GoGIS", 1, 0, "MapCanvas");
}

extern "C" void gogis_set_vertices(const float* xy, int vertex_count) {
    std::lock_guard<std::mutex> lock(g_vertices_mutex);
    const size_t count = xy != nullptr && vertex_count > 0
                             ? static_cast<size_t>(vertex_count)
                             : 0;
    if (count > kMaxSourceVertexCount) {
        std::vector<GoGISVertex>().swap(g_vertices);
        set_render_status_safely("Render error: source vertex safety limit exceeded");
        g_vertices_stage = 0;
        g_vertices_generation.fetch_add(1, std::memory_order_relaxed);
        return;
    }
    // Keep capacity for ordinary frame-size fluctuations; resize_vertex_buffer
    // releases it on empty views and shrinks it after a substantial reduction.
    if (!resize_vertex_buffer(count)) {
        return;
    }
    g_vertices_stage = 0;
    for (size_t index = 0; index < count; ++index) {
        g_vertices[index] = {xy[index * 2], xy[index * 2 + 1], 0, 0, 0};
    }
    g_vertices_generation.fetch_add(1, std::memory_order_relaxed);
}

extern "C" void gogis_set_vertices_vertex_layout(const void* raw_vertices,
                                                   int vertex_count) {
    gogis_set_vertices_vertex_layout_stage(raw_vertices, vertex_count, 0);
}

extern "C" void gogis_set_vertices_vertex_layout_stage(const void* raw_vertices,
                                                         int vertex_count, int stage) {
    const auto* vertices = static_cast<const GoGISVertex*>(raw_vertices);
    std::lock_guard<std::mutex> lock(g_vertices_mutex);
    const size_t count = vertices != nullptr && vertex_count > 0
                             ? static_cast<size_t>(vertex_count)
                             : 0;
    if (count > kMaxSourceVertexCount) {
        std::vector<GoGISVertex>().swap(g_vertices);
        set_render_status_safely("Render error: source vertex safety limit exceeded");
        g_vertices_stage = stage;
        g_vertices_generation.fetch_add(1, std::memory_order_relaxed);
        return;
    }
    if (!resize_vertex_buffer(count)) {
        return;
    }
    g_vertices_stage = stage;
    if (count > 0) {
        std::memcpy(g_vertices.data(), vertices, count * sizeof(GoGISVertex));
    }
    g_vertices_generation.fetch_add(1, std::memory_order_relaxed);
    if (count > 0) {
        trace_load_event("vertices-published", stage, count);
    }
}

extern "C" void gogis_trace_load_start(void) {
    if (!perf_trace_enabled()) {
        return;
    }
    g_perf_load_started_ns.store(steady_nanoseconds(), std::memory_order_relaxed);
    std::fprintf(stderr, "GoGIS perf: load-start 0.0ms vertices=0\n");
}

extern "C" unsigned long long gogis_viewport_generation(void) {
    return g_viewport_generation.load(std::memory_order_relaxed);
}

extern "C" void gogis_request_canvas_update(void) {
    std::lock_guard<std::mutex> lock(g_canvas_mutex);
    if (g_canvas != nullptr) {
        QMetaObject::invokeMethod(g_canvas, "update", Qt::QueuedConnection);
    }
}

extern "C" void gogis_canvas_viewport(double* pan_x, double* pan_y, double* zoom,
                                       double* width, double* height,
                                       double* viewport_width, double* viewport_height) {
    if (pan_x != nullptr) {
        *pan_x = g_pan_x.load(std::memory_order_relaxed);
    }
    if (pan_y != nullptr) {
        *pan_y = g_pan_y.load(std::memory_order_relaxed);
    }
    if (zoom != nullptr) {
        *zoom = g_zoom.load(std::memory_order_relaxed);
    }
    if (width != nullptr) {
        *width = g_width.load(std::memory_order_relaxed);
    }
    if (height != nullptr) {
        *height = g_height.load(std::memory_order_relaxed);
    }
    if (viewport_width != nullptr) {
        *viewport_width = g_viewport_width.load(std::memory_order_relaxed);
    }
    if (viewport_height != nullptr) {
        *viewport_height = g_viewport_height.load(std::memory_order_relaxed);
    }
}

extern "C" int gogis_canvas_visible(void) {
    return g_visible.load(std::memory_order_relaxed) ? 1 : 0;
}

extern "C" unsigned long long gogis_click_generation(void) {
    return g_click_generation.load(std::memory_order_relaxed);
}

extern "C" void gogis_canvas_click(double* x, double* y) {
    if (x != nullptr) {
        *x = g_click_x.load(std::memory_order_relaxed);
    }
    if (y != nullptr) {
        *y = g_click_y.load(std::memory_order_relaxed);
    }
}

extern "C" void gogis_set_selection(const char* layer, const char* feature,
                                      const char* value, const char* status) {
    {
        std::lock_guard<std::mutex> lock(g_selection_mutex);
        g_selection_layer = layer != nullptr ? layer : "";
        g_selection_feature = feature != nullptr ? feature : "";
        g_selection_value = value != nullptr ? value : "";
        g_selection_status = status != nullptr ? status : "";
    }
    g_selection_generation.fetch_add(1, std::memory_order_relaxed);
}

extern "C" unsigned long long gogis_layer_visibility_generation(void) {
    return g_layer_visibility_generation.load(std::memory_order_relaxed);
}

extern "C" void gogis_layer_visibility(char* buffer, int buffer_length) {
    if (buffer == nullptr || buffer_length <= 0) {
        return;
    }
    std::lock_guard<std::mutex> lock(g_layer_visibility_mutex);
    const auto copy_length = std::min<size_t>(g_layer_visibility_payload.size(), static_cast<size_t>(buffer_length - 1));
    std::memcpy(buffer, g_layer_visibility_payload.data(), copy_length);
    buffer[copy_length] = '\0';
}

extern "C" unsigned long long gogis_layer_settings_generation(void) {
    return g_layer_settings_generation.load(std::memory_order_relaxed);
}

extern "C" void gogis_layer_settings(char* buffer, int buffer_length) {
    if (buffer == nullptr || buffer_length <= 0) {
        return;
    }
    std::lock_guard<std::mutex> lock(g_layer_settings_mutex);
    const auto copy_length = std::min<size_t>(g_layer_settings_payload.size(), static_cast<size_t>(buffer_length - 1));
    std::memcpy(buffer, g_layer_settings_payload.data(), copy_length);
    buffer[copy_length] = '\0';
}

extern "C" unsigned long long gogis_edit_generation(void) {
    return g_edit_generation.load(std::memory_order_relaxed);
}

extern "C" void gogis_edit_event(char* action, int action_length,
                                   char* value, int value_length) {
    if (action == nullptr || action_length <= 0 || value == nullptr || value_length <= 0) {
        return;
    }
    std::lock_guard<std::mutex> lock(g_edit_mutex);
    const auto action_copy_length = std::min<size_t>(g_edit_action.size(), static_cast<size_t>(action_length - 1));
    const auto value_copy_length = std::min<size_t>(g_edit_value.size(), static_cast<size_t>(value_length - 1));
    std::memcpy(action, g_edit_action.data(), action_copy_length);
    std::memcpy(value, g_edit_value.data(), value_copy_length);
    action[action_copy_length] = '\0';
    value[value_copy_length] = '\0';
}

extern "C" void gogis_set_attribute_payload(const char* payload) {
    {
        std::lock_guard<std::mutex> lock(g_attribute_mutex);
        g_attribute_payload = payload != nullptr ? payload : "[]";
    }
    g_attribute_generation.fetch_add(1, std::memory_order_relaxed);
}

extern "C" unsigned long long gogis_attribute_page_generation(void) {
    return g_attribute_page_generation.load(std::memory_order_relaxed);
}

extern "C" int gogis_attribute_page(void) {
    return g_attribute_page.load(std::memory_order_relaxed);
}

extern "C" void gogis_set_layer_tree_payload(const char* payload) {
    {
        std::lock_guard<std::mutex> lock(g_layer_tree_mutex);
        g_layer_tree_payload = payload != nullptr ? payload : "[]";
    }
    g_layer_tree_generation.fetch_add(1, std::memory_order_relaxed);
}

extern "C" void gogis_set_layer_label_payload(const char* payload) {
    {
        std::lock_guard<std::mutex> lock(g_layer_label_mutex);
        g_layer_label_payload = payload != nullptr ? payload : "[]";
    }
    g_layer_label_generation.fetch_add(1, std::memory_order_relaxed);
}

extern "C" void gogis_set_vertex_handle_payload(const char* payload) {
    {
        std::lock_guard<std::mutex> lock(g_vertex_handle_mutex);
        g_vertex_handle_payload = payload != nullptr ? payload : "[]";
    }
    g_vertex_handle_generation.fetch_add(1, std::memory_order_relaxed);
}

extern "C" unsigned long long gogis_active_layer_generation(void) {
    return g_active_layer_generation.load(std::memory_order_relaxed);
}

extern "C" void gogis_active_layer(char* buffer, int buffer_length) {
    if (buffer == nullptr || buffer_length <= 0) {
        return;
    }
    std::lock_guard<std::mutex> lock(g_active_layer_mutex);
    const auto copy_length = std::min<size_t>(g_active_layer.size(), static_cast<size_t>(buffer_length - 1));
    std::memcpy(buffer, g_active_layer.data(), copy_length);
    buffer[copy_length] = '\0';
}

extern "C" void gogis_set_render_status(const char* status) {
    {
        std::lock_guard<std::mutex> lock(g_render_status_mutex);
        g_render_status = status != nullptr ? status : "";
    }
    g_render_status_generation.fetch_add(1, std::memory_order_relaxed);
}

extern "C" void gogis_set_diagnostic_log_payload(const char* payload) {
    {
        std::lock_guard<std::mutex> lock(g_diagnostic_log_mutex);
        g_diagnostic_log_payload = payload != nullptr ? payload : "[]";
    }
    g_diagnostic_log_generation.fetch_add(1, std::memory_order_relaxed);
}

extern "C" void gogis_set_memory_status(unsigned long long process_bytes,
                                           unsigned long long go_heap_bytes,
                                           int process_memory_kind,
                                           int available) {
    g_process_memory_bytes.store(process_bytes, std::memory_order_relaxed);
    g_go_heap_bytes.store(go_heap_bytes, std::memory_order_relaxed);
    g_process_memory_kind.store(process_memory_kind, std::memory_order_relaxed);
    g_memory_status_available.store(available != 0 ? 1 : 0, std::memory_order_relaxed);
    g_memory_status_generation.fetch_add(1, std::memory_order_release);
}

extern "C" unsigned long long gogis_retained_vertex_bytes(void) {
    std::lock_guard<std::mutex> lock(g_vertices_mutex);
    return static_cast<unsigned long long>(g_vertices.capacity() * sizeof(GoGISVertex));
}

extern "C" void gogis_set_map_metadata(const char* payload) {
    std::lock_guard<std::mutex> lock(g_map_metadata_mutex);
    g_map_metadata_payload = payload == nullptr ? "{}" : payload;
    g_map_metadata_generation.fetch_add(1, std::memory_order_relaxed);
}

extern "C" unsigned long long gogis_map_metadata_generation(void) {
    return g_map_metadata_generation.load(std::memory_order_relaxed);
}

extern "C" unsigned long long gogis_map_metadata_applied_generation(void) {
    return g_map_metadata_applied_generation.load(std::memory_order_relaxed);
}

extern "C" unsigned long long gogis_cancel_generation(void) {
    return g_cancel_generation.load(std::memory_order_relaxed);
}

extern "C" unsigned long long gogis_load_generation(void) {
    return g_load_generation.load(std::memory_order_relaxed);
}

extern "C" int gogis_load_requests(char* buffer, int buffer_length) {
    std::lock_guard<std::mutex> lock(g_load_mutex);
    const auto payload = QJsonDocument(g_load_requests).toJson(QJsonDocument::Compact);
    if (buffer == nullptr || buffer_length <= payload.size()) {
        return payload.size();
    }
    std::memcpy(buffer, payload.constData(), static_cast<size_t>(payload.size()));
    buffer[payload.size()] = '\0';
    g_load_requests = QJsonArray();
    return payload.size();
}

extern "C" unsigned long long gogis_save_generation(void) {
    return g_save_generation.load(std::memory_order_relaxed);
}

extern "C" void gogis_save_path(char* buffer, int buffer_length) {
    if (buffer == nullptr || buffer_length <= 0) {
        return;
    }
    std::lock_guard<std::mutex> lock(g_save_mutex);
    const auto copy_length = std::min<size_t>(g_save_path.size(), static_cast<size_t>(buffer_length - 1));
    std::memcpy(buffer, g_save_path.data(), copy_length);
    buffer[copy_length] = '\0';
}

extern "C" void gogis_save_profile(char* buffer, int buffer_length) {
    if (buffer == nullptr || buffer_length <= 0) {
        return;
    }
    std::lock_guard<std::mutex> lock(g_save_mutex);
    const auto copy_length = std::min<size_t>(g_save_profile.size(), static_cast<size_t>(buffer_length - 1));
    std::memcpy(buffer, g_save_profile.data(), copy_length);
    buffer[copy_length] = '\0';
}
