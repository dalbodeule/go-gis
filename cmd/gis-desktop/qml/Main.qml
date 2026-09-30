import QtQuick
import QtQuick.Window
import QtQuick.Controls
import QtQuick.Layouts
import Qt.labs.platform as Platform
import GoGIS 1.0

ApplicationWindow {
    id: rootWindow
    visible: true
    width: 1440
    height: 900
    minimumWidth: 960
    minimumHeight: 640
    title: "GoGIS — Milestone B prototype"
    color: "#f4f6f8"

    header: ToolBar {
        RowLayout {
            anchors.fill: parent
            anchors.leftMargin: 12
            anchors.rightMargin: 12
            Label {
                text: "GoGIS"
                font.bold: true
                font.pixelSize: 18
            }
            Label {
                text: "Milestone B / Qt Quick"
                color: "#65717d"
            }
            Item {
                Layout.fillWidth: true
            }
            Button {
                text: "Add vector files"
                onClicked: fileDialog.open()
            }
            Button {
                text: "Open workspace"
                onClicked: workspaceOpenDialog.open()
            }
            Button {
                text: "Save GeoPackage"
                onClicked: saveDialog.open()
            }
            Button {
                text: "Save workspace"
                onClicked: workspaceDialog.open()
            }
            Label {
                text: "QSGGeometryNode: active demo batch"
                color: "#65717d"
            }
        }
    }

    SplitView {
        anchors.fill: parent
        anchors.margins: 12

        Frame {
            SplitView.preferredWidth: 220
            Layout.fillHeight: true
            ColumnLayout {
                anchors.fill: parent
                Label {
                    text: "Layers"
                    font.bold: true
                }
                ListView {
                    id: layerList
                    Layout.fillWidth: true
                    Layout.fillHeight: true
                    model: ListModel {
                        id: layerModel
                        objectName: "layerModel"
                        ListElement {
                            name: "roads"
                            layerVisible: true
                        }
                        ListElement {
                            name: "buildings"
                            layerVisible: true
                        }
                        ListElement {
                            name: "labels"
                            layerVisible: true
                        }
                    }
                    delegate: CheckDelegate {
                        width: ListView.view.width
                        text: (model.sourceError ? "⚠ " : "") + (typeof model.displayName === "undefined" || model.displayName === "" ? model.name : model.displayName)
                        checked: layerVisible
                        onToggled: {
                            layerModel.setProperty(index, "layerVisible", checked);
                            mapViewport.syncLayerVisibility();
                        }
                        onClicked: mapViewport.selectLayer(name)
                    }
                }
            }
        }

        Frame {
            SplitView.fillWidth: true
            SplitView.fillHeight: true
            background: null
            Rectangle {
                anchors.fill: parent
                z: -1
                color: "#dfe5ea"
                radius: 4
            }
            Item {
                id: mapViewport
                objectName: "mapViewport"
                anchors.fill: parent
                clip: true
                property int viewportGeneration: 0
                property real panX: 0
                property real panY: 0
                property real mapZoom: 1.0
                property string selectedLayer: ""
                property string selectedFeature: ""
                property string selectedStatus: "No feature selected"
                property string editorValue: ""
                property string attributePayloadSeen: ""
                property string layerTreePayloadSeen: "[]"
                property int mapMetadataGenerationSeen: -1
                property string activeLayer: ""
                property string pendingWorkspaceActiveLayer: ""
                property var dataBounds: [0, 0, 1, 1]
                property string dataCRS: ""
                property real cursorX: 0
                property real cursorY: 0
                property bool cursorValid: false
                property var attributeColumns: []
                property int attributePage: 0
                property int attributePageSize: 0
                property int attributeTotal: 0
                property int layerLabelGenerationSeen: -1
                property string renderStatus: "Ready"

                ListModel {
                    id: attributeModel
                }
                ListModel {
                    id: mapLabelModel
                }

                function anyLayerVisible() {
                    for (var i = 0; i < layerModel.count; ++i) {
                        if (layerModel.get(i).layerVisible)
                            return true;
                    }
                    return false;
                }

                function selectLayer(name) {
                    activeLayer = name;
                    mapCanvas.activeLayer = name;
                    mapCanvas.activeLayerGeneration += 1;
                    for (var i = 0; i < layerModel.count; ++i) {
                        if (layerModel.get(i).name === name) {
                            attributeLayerTabs.currentIndex = i;
                            break;
                        }
                    }
                }

                function currentMapCoordinate() {
                    if (!cursorValid || mapCanvas.width <= 0 || mapCanvas.height <= 0)
                        return "—";
                    var bounds = dataBounds;
                    var zoom = Math.max(0.0001, mapZoom);
                    var nx = 0.5 + (cursorX - mapCanvas.width / 2 - panX) / (mapCanvas.width * zoom);
                    var ny = 0.5 - (cursorY - mapCanvas.height / 2 - panY) / (mapCanvas.height * zoom);
                    var x = bounds[0] + nx * (bounds[2] - bounds[0]);
                    var y = bounds[1] + ny * (bounds[3] - bounds[1]);
                    var digits = dataCRS.toUpperCase() === "EPSG:4326" ? 6 : 2;
                    return "X " + Number(x).toFixed(digits) + "  Y " + Number(y).toFixed(digits);
                }

                function currentScaleText() {
                    if (mapCanvas.width <= 0 || mapZoom <= 0)
                        return "Scale —";
                    var bounds = dataBounds;
                    var xUnitsPerPixel = (bounds[2] - bounds[0]) / (mapCanvas.width * mapZoom);
                    var metersPerUnit = 1.0;
                    if (dataCRS.toUpperCase() === "EPSG:4326") {
                        var centerLatitude = (bounds[1] + bounds[3]) / 2;
                        metersPerUnit = 111319.49 * Math.max(0.01, Math.cos(centerLatitude * Math.PI / 180));
                    }
                    var denominator = xUnitsPerPixel * metersPerUnit * 96 / 0.0254;
                    if (!isFinite(denominator) || denominator <= 0)
                        return "Scale —";
                    return "Approx. 1:" + Math.max(1, Math.round(denominator)).toLocaleString();
                }

                function currentScaleDenominator() {
                    if (mapCanvas.width <= 0 || mapViewport.mapZoom <= 0)
                        return 0;
                    var bounds = mapViewport.dataBounds;
                    var unitsPerPixel = (bounds[2] - bounds[0]) / (mapCanvas.width * mapViewport.mapZoom);
                    var metersPerUnit = 1.0;
                    if (mapViewport.dataCRS.toUpperCase() === "EPSG:4326") {
                        var latitude = (bounds[1] + bounds[3]) / 2;
                        metersPerUnit = 111319.49 * Math.max(0.01, Math.cos(latitude * Math.PI / 180));
                    }
                    var denominator = unitsPerPixel * metersPerUnit * 96 / 0.0254;
                    return isFinite(denominator) && denominator > 0 ? Math.round(denominator) : 0;
                }

                function screenRotation(mapAngle) {
                    var bounds = dataBounds;
                    var spanX = bounds[2] - bounds[0];
                    var spanY = bounds[3] - bounds[1];
                    if (spanX <= 0 || spanY <= 0 || mapCanvas.width <= 0 || mapCanvas.height <= 0)
                        return -mapAngle;
                    var radians = mapAngle * Math.PI / 180;
                    var dx = Math.cos(radians) * mapCanvas.width / spanX;
                    var dy = -Math.sin(radians) * mapCanvas.height / spanY;
                    var degrees = Math.atan2(dy, dx) * 180 / Math.PI;
                    // Keep text upright regardless of source-line digitizing direction.
                    if (degrees > 90)
                        degrees -= 180;
                    if (degrees < -90)
                        degrees += 180;
                    return degrees;
                }

                function goToCoordinate() {
                    var x = Number(coordinateXInput.text);
                    var y = Number(coordinateYInput.text);
                    if (!isFinite(x) || !isFinite(y) || dataBounds.length < 4 || dataBounds[2] === dataBounds[0] || dataBounds[3] === dataBounds[1]) {
                        coordinateNavigationStatus.text = "Enter valid map coordinates";
                        return;
                    }
                    var nx = (x - dataBounds[0]) / (dataBounds[2] - dataBounds[0]);
                    var ny = (y - dataBounds[1]) / (dataBounds[3] - dataBounds[1]);
                    panX = (0.5 - nx) * mapCanvas.width * mapZoom;
                    panY = (ny - 0.5) * mapCanvas.height * mapZoom;
                    viewportGeneration += 1;
                    coordinateNavigationStatus.text = "Centered at " + x + ", " + y;
                }

                function requestAttributePage(page) {
                    var pageSize = attributePageSize > 0 ? attributePageSize : 200;
                    var pageCount = Math.max(1, Math.ceil(attributeTotal / pageSize));
                    page = Math.max(0, Math.min(pageCount - 1, page));
                    if (page === attributePage)
                        return;
                    mapCanvas.attributePage = page;
                    mapCanvas.attributePageGeneration += 1;
                }

                function syncLayerVisibility() {
                    var visibility = {};
                    for (var i = 0; i < layerModel.count; ++i) {
                        var layer = layerModel.get(i);
                        visibility[layer.name] = layer.layerVisible;
                    }
                    mapCanvas.layerVisibilityPayload = JSON.stringify(visibility);
                    mapCanvas.layerVisibilityGeneration += 1;
                }

                MapCanvas {
                    id: mapCanvas
                    objectName: "goGisMapCanvas"
                    anchors.fill: parent
                    clip: true
                    property real clickX: 0
                    property real clickY: 0
                    property int clickGeneration: 0
                    // MapCanvas geometry uses device-independent pixels.
                    property real logicalPixelsPerMm: Screen.pixelDensity > 0 && Screen.devicePixelRatio > 0 ? Screen.pixelDensity / Screen.devicePixelRatio : 96 / 25.4
                    property string selectionLayer: ""
                    property string selectionFeature: ""
                    property string selectionValue: ""
                    property string selectionStatus: "No feature selected"
                    property string layerVisibilityPayload: "{}"
                    property int layerVisibilityGeneration: 0
                    property string editAction: ""
                    property string editValue: ""
                    property int editGeneration: 0
                    property string attributePayload: "[]"
                    property int attributePage: 0
                    property int attributePageGeneration: 0
                    property string layerTreePayload: "[]"
                    property string layerSettingsPayload: ""
                    property int layerSettingsGeneration: 0
                    property string layerLabelPayload: "[]"
                    property int layerLabelGeneration: 0
                    property string activeLayer: ""
                    property int activeLayerGeneration: 0
                    property string renderStatus: "Ready"
                    property string mapMetadataPayload: ""
                    property int mapMetadataGeneration: 0
                    property int cancelGeneration: 0
                    property string loadPath: ""
                    property int loadGeneration: 0
                    property string savePath: ""
                    property int saveGeneration: 0
                    x: mapViewport.panX
                    y: mapViewport.panY
                    scale: mapViewport.mapZoom
                    transformOrigin: Item.Center
                    visible: mapViewport.anyLayerVisible()
                }

                Repeater {
                    model: mapLabelModel
                    delegate: Text {
                        z: 2
                        x: mapCanvas.x + mapCanvas.width / 2 + (model.x - 0.5) * mapCanvas.width * mapCanvas.scale - width / 2
                        y: mapCanvas.y + mapCanvas.height / 2 - (model.y - 0.5) * mapCanvas.height * mapCanvas.scale - height / 2
                        rotation: mapViewport.screenRotation(model.rotation)
                        text: model.text
                        textFormat: Text.PlainText
                        font.pixelSize: Math.max(1, model.heightMm * mapCanvas.logicalPixelsPerMm)
                        color: "#17212b"
                        style: Text.Outline
                        styleColor: "#ffffff"
                        visible: {
                            var shown = false;
                            for (var i = 0; i < layerModel.count; ++i) {
                                var row = layerModel.get(i);
                                if (row.name === model.layer) {
                                    shown = row.layerVisible;
                                    break;
                                }
                            }
                            var denominator = mapViewport.currentScaleDenominator();
                            return shown && (model.minScale <= 0 || denominator >= model.minScale) && (model.maxScale <= 0 || denominator <= model.maxScale);
                        }
                    }
                }

                Component.onCompleted: mapViewport.syncLayerVisibility()

                DropArea {
                    anchors.fill: parent
                    z: 10
                    keys: ["text/uri-list"]
                    Rectangle {
                        anchors.fill: parent
                        visible: parent.containsDrag
                        color: "#337ab7"
                        opacity: 0.16
                        border.color: "#2b6cb0"
                        border.width: 2
                        radius: 4
                    }
                    Label {
                        anchors.centerIn: parent
                        visible: parent.containsDrag
                        text: "Drop SHP, GeoPackage, or GeoJSON"
                        color: "#1f4e79"
                        font.bold: true
                    }
                    onDropped: function (drop) {
                        if (drop.urls.length > 0) {
                            // Keep the QUrl intact so requestLoad can use
                            // toLocalFile() on Windows instead of passing a
                            // file:/// URI to the Go/GDAL boundary.
                            mapViewport.requestLoad(drop.urls[0]);
                            drop.acceptProposedAction();
                        }
                    }
                }

                function requestLoad(url) {
                    requestLoadFiles([url]);
                }

                function requestLoadFiles(urls) {
                    var paths = [];
                    for (var i = 0; i < urls.length; ++i) {
                        var path = localPathFromUrl(urls[i]);
                        if (path.length > 0)
                            paths.push(path);
                    }
                    mapCanvas.loadPath = JSON.stringify(paths);
                    mapCanvas.loadGeneration += 1;
                }

                function localPathFromUrl(url) {
                    var path = (url && typeof url.toLocalFile === "function") ? url.toLocalFile() : String(url);
                    if (path.indexOf("file://") === 0) {
                        path = decodeURIComponent(path.substring(7));
                        // file:///C:/... becomes /C:/... after removing the
                        // URI prefix; remove that extra slash on Windows.
                        if (path.length >= 3 && path[0] === "/" && path[2] === ":") {
                            path = path.substring(1);
                        }
                    }
                    return path;
                }

                MouseArea {
                    id: mapMouseArea
                    anchors.fill: parent
                    hoverEnabled: true
                    property real lastX: 0
                    property real lastY: 0

                    onPressed: function (mouse) {
                        lastX = mouse.x;
                        lastY = mouse.y;
                    }
                    onPositionChanged: function (mouse) {
                        mapViewport.cursorX = mouse.x;
                        mapViewport.cursorY = mouse.y;
                        mapViewport.cursorValid = true;
                        if (!pressed)
                            return;
                        mapViewport.panX += mouse.x - lastX;
                        mapViewport.panY += mouse.y - lastY;
                        lastX = mouse.x;
                        lastY = mouse.y;
                        mapViewport.viewportGeneration += 1;
                    }
                    onExited: mapViewport.cursorValid = false
                    onWheel: function (wheel) {
                        var factor = wheel.angleDelta.y > 0 ? 1.15 : 1 / 1.15;
                        mapViewport.mapZoom = Math.max(0.25, Math.min(8.0, mapViewport.mapZoom * factor));
                        mapViewport.viewportGeneration += 1;
                    }
                    onClicked: function (mouse) {
                        if (!mapViewport.mapZoom)
                            return;
                        mapCanvas.clickX = mouse.x;
                        mapCanvas.clickY = mouse.y;
                        mapCanvas.clickGeneration += 1;
                    }
                }

                Timer {
                    interval: 16
                    repeat: true
                    running: true
                    onTriggered: {
                        if (mapViewport.selectedFeature !== mapCanvas.selectionFeature) {
                            mapViewport.editorValue = mapCanvas.selectionValue;
                        }
                        if (mapCanvas.attributePayload !== mapViewport.attributePayloadSeen) {
                            mapViewport.attributePayloadSeen = mapCanvas.attributePayload;
                            attributeModel.clear();
                            var table = JSON.parse(mapCanvas.attributePayload);
                            mapViewport.attributeColumns = table.columns || [];
                            mapViewport.attributePage = table.page || 0;
                            mapViewport.attributePageSize = table.pageSize || 0;
                            mapViewport.attributeTotal = table.total || 0;
                            var rows = table.rows || [];
                            for (var i = 0; i < rows.length; ++i) {
                                attributeModel.append(rows[i]);
                            }
                        }
                        if (mapCanvas.layerTreePayload !== mapViewport.layerTreePayloadSeen) {
                            mapViewport.layerTreePayloadSeen = mapCanvas.layerTreePayload;
                            var layers = JSON.parse(mapCanvas.layerTreePayload);
                            layerModel.clear();
                            for (var layerIndex = 0; layerIndex < layers.length; ++layerIndex) {
                                layerModel.append({
                                    name: layers[layerIndex].name,
                                    displayName: layers[layerIndex].displayName || layers[layerIndex].name,
                                    sourcePath: layers[layerIndex].sourcePath || "",
                                    sourceLayerName: layers[layerIndex].sourceLayerName || "",
                                    sourceEncoding: layers[layerIndex].sourceEncoding || "",
                                    sourceError: layers[layerIndex].sourceError || "",
                                    crs: layers[layerIndex].crs || "",
                                    layerVisible: layers[layerIndex].visible !== false,
                                    style: layers[layerIndex].style || ({}),
                                    labels: layers[layerIndex].labels || ({
                                            enabled: false,
                                            expression: "",
                                            placement: "center",
                                            heightMm: 2.5
                                        })
                                });
                            }
                            if (layers.length > 0)
                                mapViewport.selectLayer(layers[0].name);
                            if (mapViewport.pendingWorkspaceActiveLayer !== "") {
                                for (var activeIndex = 0; activeIndex < layerModel.count; ++activeIndex) {
                                    if (layerModel.get(activeIndex).name === mapViewport.pendingWorkspaceActiveLayer) {
                                        mapViewport.selectLayer(mapViewport.pendingWorkspaceActiveLayer);
                                        break;
                                    }
                                }
                                mapViewport.pendingWorkspaceActiveLayer = "";
                            }
                            attributeLayerTabs.currentIndex = 0;
                            mapViewport.syncLayerVisibility();
                        }
                        if (mapCanvas.layerLabelGeneration !== mapViewport.layerLabelGenerationSeen) {
                            mapViewport.layerLabelGenerationSeen = mapCanvas.layerLabelGeneration;
                            mapLabelModel.clear();
                            var labels = JSON.parse(mapCanvas.layerLabelPayload || "[]");
                            for (var labelIndex = 0; labelIndex < labels.length; ++labelIndex)
                                mapLabelModel.append(labels[labelIndex]);
                        }
                        if (mapCanvas.mapMetadataGeneration !== mapViewport.mapMetadataGenerationSeen) {
                            mapViewport.mapMetadataGenerationSeen = mapCanvas.mapMetadataGeneration;
                            var metadata = ({});
                            try {
                                metadata = JSON.parse(mapCanvas.mapMetadataPayload);
                                if (metadata.bounds && metadata.bounds.length === 4) {
                                    mapViewport.dataBounds = metadata.bounds;
                                    mapViewport.dataCRS = metadata.crs || "";
                                }
                            } catch (error) {
                                mapViewport.dataBounds = [0, 0, 1, 1];
                                mapViewport.dataCRS = "";
                            }
                            mapViewport.panX = 0;
                            mapViewport.panY = 0;
                            mapViewport.mapZoom = 1;
                            if (metadata.view) {
                                var view = metadata.view;
                                var zoom = Math.max(0.0001, Number(view.zoom) || 1);
                                mapViewport.mapZoom = zoom;
                                mapViewport.panX = (0.5 - Number(view.centerX)) * mapCanvas.width * zoom;
                                mapViewport.panY = (Number(view.centerY) - 0.5) * mapCanvas.height * zoom;
                                mapViewport.pendingWorkspaceActiveLayer = view.activeLayer || "";
                                for (var viewLayerIndex = 0; viewLayerIndex < layerModel.count; ++viewLayerIndex) {
                                    if (layerModel.get(viewLayerIndex).name === mapViewport.pendingWorkspaceActiveLayer) {
                                        mapViewport.selectLayer(mapViewport.pendingWorkspaceActiveLayer);
                                        mapViewport.pendingWorkspaceActiveLayer = "";
                                        break;
                                    }
                                }
                            } else {
                                mapViewport.pendingWorkspaceActiveLayer = "";
                            }
                            mapViewport.viewportGeneration += 1;
                        }
                        mapViewport.renderStatus = mapCanvas.renderStatus;
                        mapViewport.selectedLayer = mapCanvas.selectionLayer;
                        mapViewport.selectedFeature = mapCanvas.selectionFeature;
                        mapViewport.selectedStatus = mapCanvas.selectionStatus;
                    }
                }

                Label {
                    anchors.left: parent.left
                    anchors.bottom: parent.bottom
                    anchors.margins: 10
                    text: "Pan: drag · Zoom: wheel"
                    color: "#65717d"
                }
            }
        }

        Frame {
            SplitView.preferredWidth: 260
            Layout.fillHeight: true
            ColumnLayout {
                anchors.fill: parent
                RowLayout {
                    Layout.fillWidth: true
                    Label {
                        text: "Layer attributes"
                        font.bold: true
                        Layout.fillWidth: true
                    }
                    Button {
                        text: "Layer properties"
                        enabled: layerModel.count > 0
                        onClicked: layerSettingsDialog.open()
                    }
                }
                Flickable {
                    Layout.fillWidth: true
                    Layout.preferredHeight: attributeLayerTabs.implicitHeight
                    contentWidth: attributeLayerTabs.implicitWidth
                    clip: true
                    boundsBehavior: Flickable.StopAtBounds
                    TabBar {
                        id: attributeLayerTabs
                        width: implicitWidth
                        Repeater {
                            model: layerModel
                            TabButton {
                                text: typeof model.displayName === "undefined" || model.displayName === "" ? model.name : model.displayName
                                onClicked: mapViewport.selectLayer(model.name)
                            }
                        }
                    }
                }
                Label {
                    text: "Selected feature"
                    font.bold: true
                    visible: mapViewport.selectedFeature !== ""
                }
                Label {
                    text: mapViewport.selectedStatus
                    wrapMode: Text.WordWrap
                    visible: mapViewport.selectedFeature !== ""
                    color: mapViewport.selectedFeature ? "#2e7d32" : "#65717d"
                }
                TextField {
                    id: propertyEditor
                    Layout.fillWidth: true
                    visible: mapViewport.selectedFeature !== ""
                    placeholderText: "Feature name"
                    text: mapViewport.editorValue
                    onTextChanged: if (activeFocus)
                        mapViewport.editorValue = text
                }
                RowLayout {
                    visible: mapViewport.selectedFeature !== ""
                    Button {
                        text: "Save"
                        onClicked: {
                            mapCanvas.editAction = "commit";
                            mapCanvas.editValue = mapViewport.editorValue;
                            mapCanvas.editGeneration += 1;
                        }
                    }
                    Button {
                        text: "Cancel"
                        onClicked: {
                            mapCanvas.editAction = "rollback";
                            mapCanvas.editValue = "";
                            mapCanvas.editGeneration += 1;
                            mapViewport.editorValue = mapCanvas.selectionValue;
                        }
                    }
                }
                GridLayout {
                    columns: 2
                    visible: mapViewport.selectedFeature !== ""
                    Label {
                        text: "Layer"
                        font.bold: true
                    }
                    Label {
                        text: mapViewport.selectedLayer
                    }
                    Label {
                        text: "Feature"
                        font.bold: true
                    }
                    Label {
                        text: mapViewport.selectedFeature
                    }
                    Label {
                        text: "Editable"
                        font.bold: true
                    }
                    Label {
                        text: "Yes"
                        color: "#2e7d32"
                    }
                }
                RowLayout {
                    Layout.fillWidth: true
                    visible: mapViewport.attributeTotal > mapViewport.attributePageSize && mapViewport.attributePageSize > 0
                    Button {
                        text: "Previous"
                        enabled: mapViewport.attributePage > 0
                        onClicked: mapViewport.requestAttributePage(mapViewport.attributePage - 1)
                    }
                    Label {
                        text: "Page " + (mapViewport.attributePage + 1) + " / " + Math.max(1, Math.ceil(mapViewport.attributeTotal / mapViewport.attributePageSize))
                        Layout.fillWidth: true
                        horizontalAlignment: Text.AlignHCenter
                    }
                    Button {
                        text: "Next"
                        enabled: mapViewport.attributePage + 1 < Math.ceil(mapViewport.attributeTotal / mapViewport.attributePageSize)
                        onClicked: mapViewport.requestAttributePage(mapViewport.attributePage + 1)
                    }
                }
                ListView {
                    Layout.fillWidth: true
                    Layout.fillHeight: true
                    visible: layerModel.count > 0 && mapViewport.attributeTotal > 0
                    model: attributeModel
                    clip: true
                    spacing: 6
                    delegate: Rectangle {
                        id: featureCard
                        width: ListView.view.width
                        property var rowValues: model.values || ({})
                        property var rowFeatureID: model.featureId
                        implicitHeight: cardLayout.implicitHeight + 16
                        color: "#ffffff"
                        border.color: "#d9e0e6"
                        radius: 4
                        ColumnLayout {
                            id: cardLayout
                            anchors.fill: parent
                            anchors.margins: 8
                            spacing: 4
                            Label {
                                text: "Feature " + String(featureCard.rowFeatureID)
                                font.bold: true
                                color: "#45515c"
                            }
                            Repeater {
                                model: mapViewport.attributeColumns
                                delegate: RowLayout {
                                    required property string modelData
                                    width: parent.width
                                    spacing: 8
                                    Label {
                                        text: modelData
                                        Layout.preferredWidth: 82
                                        Layout.minimumWidth: 82
                                        color: "#65717d"
                                        elide: Text.ElideRight
                                    }
                                    Label {
                                        Layout.fillWidth: true
                                        text: {
                                            var value = featureCard.rowValues[modelData];
                                            return value === undefined || value === null ? "" : String(value);
                                        }
                                        wrapMode: Text.Wrap
                                        textFormat: Text.PlainText
                                    }
                                }
                            }
                        }
                    }
                }
                Label {
                    Layout.fillWidth: true
                    visible: layerModel.count > 0 && mapViewport.attributeTotal === 0
                    text: "No attribute records in this layer"
                    color: "#65717d"
                    horizontalAlignment: Text.AlignHCenter
                    padding: 16
                }
                Item {
                    Layout.fillHeight: true
                }
            }
        }
    }

    footer: ToolBar {
        RowLayout {
            anchors.fill: parent
            anchors.leftMargin: 12
            anchors.rightMargin: 12
            BusyIndicator {
                objectName: "loadBusyIndicator"
                running: mapViewport.renderStatus.toLowerCase().indexOf("loading") >= 0
                visible: running
                implicitWidth: 22
                implicitHeight: 22
            }
            Label {
                objectName: "renderStatusLabel"
                text: mapViewport.renderStatus
                color: {
                    var status = mapViewport.renderStatus.toLowerCase();
                    return status.indexOf("failed") >= 0 || status.indexOf("error") >= 0 || status.indexOf("invalid") >= 0 ? "#b42318" : "#2e7d32";
                }
            }
            TextField {
                id: coordinateXInput
                Layout.preferredWidth: 115
                placeholderText: "X coordinate"
            }
            TextField {
                id: coordinateYInput
                Layout.preferredWidth: 115
                placeholderText: "Y coordinate"
                onAccepted: mapViewport.goToCoordinate()
            }
            Button {
                text: "Go"
                onClicked: mapViewport.goToCoordinate()
            }
            Label {
                id: coordinateNavigationStatus
                color: "#65717d"
            }
            Button {
                text: "Cancel loading/render"
                enabled: mapViewport.renderStatus.toLowerCase().indexOf("loading") >= 0
                onClicked: mapCanvas.cancelGeneration += 1
            }
            Item {
                Layout.fillWidth: true
            }
            Label {
                text: "Layers: " + layerModel.count + "  ·  " + (mapViewport.dataCRS || "CRS unknown") + "  ·  " + mapViewport.currentMapCoordinate()
                color: "#65717d"
            }
            Label {
                text: mapViewport.currentScaleText()
                color: "#65717d"
            }
        }
    }

    Dialog {
        id: layerSettingsDialog
        objectName: "layerSettingsDialog"
        modal: true
        title: "Layer properties"
        width: 500
        height: Math.max(320, Math.min(760, rootWindow.height - 120))
        standardButtons: Dialog.Apply | Dialog.Cancel
        property string targetLayerName: ""
        property string originalSourcePath: ""
        property string originalSourceLayerName: ""
        property string originalSourceEncoding: ""

        function loadLayer() {
            if (layerModel.count === 0)
                return;
            var rowIndex = Math.max(0, Math.min(attributeLayerTabs.currentIndex, layerModel.count - 1));
            var layer = layerModel.get(rowIndex);
            targetLayerName = layer.name;
            displayNameField.text = layer.displayName || layer.name;
            sourcePathField.text = layer.sourcePath || "";
            originalSourcePath = sourcePathField.text.trim();
            sourceStatusLabel.text = layer.sourceError || "";
            sourceLayerField.text = layer.sourceLayerName || layer.name;
            originalSourceLayerName = sourceLayerField.text.trim();
            sourceEncodingField.currentIndex = -1;
            sourceEncodingField.editText = layer.sourceEncoding || "";
            originalSourceEncoding = sourceEncodingField.editText.trim();
            visibleField.checked = layer.layerVisible;
            var style = layer.style || ({});
            pointColorField.text = style.pointColor || "#d1495b";
            lineColorField.text = style.lineColor || "#2b6cb0";
            polygonColorField.text = style.polygonColor || "#75b798";
            pointSizeField.text = String(style.pointSizeMm || 2.2);
            lineWidthField.text = String(style.lineWidthMm || 0.45);
            fillOpacityField.text = String(style.fillOpacity === undefined ? 0.35 : style.fillOpacity);
            var labels = layer.labels || ({});
            labelsEnabledField.checked = labels.enabled === true;
            labelExpressionField.text = labels.expression || "";
            labelPlacementField.currentIndex = Math.max(0, ["center", "center-rotated", "free-angle"].indexOf(labels.placement || "center"));
            labelRotationField.text = labels.rotationField || "";
            labelHeightField.text = String(labels.heightMm || 2.5);
            labelMinScaleField.text = String(labels.minScale || "");
            labelMaxScaleField.text = String(labels.maxScale || "");
            labelRuleField.text = labels.rule || "";
            labelLuaField.text = labels.luaScript || "";
        }

        function submitLayer() {
            var index = -1;
            for (var i = 0; i < layerModel.count; ++i) {
                if (layerModel.get(i).name === targetLayerName) {
                    index = i;
                    break;
                }
            }
            if (index < 0)
                return;
            var settings = {
                name: targetLayerName,
                displayName: displayNameField.text.trim(),
                sourcePath: sourcePathField.text,
                sourceLayerName: sourceLayerField.text,
                sourceEncoding: sourceEncodingField.editText.trim(),
                visible: visibleField.checked,
                style: {
                    pointColor: pointColorField.text,
                    lineColor: lineColorField.text,
                    polygonColor: polygonColorField.text,
                    pointSizeMm: Number(pointSizeField.text),
                    lineWidthMm: Number(lineWidthField.text),
                    fillOpacity: Number(fillOpacityField.text)
                },
                labels: {
                    enabled: labelsEnabledField.checked,
                    expression: labelExpressionField.text,
                    luaScript: labelLuaField.text,
                    placement: ["center", "center-rotated", "free-angle"][labelPlacementField.currentIndex],
                    rotationField: labelRotationField.text,
                    heightMm: Number(labelHeightField.text),
                    minScale: Number(labelMinScaleField.text || 0),
                    maxScale: Number(labelMaxScaleField.text || 0),
                    rule: labelRuleField.text
                }
            };
            // Keep the visible layer tree authoritative. The runtime validates
            // and applies the request, then publishes a refreshed tree; an
            // optimistic edit here would make rejected settings look saved.
            mapCanvas.layerSettingsPayload = JSON.stringify(settings);
            mapCanvas.layerSettingsGeneration += 1;
        }

        onOpened: loadLayer()
        onApplied: submitLayer()
        onAccepted: submitLayer()

        contentItem: ScrollView {
            clip: true
            ColumnLayout {
                width: parent.width
                spacing: 8
                Label {
                    text: "Project layer"
                    font.bold: true
                }
                TextField {
                    id: displayNameField
                    objectName: "displayNameField"
                    Layout.fillWidth: true
                    placeholderText: "Display name"
                }
                RowLayout {
                    Layout.fillWidth: true
                    TextField {
                        id: sourcePathField
                        objectName: "sourcePathField"
                        Layout.fillWidth: true
                        placeholderText: "Original source path"
                    }
                    Button {
                        text: "Browse…"
                        onClicked: relinkFileDialog.open()
                    }
                }
                Label {
                    id: sourceStatusLabel
                    Layout.fillWidth: true
                    visible: text !== ""
                    color: "#b45309"
                    wrapMode: Text.Wrap
                    textFormat: Text.PlainText
                }
                RowLayout {
                    Layout.fillWidth: true
                    ColumnLayout {
                        Layout.fillWidth: true
                        Label {
                            text: "Layer in source"
                            color: "#65717d"
                        }
                        TextField {
                            id: sourceLayerField
                            objectName: "sourceLayerField"
                            Layout.fillWidth: true
                            placeholderText: "Internal layer name"
                        }
                    }
                    ColumnLayout {
                        Layout.preferredWidth: 180
                        Label {
                            text: "Shapefile encoding"
                            color: "#65717d"
                        }
                        ComboBox {
                            id: sourceEncodingField
                            objectName: "sourceEncodingField"
                            Layout.fillWidth: true
                            editable: true
                            model: ["", "UTF-8", "CP949", "EUC-KR", "ISO-8859-1"]
                            displayText: currentText === "" ? "Auto encoding" : currentText
                        }
                    }
                }
                Label {
                    objectName: "sourceChangeWarning"
                    Layout.fillWidth: true
                    visible: sourcePathField.text.trim() !== layerSettingsDialog.originalSourcePath || sourceLayerField.text.trim() !== layerSettingsDialog.originalSourceLayerName || sourceEncodingField.editText.trim() !== layerSettingsDialog.originalSourceEncoding
                    text: "Changing the source path, internal layer, or encoding reloads this layer. Save unsaved feature edits in it first."
                    color: "#9a6700"
                    wrapMode: Text.Wrap
                    textFormat: Text.PlainText
                }
                CheckBox {
                    id: visibleField
                    objectName: "visibleField"
                    text: "Layer visible"
                }
                Label {
                    text: "Symbol"
                    font.bold: true
                    topPadding: 8
                }
                GridLayout {
                    Layout.fillWidth: true
                    columns: 2
                    Label {
                        text: "Point color"
                    }
                    TextField {
                        id: pointColorField
                        objectName: "pointColorField"
                        Layout.fillWidth: true
                    }
                    Label {
                        text: "Point size (mm)"
                    }
                    TextField {
                        id: pointSizeField
                        objectName: "pointSizeField"
                        Layout.fillWidth: true
                        inputMethodHints: Qt.ImhFormattedNumbersOnly
                    }
                    Label {
                        text: "Line color"
                    }
                    TextField {
                        id: lineColorField
                        objectName: "lineColorField"
                        Layout.fillWidth: true
                    }
                    Label {
                        text: "Line width (mm)"
                    }
                    TextField {
                        id: lineWidthField
                        objectName: "lineWidthField"
                        Layout.fillWidth: true
                        inputMethodHints: Qt.ImhFormattedNumbersOnly
                    }
                    Label {
                        text: "Polygon color"
                    }
                    TextField {
                        id: polygonColorField
                        objectName: "polygonColorField"
                        Layout.fillWidth: true
                    }
                    Label {
                        text: "Fill opacity (0–1)"
                    }
                    TextField {
                        id: fillOpacityField
                        objectName: "fillOpacityField"
                        Layout.fillWidth: true
                        inputMethodHints: Qt.ImhFormattedNumbersOnly
                    }
                }
                Label {
                    text: "Labels"
                    font.bold: true
                    topPadding: 8
                }
                CheckBox {
                    id: labelsEnabledField
                    objectName: "labelsEnabledField"
                    text: "Show labels"
                }
                Label {
                    text: "Label field / template"
                }
                TextField {
                    id: labelExpressionField
                    objectName: "labelExpressionField"
                    Layout.fillWidth: true
                    placeholderText: "e.g. ${name}"
                }
                RowLayout {
                    Layout.fillWidth: true
                    Label {
                        text: "Placement"
                    }
                    ComboBox {
                        id: labelPlacementField
                        objectName: "labelPlacementField"
                        Layout.fillWidth: true
                        model: ["Center", "Center + rotation", "Free angle"]
                    }
                }
                TextField {
                    id: labelRotationField
                    objectName: "labelRotationField"
                    Layout.fillWidth: true
                    placeholderText: "Rotation field (optional)"
                }
                GridLayout {
                    Layout.fillWidth: true
                    columns: 2
                    Label {
                        text: "Text height (mm)"
                    }
                    TextField {
                        id: labelHeightField
                        objectName: "labelHeightField"
                        Layout.fillWidth: true
                        inputMethodHints: Qt.ImhFormattedNumbersOnly
                    }
                    Label {
                        text: "Minimum scale denominator"
                    }
                    TextField {
                        id: labelMinScaleField
                        objectName: "labelMinScaleField"
                        Layout.fillWidth: true
                        inputMethodHints: Qt.ImhFormattedNumbersOnly
                    }
                    Label {
                        text: "Maximum scale denominator"
                    }
                    TextField {
                        id: labelMaxScaleField
                        objectName: "labelMaxScaleField"
                        Layout.fillWidth: true
                        inputMethodHints: Qt.ImhFormattedNumbersOnly
                    }
                }
                Label {
                    text: "Label display rule (Lua expression returning true/false)"
                    wrapMode: Text.WordWrap
                }
                TextField {
                    id: labelRuleField
                    objectName: "labelRuleField"
                    Layout.fillWidth: true
                    placeholderText: "e.g. return feature.class == \"primary\""
                }
                Label {
                    text: "Lua label script (return a string using feature properties)"
                    wrapMode: Text.WordWrap
                }
                TextArea {
                    id: labelLuaField
                    objectName: "labelLuaField"
                    Layout.fillWidth: true
                    Layout.preferredHeight: 120
                    placeholderText: "Return a label string from feature properties"
                    wrapMode: TextEdit.Wrap
                }
            }
        }
    }

    Platform.FileDialog {
        id: fileDialog
        title: "Add vector files as layers"
        fileMode: Platform.FileDialog.OpenFiles
        nameFilters: ["Vector files (*.shp *.gpkg *.geojson *.json)", "All files (*)"]
        onAccepted: mapViewport.requestLoadFiles(files)
    }

    Platform.FileDialog {
        id: saveDialog
        title: "Save all layers as GeoPackage"
        fileMode: Platform.FileDialog.SaveFile
        nameFilters: ["GeoPackage (*.gpkg)"]
        onAccepted: {
            var path = mapViewport.localPathFromUrl(file);
            if (!path.toLowerCase().endsWith(".gpkg"))
                path += ".gpkg";
            mapCanvas.savePath = path;
            mapCanvas.saveGeneration += 1;
        }
    }

    Platform.FileDialog {
        id: workspaceDialog
        title: "Save GoGIS workspace"
        fileMode: Platform.FileDialog.SaveFile
        nameFilters: ["GoGIS workspace (*.gogis)"]
        onAccepted: {
            var path = mapViewport.localPathFromUrl(file);
            if (!path.toLowerCase().endsWith(".gogis"))
                path += ".gogis";
            mapCanvas.savePath = path;
            mapCanvas.saveGeneration += 1;
        }
    }

    Platform.FileDialog {
        id: workspaceOpenDialog
        title: "Open GoGIS workspace"
        fileMode: Platform.FileDialog.OpenFile
        nameFilters: ["GoGIS workspace (*.gogis)"]
        onAccepted: mapViewport.requestLoad(file)
    }

    Platform.FileDialog {
        id: relinkFileDialog
        title: "Select original layer source"
        fileMode: Platform.FileDialog.OpenFile
        nameFilters: ["Vector files (*.shp *.gpkg *.geojson *.json)", "All files (*)"]
        onAccepted: sourcePathField.text = mapViewport.localPathFromUrl(file)
    }
}
