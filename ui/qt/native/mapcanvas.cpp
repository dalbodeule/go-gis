#include "mapcanvas.h"

#include <QColor>
#include <QQuickItem>
#include <QSGFlatColorMaterial>
#include <QSGGeometryNode>
#include <QMetaObject>
#include <QTimer>
#include <QVariant>
#include <QtQml/qqml.h>
#include <algorithm>
#include <atomic>
#include <cstdint>
#include <cstring>
#include <mutex>
#include <string>
#include <vector>

namespace {

std::mutex g_vertices_mutex;
struct GoGISVertex {
    float x;
    float y;
    std::uint32_t color;
};
static_assert(sizeof(GoGISVertex) == 12, "unexpected Go vertex layout");
std::vector<GoGISVertex> g_vertices;
std::atomic<unsigned long long> g_vertices_generation{0};
std::atomic<unsigned long long> g_viewport_generation{0};
std::atomic<double> g_pan_x{0};
std::atomic<double> g_pan_y{0};
std::atomic<double> g_zoom{1};
std::atomic<double> g_width{1};
std::atomic<double> g_height{1};
std::atomic<bool> g_visible{true};
std::atomic<unsigned long long> g_click_generation{0};
std::atomic<double> g_click_x{0};
std::atomic<double> g_click_y{0};
std::mutex g_layer_visibility_mutex;
std::string g_layer_visibility_payload;
std::atomic<unsigned long long> g_layer_visibility_generation{0};
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
std::mutex g_active_layer_mutex;
std::string g_active_layer;
std::atomic<unsigned long long> g_active_layer_generation{0};
std::mutex g_render_status_mutex;
std::string g_render_status;
std::atomic<unsigned long long> g_render_status_generation{0};
std::atomic<unsigned long long> g_cancel_generation{0};
std::mutex g_load_mutex;
std::string g_load_path;
std::atomic<unsigned long long> g_load_generation{0};
std::mutex g_selection_mutex;
std::string g_selection_layer;
std::string g_selection_feature;
std::string g_selection_value;
std::string g_selection_status;
std::atomic<unsigned long long> g_selection_generation{0};
std::mutex g_canvas_mutex;
QQuickItem* g_canvas = nullptr;

void update_viewport_snapshot(const QQuickItem* item) {
    g_pan_x.store(item->x(), std::memory_order_relaxed);
    g_pan_y.store(item->y(), std::memory_order_relaxed);
    g_zoom.store(item->scale(), std::memory_order_relaxed);
    g_width.store(item->width(), std::memory_order_relaxed);
    g_height.store(item->height(), std::memory_order_relaxed);
    g_visible.store(item->isVisible(), std::memory_order_relaxed);
}

} // namespace

class GoGISMapCanvas : public QQuickItem {
public:
    explicit GoGISMapCanvas(QQuickItem* parent = nullptr)
        : QQuickItem(parent),
          geometry_(QSGGeometry::defaultAttributes_Point2D(), 0),
          material_() {
        setFlag(ItemHasContents, true);
        material_.setColor(QColor(QStringLiteral("#2b6cb0")));
        update_viewport_snapshot(this);

        // QML adds clickX/clickY/clickGeneration to the registered item. Read
        // those dynamic properties on the GUI thread so Go never touches a
        // QObject from its render polling goroutine.
        click_timer_.setInterval(16);
        QObject::connect(&click_timer_, &QTimer::timeout, this, [this]() {
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

            const auto render_status_generation = g_render_status_generation.load(std::memory_order_relaxed);
            if (render_status_generation != render_status_generation_) {
                std::lock_guard<std::mutex> lock(g_render_status_mutex);
                setProperty("renderStatus", QString::fromStdString(g_render_status));
                render_status_generation_ = render_status_generation;
            }

            const auto cancel_generation = property("cancelGeneration").toULongLong();
            if (cancel_generation != g_cancel_generation.load(std::memory_order_relaxed)) {
                g_cancel_generation.store(cancel_generation, std::memory_order_relaxed);
            }

            const auto load_generation = property("loadGeneration").toULongLong();
            if (load_generation != g_load_generation.load(std::memory_order_relaxed)) {
                std::lock_guard<std::mutex> lock(g_load_mutex);
                g_load_path = property("loadPath").toString().toStdString();
                g_load_generation.store(load_generation, std::memory_order_relaxed);
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
        auto* node = static_cast<QSGGeometryNode*>(oldNode);
        if (node == nullptr) {
            node = new QSGGeometryNode();
            node->setGeometry(&geometry_);
            node->setMaterial(&material_);
            node->setFlag(QSGNode::OwnsMaterial, false);
            node->setFlag(QSGNode::OwnsGeometry, false);
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
            if (source_generation != rendered_generation_ || width != rendered_width_ ||
                height != rendered_height_) {
                const size_t source_vertex_count = g_vertices.size();
                if (source_vertex_count != rendered_vertex_count_) {
                    geometry_.allocate(static_cast<int>(source_vertex_count));
                    rendered_vertex_count_ = source_vertex_count;
                }
                auto* vertices = geometry_.vertexDataAsPoint2D();
                for (size_t i = 0; i < source_vertex_count; ++i) {
                    vertices[i] = {
                        g_vertices[i].x * width,
                        (1.0f - g_vertices[i].y) * height,
                    };
                }
                rendered_generation_ = source_generation;
                rendered_width_ = width;
                rendered_height_ = height;
                geometry_changed = true;
            }
        }
        if (geometry_changed) {
            geometry_.setDrawingMode(QSGGeometry::DrawLines);
            node->markDirty(QSGNode::DirtyGeometry);
        }
        return node;
    }

    void itemChange(ItemChange change, const ItemChangeData& value) override {
        QQuickItem::itemChange(change, value);
        if (change == ItemScaleHasChanged || change == ItemTransformHasChanged ||
            change == ItemVisibleHasChanged) {
            update_viewport_snapshot(this);
            g_viewport_generation.fetch_add(1, std::memory_order_relaxed);
        }
    }

    void geometryChange(const QRectF& newGeometry, const QRectF& oldGeometry) override {
        QQuickItem::geometryChange(newGeometry, oldGeometry);
        update_viewport_snapshot(this);
    }

private:
    QSGGeometry geometry_;
    QSGFlatColorMaterial material_;
    QTimer click_timer_;
    unsigned long long selection_generation_ = 0;
    unsigned long long attribute_generation_ = 0;
    unsigned long long layer_tree_generation_ = 0;
    unsigned long long render_status_generation_ = 0;
    unsigned long long rendered_generation_ = 0;
    size_t rendered_vertex_count_ = 0;
    float rendered_width_ = -1.0f;
    float rendered_height_ = -1.0f;
};

extern "C" void gogis_register_qml_types(void) {
    qmlRegisterType<GoGISMapCanvas>("GoGIS", 1, 0, "MapCanvas");
}

extern "C" void gogis_set_vertices(const float* xy, int vertex_count) {
    std::lock_guard<std::mutex> lock(g_vertices_mutex);
    const size_t count = xy != nullptr && vertex_count > 0
                             ? static_cast<size_t>(vertex_count)
                             : 0;
    // Keep the vector capacity across frame publishes. The Go bridge already
    // holds a reusable scratch buffer, so recreating this C++ vector here
    // would add a second heap allocation to every batch update.
    g_vertices.resize(count);
    for (size_t index = 0; index < count; ++index) {
        g_vertices[index] = {xy[index * 2], xy[index * 2 + 1], 0};
    }
    g_vertices_generation.fetch_add(1, std::memory_order_relaxed);
}

extern "C" void gogis_set_vertices_vertex_layout(const void* raw_vertices,
                                                   int vertex_count) {
    const auto* vertices = static_cast<const GoGISVertex*>(raw_vertices);
    std::lock_guard<std::mutex> lock(g_vertices_mutex);
    const size_t count = vertices != nullptr && vertex_count > 0
                             ? static_cast<size_t>(vertex_count)
                             : 0;
    g_vertices.resize(count);
    if (count > 0) {
        std::memcpy(g_vertices.data(), vertices, count * sizeof(GoGISVertex));
    }
    g_vertices_generation.fetch_add(1, std::memory_order_relaxed);
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
                                       double* width, double* height) {
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

extern "C" unsigned long long gogis_cancel_generation(void) {
    return g_cancel_generation.load(std::memory_order_relaxed);
}

extern "C" unsigned long long gogis_load_generation(void) {
    return g_load_generation.load(std::memory_order_relaxed);
}

extern "C" void gogis_load_path(char* buffer, int buffer_length) {
    if (buffer == nullptr || buffer_length <= 0) {
        return;
    }
    std::lock_guard<std::mutex> lock(g_load_mutex);
    const auto copy_length = std::min<size_t>(g_load_path.size(), static_cast<size_t>(buffer_length - 1));
    std::memcpy(buffer, g_load_path.data(), copy_length);
    buffer[copy_length] = '\0';
}
