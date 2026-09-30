import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import Qt.labs.platform as Platform
import GoGIS 1.0

ApplicationWindow {
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
            Label { text: "GoGIS"; font.bold: true; font.pixelSize: 18 }
            Label { text: "Milestone B / Qt Quick"; color: "#65717d" }
            Item { Layout.fillWidth: true }
            Button {
                text: "Add vector files"
                onClicked: fileDialog.open()
            }
            Button {
                text: "Save GeoPackage"
                onClicked: saveDialog.open()
            }
            Label { text: "QSGGeometryNode: active demo batch"; color: "#65717d" }
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
                Label { text: "Layers"; font.bold: true }
                ListView {
                    id: layerList
                    Layout.fillWidth: true
                    Layout.fillHeight: true
                    model: ListModel {
                        id: layerModel
                        ListElement { name: "roads"; layerVisible: true }
                        ListElement { name: "buildings"; layerVisible: true }
                        ListElement { name: "labels"; layerVisible: true }
                    }
                    delegate: CheckDelegate {
                        width: ListView.view.width
                        text: name
                        checked: layerVisible
                        onToggled: {
                            layerModel.setProperty(index, "layerVisible", checked)
                            mapViewport.syncLayerVisibility()
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
                property string layerTreePayloadSeen: ""
                property int mapMetadataGenerationSeen: -1
                property string activeLayer: ""
                property var dataBounds: [0, 0, 1, 1]
                property string dataCRS: ""
                property real cursorX: 0
                property real cursorY: 0
                property bool cursorValid: false
                property var attributeColumns: []
                property int attributePage: 0
                property int attributePageSize: 0
                property int attributeTotal: 0
                property string renderStatus: "Ready"

                ListModel { id: attributeModel }

                function anyLayerVisible() {
                    for (var i = 0; i < layerModel.count; ++i) {
                        if (layerModel.get(i).layerVisible) return true
                    }
                    return false
                }

                function selectLayer(name) {
                    activeLayer = name
                    mapCanvas.activeLayer = name
                    mapCanvas.activeLayerGeneration += 1
                    for (var i = 0; i < layerModel.count; ++i) {
                        if (layerModel.get(i).name === name) {
                            attributeLayerTabs.currentIndex = i
                            break
                        }
                    }
                }

                function currentMapCoordinate() {
                    if (!cursorValid || mapCanvas.width <= 0 || mapCanvas.height <= 0)
                        return "—"
                    var bounds = dataBounds
                    var zoom = Math.max(0.0001, mapZoom)
                    var nx = 0.5 + (cursorX - mapCanvas.width / 2 - panX) / (mapCanvas.width * zoom)
                    var ny = 0.5 - (cursorY - mapCanvas.height / 2 - panY) / (mapCanvas.height * zoom)
                    var x = bounds[0] + nx * (bounds[2] - bounds[0])
                    var y = bounds[1] + ny * (bounds[3] - bounds[1])
                    var digits = dataCRS.toUpperCase() === "EPSG:4326" ? 6 : 2
                    return "X " + Number(x).toFixed(digits) + "  Y " + Number(y).toFixed(digits)
                }

                function currentScaleText() {
                    if (mapCanvas.width <= 0 || mapZoom <= 0)
                        return "Scale —"
                    var bounds = dataBounds
                    var xUnitsPerPixel = (bounds[2] - bounds[0]) / (mapCanvas.width * mapZoom)
                    var metersPerUnit = 1.0
                    if (dataCRS.toUpperCase() === "EPSG:4326") {
                        var centerLatitude = (bounds[1] + bounds[3]) / 2
                        metersPerUnit = 111319.49 * Math.max(0.01, Math.cos(centerLatitude * Math.PI / 180))
                    }
                    var denominator = xUnitsPerPixel * metersPerUnit * 96 / 0.0254
                    if (!isFinite(denominator) || denominator <= 0)
                        return "Scale —"
                    return "Approx. 1:" + Math.max(1, Math.round(denominator)).toLocaleString()
                }

                function goToCoordinate() {
                    var x = Number(coordinateXInput.text)
                    var y = Number(coordinateYInput.text)
                    if (!isFinite(x) || !isFinite(y) || dataBounds.length < 4 ||
                            dataBounds[2] === dataBounds[0] || dataBounds[3] === dataBounds[1]) {
                        coordinateNavigationStatus.text = "Enter valid map coordinates"
                        return
                    }
                    var nx = (x - dataBounds[0]) / (dataBounds[2] - dataBounds[0])
                    var ny = (y - dataBounds[1]) / (dataBounds[3] - dataBounds[1])
                    panX = (0.5 - nx) * mapCanvas.width * mapZoom
                    panY = (ny - 0.5) * mapCanvas.height * mapZoom
                    viewportGeneration += 1
                    coordinateNavigationStatus.text = "Centered at " + x + ", " + y
                }

                function requestAttributePage(page) {
                    var pageSize = attributePageSize > 0 ? attributePageSize : 200
                    var pageCount = Math.max(1, Math.ceil(attributeTotal / pageSize))
                    page = Math.max(0, Math.min(pageCount - 1, page))
                    if (page === attributePage) return
                    mapCanvas.attributePage = page
                    mapCanvas.attributePageGeneration += 1
                }

                function syncLayerVisibility() {
                    var visibility = {}
                    for (var i = 0; i < layerModel.count; ++i) {
                        var layer = layerModel.get(i)
                        visibility[layer.name] = layer.layerVisible
                    }
                    mapCanvas.layerVisibilityPayload = JSON.stringify(visibility)
                    mapCanvas.layerVisibilityGeneration += 1
                }

                MapCanvas {
                    id: mapCanvas
                    anchors.fill: parent
                    clip: true
                    property real clickX: 0
                    property real clickY: 0
                    property int clickGeneration: 0
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
                    onDropped: function(drop) {
                        if (drop.urls.length > 0) {
                            // Keep the QUrl intact so requestLoad can use
                            // toLocalFile() on Windows instead of passing a
                            // file:/// URI to the Go/GDAL boundary.
                            mapViewport.requestLoad(drop.urls[0])
                            drop.acceptProposedAction()
                        }
                    }
                }

                function requestLoad(url) {
                    requestLoadFiles([url])
                }

                function requestLoadFiles(urls) {
                    var paths = []
                    for (var i = 0; i < urls.length; ++i) {
                        var path = localPathFromUrl(urls[i])
                        if (path.length > 0) paths.push(path)
                    }
                    mapCanvas.loadPath = JSON.stringify(paths)
                    mapCanvas.loadGeneration += 1
                }

                function localPathFromUrl(url) {
                    var path = (url && typeof url.toLocalFile === "function") ? url.toLocalFile() : String(url)
                    if (path.indexOf("file://") === 0) {
                        path = decodeURIComponent(path.substring(7))
                        // file:///C:/... becomes /C:/... after removing the
                        // URI prefix; remove that extra slash on Windows.
                        if (path.length >= 3 && path[0] === "/" && path[2] === ":") {
                            path = path.substring(1)
                        }
                    }
                    return path
                }

                MouseArea {
                    id: mapMouseArea
                    anchors.fill: parent
                    hoverEnabled: true
                    property real lastX: 0
                    property real lastY: 0

                    onPressed: function(mouse) {
                        lastX = mouse.x
                        lastY = mouse.y
                    }
                    onPositionChanged: function(mouse) {
                        mapViewport.cursorX = mouse.x
                        mapViewport.cursorY = mouse.y
                        mapViewport.cursorValid = true
                        if (!pressed) return
                        mapViewport.panX += mouse.x - lastX
                        mapViewport.panY += mouse.y - lastY
                        lastX = mouse.x
                        lastY = mouse.y
                        mapViewport.viewportGeneration += 1
                    }
                    onExited: mapViewport.cursorValid = false
                    onWheel: function(wheel) {
                        var factor = wheel.angleDelta.y > 0 ? 1.15 : 1 / 1.15
                        mapViewport.mapZoom = Math.max(0.25, Math.min(8.0, mapViewport.mapZoom * factor))
                        mapViewport.viewportGeneration += 1
                    }
                    onClicked: function(mouse) {
                        if (!mapViewport.mapZoom) return
                        mapCanvas.clickX = mouse.x
                        mapCanvas.clickY = mouse.y
                        mapCanvas.clickGeneration += 1
                    }
                }

                Timer {
                    interval: 16
                    repeat: true
                    running: true
                    onTriggered: {
                        if (mapViewport.selectedFeature !== mapCanvas.selectionFeature) {
                            mapViewport.editorValue = mapCanvas.selectionValue
                        }
                        if (mapCanvas.attributePayload !== mapViewport.attributePayloadSeen) {
                            mapViewport.attributePayloadSeen = mapCanvas.attributePayload
                            attributeModel.clear()
                            var table = JSON.parse(mapCanvas.attributePayload)
                            mapViewport.attributeColumns = table.columns || []
                            mapViewport.attributePage = table.page || 0
                            mapViewport.attributePageSize = table.pageSize || 0
                            mapViewport.attributeTotal = table.total || 0
                            var rows = table.rows || []
                            for (var i = 0; i < rows.length; ++i) {
                                attributeModel.append(rows[i])
                            }
                        }
                        if (mapCanvas.layerTreePayload !== mapViewport.layerTreePayloadSeen) {
                            mapViewport.layerTreePayloadSeen = mapCanvas.layerTreePayload
                            var layers = JSON.parse(mapCanvas.layerTreePayload)
                            layerModel.clear()
                            for (var layerIndex = 0; layerIndex < layers.length; ++layerIndex) {
                                layerModel.append({name: layers[layerIndex].name,
                                                   layerVisible: layers[layerIndex].visible !== false})
                            }
                            if (layers.length > 0) mapViewport.selectLayer(layers[0].name)
                            attributeLayerTabs.currentIndex = 0
                            mapViewport.syncLayerVisibility()
                        }
                        if (mapCanvas.mapMetadataGeneration !== mapViewport.mapMetadataGenerationSeen) {
                            mapViewport.mapMetadataGenerationSeen = mapCanvas.mapMetadataGeneration
                            try {
                                var metadata = JSON.parse(mapCanvas.mapMetadataPayload)
                                if (metadata.bounds && metadata.bounds.length === 4) {
                                    mapViewport.dataBounds = metadata.bounds
                                    mapViewport.dataCRS = metadata.crs || ""
                                }
                            } catch (error) {
                                mapViewport.dataBounds = [0, 0, 1, 1]
                                mapViewport.dataCRS = ""
                            }
                            mapViewport.panX = 0
                            mapViewport.panY = 0
                            mapViewport.mapZoom = 1
                            mapViewport.viewportGeneration += 1
                        }
                        mapViewport.renderStatus = mapCanvas.renderStatus
                        mapViewport.selectedLayer = mapCanvas.selectionLayer
                        mapViewport.selectedFeature = mapCanvas.selectionFeature
                        mapViewport.selectedStatus = mapCanvas.selectionStatus
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
                Label { text: "Layer attributes"; font.bold: true }
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
                                text: model.name
                                onClicked: mapViewport.selectLayer(model.name)
                            }
                        }
                    }
                }
                Label { text: "Selected feature"; font.bold: true; visible: mapViewport.selectedFeature !== "" }
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
                    onTextChanged: if (activeFocus) mapViewport.editorValue = text
                }
                RowLayout {
                    visible: mapViewport.selectedFeature !== ""
                    Button {
                        text: "Save"
                        onClicked: {
                            mapCanvas.editAction = "commit"
                            mapCanvas.editValue = mapViewport.editorValue
                            mapCanvas.editGeneration += 1
                        }
                    }
                    Button {
                        text: "Cancel"
                        onClicked: {
                            mapCanvas.editAction = "rollback"
                            mapCanvas.editValue = ""
                            mapCanvas.editGeneration += 1
                            mapViewport.editorValue = mapCanvas.selectionValue
                        }
                    }
                }
                GridLayout {
                    columns: 2
                    visible: mapViewport.selectedFeature !== ""
                    Label { text: "Layer"; font.bold: true }
                    Label { text: mapViewport.selectedLayer }
                    Label { text: "Feature"; font.bold: true }
                    Label { text: mapViewport.selectedFeature }
                    Label { text: "Editable"; font.bold: true }
                    Label { text: "Yes"; color: "#2e7d32" }
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
                        text: "Page " + (mapViewport.attributePage + 1) + " / " +
                              Math.max(1, Math.ceil(mapViewport.attributeTotal / mapViewport.attributePageSize))
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
                                            var value = featureCard.rowValues[modelData]
                                            return value === undefined || value === null ? "" : String(value)
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
                Item { Layout.fillHeight: true }
            }
        }
    }

    footer: ToolBar {
        RowLayout {
            anchors.fill: parent
            anchors.leftMargin: 12
            anchors.rightMargin: 12
            Label { text: mapViewport.renderStatus; color: "#2e7d32" }
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
            Button { text: "Go"; onClicked: mapViewport.goToCoordinate() }
            Label { id: coordinateNavigationStatus; color: "#65717d" }
            Button {
                text: "Cancel render"
                enabled: mapViewport.renderStatus.indexOf("Loading") === 0
                onClicked: mapCanvas.cancelGeneration += 1
            }
            Item { Layout.fillWidth: true }
            Label {
                text: "Layers: " + layerModel.count +
                      "  ·  " + (mapViewport.dataCRS || "CRS unknown") +
                      "  ·  " + mapViewport.currentMapCoordinate()
                color: "#65717d"
            }
            Label {
                text: mapViewport.currentScaleText()
                color: "#65717d"
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
            var path = mapViewport.localPathFromUrl(file)
            if (!path.toLowerCase().endsWith(".gpkg")) path += ".gpkg"
            mapCanvas.savePath = path
            mapCanvas.saveGeneration += 1
        }
    }
}
