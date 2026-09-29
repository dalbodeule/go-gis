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
                text: "Open file"
                onClicked: fileDialog.open()
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
                property string activeLayer: ""
                property var attributeColumns: []
                property string renderStatus: "Ready"

                ListModel { id: attributeModel }

                function formatAttributeValues(values) {
                    var parts = []
                    for (var i = 0; i < attributeColumns.length; ++i) {
                        var key = attributeColumns[i]
                        var value = values[key]
                        parts.push(key + "=" + (value === undefined || value === null ? "" : String(value)))
                    }
                    return parts.join(" · ")
                }

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
                    property string layerTreePayload: "[]"
                    property string activeLayer: ""
                    property int activeLayerGeneration: 0
                    property string renderStatus: "Ready"
                    property int cancelGeneration: 0
                    property string loadPath: ""
                    property int loadGeneration: 0
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
                            mapViewport.requestLoad(drop.urls[0].toString())
                            drop.acceptProposedAction()
                        }
                    }
                }

                function requestLoad(url) {
                    var path = (url && url.toLocalFile) ? url.toLocalFile() : String(url)
                    if (path.indexOf("file://") === 0) path = decodeURIComponent(path.substring(7))
                    mapCanvas.loadPath = path
                    mapCanvas.loadGeneration += 1
                }

                MouseArea {
                    id: mapMouseArea
                    anchors.fill: parent
                    hoverEnabled: true
                    property real lastX: 0
                    property real lastY: 0

                    onPressed: {
                        lastX = mouseX
                        lastY = mouseY
                    }
                    onPositionChanged: {
                        if (!pressed) return
                        mapViewport.panX += mouseX - lastX
                        mapViewport.panY += mouseY - lastY
                        lastX = mouseX
                        lastY = mouseY
                        mapViewport.viewportGeneration += 1
                    }
                    onWheel: {
                        var factor = wheel.angleDelta.y > 0 ? 1.15 : 1 / 1.15
                        mapViewport.mapZoom = Math.max(0.25, Math.min(8.0, mapViewport.mapZoom * factor))
                        mapViewport.viewportGeneration += 1
                    }
                    onClicked: {
                        if (!mapViewport.mapZoom) return
                        mapCanvas.clickX = mouseX
                        mapCanvas.clickY = mouseY
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
                                layerModel.append({name: layers[layerIndex].name, layerVisible: true})
                            }
                            if (layers.length > 0) mapViewport.selectLayer(layers[0].name)
                            mapViewport.syncLayerVisibility()
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
                Label { text: "Properties"; font.bold: true }
                Label {
                    text: mapViewport.selectedStatus
                    wrapMode: Text.WordWrap
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
                Label {
                    text: "Attributes"
                    font.bold: true
                    visible: attributeModel.count > 0
                }
                ListView {
                    Layout.fillWidth: true
                    Layout.fillHeight: true
                    visible: attributeModel.count > 0
                    model: attributeModel
                    clip: true
                    delegate: RowLayout {
                        width: ListView.view.width
                        spacing: 6
                        Label { text: String(featureId); Layout.preferredWidth: 92; elide: Text.ElideRight }
                        Label {
                            text: mapViewport.formatAttributeValues(values)
                            Layout.fillWidth: true
                            elide: Text.ElideRight
                        }
                    }
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
            Button {
                text: "Cancel render"
                enabled: mapViewport.renderStatus.indexOf("Loading") === 0
                onClicked: mapCanvas.cancelGeneration += 1
            }
            Item { Layout.fillWidth: true }
            Label {
                text: "Layers: " + layerModel.count +
                      " / generation " +
                      (mapViewport ? mapViewport.viewportGeneration : 0) +
                      " / zoom " + (mapViewport ? mapViewport.mapZoom.toFixed(2) : "1.00")
                color: "#65717d"
            }
        }
    }

    Platform.FileDialog {
        id: fileDialog
        title: "Open vector layer"
        fileMode: Platform.FileDialog.OpenFile
        nameFilters: ["Vector files (*.shp *.gpkg *.geojson *.json)", "All files (*)"]
        onAccepted: mapViewport.requestLoad(file)
    }
}
