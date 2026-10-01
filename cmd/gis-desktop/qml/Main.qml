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
    property bool vertexEditMode: false
    property string language: typeof appLanguage === "undefined" ? "en" : appLanguage
    property string versionText: typeof appVersion === "undefined" ? "0.1.0-dev" : appVersion
    property string runtimeText: typeof appRuntime === "undefined" ? "Go runtime" : appRuntime
    property string buildTargetText: typeof appBuildTarget === "undefined" ? "desktop" : appBuildTarget
    property var translations: ({
        en: ({"Add vector files": "Add vector files", "Open workspace": "Open workspace", "Attributes": "Attributes", "Edit vertices": "Edit vertices", "Finish vertex edit": "Finish vertex edit", "Save GeoPackage": "Save GeoPackage", "Save workspace": "Save workspace", "About GoGIS": "About GoGIS", "Layers": "Layers", "No layers yet": "No layers yet", "Add vector files or open a workspace to begin.": "Add vector files or open a workspace to begin.", "Drag to pan · Scroll to zoom · Click a feature to inspect": "Drag to pan · Scroll to zoom · Click a feature to inspect", "General": "General", "Data source": "Data source", "Symbology": "Symbology", "Labels and expressions": "Labels and expressions", "Layer properties": "Layer properties", "Version": "Version", "Build": "Build", "Runtime": "Runtime", "License": "License", "Close": "Close", "Layer": "Layer", "Layer visible": "Layer visible", "Browse…": "Browse…", "Layer in source": "Layer in source", "Shapefile encoding": "Shapefile encoding", "Point color": "Point color", "Point size (mm)": "Point size (mm)", "Line color": "Line color", "Line width (mm)": "Line width (mm)", "Polygon color": "Polygon color", "Fill opacity (0–1)": "Fill opacity (0–1)", "Show labels": "Show labels", "Label field / template": "Label field / template", "Placement": "Placement", "Rotation field (optional)": "Rotation field (optional)", "Text height (mm)": "Text height (mm)", "Minimum scale denominator": "Minimum scale denominator", "Maximum scale denominator": "Maximum scale denominator", "Display rule — return true to show this feature's label": "Display rule — return true to show this feature's label", "Label text — return string, number, or nil": "Label text — return string, number, or nil", "Insert label example": "Insert label example", "Insert rule example": "Insert rule example", "Scripts run once for each feature. The read-only `feature` table exposes the layer's attributes. Use feature.FIELD or feature[\"FIELD NAME\"] for field names with spaces.": "Scripts run once for each feature. The read-only `feature` table exposes the layer's attributes. Use feature.FIELD or feature[\"FIELD NAME\"] for field names with spaces.", "Layer settings": "Layer settings", "Source path": "Source path", "Data properties": "Data properties"}),
        ko: ({"Add vector files": "벡터 파일 추가", "Open workspace": "작업공간 열기", "Attributes": "속성 테이블", "Edit vertices": "정점 편집", "Finish vertex edit": "정점 편집 종료", "Save GeoPackage": "GeoPackage 저장", "Save workspace": "작업공간 저장", "About GoGIS": "GoGIS 정보", "Layers": "레이어", "No layers yet": "레이어가 없습니다", "Add vector files or open a workspace to begin.": "벡터 파일을 추가하거나 작업공간을 열어 시작하세요.", "Drag to pan · Scroll to zoom · Click a feature to inspect": "드래그: 이동 · 휠: 확대/축소 · 피처 클릭: 정보 확인", "General": "일반", "Data source": "데이터 원본", "Symbology": "심볼로지", "Labels and expressions": "레이블 및 표현식", "Layer properties": "레이어 속성", "Version": "버전", "Build": "빌드", "Runtime": "실행 환경", "License": "라이선스", "Close": "닫기", "Layer": "레이어", "Layer visible": "레이어 표시", "Browse…": "찾아보기…", "Layer in source": "원본 내부 레이어", "Shapefile encoding": "Shapefile 인코딩", "Point color": "점 색상", "Point size (mm)": "점 크기 (mm)", "Line color": "선 색상", "Line width (mm)": "선 두께 (mm)", "Polygon color": "폴리곤 색상", "Fill opacity (0–1)": "채우기 불투명도 (0–1)", "Show labels": "레이블 표시", "Label field / template": "레이블 필드 / 템플릿", "Placement": "배치", "Rotation field (optional)": "회전 필드 (선택)", "Text height (mm)": "글자 높이 (mm)", "Minimum scale denominator": "최소 축척 분모", "Maximum scale denominator": "최대 축척 분모", "Display rule — return true to show this feature's label": "표시 규칙 — 레이블 표시 시 true 반환", "Label text — return string, number, or nil": "레이블 문자열 — 문자열, 숫자 또는 nil 반환", "Insert label example": "레이블 예제 삽입", "Insert rule example": "규칙 예제 삽입", "Scripts run once for each feature. The read-only `feature` table exposes the layer's attributes. Use feature.FIELD or feature[\"FIELD NAME\"] for field names with spaces.": "스크립트는 피처마다 실행됩니다. 읽기 전용 `feature` 테이블로 속성에 접근합니다. 공백이 있는 필드는 feature[\"필드 이름\"] 형식을 사용하세요.", "Layer settings": "레이어 설정", "Source path": "원본 경로", "Data properties": "데이터 속성"}),
        jp: ({"Add vector files": "ベクターファイルを追加", "Open workspace": "ワークスペースを開く", "Attributes": "属性テーブル", "Edit vertices": "頂点を編集", "Finish vertex edit": "頂点編集を終了", "Save GeoPackage": "GeoPackageを保存", "Save workspace": "ワークスペースを保存", "About GoGIS": "GoGISについて", "Layers": "レイヤー", "No layers yet": "レイヤーがありません", "Add vector files or open a workspace to begin.": "ベクターファイルを追加するか、ワークスペースを開いてください。", "Drag to pan · Scroll to zoom · Click a feature to inspect": "ドラッグ: 移動 · ホイール: 拡大/縮小 · 地物をクリック: 情報表示", "General": "一般", "Data source": "データソース", "Symbology": "シンボロジ", "Labels and expressions": "ラベルと式", "Layer properties": "レイヤーのプロパティ", "Version": "バージョン", "Build": "ビルド", "Runtime": "ランタイム", "License": "ライセンス", "Close": "閉じる", "Layer": "レイヤー", "Layer visible": "レイヤーを表示", "Browse…": "参照…", "Layer in source": "ソース内レイヤー", "Shapefile encoding": "Shapefileの文字コード", "Point color": "ポイント色", "Point size (mm)": "ポイントサイズ (mm)", "Line color": "ライン色", "Line width (mm)": "ライン幅 (mm)", "Polygon color": "ポリゴン色", "Fill opacity (0–1)": "塗りの不透明度 (0–1)", "Show labels": "ラベルを表示", "Label field / template": "ラベルフィールド / テンプレート", "Placement": "配置", "Rotation field (optional)": "回転フィールド (任意)", "Text height (mm)": "文字の高さ (mm)", "Minimum scale denominator": "最小縮尺分母", "Maximum scale denominator": "最大縮尺分母", "Display rule — return true to show this feature's label": "表示ルール — ラベル表示時にtrueを返す", "Label text — return string, number, or nil": "ラベル文字列 — 文字列、数値、またはnilを返す", "Insert label example": "ラベル例を挿入", "Insert rule example": "ルール例を挿入", "Scripts run once for each feature. The read-only `feature` table exposes the layer's attributes. Use feature.FIELD or feature[\"FIELD NAME\"] for field names with spaces.": "スクリプトは地物ごとに実行されます。読み取り専用の`feature`テーブルから属性を参照できます。空白を含むフィールド名はfeature[\"フィールド名\"]を使用します。", "Layer settings": "レイヤー設定", "Source path": "ソースパス", "Data properties": "データ属性"})
    })
    property var translationOverrides: ({
        en: ({"Desktop GIS": "Desktop GIS", "Layer properties": "Layer properties", "Select original layer source": "Select original layer source", "Add vector files as layers": "Add vector files as layers", "Save GoGIS workspace": "Save GoGIS workspace", "Open GoGIS workspace": "Open GoGIS workspace", "Drop SHP, GeoPackage, or GeoJSON": "Drop SHP, GeoPackage, or GeoJSON", "Display name": "Display name", "Original source path": "Original source path", "Internal layer name": "Internal layer name", "Auto encoding": "Auto encoding", "Selected feature": "Selected feature", "Feature name": "Feature name", "Save": "Save", "Cancel": "Cancel", "Previous": "Previous", "Next": "Next", "No attribute records in this layer": "No attribute records in this layer", "X coordinate": "X coordinate", "Y coordinate": "Y coordinate", "Go": "Go", "Cancel loading/render": "Cancel loading/render", "Layer settings": "Layer settings", "Source path": "Source path", "Data properties": "Data properties", "Point color": "Point color", "Point size (mm)": "Point size (mm)", "Line color": "Line color", "Line width (mm)": "Line width (mm)", "Polygon color": "Polygon color", "Fill opacity (0–1)": "Fill opacity (0–1)", "Show labels": "Show labels", "Label field / template": "Label field / template", "Placement": "Placement", "Rotation field (optional)": "Rotation field (optional)", "Text height (mm)": "Text height (mm)", "Minimum scale denominator": "Minimum scale denominator", "Maximum scale denominator": "Maximum scale denominator"}),
        ko: ({"Desktop GIS": "데스크톱 GIS", "Layer properties": "레이어 속성", "Select original layer source": "레이어 원본 선택", "Add vector files as layers": "벡터 파일을 레이어로 추가", "Save GoGIS workspace": "GoGIS 작업공간 저장", "Open GoGIS workspace": "GoGIS 작업공간 열기", "Drop SHP, GeoPackage, or GeoJSON": "SHP, GeoPackage 또는 GeoJSON 파일을 놓으세요", "Display name": "표시 이름", "Original source path": "원본 경로", "Internal layer name": "내부 레이어 이름", "Auto encoding": "인코딩 자동 감지", "Selected feature": "선택한 피처", "Feature name": "피처 이름", "Save": "저장", "Cancel": "취소", "Previous": "이전", "Next": "다음", "No attribute records in this layer": "이 레이어에 속성 레코드가 없습니다", "X coordinate": "X 좌표", "Y coordinate": "Y 좌표", "Go": "이동", "Cancel loading/render": "불러오기/렌더링 취소", "Layer settings": "레이어 설정", "Source path": "원본 경로", "Data properties": "데이터 속성", "Point color": "점 색상", "Point size (mm)": "점 크기 (mm)", "Line color": "선 색상", "Line width (mm)": "선 두께 (mm)", "Polygon color": "폴리곤 색상", "Fill opacity (0–1)": "채우기 불투명도 (0–1)", "Show labels": "레이블 표시", "Label field / template": "레이블 필드 / 템플릿", "Placement": "배치", "Rotation field (optional)": "회전 필드 (선택)", "Text height (mm)": "글자 높이 (mm)", "Minimum scale denominator": "최소 축척 분모", "Maximum scale denominator": "최대 축척 분모"}),
        jp: ({"Desktop GIS": "デスクトップGIS", "Layer properties": "レイヤーのプロパティ", "Select original layer source": "レイヤーソースを選択", "Add vector files as layers": "ベクターファイルをレイヤーとして追加", "Save GoGIS workspace": "GoGISワークスペースを保存", "Open GoGIS workspace": "GoGISワークスペースを開く", "Drop SHP, GeoPackage, or GeoJSON": "SHP、GeoPackage、GeoJSONをドロップ", "Display name": "表示名", "Original source path": "元のソースパス", "Internal layer name": "内部レイヤー名", "Auto encoding": "文字コードを自動判定", "Selected feature": "選択地物", "Feature name": "地物名", "Save": "保存", "Cancel": "キャンセル", "Previous": "前へ", "Next": "次へ", "No attribute records in this layer": "このレイヤーに属性レコードはありません", "X coordinate": "X座標", "Y coordinate": "Y座標", "Go": "移動", "Cancel loading/render": "読み込み/描画をキャンセル", "Layer settings": "レイヤー設定", "Source path": "ソースパス", "Data properties": "データ属性", "Point color": "ポイント色", "Point size (mm)": "ポイントサイズ (mm)", "Line color": "ライン色", "Line width (mm)": "ライン幅 (mm)", "Polygon color": "ポリゴン色", "Fill opacity (0–1)": "塗りの不透明度 (0–1)", "Show labels": "ラベルを表示", "Label field / template": "ラベルフィールド / テンプレート", "Placement": "配置", "Rotation field (optional)": "回転フィールド (任意)", "Text height (mm)": "文字の高さ (mm)", "Minimum scale denominator": "最小縮尺分母", "Maximum scale denominator": "最大縮尺分母"})
    })

    function tr(key) {
        var override = translationOverrides[language] || translationOverrides.en;
        if (override[key] !== undefined)
            return override[key];
        var dictionary = translations[language] || translations.en;
        return dictionary[key] || translations.en[key] || key;
    }

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
                text: rootWindow.tr("Desktop GIS")
                color: "#65717d"
            }
            Item {
                Layout.fillWidth: true
            }
            Button {
                text: rootWindow.tr("Add vector files")
                onClicked: fileDialog.open()
            }
            Button {
                text: rootWindow.tr("Open workspace")
                onClicked: workspaceOpenDialog.open()
            }
            Button {
                objectName: "openAttributesButton"
                text: rootWindow.tr("Attributes")
                enabled: layerModel.count > 0
                onClicked: attributeDialog.open()
            }
            Button {
                objectName: "toggleVertexEditButton"
                text: rootWindow.vertexEditMode ? "Finish vertex edit" : "Edit vertices"
                enabled: layerModel.count > 0 && vertexHandleModel.count > 0
                onClicked: rootWindow.vertexEditMode = !rootWindow.vertexEditMode
            }
            Button {
                text: rootWindow.tr("Save GeoPackage")
                onClicked: saveDialog.open()
            }
            Button {
                text: rootWindow.tr("Save workspace")
                onClicked: workspaceDialog.open()
            }
            Button {
                objectName: "aboutButton"
                text: rootWindow.tr("About GoGIS")
                onClicked: aboutDialog.open()
            }
        }
    }

    Menu {
        id: layerContextMenu
        objectName: "layerContextMenu"
        closePolicy: Popup.CloseOnEscape
        property string targetLayerName: ""
        MenuItem {
            objectName: "layerContextGeneral"
            text: rootWindow.tr("General")
            onTriggered: mapViewport.openLayerPropertiesForCategory(layerContextMenu.targetLayerName, "general")
        }
        MenuItem {
            objectName: "layerContextDataSource"
            text: rootWindow.tr("Data source")
            onTriggered: mapViewport.openLayerPropertiesForCategory(layerContextMenu.targetLayerName, "source")
        }
        MenuItem {
            objectName: "layerContextSymbology"
            text: rootWindow.tr("Symbology")
            onTriggered: mapViewport.openLayerPropertiesForCategory(layerContextMenu.targetLayerName, "symbology")
        }
        MenuItem {
            objectName: "layerContextLabels"
            text: rootWindow.tr("Labels and expressions")
            onTriggered: mapViewport.openLayerPropertiesForCategory(layerContextMenu.targetLayerName, "labels")
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
                    text: rootWindow.tr("Layers")
                    font.bold: true
                }
                ListView {
                    id: layerList
                    objectName: "layerList"
                    Layout.fillWidth: true
                    Layout.fillHeight: true
                    model: ListModel {
                        id: layerModel
                        objectName: "layerModel"
                    }
                    delegate: CheckDelegate {
                        objectName: "layerDelegate_" + name
                        width: ListView.view.width
                        text: (model.sourceError ? "⚠ " : "") + (typeof model.displayName === "undefined" || model.displayName === "" ? model.name : model.displayName)
                        checked: layerVisible
                        onToggled: {
                            layerModel.setProperty(index, "layerVisible", checked);
                            mapViewport.syncLayerVisibility();
                        }
                        onClicked: mapViewport.selectLayer(name)
                    }
                    MouseArea {
                        objectName: "layerContextMouseArea"
                        anchors.fill: parent
                        z: 10
                        acceptedButtons: Qt.RightButton
                        onClicked: {
                            var index = layerList.indexAt(mouse.x, mouse.y + layerList.contentY);
                            if (index >= 0)
                                mapViewport.showLayerContextMenu(layerModel.get(index).name);
                        }
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
                property int vertexHandleGenerationSeen: -1
                property string renderStatus: "Ready"

                ListModel {
                    id: attributeModel
                }
                ListModel {
                    id: mapLabelModel
                }
                ListModel {
                    id: vertexHandleModel
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

                function openLayerPropertiesForCategory(name, category) {
                    for (var i = 0; i < layerModel.count; ++i) {
                        if (layerModel.get(i).name === name) {
                            selectLayer(name);
                            layerSettingsDialog.targetLayerName = name;
                            layerSettingsDialog.activeCategory = category;
                            layerSettingsDialog.open();
                            return;
                        }
                    }
                }

                function showLayerContextMenu(name) {
                    if (findLayerIndex(name) < 0)
                        return;
                    selectLayer(name);
                    layerContextMenu.targetLayerName = name;
                    layerContextMenu.popup();
                }

                function findLayerIndex(name) {
                    for (var i = 0; i < layerModel.count; ++i) {
                        if (layerModel.get(i).name === name)
                            return i;
                    }
                    return -1;
                }

                function currentMapCoordinate() {
                    if (!cursorValid || mapCanvas.width <= 0 || mapCanvas.height <= 0)
                        return "—";
                    var bounds = dataBounds;
                    var zoom = Math.max(0.0001, mapZoom);
                    var nx = 0.5 + (cursorX - mapCanvas.x - mapCanvas.width / 2) / (mapCanvas.width * zoom);
                    var ny = 0.5 - (cursorY - mapCanvas.y - mapCanvas.height / 2) / (mapCanvas.height * zoom);
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
                    property real mapAspectRatio: {
                        var bounds = mapViewport.dataBounds;
                        var spanX = bounds[2] - bounds[0];
                        var spanY = bounds[3] - bounds[1];
                        return spanX > 0 && spanY > 0 ? spanX / spanY : 1;
                    }
                    property real viewportPanX: mapViewport.panX
                    property real viewportPanY: mapViewport.panY
                    width: Math.min(parent.width, parent.height * mapAspectRatio)
                    height: Math.min(parent.height, parent.width / mapAspectRatio)
                    x: (parent.width - width) / 2 + mapViewport.panX
                    y: (parent.height - height) / 2 + mapViewport.panY
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
                    property string vertexHandlePayload: "[]"
                    property int vertexHandleGeneration: 0
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

                Repeater {
                    objectName: "vertexHandleRepeater"
                    z: 15
                    model: vertexHandleModel
                    delegate: Rectangle {
                        id: vertexHandle
                        property alias dragArea: vertexMouseArea
                        objectName: "vertexHandle_" + model.vertexIndex
                        z: 20
                        width: 12
                        height: 12
                        radius: 6
                        x: mapCanvas.x + mapCanvas.width / 2 + (model.x - 0.5) * mapCanvas.width * mapCanvas.scale - width / 2
                        y: mapCanvas.y + mapCanvas.height / 2 - (model.y - 0.5) * mapCanvas.height * mapCanvas.scale - height / 2
                        color: "#fff"
                        border.color: "#145da0"
                        border.width: 2
                        visible: rootWindow.vertexEditMode
                        MouseArea {
                            id: vertexMouseArea
                            objectName: "vertexDragArea_" + model.vertexIndex
                            anchors.fill: parent
                            enabled: rootWindow.vertexEditMode
                            cursorShape: Qt.SizeAllCursor
                            property real startPointerX: 0
                            property real startPointerY: 0
                            property real startHandleX: 0
                            property real startHandleY: 0
                            onPressed: function (mouse) {
                                var point = mapToItem(mapViewport, mouse.x, mouse.y);
                                startPointerX = point.x;
                                startPointerY = point.y;
                                startHandleX = vertexHandle.x;
                                startHandleY = vertexHandle.y;
                            }
                            onPositionChanged: function (mouse) {
                                if (!pressed)
                                    return;
                                var point = mapToItem(mapViewport, mouse.x, mouse.y);
                                vertexHandle.x = startHandleX + point.x - startPointerX;
                                vertexHandle.y = startHandleY + point.y - startPointerY;
                            }
                            function submitVertexEdit() {
                                var nx = 0.5 + (vertexHandle.x + vertexHandle.width / 2 - mapCanvas.x - mapCanvas.width / 2) / (mapCanvas.width * mapCanvas.scale);
                                var ny = 0.5 - (vertexHandle.y + vertexHandle.height / 2 - mapCanvas.y - mapCanvas.height / 2) / (mapCanvas.height * mapCanvas.scale);
                                var bounds = mapViewport.dataBounds;
                                mapCanvas.editAction = "moveVertex";
                                mapCanvas.editValue = JSON.stringify({vertexIndex: model.vertexIndex, x: bounds[0] + nx * (bounds[2] - bounds[0]), y: bounds[1] + ny * (bounds[3] - bounds[1])});
                                mapCanvas.editGeneration += 1;
                            }
                            onReleased: submitVertexEdit()
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
                        text: rootWindow.tr("Drop SHP, GeoPackage, or GeoJSON")
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
                    objectName: "mapMouseArea"
                    anchors.fill: parent
                    z: 5
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
                        var oldZoom = mapViewport.mapZoom;
                        var newZoom = Math.max(0.25, Math.min(8.0, oldZoom * factor));
                        var normalizedX = 0.5 + (wheel.x - mapCanvas.x - mapCanvas.width / 2) / (mapCanvas.width * oldZoom);
                        var normalizedY = 0.5 - (wheel.y - mapCanvas.y - mapCanvas.height / 2) / (mapCanvas.height * oldZoom);
                        var baseX = (mapViewport.width - mapCanvas.width) / 2;
                        var baseY = (mapViewport.height - mapCanvas.height) / 2;
                        mapViewport.mapZoom = newZoom;
                        mapViewport.panX = wheel.x - baseX - mapCanvas.width / 2 - (normalizedX - 0.5) * mapCanvas.width * newZoom;
                        mapViewport.panY = wheel.y - baseY - mapCanvas.height / 2 + (normalizedY - 0.5) * mapCanvas.height * newZoom;
                        mapViewport.viewportGeneration += 1;
                    }
                    onClicked: function (mouse) {
                        if (!mapViewport.mapZoom)
                            return;
                        mapCanvas.clickX = mouse.x - mapCanvas.x;
                        mapCanvas.clickY = mouse.y - mapCanvas.y;
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
                        if (mapCanvas.vertexHandleGeneration !== mapViewport.vertexHandleGenerationSeen) {
                            mapViewport.vertexHandleGenerationSeen = mapCanvas.vertexHandleGeneration;
                            vertexHandleModel.clear();
                            var handles = JSON.parse(mapCanvas.vertexHandlePayload || "[]");
                            for (var handleIndex = 0; handleIndex < handles.length; ++handleIndex)
                                vertexHandleModel.append(handles[handleIndex]);
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
                    text: rootWindow.tr("Drag to pan · Scroll to zoom · Click a feature to inspect")
                    color: "#65717d"
                }
                ColumnLayout {
                    anchors.centerIn: parent
                    visible: layerModel.count === 0
                    spacing: 8
                    Label {
                        Layout.alignment: Qt.AlignHCenter
                        text: rootWindow.tr("No layers yet")
                        font.pixelSize: 22
                        font.bold: true
                        color: "#45515c"
                    }
                    Label {
                        Layout.alignment: Qt.AlignHCenter
                        text: rootWindow.tr("Add vector files or open a workspace to begin.")
                        color: "#65717d"
                    }
                    Button {
                        Layout.alignment: Qt.AlignHCenter
                        text: rootWindow.tr("Add vector files")
                        onClicked: fileDialog.open()
                    }
                }
            }
        }
    }

    Dialog {
        id: aboutDialog
        objectName: "aboutDialog"
        modal: true
        title: rootWindow.tr("About GoGIS")
        width: Math.min(460, rootWindow.width - 48)
        standardButtons: Dialog.Close
        contentItem: ColumnLayout {
            spacing: 8
            Label {
                text: "GoGIS"
                font.pixelSize: 22
                font.bold: true
            }
            GridLayout {
                columns: 2
                Label { text: rootWindow.tr("Version") }
                Label { objectName: "aboutVersionValue"; text: rootWindow.versionText }
                Label { text: rootWindow.tr("Build") }
                Label { objectName: "aboutBuildValue"; text: rootWindow.buildTargetText }
                Label { text: rootWindow.tr("Runtime") }
                Label { objectName: "aboutRuntimeValue"; text: rootWindow.runtimeText }
                Label { text: rootWindow.tr("License") }
                Label { text: "MIT" }
            }
        }
    }

    Dialog {
        id: attributeDialog
        objectName: "attributeDialog"
        modal: true
        title: rootWindow.tr("Attributes") + " — " + mapViewport.activeLayer
        width: Math.min(780, rootWindow.width - 48)
        height: Math.min(680, rootWindow.height - 80)
        standardButtons: Dialog.Close
        contentItem: ColumnLayout {
            spacing: 8
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
                    text: rootWindow.tr("Selected feature")
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
                    placeholderText: rootWindow.tr("Feature name")
                    text: mapViewport.editorValue
                    onTextChanged: if (activeFocus)
                        mapViewport.editorValue = text
                }
                RowLayout {
                    visible: mapViewport.selectedFeature !== ""
                    Button {
                        text: rootWindow.tr("Save")
                        onClicked: {
                            mapCanvas.editAction = "commit";
                            mapCanvas.editValue = mapViewport.editorValue;
                            mapCanvas.editGeneration += 1;
                        }
                    }
                    Button {
                        text: rootWindow.tr("Cancel")
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
                        text: rootWindow.tr("Previous")
                        enabled: mapViewport.attributePage > 0
                        onClicked: mapViewport.requestAttributePage(mapViewport.attributePage - 1)
                    }
                    Label {
                        text: "Page " + (mapViewport.attributePage + 1) + " / " + Math.max(1, Math.ceil(mapViewport.attributeTotal / mapViewport.attributePageSize))
                        Layout.fillWidth: true
                        horizontalAlignment: Text.AlignHCenter
                    }
                    Button {
                        text: rootWindow.tr("Next")
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
                    text: rootWindow.tr("No attribute records in this layer")
                    color: "#65717d"
                    horizontalAlignment: Text.AlignHCenter
                    padding: 16
                }
                Item { Layout.fillHeight: true }
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
                placeholderText: rootWindow.tr("X coordinate")
            }
            TextField {
                id: coordinateYInput
                Layout.preferredWidth: 115
                placeholderText: rootWindow.tr("Y coordinate")
                onAccepted: mapViewport.goToCoordinate()
            }
            Button {
                text: rootWindow.tr("Go")
                onClicked: mapViewport.goToCoordinate()
            }
            Label {
                id: coordinateNavigationStatus
                color: "#65717d"
            }
            Button {
                text: rootWindow.tr("Cancel loading/render")
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
        title: rootWindow.tr("Layer properties") + " — " + targetLayerName
        width: 620
        height: Math.max(320, Math.min(760, rootWindow.height - 120))
        standardButtons: Dialog.Apply | Dialog.Cancel
        property string targetLayerName: ""
        property string originalSourcePath: ""
        property string originalSourceLayerName: ""
        property string originalSourceEncoding: ""
        property string labelLuaSource: ""
        property string activeCategory: "general"

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
            labelLuaSource = labels.luaScript || "";
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
                    luaScript: labelLuaSource,
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
                TabBar {
                    id: layerCategoryTabs
                    objectName: "layerCategoryTabs"
                    Layout.fillWidth: true
                    currentIndex: Math.max(0, ["general", "source", "symbology", "labels"].indexOf(layerSettingsDialog.activeCategory))
                    onCurrentIndexChanged: {
                        if (currentIndex >= 0)
                            layerSettingsDialog.activeCategory = ["general", "source", "symbology", "labels"][currentIndex];
                    }
                    TabButton { text: rootWindow.tr("General") }
                    TabButton { text: rootWindow.tr("Data source") }
                    TabButton { text: rootWindow.tr("Symbology") }
                    TabButton { text: rootWindow.tr("Labels and expressions") }
                }
                Label {
                    text: rootWindow.tr("General")
                    font.bold: true
                    visible: layerSettingsDialog.activeCategory === "general"
                }
                TextField {
                    id: displayNameField
                    objectName: "displayNameField"
                    Layout.fillWidth: true
                    placeholderText: rootWindow.tr("Display name")
                    visible: layerSettingsDialog.activeCategory === "general"
                }
                CheckBox {
                    id: visibleField
                    objectName: "visibleField"
                    text: rootWindow.tr("Layer visible")
                    visible: layerSettingsDialog.activeCategory === "general"
                }
                Label {
                    text: rootWindow.tr("Data source")
                    font.bold: true
                    visible: layerSettingsDialog.activeCategory === "source"
                }
                RowLayout {
                    Layout.fillWidth: true
                    visible: layerSettingsDialog.activeCategory === "source"
                    TextField {
                        id: sourcePathField
                        objectName: "sourcePathField"
                        Layout.fillWidth: true
                        placeholderText: rootWindow.tr("Original source path")
                    }
                    Button {
                        text: rootWindow.tr("Browse…")
                        onClicked: relinkFileDialog.open()
                    }
                }
                Label {
                    id: sourceStatusLabel
                    Layout.fillWidth: true
                    color: "#b45309"
                    wrapMode: Text.Wrap
                    textFormat: Text.PlainText
                    visible: layerSettingsDialog.activeCategory === "source" && text !== ""
                }
                RowLayout {
                    Layout.fillWidth: true
                    visible: layerSettingsDialog.activeCategory === "source"
                    ColumnLayout {
                        Layout.fillWidth: true
                        Label {
                            text: rootWindow.tr("Layer in source")
                            color: "#65717d"
                        }
                        TextField {
                            id: sourceLayerField
                            objectName: "sourceLayerField"
                            Layout.fillWidth: true
                            placeholderText: rootWindow.tr("Internal layer name")
                        }
                    }
                    ColumnLayout {
                        Layout.preferredWidth: 180
                        Label {
                            text: rootWindow.tr("Shapefile encoding")
                            color: "#65717d"
                        }
                        ComboBox {
                            id: sourceEncodingField
                            objectName: "sourceEncodingField"
                            Layout.fillWidth: true
                            editable: true
                            model: ["", "UTF-8", "CP949", "EUC-KR", "ISO-8859-1"]
                    displayText: currentText === "" ? rootWindow.tr("Auto encoding") : currentText
                        }
                    }
                }
                Label {
                    objectName: "sourceChangeWarning"
                    Layout.fillWidth: true
                    visible: layerSettingsDialog.activeCategory === "source" && (sourcePathField.text.trim() !== layerSettingsDialog.originalSourcePath || sourceLayerField.text.trim() !== layerSettingsDialog.originalSourceLayerName || sourceEncodingField.editText.trim() !== layerSettingsDialog.originalSourceEncoding)
                    text: "Changing the source path, internal layer, or encoding reloads this layer. Save unsaved feature edits in it first."
                    color: "#9a6700"
                    wrapMode: Text.Wrap
                    textFormat: Text.PlainText
                }
                Label {
                    text: rootWindow.tr("Symbology")
                    font.bold: true
                    topPadding: 8
                    visible: layerSettingsDialog.activeCategory === "symbology"
                }
                GridLayout {
                    Layout.fillWidth: true
                    columns: 2
                    visible: layerSettingsDialog.activeCategory === "symbology"
                    Label {
                        text: rootWindow.tr("Point color")
                    }
                    TextField {
                        id: pointColorField
                        objectName: "pointColorField"
                        Layout.fillWidth: true
                    }
                    Label {
                        text: rootWindow.tr("Point size (mm)")
                    }
                    TextField {
                        id: pointSizeField
                        objectName: "pointSizeField"
                        Layout.fillWidth: true
                        inputMethodHints: Qt.ImhFormattedNumbersOnly
                    }
                    Label {
                        text: rootWindow.tr("Line color")
                    }
                    TextField {
                        id: lineColorField
                        objectName: "lineColorField"
                        Layout.fillWidth: true
                    }
                    Label {
                        text: rootWindow.tr("Line width (mm)")
                    }
                    TextField {
                        id: lineWidthField
                        objectName: "lineWidthField"
                        Layout.fillWidth: true
                        inputMethodHints: Qt.ImhFormattedNumbersOnly
                    }
                    Label {
                        text: rootWindow.tr("Polygon color")
                    }
                    TextField {
                        id: polygonColorField
                        objectName: "polygonColorField"
                        Layout.fillWidth: true
                    }
                    Label {
                        text: rootWindow.tr("Fill opacity (0–1)")
                    }
                    TextField {
                        id: fillOpacityField
                        objectName: "fillOpacityField"
                        Layout.fillWidth: true
                        inputMethodHints: Qt.ImhFormattedNumbersOnly
                    }
                }
                Label {
                    text: rootWindow.tr("Labels and expressions")
                    font.bold: true
                    topPadding: 8
                    visible: layerSettingsDialog.activeCategory === "labels"
                }
                CheckBox {
                    id: labelsEnabledField
                    objectName: "labelsEnabledField"
                    text: rootWindow.tr("Show labels")
                    visible: layerSettingsDialog.activeCategory === "labels"
                }
                Label {
                    text: rootWindow.tr("Label field / template")
                    visible: layerSettingsDialog.activeCategory === "labels"
                }
                TextField {
                    id: labelExpressionField
                    objectName: "labelExpressionField"
                    Layout.fillWidth: true
                    placeholderText: "e.g. ${name}"
                    visible: layerSettingsDialog.activeCategory === "labels"
                }
                RowLayout {
                    Layout.fillWidth: true
                    visible: layerSettingsDialog.activeCategory === "labels"
                    Label {
                        text: rootWindow.tr("Placement")
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
                    placeholderText: rootWindow.tr("Rotation field (optional)")
                    visible: layerSettingsDialog.activeCategory === "labels"
                }
                GridLayout {
                    Layout.fillWidth: true
                    columns: 2
                    visible: layerSettingsDialog.activeCategory === "labels"
                    Label {
                        text: rootWindow.tr("Text height (mm)")
                    }
                    TextField {
                        id: labelHeightField
                        objectName: "labelHeightField"
                        Layout.fillWidth: true
                        inputMethodHints: Qt.ImhFormattedNumbersOnly
                    }
                    Label {
                        text: rootWindow.tr("Minimum scale denominator")
                    }
                    TextField {
                        id: labelMinScaleField
                        objectName: "labelMinScaleField"
                        Layout.fillWidth: true
                        inputMethodHints: Qt.ImhFormattedNumbersOnly
                    }
                    Label {
                        text: rootWindow.tr("Maximum scale denominator")
                    }
                    TextField {
                        id: labelMaxScaleField
                        objectName: "labelMaxScaleField"
                        Layout.fillWidth: true
                        inputMethodHints: Qt.ImhFormattedNumbersOnly
                    }
                }
                Label {
                    text: "Label display rule (Lua; return true/false)"
                    wrapMode: Text.WordWrap
                    visible: layerSettingsDialog.activeCategory === "labels"
                }
                TextField {
                    id: labelRuleField
                    objectName: "labelRuleField"
                    Layout.fillWidth: true
                    placeholderText: "e.g. return feature.CLASS == \"primary\""
                    visible: layerSettingsDialog.activeCategory === "labels"
                }
                Label {
                    text: "Lua label script (return text using feature fields)"
                    wrapMode: Text.WordWrap
                    visible: layerSettingsDialog.activeCategory === "labels"
                }
                Label {
                    Layout.fillWidth: true
                    text: "Available fields: " + (mapViewport.attributeColumns.length ? mapViewport.attributeColumns.join(", ") : "open the attribute table to inspect the layer schema")
                    color: "#65717d"
                    wrapMode: Text.WordWrap
                    visible: layerSettingsDialog.activeCategory === "labels"
                }
                Button {
                    objectName: "openLuaEditorButton"
                    text: "Open Lua editor and examples…"
                    Layout.fillWidth: true
                    onClicked: luaEditorDialog.open()
                    visible: layerSettingsDialog.activeCategory === "labels"
                }
            }
        }
    }

    Dialog {
        id: luaEditorDialog
        objectName: "luaEditorDialog"
        modal: true
        title: rootWindow.tr("Lua label editor") + " — " + layerSettingsDialog.targetLayerName
        width: Math.min(760, rootWindow.width - 64)
        height: Math.min(680, rootWindow.height - 80)
        standardButtons: Dialog.Ok | Dialog.Cancel
        onOpened: {
            labelRuleEditor.text = labelRuleField.text;
            labelLuaField.text = layerSettingsDialog.labelLuaSource;
        }
        onAccepted: {
            labelRuleField.text = labelRuleEditor.text;
            layerSettingsDialog.labelLuaSource = labelLuaField.text;
        }
        contentItem: ColumnLayout {
            spacing: 8
            Label {
                Layout.fillWidth: true
                text: rootWindow.tr("Scripts run once for each feature. The read-only `feature` table exposes the layer's attributes. Use feature.FIELD or feature[\"FIELD NAME\"] for field names with spaces.")
                wrapMode: Text.WordWrap
                color: "#45515c"
            }
            Label {
                text: "Available fields: " + (mapViewport.attributeColumns.length ? mapViewport.attributeColumns.join(", ") : "none loaded")
                Layout.fillWidth: true
                wrapMode: Text.WordWrap
                color: "#65717d"
            }
            Label {
                text: rootWindow.tr("Display rule — return true to show this feature's label")
                font.bold: true
            }
            TextArea {
                id: labelRuleEditor
                objectName: "labelRuleEditor"
                Layout.fillWidth: true
                Layout.preferredHeight: 100
                placeholderText: "return feature.CLASS == \"primary\""
                wrapMode: TextEdit.Wrap
            }
            Label {
                text: rootWindow.tr("Label text — return string, number, or nil")
                font.bold: true
            }
            TextArea {
                id: labelLuaField
                objectName: "labelLuaField"
                Layout.fillWidth: true
                Layout.fillHeight: true
                placeholderText: "return string.format(\"%s (%s)\", feature.NAME, feature.CLASS)"
                wrapMode: TextEdit.Wrap
                selectByMouse: true
                font.family: "monospace"
            }
            RowLayout {
                Layout.fillWidth: true
                Label { Layout.fillWidth: true }
                Button {
                    text: rootWindow.tr("Insert label example")
                    onClicked: labelLuaField.text = "return string.format(\"%s (%s)\", feature.NAME, feature.CLASS)"
                }
                Button {
                    text: rootWindow.tr("Insert rule example")
                    onClicked: labelRuleEditor.text = "return feature.CLASS == \"primary\""
                }
            }
        }
    }

    Platform.FileDialog {
        id: fileDialog
        title: rootWindow.tr("Add vector files as layers")
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
        title: rootWindow.tr("Save GoGIS workspace")
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
        title: rootWindow.tr("Open GoGIS workspace")
        fileMode: Platform.FileDialog.OpenFile
        nameFilters: ["GoGIS workspace (*.gogis)"]
        onAccepted: mapViewport.requestLoad(file)
    }

    Platform.FileDialog {
        id: relinkFileDialog
        title: rootWindow.tr("Select original layer source")
        fileMode: Platform.FileDialog.OpenFile
        nameFilters: ["Vector files (*.shp *.gpkg *.geojson *.json)", "All files (*)"]
        onAccepted: sourcePathField.text = mapViewport.localPathFromUrl(file)
    }
}
