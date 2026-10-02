import QtQuick
import QtQuick.Window

Item {
    property real clickX: 0
    property real clickY: 0
    property real logicalPixelsPerMm: Screen.pixelDensity > 0 && Screen.devicePixelRatio > 0 ? Screen.pixelDensity / Screen.devicePixelRatio : 96 / 25.4
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
    property string layerSettingsPayload: ""
    property int layerSettingsGeneration: 0
    property string layerLabelPayload: "[]"
    property int layerLabelGeneration: 0
    property string vertexHandlePayload: "[]"
    property int vertexHandleGeneration: 0
    property string activeLayer: ""
    property int activeLayerGeneration: 0
    property string renderStatus: "Ready"
    property string diagnosticLogPayload: "[]"
    property double processMemoryBytes: 0
    property double goHeapBytes: 0
    property int processMemoryKind: 0
    property bool memoryStatusAvailable: false
    property string mapMetadataPayload: "{}"
    property int mapMetadataGeneration: 0
    property int cancelGeneration: 0
    property string loadPath: ""
    property string loadRequestJournal: "[]"
    property int loadGeneration: 0
    property int loadCapturedGeneration: 0
    property string savePath: ""
    property string saveProfile: ""
    property int saveGeneration: 0
}
