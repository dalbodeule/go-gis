import QtQuick
import QtQuick.Controls
import QtTest
import "../qml" as GoGISApp

TestCase {
    id: testCase
    name: "LayerPropertiesDialog"
    when: windowShown
    property var appWindow
    property var dialogItem

    SignalSpy {
        id: appliedSpy
        target: testCase.dialogItem
        signalName: "applied"
    }

    Component {
        id: appComponent
        GoGISApp.Main {}
    }

    function initTestCase() {
        appWindow = appComponent.createObject(testCase);
        verify(appWindow !== null, "desktop QML should instantiate with the test canvas");
        appWindow.show();
        wait(100);
    }

    function cleanupTestCase() {
        if (appWindow)
            appWindow.destroy();
    }

    function test_fileUrlsBecomePlatformPaths() {
        var viewport = findChild(appWindow, "mapViewport");
        verify(viewport !== null);
        compare(viewport.localPathFromUrl("file:///C:/GIS/My%20Roads.shp"), "C:/GIS/My Roads.shp");
        compare(viewport.localPathFromUrl("file:///tmp/My%20Roads.shp"), "/tmp/My Roads.shp");
        compare(viewport.localPathFromUrl("/data/My Roads.shp"), "/data/My Roads.shp");
    }

    function test_fullScreenKeepsWorkspacePanelsVisible() {
        var viewport = findChild(appWindow, "mapViewport");
        var layers = findChild(appWindow, "layerList");
        var addButton = findChild(appWindow, "addVectorFilesButton");
        var status = findChild(appWindow, "renderStatusLabel");
        var canvas = findChild(appWindow, "goGisMapCanvas");
        var layerModel = findChild(appWindow, "layerModel");
        verify(viewport !== null && layers !== null && addButton !== null && status !== null && canvas !== null);
        var layerCount = layerModel.count;
        try {
            appWindow.showMaximized();
            tryVerify(function() { return appWindow.visibility === Window.Maximized; });
            appWindow.showFullScreen();
            tryVerify(function() { return appWindow.visibility === Window.FullScreen; });
            tryVerify(function() {
                var mapCenter = viewport.mapToItem(appWindow.contentItem, viewport.width / 2, viewport.height / 2);
                var layerCenter = layers.mapToItem(appWindow.contentItem, layers.width / 2, layers.height / 2);
                return layerModel.count === layerCount && viewport.visible && viewport.width > 0 && viewport.height > 0 &&
                       layers.visible && layers.width > 0 && layers.height > 0 &&
                       mapCenter.x >= 0 && mapCenter.x <= appWindow.contentItem.width &&
                       layerCenter.x >= 0 && layerCenter.x <= appWindow.contentItem.width &&
                       canvas.visible && addButton.visible && status.visible;
            }, 2000, "fullscreen must retain layer pane, map, toolbar, and status");
        } finally {
            appWindow.showNormal();
            tryVerify(function() { return appWindow.visibility === Window.Windowed; });
        }
    }

    function test_fullScreenPreservesPannedMapCenterAndScale() {
        var viewport = findChild(appWindow, "mapViewport");
        var canvas = findChild(appWindow, "goGisMapCanvas");
        var layerModel = findChild(appWindow, "layerModel");
        var oldPayload = canvas.layerTreePayload;
        var oldBounds = viewport.dataBounds;
        var oldCRS = viewport.dataCRS;
        var oldMetadata = viewport.hasMapMetadata;
        var oldZoom = viewport.mapZoom;
        var oldPanX = viewport.panX;
        var oldPanY = viewport.panY;
        function centerX() { return 0.5 - viewport.panX / (canvas.width * viewport.mapZoom); }
        function centerY() { return 0.5 + viewport.panY / (canvas.height * viewport.mapZoom); }
        function unitsPerPixel() { return 1 / (canvas.width * viewport.mapZoom); }
        try {
            canvas.layerTreePayload = JSON.stringify([{name: "fullscreen-map", visible: true}]);
            tryCompare(layerModel, "count", 1);
            viewport.dataBounds = [0, 0, 1000, 1000];
            viewport.dataCRS = "EPSG:5186";
            viewport.hasMapMetadata = true;
            viewport.mapZoom = 2276.520688194071;
            viewport.panX = (0.5 - 0.62) * canvas.width * viewport.mapZoom;
            viewport.panY = (0.57 - 0.5) * canvas.height * viewport.mapZoom;
            var x = centerX(), y = centerY(), resolution = unitsPerPixel();
            appWindow.showMaximized();
            tryVerify(function() { return appWindow.visibility === Window.Maximized; });
            appWindow.showFullScreen();
            tryVerify(function() { return appWindow.visibility === Window.FullScreen; });
            wait(200);
            compare(canvas.viewGeneration, viewport.viewportGeneration);
            verify(Math.abs(centerX() - x) < 0.000001, "fullscreen changed map X center");
            verify(Math.abs(centerY() - y) < 0.000001, "fullscreen changed map Y center");
            verify(Math.abs(unitsPerPixel() - resolution) < 1e-9, "fullscreen changed map scale");
        } finally {
            appWindow.showNormal();
            tryVerify(function() { return appWindow.visibility === Window.Windowed; });
            canvas.layerTreePayload = oldPayload;
            viewport.dataBounds = oldBounds;
            viewport.dataCRS = oldCRS;
            viewport.hasMapMetadata = oldMetadata;
            viewport.mapZoom = oldZoom;
            viewport.panX = oldPanX;
            viewport.panY = oldPanY;
        }
    }

    function test_resizingDeeplyPannedMapKeepsWorldCenterAndScale() {
        var viewport = findChild(appWindow, "mapViewport");
        var canvas = findChild(appWindow, "goGisMapCanvas");
        var layerModel = findChild(appWindow, "layerModel");
        var oldWidth = appWindow.width;
        var oldHeight = appWindow.height;
        var oldPayload = canvas.layerTreePayload;
        var oldBounds = viewport.dataBounds;
        var oldCRS = viewport.dataCRS;
        var oldMetadata = viewport.hasMapMetadata;
        var oldZoom = viewport.mapZoom;
        var oldPanX = viewport.panX;
        var oldPanY = viewport.panY;
        var oldCursorValid = viewport.cursorValid;
        function worldCenter() {
            return [viewport.dataBounds[0] + (0.5 - viewport.panX / (canvas.width * viewport.mapZoom)) *
                    (viewport.dataBounds[2] - viewport.dataBounds[0]),
                    viewport.dataBounds[1] + (0.5 + viewport.panY / (canvas.height * viewport.mapZoom)) *
                    (viewport.dataBounds[3] - viewport.dataBounds[1])];
        }
        function unitsPerPixel() {
            return (viewport.dataBounds[2] - viewport.dataBounds[0]) / (canvas.width * viewport.mapZoom);
        }
        try {
            canvas.layerTreePayload = JSON.stringify([{name: "resize-map", visible: true}]);
            tryCompare(layerModel, "count", 1);
            viewport.dataBounds = [0, 0, 1000, 1000];
            viewport.dataCRS = "EPSG:5186";
            viewport.hasMapMetadata = true;
            viewport.mapZoom = 2200;
            viewport.panX = (0.5 - 0.8) * canvas.width * viewport.mapZoom;
            viewport.panY = (0.2 - 0.5) * canvas.height * viewport.mapZoom;
            viewport.cursorValid = true;
            var centerBefore = worldCenter();
            var unitsBefore = unitsPerPixel();
            var viewportWidthBefore = viewport.width;
            var viewportHeightBefore = viewport.height;
            appWindow.width = oldWidth + 360;
            appWindow.height = oldHeight + 114;
            tryVerify(function() { return viewport.width > viewportWidthBefore && viewport.height > viewportHeightBefore; });
            var centerAfter = worldCenter();
            verify(Math.abs(centerAfter[0] - centerBefore[0]) < 0.0001, "resize changed map X center");
            verify(Math.abs(centerAfter[1] - centerBefore[1]) < 0.0001, "resize changed map Y center");
            verify(Math.abs(unitsPerPixel() - unitsBefore) < 1e-9, "resize changed map scale");
            compare(viewport.cursorValid, false);
            appWindow.width = oldWidth;
            appWindow.height = oldHeight;
            tryVerify(function() { return viewport.width === viewportWidthBefore && viewport.height === viewportHeightBefore; });
            centerAfter = worldCenter();
            verify(Math.abs(centerAfter[0] - centerBefore[0]) < 0.0001, "restore changed map X center");
            verify(Math.abs(centerAfter[1] - centerBefore[1]) < 0.0001, "restore changed map Y center");
        } finally {
            appWindow.width = oldWidth;
            appWindow.height = oldHeight;
            canvas.layerTreePayload = oldPayload;
            viewport.dataBounds = oldBounds;
            viewport.dataCRS = oldCRS;
            viewport.hasMapMetadata = oldMetadata;
            viewport.mapZoom = oldZoom;
            viewport.panX = oldPanX;
            viewport.panY = oldPanY;
            viewport.cursorValid = oldCursorValid;
        }
    }

    function test_addVectorDialogSelectionRequestsLoad() {
        var canvas = findChild(appWindow, "goGisMapCanvas");
        var viewport = findChild(appWindow, "mapViewport");
        var layerModel = findChild(appWindow, "layerModel");
        var generation = canvas.loadGeneration;
        viewport.requestLoadFiles(["file:///tmp/roads.shp"]);
        tryCompare(canvas, "loadGeneration", generation + 1);
        compare(JSON.parse(canvas.loadPath), ["/tmp/roads.shp"]);
    }

    function test_rapidFileSelectionsSurviveOneGuiFrame() {
        var canvas = findChild(appWindow, "goGisMapCanvas");
        var viewport = findChild(appWindow, "mapViewport");
        var generation = canvas.loadGeneration;
        canvas.loadCapturedGeneration = generation;
        viewport.requestLoadFiles(["file:///tmp/first.shp"]);
        viewport.requestLoadFiles(["file:///tmp/second.shp"]);
        var pending = JSON.parse(canvas.loadRequestJournal);
        compare(pending.length, 2);
        compare(pending[0].generation, generation + 1);
        compare(pending[0].paths, ["/tmp/first.shp"]);
        compare(pending[1].generation, generation + 2);
        compare(pending[1].paths, ["/tmp/second.shp"]);
        canvas.loadCapturedGeneration = generation + 1;
        compare(JSON.parse(canvas.loadRequestJournal).length, 1);
        canvas.loadCapturedGeneration = generation + 2;
        compare(JSON.parse(canvas.loadRequestJournal).length, 0);
    }

    function test_translationsCoverSelectedLanguages() {
        appWindow.language = "ko";
        compare(appWindow.tr("Layers"), "레이어");
        compare(appWindow.tr("Add vector files"), "벡터 파일 추가");
        compare(appWindow.tr("Center + rotation"), "중앙 + 회전");
        compare(appWindow.tr("Lua field types"), "Lua 필드 형식");
        compare(appWindow.tr("Display rule — return true to show this feature's label"), "표시 규칙 — 레이블 표시 시 true 반환");
        compare(appWindow.tr("Lua label editor"), "Lua 레이블 편집기");
        verify(appWindow.tr("Lua field access hint").indexOf("필드 형식: %1") >= 0);
        appWindow.language = "jp";
        compare(appWindow.tr("Layers"), "レイヤー");
        compare(appWindow.tr("Add vector files"), "ベクターファイルを追加");
        compare(appWindow.tr("Lua label editor"), "Luaラベルエディター");
        verify(appWindow.tr("Lua field access hint").indexOf("型: %1") >= 0);
        appWindow.language = "en";
        compare(appWindow.tr("Layers"), "Layers");
        compare(appWindow.tr("Lua label editor"), "Lua label editor");
    }

    function test_dynamicStatusIsLocalizedAndSemanticallyColored() {
        var canvas = findChild(appWindow, "goGisMapCanvas");
        var status = findChild(appWindow, "renderStatusLabel");
        verify(canvas !== null && status !== null);
        canvas.renderStatus = "Loading cancelled";
        appWindow.language = "ko";
        tryCompare(status, "text", "불러오기 취소됨");
        compare(status.color.toString(), "#9a6700");
        canvas.renderStatus = "Render incomplete; zoom in and try again: viewport budget exceeded";
        tryCompare(status, "text", "일부 렌더링을 완료하지 못했습니다. 확대 후 다시 시도하세요: viewport budget exceeded");
        compare(status.color.toString(), "#b42318");
        canvas.renderStatus = "Dataset has at least 250000 features; opened read-only to limit memory";
        tryCompare(status, "text", "피처 250000개 이상 · 메모리 보호를 위해 읽기 전용으로 열었습니다");
        compare(status.color.toString(), "#175cd3");
        canvas.renderStatus = "Save failed: read-only dataset is not editable";
        tryVerify(function() { return status.color.toString() === "#b42318"; });
        canvas.renderStatus = "Reprojecting project to EPSG:3857";
        tryCompare(status, "text", "프로젝트 좌표계로 변환 중: EPSG:3857");
        compare(status.color.toString(), "#175cd3");
        verify(appWindow.statusIsBusy(canvas.renderStatus));
        canvas.renderStatus = "Project CRS set to EPSG:3857";
        tryCompare(status, "text", "프로젝트 좌표계 적용 완료: EPSG:3857");
        compare(status.color.toString(), "#2e7d32");
        verify(appWindow.statusIsBusy("Saving output.gpkg"));
        verify(appWindow.statusIsBusy("Loading 12/25"));
        verify(!appWindow.statusIsBusy("Saved output.gpkg"));
        appWindow.language = "jp";
        compare(appWindow.localizedStatus("Loading: checking feature count"), "地物数を確認中");
        appWindow.language = "en";
    }

    function test_diagnosticLogDialogShowsCapturedProcessAndApplicationMessages() {
        var canvas = findChild(appWindow, "goGisMapCanvas");
        var button = findChild(appWindow, "openDiagnosticLogsButton");
        var dialog = findChild(appWindow, "diagnosticLogDialog");
        var logText = findChild(appWindow, "diagnosticLogTextArea");
        verify(canvas !== null && button !== null && dialog !== null && logText !== null);
        verify(button.visible, "Logs button should remain available in the footer toolbar");
        button.clicked();
        tryCompare(dialog, "visible", true);
        canvas.diagnosticLogPayload = JSON.stringify([
            {time: "12:34:56.789", stream: "stderr", message: "GDAL driver warning: test detail"},
            {time: "12:34:57.000", stream: "application", message: "Render incomplete; zoom in and try again: query budget exceeded"}
        ]);
        tryVerify(function() {
            return logText.text.indexOf("GDAL driver warning: test detail") >= 0 &&
                   logText.text.indexOf("query budget exceeded") >= 0;
        });
        appWindow.language = "ko";
        compare(dialog.title, "애플리케이션 로그");
        dialog.close();
        appWindow.language = "en";
    }

    function test_layerVisibilityToggleRequestsViewportRefresh() {
        var canvas = findChild(appWindow, "goGisMapCanvas");
        var layerModel = findChild(appWindow, "layerModel");
        var layerList = findChild(appWindow, "layerList");
        canvas.layerTreePayload = JSON.stringify([{name: "roads", visible: true}]);
        tryCompare(layerModel, "count", 1);
        tryVerify(function() { return layerModel.get(0).name === "roads"; });
        tryVerify(function() {
            var current = layerList.itemAtIndex(0);
            return current !== null && current.objectName === "layerDelegate_roads";
        });
        var delegate = layerList.itemAtIndex(0);
        wait(50);

        var generation = canvas.layerVisibilityGeneration;
        mouseClick(delegate, delegate.width / 2, delegate.height / 2);
        tryCompare(canvas, "layerVisibilityGeneration", generation + 1);
        compare(JSON.parse(canvas.layerVisibilityPayload).roads, false);

        generation = canvas.layerVisibilityGeneration;
        mouseClick(delegate, delegate.width / 2, delegate.height / 2);
        tryCompare(canvas, "layerVisibilityGeneration", generation + 1);
        compare(JSON.parse(canvas.layerVisibilityPayload).roads, true);
    }

    function test_zoomToLayerExtentFitsPolygonWithoutChangingProjectBounds() {
        var canvas = findChild(appWindow, "goGisMapCanvas");
        var viewport = findChild(appWindow, "mapViewport");
        var layerModel = findChild(appWindow, "layerModel");
        verify(canvas !== null && viewport !== null);
        viewport.dataBounds = [0, 0, 1000, 1000];
        canvas.layerTreePayload = JSON.stringify([
            {name: "survey-points", geometryType: "POINT", bounds: [50, 40, 960, 980], visible: true},
            {name: "parcels", geometryType: "POLYGON", bounds: [200, 300, 400, 500], visible: true}
        ]);
        tryCompare(findChild(appWindow, "layerModel"), "count", 2);
        viewport.mapZoom = 1;
        viewport.panX = 0;
        viewport.panY = 0;
        viewport.pendingInitialLayerFit = true;
        var generation = viewport.viewportGeneration;
        verify(viewport.zoomToLayerExtent("parcels"), "layer fit should use the polygon bounds rather than outlier point bounds");
        verify(!viewport.pendingInitialLayerFit);
        verify(viewport.mapZoom > 1, "the polygon layer extent should be enlarged to fit the canvas");
        verify(Math.abs(viewport.panX) > 0 && Math.abs(viewport.panY) > 0,
               "the viewport should center on the selected layer extent");
        compare(viewport.dataBounds, [0, 0, 1000, 1000], "zooming must not discard outlier data bounds");
        compare(viewport.viewportGeneration, generation + 1);
    }

    function test_zoomToFullExtentUsesPreferredPolygonFitBounds() {
        var viewport = findChild(appWindow, "mapViewport");
        verify(viewport !== null);
        viewport.dataBounds = [211407.24, 43257.02, 2287874.9, 459484.82];
        viewport.fitBounds = [211407.24, 423223.66, 236805.50, 459484.82];
        viewport.mapZoom = 1;
        viewport.panX = 0;
        viewport.panY = 0;
        verify(viewport.zoomToFullExtent(), "full-extent should fit the parcel area without discarding outlier data bounds");
        verify(viewport.mapZoom > 1, "parcel extent should be enlarged inside the complete data canvas");
        verify(Math.abs(viewport.panX) > 0 && Math.abs(viewport.panY) > 0,
               "the fitted parcel extent should be centered within the complete data canvas");
        compare(viewport.dataBounds, [211407.24, 43257.02, 2287874.9, 459484.82]);
    }

    function test_outlierBoundsStillAllowTwentyTimesMoreZoomFromParcelView() {
        var viewport = findChild(appWindow, "mapViewport");
        var canvas = findChild(appWindow, "goGisMapCanvas");
        viewport.dataBounds = [211407.24, 43257.02, 2287874.9, 459484.82];
        viewport.dataCRS = "EPSG:5186";
        verify(canvas.width > 0 && viewport.width > 0);
        var spanX = viewport.dataBounds[2] - viewport.dataBounds[0];
        // Approx. 1:6,437 is the scale shown in the reported screen.
        var zoomAtReportedScale = spanX * 96 / (canvas.width * 0.0254 * 6437);
        verify(viewport.maxMapZoom() >= 20 * zoomAtReportedScale,
               "the cadastral view must allow at least twenty times more zoom");
    }

    function test_memorySummaryDistinguishesPeakAndGoHeap() {
        appWindow.language = "ko";
        compare(appWindow.memorySummary(100 * 1024 * 1024, 20 * 1024 * 1024, true, 2), "프로세스 최고 RSS 100 MiB · Go 힙 20 MiB");
        compare(appWindow.memorySummary(100 * 1024 * 1024, 20 * 1024 * 1024, true, 1), "프로세스 작업 집합 100 MiB · Go 힙 20 MiB");
        verify(appWindow.memorySummary(0, 20 * 1024 * 1024, false, 0).indexOf("프로세스 메모리 측정 불가") >= 0);

        var canvas = findChild(appWindow, "goGisMapCanvas");
        var label = findChild(appWindow, "memoryStatusLabel");
        canvas.processMemoryBytes = 100 * 1024 * 1024;
        canvas.goHeapBytes = 20 * 1024 * 1024;
        canvas.processMemoryKind = 1;
        canvas.memoryStatusAvailable = true;
        tryCompare(label, "text", "프로세스 작업 집합 100 MiB · Go 힙 20 MiB");
        appWindow.language = "en";
    }

    function test_queuedFileSelectionStatusIsLocalized() {
        appWindow.language = "ko";
        compare(appWindow.localizedStatus("Loading in progress; 2 selection(s) queued"),
                "파일 불러오는 중 · 추가 요청 2건 대기");
        appWindow.language = "jp";
        compare(appWindow.localizedStatus("Loading in progress; 2 selection(s) queued"),
                "ファイル読み込み中 · 追加要求 2 件待機");
        appWindow.language = "en";
    }

    function test_layerContextMenuOpensRequestedCategory() {
        var canvas = findChild(appWindow, "goGisMapCanvas");
        var layerModel = findChild(appWindow, "layerModel");
        var viewport = findChild(appWindow, "mapViewport");
        var menu = findChild(appWindow, "layerContextMenu");
        var dialog = findChild(appWindow, "layerSettingsDialog");
        canvas.layerTreePayload = JSON.stringify([{name: "roads", visible: true}]);
        tryCompare(layerModel, "count", 1);
        var layerDelegate = findChild(appWindow, "layerList").itemAtIndex(0);
        verify(layerDelegate !== null);
        var contextMouseArea = findChild(appWindow, "layerContextMouseArea");
        verify(contextMouseArea !== null);
        verify(contextMouseArea.width > 0 && contextMouseArea.height > 0);
        viewport.activeLayer = "";
        tryCompare(menu, "visible", false);
        mouseClick(contextMouseArea, contextMouseArea.width / 2, contextMouseArea.height / 2, Qt.RightButton);
        tryCompare(viewport, "activeLayer", "roads");
        compare(menu.targetLayerName, "roads");
        menu.popup();
        tryCompare(menu, "visible", true);
        compare(menu.targetLayerName, "roads");
        mouseClick(viewport, viewport.width / 2, viewport.height / 2);
        tryCompare(menu, "visible", false);
        menu.popup();
        tryCompare(menu, "visible", true);
        mouseClick(findChild(appWindow, "layerContextSymbology"));
        tryCompare(dialog, "visible", true);
        compare(dialog.targetLayerName, "roads");
        compare(dialog.activeCategory, "symbology");
        dialog.close();

        var removeDialog = findChild(appWindow, "removeLayerDialog");
        var removeItem = findChild(appWindow, "layerContextRemove");
        var beforeRemoveGeneration = canvas.layerSettingsGeneration;
        menu.popup();
        tryCompare(menu, "visible", true);
        mouseClick(removeItem);
        tryCompare(removeDialog, "visible", true);
        compare(removeDialog.targetLayerName, "roads");
        mouseClick(removeDialog.standardButton(Dialog.Cancel));
        tryCompare(removeDialog, "visible", false);
        compare(canvas.layerSettingsGeneration, beforeRemoveGeneration);
        menu.popup();
        tryCompare(menu, "visible", true);
        mouseClick(removeItem);
        tryCompare(removeDialog, "visible", true);
        mouseClick(removeDialog.standardButton(Dialog.Ok));
        tryCompare(canvas, "layerSettingsGeneration", beforeRemoveGeneration + 1);
        compare(JSON.parse(canvas.layerSettingsPayload).operation, "remove");
        compare(JSON.parse(canvas.layerSettingsPayload).name, "roads");
        canvas.layerTreePayload = "[]";
        tryCompare(layerModel, "count", 0);
        compare(viewport.activeLayer, "");
        compare(menu.visible, false);
    }

    function test_aboutDialogShowsBuildInformation() {
        var button = findChild(appWindow, "aboutButton");
        var dialog = findChild(appWindow, "aboutDialog");
        verify(button !== null);
        mouseClick(button);
        tryCompare(dialog, "visible", true);
        verify(findChild(dialog, "aboutVersionValue").text.length > 0);
        verify(findChild(dialog, "aboutBuildValue").text.length > 0);
        verify(findChild(dialog, "aboutRuntimeValue").text.indexOf("Go") === 0);
        dialog.close();
    }

    function test_applicationSettingsExposeLanguageAndRestoreDefaults() {
        var button = findChild(appWindow, "applicationSettingsButton");
        var dialog = findChild(appWindow, "applicationSettingsDialog");
        var language = findChild(appWindow, "interfaceLanguageCombo");
        var store = findChild(appWindow, "applicationSettingsStore");
        var indexThreshold = findChild(appWindow, "shapefileIndexThresholdSpinBox");
        var reset = findChild(appWindow, "resetApplicationSettingsButton");
        verify(button !== null && dialog !== null && language !== null && store !== null && indexThreshold !== null && reset !== null);
        var previousPreference = store.interfaceLanguage;
        var previousThreshold = store.shapefileIndexThreshold;
        try {
            mouseClick(button);
            tryCompare(dialog, "visible", true);
            compare(language.valueAt(language.currentIndex), previousPreference);
            compare(indexThreshold.value, 10000);
            indexThreshold.value = 50000;
            indexThreshold.valueModified();
            compare(store.shapefileIndexThreshold, 50000);
            language.currentIndex = language.indexOfValue("ko");
            compare(store.interfaceLanguage, "ko");
            tryCompare(appWindow, "language", "ko");
            mouseClick(reset);
            compare(store.interfaceLanguage, "system");
            compare(store.shapefileIndexThreshold, 10000);
        } finally {
            store.interfaceLanguage = previousPreference;
            store.shapefileIndexThreshold = previousThreshold;
            dialog.close();
        }
    }

    function test_luaEditorHighlightsSyntaxAndSuggestsTypedFields() {
        var dialog = findChild(appWindow, "luaEditorDialog");
        var editor = findChild(dialog, "labelLuaField");
        var displayEditor = findChild(dialog, "featureDisplayRuleEditor");
        var viewport = findChild(appWindow, "mapViewport");
        verify(editor !== null);
        verify(displayEditor !== null);
        dialog.open();
        tryCompare(dialog, "visible", true);
        var keywordScript = 'if feature.NAME then return "road" end';
        editor.text = keywordScript;
        tryVerify(function() { return editor.highlightedHTML.indexOf("road") >= 0 && editor.highlightedHTML.indexOf("#7b2cbf") >= 0 && editor.highlightedHTML.indexOf("#16803c") >= 0; });
        verify(editor.highlightedHTML.indexOf("#7b2cbf") >= 0, "Lua keywords should be highlighted");
        verify(editor.highlightedHTML.indexOf("#16803c") >= 0, "Lua strings should be highlighted");
        displayEditor.text = 'return feature.ACTIVE == true';
        tryVerify(function() { return displayEditor.highlightedHTML.indexOf("#7b2cbf") >= 0; });
        var escapedScript = 'return feature["road<&\"name"] -- <safe>';
        editor.text = escapedScript;
        tryVerify(function() { return editor.highlightedHTML.indexOf("road&lt;&amp;") >= 0 && editor.highlightedHTML.indexOf("<safe>") < 0; });
        verify(editor.highlightedHTML.indexOf("road&lt;&amp;") >= 0, "source text must be escaped before rendering as rich text");
        verify(editor.highlightedHTML.indexOf("&quot;") >= 0, "quotes must be escaped before rendering as rich text");
        var longBracketScript = "local value = [[<literal>]]\n--[=[ <block comment> ]=]";
        editor.text = longBracketScript;
        tryVerify(function() { return editor.highlightedHTML.indexOf("&lt;literal&gt;") >= 0 && editor.highlightedHTML.indexOf("&lt;block comment&gt;") >= 0; });
        verify(editor.highlightedHTML.indexOf("#16803c") >= 0, "Lua long-bracket strings should be highlighted");
        verify(editor.highlightedHTML.indexOf("#78838e") >= 0, "Lua long-bracket comments should be highlighted");
        verify(editor.highlightedHTML.indexOf("&lt;literal&gt;") >= 0, "long-bracket strings must remain escaped");
        compare(editor.lineNumberText("first\nsecond\nthird"), "1\n2\n3");
        // Exercise highlighting close to the 64 KiB label-script source cap.
        var longScript = "local n = 1\n".repeat(5000);
        editor.text = longScript;
        tryVerify(function() { return editor.highlightedHTML.length > longScript.length && editor.highlightedHTML.indexOf("local") >= 0; });
        verify(editor.highlightedHTML.length > longScript.length, "large scripts should still be highlighted");
        compare(editor.highlightedLineNumbers.split("\n").length, 5001);
        viewport.attributeFieldHints = [{name: "road class", type: "text"}];
        compare(viewport.luaFieldAccess("road class"), 'feature["road class"]');
        dialog.close();
    }

    function test_mapDragPansViewport() {
        findChild(appWindow, "layerSettingsDialog").close();
        findChild(appWindow, "attributeDialog").close();
        var canvas = findChild(appWindow, "goGisMapCanvas");
        var viewport = findChild(appWindow, "mapViewport");
        var mouseArea = findChild(appWindow, "mapMouseArea");
        var layerModel = findChild(appWindow, "layerModel");
        canvas.layerTreePayload = "[]";
        wait(100);
        tryCompare(layerModel, "count", 0);
        verify(mouseArea !== null);
        var beforeX = viewport.panX;
        var beforeY = viewport.panY;
        mouseDrag(mouseArea, mouseArea.width / 2, mouseArea.height / 2, 40, 25);
        verify(Math.abs(viewport.panX - beforeX) > 1 || Math.abs(viewport.panY - beforeY) > 1);
    }

    function test_mapCanvasPreservesCoordinateAspectRatio() {
        var canvas = findChild(appWindow, "goGisMapCanvas");
        var viewport = findChild(appWindow, "mapViewport");
        canvas.mapMetadataPayload = JSON.stringify({bounds: [0, 0, 200, 100], crs: "EPSG:3857"});
        canvas.mapMetadataGeneration += 1;
        tryCompare(viewport, "dataCRS", "EPSG:3857");
        compare(Math.round(canvas.width / canvas.height * 1000) / 1000, 2);
    }

    function test_rapidLayerMetadataKeepsWorldCenterAndMeterScale() {
        var canvas = findChild(appWindow, "goGisMapCanvas");
        var viewport = findChild(appWindow, "mapViewport");
        var layerModel = findChild(appWindow, "layerModel");
        canvas.layerTreePayload = JSON.stringify([{name: "first", visible: true}]);
        canvas.mapMetadataPayload = JSON.stringify({bounds: [0, 0, 200, 100], crs: "EPSG:5186"});
        canvas.mapMetadataGeneration += 1;
        tryVerify(function() { return layerModel.count === 1 && layerModel.get(0).name === "first"; });
        tryVerify(function() { return viewport.dataBounds[2] === 200 && viewport.dataCRS === "EPSG:5186"; });
        viewport.mapZoom = 2.3;
        viewport.panX = 70;
        viewport.panY = -30;
        function center() {
            var bounds = viewport.dataBounds;
            return [bounds[0] + (0.5 - viewport.panX / (canvas.width * viewport.mapZoom)) * (bounds[2] - bounds[0]),
                    bounds[1] + (0.5 + viewport.panY / (canvas.height * viewport.mapZoom)) * (bounds[3] - bounds[1])];
        }
        function unitsPerPixel() {
            return (viewport.dataBounds[2] - viewport.dataBounds[0]) / (canvas.width * viewport.mapZoom);
        }
        var beforeCenter = center();
        var beforeScale = unitsPerPixel();
        canvas.layerTreePayload = JSON.stringify([{name: "first", visible: true}, {name: "second", visible: true}]);
        canvas.mapMetadataPayload = JSON.stringify({bounds: [-100, -500, 1100, 300], crs: "EPSG:5186"});
        canvas.mapMetadataGeneration += 1;
        tryVerify(function() { return viewport.dataBounds[2] === 1100 && layerModel.count === 2; });
        canvas.layerTreePayload = JSON.stringify([{name: "first", visible: true}, {name: "second", visible: true}, {name: "third", visible: true}]);
        canvas.mapMetadataPayload = JSON.stringify({bounds: [-1000, -600, 1200, 400], crs: "EPSG:5186"});
        canvas.mapMetadataGeneration += 1;
        tryVerify(function() { return viewport.dataBounds[2] === 1200 && layerModel.count === 3; });
        compare(canvas.mapMetadataAppliedGeneration, canvas.mapMetadataGeneration);
        var afterCenter = center();
        verify(Math.abs(afterCenter[0] - beforeCenter[0]) < 1e-6);
        verify(Math.abs(afterCenter[1] - beforeCenter[1]) < 1e-6);
        verify(Math.abs(unitsPerPixel() - beforeScale) < 1e-9);
        var spanX = viewport.dataBounds[2] - viewport.dataBounds[0];
        var spanY = viewport.dataBounds[3] - viewport.dataBounds[1];
        verify(Math.abs(canvas.width / spanX - canvas.height / spanY) < 1e-9);

        canvas.mapMetadataPayload = JSON.stringify({bounds: [126, 36, 128, 38], crs: "EPSG:4326"});
        canvas.mapMetadataGeneration += 1;
        tryCompare(viewport, "dataCRS", "EPSG:4326");
        var expectedAspect = Math.cos(37 * Math.PI / 180);
        verify(Math.abs(canvas.width / canvas.height - expectedAspect) < 1e-6);
    }

    function test_nullLabelAndHandlePayloadsBecomeEmptyLists() {
        var canvas = findChild(appWindow, "goGisMapCanvas");
        var labels = findChild(appWindow, "mapLabelModel");
        var handles = findChild(appWindow, "vertexHandleModel");
        verify(canvas !== null);
        verify(labels !== null);
        verify(handles !== null);
        canvas.layerLabelPayload = "null";
        canvas.layerLabelGeneration += 1;
        canvas.vertexHandlePayload = "null";
        canvas.vertexHandleGeneration += 1;
        tryCompare(labels, "count", 0);
        tryCompare(handles, "count", 0);
    }

    function test_projectCRSChangeAndDXFEncodingDialogs() {
        var canvas = findChild(appWindow, "goGisMapCanvas");
        var projectDialog = findChild(appWindow, "projectCRSDialog");
        var projectCRS = findChild(appWindow, "projectCRSField");
        var encodingDialog = findChild(appWindow, "dxfEncodingDialog");
        var choice = findChild(appWindow, "dxfEncodingChoice");
        verify(canvas && projectDialog && projectCRS && encodingDialog && choice);

        projectDialog.open();
        projectCRS.text = "EPSG:5179";
        projectDialog.accept();
        compare(JSON.parse(canvas.layerSettingsPayload).operation, "project-crs");
        compare(JSON.parse(canvas.layerSettingsPayload).projectCrs, "EPSG:5179");

        appWindow.language = "ko";
        encodingDialog.open();
        tryCompare(choice, "currentIndex", 0);
        compare(choice.currentValue, "ares-cp949");
        encodingDialog.close();
        appWindow.language = "jp";
        encodingDialog.open();
        tryCompare(choice, "currentIndex", 0);
        compare(choice.currentValue, "ares-shift-jis");
        encodingDialog.close();
        appWindow.language = "en";
    }

    function test_dxfLayerExportPlanCanExcludeAndRenameLayers() {
        var canvas = findChild(appWindow, "goGisMapCanvas");
        var layerModel = findChild(appWindow, "layerModel");
        var dialog = findChild(appWindow, "dxfEncodingDialog");
        var options = findChild(appWindow, "dxfLayerOptionsModel");
        var fileDialog = findChild(appWindow, "dxfExportDialog");
        verify(canvas && layerModel && dialog && options && fileDialog);
        var originalPayload = canvas.layerTreePayload;
        try {
            canvas.layerTreePayload = JSON.stringify([
                {name: "parcels", displayName: "0-연속지적도", geometryType: "Polygon", visible: true},
                {name: "points", displayName: "0-지적도근점", geometryType: "Point", visible: true},
                {name: "buildings", displayName: "0-건물", geometryType: "Polygon", visible: true}
            ]);
            tryCompare(layerModel, "count", 3);
            options.clear();
            dialog.open();
            tryCompare(options, "count", 3);
            options.setProperty(1, "included", false);
            options.setProperty(2, "cadName", "Buildings CAD");
            options.setProperty(2, "labelsEnabled", false);
            dialog.accept();
            var plan = JSON.parse(fileDialog.selectedOptions);
            compare(plan.length, 3);
            compare(plan[0].cadName, "0-연속지적도");
            compare(plan[1].include, false);
            compare(plan[2].cadName, "Buildings CAD");
            compare(plan[2].labels, false);
            fileDialog.close();
        } finally {
            dialog.close();
            canvas.layerTreePayload = originalPayload;
        }
    }

    function test_projectCRSPickerShowsCommonNearbyAndSearchableCatalog() {
        var dialog = findChild(appWindow, "projectCRSDialog");
        var field = findChild(appWindow, "projectCRSField");
        var tabs = findChild(appWindow, "crsCategoryTabs");
        var common = findChild(appWindow, "commonCRSList");
        var nearby = findChild(appWindow, "nearbyCRSList");
        var country = findChild(appWindow, "countryCRSChoice");
        var all = findChild(appWindow, "allCRSList");
        var search = findChild(appWindow, "crsSearchField");
        var details = findChild(appWindow, "selectedCRSDetails");
        var canvas = findChild(appWindow, "goGisMapCanvas");
        var viewport = findChild(appWindow, "mapViewport");
        verify(dialog && field && tabs && common && nearby && country && all && search && details);
        var previousCatalog = dialog.catalogResponse;
        var previousCRS = viewport.dataCRS;
        try {
            dialog.catalogResponse = {available: true, entries: [
                {code: "EPSG:5186", name: "Korea 2000 / Central Belt", area: "Republic of Korea", kind: "projected"},
                {code: "EPSG:5179", name: "Korea 2000 / Unified CS", area: "Republic of Korea", kind: "projected"},
                {code: "EPSG:5187", name: "Korea 2000 / East Belt", area: "Republic of Korea", kind: "projected"},
                {code: "EPSG:6677", name: "JGD2011 / Japan Plane Rectangular CS IX", area: "Japan", kind: "projected"},
                {code: "EPSG:5070", name: "NAD83 / Conus Albers", area: "United States", kind: "projected"},
                {code: "EPSG:32643", name: "WGS 84 / UTM zone 43N", area: "India", kind: "projected"},
                {code: "EPSG:26916", name: "NAD83 / UTM zone 16N", area: "United States - Indiana", kind: "projected"},
                {code: "EPSG:32652", name: "WGS 84 / UTM zone 52N", area: "Asia", kind: "projected"},
                {code: "EPSG:4326", name: "WGS 84", area: "World", kind: "geographic"}
            ]};
            viewport.dataCRS = "EPSG:6677";
            dialog.open();
            tryCompare(dialog, "visible", true);
            compare(country.currentIndex, 1);
            compare(field.text, "EPSG:6677");
            verify(common.count >= 3);
            tabs.currentIndex = 1;
            country.currentIndex = 0;
            tryCompare(nearby, "count", 3);
            country.currentIndex = 1;
            tryCompare(nearby, "count", 1);
            compare(nearby.model[0].code, "EPSG:6677");
            country.currentIndex = 3;
            tryCompare(nearby, "count", 2);
            country.currentIndex = 10;
            tryCompare(nearby, "count", 1);
            compare(nearby.model[0].code, "EPSG:32643");
            tabs.currentIndex = 2;
            search.text = "east belt";
            tryCompare(all, "count", 1);
            tryVerify(function() { return all.itemAtIndex(0) !== null; });
            mouseClick(all.itemAtIndex(0));
            compare(field.text, "EPSG:5187");
            verify(details.text.indexOf("East Belt") >= 0);
            dialog.accept();
            compare(JSON.parse(canvas.layerSettingsPayload).projectCrs, "EPSG:5187");
        } finally {
            dialog.close();
            dialog.catalogResponse = previousCatalog;
            viewport.dataCRS = previousCRS;
        }
    }

    function test_colorPickerChangesStyleColorWithoutTypingHex() {
        var canvas = findChild(appWindow, "goGisMapCanvas");
        var viewport = findChild(appWindow, "mapViewport");
        var dialog = findChild(appWindow, "layerSettingsDialog");
        var picker = findChild(appWindow, "layerColorPicker");
        var presets = findChild(appWindow, "presetColorRepeater");
        var customPicker = findChild(appWindow, "nativeLayerColorPicker");
        var hexField = findChild(appWindow, "presetHexField");
        var applyColor = findChild(appWindow, "applyPickedColorButton");
        var pointField = findChild(appWindow, "pointColorField");
        var lineField = findChild(appWindow, "lineColorField");
        var polygonField = findChild(appWindow, "polygonColorField");
        verify(picker && presets && customPicker && hexField && applyColor && pointField && lineField && polygonField);
        canvas.layerTreePayload = JSON.stringify([{name: "colors", visible: true, geometryType: "GEOMETRYCOLLECTION"}]);
        tryVerify(function() {
            var model = findChild(appWindow, "layerModel");
            return model.count === 1 && model.get(0).name === "colors";
        });
        viewport.openLayerPropertiesForCategory("colors", "symbology");
        tryCompare(dialog, "visible", true);
        try {
            var cases = [
                {button: "pointColorPickerButton", field: pointField, selected: "#80123456", color: "#123456"},
                {button: "lineColorPickerButton", field: lineField, color: "#abcdef"},
                {button: "polygonColorPickerButton", field: polygonField, color: "#654321"}
            ];
            for (var i = 0; i < cases.length; ++i) {
                var button = findChild(appWindow, cases[i].button);
                verify(button && button.visible);
                mouseClick(button);
                compare(picker.targetField, cases[i].field);
                if (i === 0) {
                    compare(presets.count, 10);
                    hexField.text = "#12";
                    compare(applyColor.enabled, false);
                    mouseClick(presets.itemAt(2));
                    compare(picker.selectedColor, "#d1495b");
                    compare(applyColor.enabled, true);
                    mouseClick(findChild(appWindow, "customColorPickerButton"));
                    customPicker.selectedColor = "#80123456";
                    customPicker.accept();
                    compare(picker.selectedColor, "#123456");
                }
                picker.selectedColor = cases[i].selected || cases[i].color;
                picker.accept();
                compare(cases[i].field.text.toLowerCase(), cases[i].color);
                compare(cases[i].field.readOnly, true);
            }
            dialog.submitLayer();
            var style = JSON.parse(canvas.layerSettingsPayload).style;
            compare(style.pointColor, "#123456");
            compare(style.lineColor, "#abcdef");
            compare(style.polygonColor, "#654321");
        } finally {
            picker.reject();
            dialog.close();
        }
    }

    function test_statusCoordinatesUseFourDecimalsAndDigitGrouping() {
        var viewport = findChild(appWindow, "mapViewport");
        var canvas = findChild(appWindow, "goGisMapCanvas");
        verify(viewport && canvas);
        var oldBounds = viewport.dataBounds;
        var oldCRS = viewport.dataCRS;
        var oldZoom = viewport.mapZoom;
        var oldCursorX = viewport.cursorX;
        var oldCursorY = viewport.cursorY;
        var oldCursorValid = viewport.cursorValid;
        viewport.dataBounds = [0, 0, 1000000, 100000];
        viewport.dataCRS = "EPSG:5186";
        viewport.mapZoom = 1;
        viewport.cursorX = canvas.x + canvas.width / 2;
        viewport.cursorY = canvas.y + canvas.height / 2;
        viewport.cursorValid = true;
        compare(viewport.currentMapCoordinate(), "X 500,000.0000  Y 50,000.0000");
        viewport.dataBounds = oldBounds;
        viewport.dataCRS = oldCRS;
        viewport.mapZoom = oldZoom;
        viewport.cursorX = oldCursorX;
        viewport.cursorY = oldCursorY;
        viewport.cursorValid = oldCursorValid;
    }

    function test_primaryStatusKeepsCRSCoordinatesAndScaleVisible() {
        var viewport = findChild(appWindow, "mapViewport");
        var canvas = findChild(appWindow, "goGisMapCanvas");
        var layerModel = findChild(appWindow, "layerModel");
        var crs = findChild(appWindow, "crsStatusLabel");
        var coordinates = findChild(appWindow, "coordinateStatusLabel");
        var scale = findChild(appWindow, "scaleStatusLabel");
        var dialog = findChild(appWindow, "coordinateNavigationDialog");
        var navButton = findChild(appWindow, "openCoordinateNavigationButton");
        verify(viewport && canvas && layerModel && crs && coordinates && scale && dialog && navButton);
        var oldPayload = canvas.layerTreePayload;
        var oldBounds = viewport.dataBounds;
        var oldCRS = viewport.dataCRS;
        var oldZoom = viewport.mapZoom;
        var oldPanX = viewport.panX;
        var oldPanY = viewport.panY;
        var oldValid = viewport.cursorValid;
        var oldLanguage = appWindow.language;
        try {
            canvas.layerTreePayload = JSON.stringify([{name: "status-map", visible: true}]);
            tryCompare(layerModel, "count", 1);
            viewport.dataBounds = [0, 0, 1000, 1000];
            viewport.dataCRS = "EPSG:5186";
            viewport.mapZoom = 1;
            viewport.panX = 0;
            viewport.panY = 0;
            viewport.cursorValid = false;
            appWindow.language = "ko";
            compare(crs.text, "EPSG:5186");
            compare(coordinates.text, "중심  X 500.0000  Y 500.0000");
            verify(scale.text.indexOf("≈ 1:") === 0);
            verify(coordinates.width >= coordinates.implicitWidth, "primary coordinates should not be elided at normal width");
            var rightEdge = scale.mapToItem(appWindow.contentItem, scale.width, 0);
            verify(rightEdge.x <= appWindow.contentItem.width, "scale must remain inside the window");
            viewport.cursorX = canvas.x + canvas.width / 2;
            viewport.cursorY = canvas.y + canvas.height / 2;
            viewport.cursorValid = true;
            compare(coordinates.text, "커서  X 500.0000  Y 500.0000");
            mouseClick(navButton);
            tryVerify(function() { return dialog.visible; });
            var xInput = findChild(appWindow, "coordinateXInput");
            var yInput = findChild(appWindow, "coordinateYInput");
            verify(xInput && yInput);
            xInput.text = "200";
            yInput.text = "300";
            mouseClick(findChild(appWindow, "coordinateGoButton"));
            tryVerify(function() { return !dialog.visible; });
            viewport.cursorValid = false;
            compare(coordinates.text, "중심  X 200.0000  Y 300.0000");
        } finally {
            dialog.close();
            canvas.layerTreePayload = oldPayload;
            viewport.dataBounds = oldBounds;
            viewport.dataCRS = oldCRS;
            viewport.mapZoom = oldZoom;
            viewport.panX = oldPanX;
            viewport.panY = oldPanY;
            viewport.cursorValid = oldValid;
            appWindow.language = oldLanguage;
        }
    }

    function test_vertexHandleDragEmitsFeatureVertexEdit() {
        findChild(appWindow, "layerSettingsDialog").close();
        findChild(appWindow, "attributeDialog").close();
        var canvas = findChild(appWindow, "goGisMapCanvas");
        var layerModel = findChild(appWindow, "layerModel");
        var toggle = findChild(appWindow, "toggleVertexEditButton");
        canvas.layerTreePayload = JSON.stringify([{name: "roads", visible: true}]);
        tryCompare(layerModel, "count", 1);
        canvas.vertexHandlePayload = JSON.stringify([{x: 0.5, y: 0.5, vertexIndex: 3}]);
        canvas.vertexHandleGeneration += 1;
        var repeater = findChild(appWindow, "vertexHandleRepeater");
        verify(repeater !== null);
        tryCompare(repeater, "count", 1);
        verify(toggle.enabled);
        toggle.click();
        tryCompare(appWindow, "vertexEditMode", true);
        var handle = repeater.itemAt(0);
        verify(handle !== null);
        var dragArea = handle.dragArea;
        verify(dragArea !== null);
        verify(dragArea.enabled && dragArea.visible);
        verify(handle.visible);
        var eventTarget = appWindow.contentItem;
        var start = handle.mapToItem(eventTarget, handle.width / 2, handle.height / 2);
        mousePress(eventTarget, start.x, start.y);
        tryCompare(dragArea, "pressed", true);
        mouseMove(eventTarget, start.x + 24, start.y - 16, 100);
        mouseRelease(eventTarget, start.x + 24, start.y - 16);
        compare(canvas.editAction, "moveVertex");
        var edit = JSON.parse(canvas.editValue);
        compare(edit.vertexIndex, 3);
        verify(isFinite(edit.x) && isFinite(edit.y));
        verify(Math.abs(edit.x - 0.5) > 0.001 || Math.abs(edit.y - 0.5) > 0.001);
    }

    function test_applySubmitsLayerSettings() {
        appWindow.language = "en";
        var canvas = findChild(appWindow, "goGisMapCanvas");
        var renderStatus = findChild(appWindow, "renderStatusLabel");
        var loadIndicator = findChild(appWindow, "loadBusyIndicator");
        var layerModel = findChild(appWindow, "layerModel");
        var dialog = findChild(appWindow, "layerSettingsDialog");
        var attributesDialog = findChild(appWindow, "attributeDialog");
        var viewport = findChild(appWindow, "mapViewport");
        var openAttributesButton = findChild(appWindow, "openAttributesButton");
        var displayName = findChild(appWindow, "displayNameField");
        var sourcePath = findChild(appWindow, "sourcePathField");
        var sourceLayer = findChild(appWindow, "sourceLayerField");
        var sourceEncoding = findChild(appWindow, "sourceEncodingField");
        var sourceCRS = findChild(appWindow, "sourceCRSField");
        var sourceChangeWarning = findChild(appWindow, "sourceChangeWarning");
        var visible = findChild(appWindow, "visibleField");
        var pointColor = findChild(appWindow, "pointColorField");
        var pointSize = findChild(appWindow, "pointSizeField");
        var lineColor = findChild(appWindow, "lineColorField");
        var lineWidth = findChild(appWindow, "lineWidthField");
        var polygonColor = findChild(appWindow, "polygonColorField");
        var fillOpacity = findChild(appWindow, "fillOpacityField");
        var labelsEnabled = findChild(appWindow, "labelsEnabledField");
        var labelExpression = findChild(appWindow, "labelExpressionField");
        var labelRule = findChild(appWindow, "labelRuleField");
        var labelLua = findChild(appWindow, "labelLuaField");
        var luaDialog = findChild(appWindow, "luaEditorDialog");
        var luaOpenButton = findChild(appWindow, "openLuaEditorButton");
        var labelRuleEditor = findChild(appWindow, "labelRuleEditor");
        var displayRuleEditor = findChild(appWindow, "featureDisplayRuleEditor");
        var labelPlacement = findChild(appWindow, "labelPlacementField");
        var labelRotation = findChild(appWindow, "labelRotationField");
        var pointPlacement = findChild(appWindow, "pointLabelPlacementField");
        var pointOffset = findChild(appWindow, "pointLabelOffsetField");
        var labelHeight = findChild(appWindow, "labelHeightField");
        var labelMinScale = findChild(appWindow, "labelMinScaleField");
        var labelMaxScale = findChild(appWindow, "labelMaxScaleField");
        dialogItem = dialog;
        verify(canvas !== null);
        verify(renderStatus !== null);
        verify(loadIndicator !== null);
        verify(layerModel !== null);
        compare(layerModel.count, 0, "new projects should start without demo layers");
        verify(canvas.logicalPixelsPerMm > 0);
        verify(dialog !== null);
        verify(attributesDialog !== null);
        verify(viewport !== null);
        verify(openAttributesButton !== null);
        verify(displayName !== null);
        verify(sourcePath !== null);
        verify(sourceLayer !== null);
        verify(sourceEncoding !== null);
        verify(sourceCRS !== null);
        verify(sourceChangeWarning !== null);
        verify(visible !== null);
        verify(pointColor !== null);
        verify(pointSize !== null);
        verify(lineColor !== null);
        verify(lineWidth !== null);
        verify(polygonColor !== null);
        verify(fillOpacity !== null);
        verify(labelsEnabled !== null);
        verify(labelExpression !== null);
        verify(labelRule !== null);
        verify(labelLua !== null);
        verify(luaDialog !== null);
        verify(luaOpenButton !== null);
        verify(labelRuleEditor !== null);
        verify(displayRuleEditor !== null);
        verify(labelPlacement !== null);
        verify(labelRotation !== null);
        verify(pointPlacement !== null);
        verify(pointOffset !== null);
        verify(labelHeight !== null);
        verify(labelMinScale !== null);
        verify(labelMaxScale !== null);

        canvas.layerTreePayload = JSON.stringify([
            {
                name: "roads",
                displayName: "Named roads",
                sourcePath: "/data/roads.shp",
                sourceLayerName: "roads",
                sourceEncoding: "EUC-KR",
                sourceCrs: "EPSG:5186",
                visible: true,
                style: {
                    pointColor: "#112233",
                    pointSizeMm: 2.4,
                    lineColor: "#445566",
                    lineWidthMm: 0.8,
                    polygonColor: "#778899",
                    fillOpacity: 0.4
                },
                labels: {
                    enabled: true,
                    expression: "${label}",
                    rule: "return feature.visible == true",
                    luaScript: "return feature.label",
                    placement: "center-rotated",
                    rotationField: "angle",
                    pointPlacement: "SW",
                    pointOffsetMm: 2,
                    heightMm: 2,
                    minScale: 500,
                    maxScale: 25000
                },
                displayRule: "return feature.visible == true"
            }
        ]);
        tryCompare(layerModel, "count", 1);
        viewport.showLayerContextMenu("roads");
        var contextMenu = findChild(appWindow, "layerContextMenu");
        tryCompare(contextMenu, "visible", true);
        contextMenu.dismiss();
        mouseClick(openAttributesButton);
        tryCompare(attributesDialog, "visible", true);
        attributesDialog.close();
        viewport.openLayerPropertiesForCategory("roads", "general");
        tryCompare(dialog, "visible", true);
        wait(300);
        compare(dialog.targetLayerName, "roads");
        compare(displayName.text, "Named roads");
        dialog.activeCategory = "source";
        compare(sourcePath.text, "/data/roads.shp");
        compare(sourceLayer.text, "roads");
        compare(sourceEncoding.editText, "EUC-KR");
        compare(sourceCRS.text, "EPSG:5186");
        compare(visible.checked, true);
        compare(sourceChangeWarning.visible, false);
        dialog.activeCategory = "symbology";
        compare(pointColor.text, "#112233");
        compare(pointSize.text, "2.4");
        compare(lineColor.text, "#445566");
        compare(lineWidth.text, "0.8");
        compare(polygonColor.text, "#778899");
        compare(fillOpacity.text, "0.4");
        dialog.activeCategory = "labels";
        compare(labelsEnabled.checked, true);
        compare(labelExpression.text, "${label}");
        canvas.layerLabelPayload = JSON.stringify([{layer: "roads", text: "Sample", x: 0.5, y: 0.5, heightMm: 2.5, minScale: 0, maxScale: 0}]);
        canvas.layerLabelGeneration += 1;
        var labelHint = findChild(appWindow, "labelVisibilityHint");
        verify(labelHint !== null);
        tryVerify(function() { return labelHint.text.indexOf("1 labels are available") >= 0; });
        var labelRepeater = findChild(appWindow, "mapLabelRepeater");
        verify(labelRepeater !== null);
        tryCompare(labelRepeater, "count", 1);
        var mapLabel = labelRepeater.itemAt(0);
        verify(mapLabel !== null, "label delegate must be instantiated");
        compare(mapLabel.text, "Sample");
        tryVerify(function() { return mapLabel.visible; }, 1000, "label must be visible on the map");
        compare(labelRule.text, "return feature.visible == true");
        luaDialog.open();
        tryCompare(luaDialog, "visible", true);
        compare(labelRuleEditor.text, "return feature.visible == true");
        compare(labelLua.text, "return feature.label");
        compare(labelPlacement.currentIndex, 3);
        compare(labelRotation.text, "angle");
        compare(pointPlacement.currentIndex, 5);
        compare(pointOffset.text, "2");
        compare(labelHeight.text, "2");
        compare(labelMinScale.text, "500");
        compare(labelMaxScale.text, "25000");
        dialog.activeCategory = "general";
        displayName.text = "Renamed roads";
        dialog.activeCategory = "source";
        sourcePath.text = "/tmp/roads.shp";
        sourceLayer.text = "roads_internal";
        sourceEncoding.editText = "CP949";
        sourceCRS.text = "EPSG:5179";
        compare(sourceChangeWarning.visible, true);
        verify(sourceChangeWarning.text.indexOf("Save unsaved feature edits") >= 0);
        visible.checked = false;
        dialog.activeCategory = "symbology";
        pointColor.text = "#aabbcc";
        pointSize.text = "3.2";
        lineColor.text = "#010203";
        lineWidth.text = "1.4";
        polygonColor.text = "#123456";
        fillOpacity.text = "0.6";
        labelsEnabled.checked = true;
        dialog.activeCategory = "labels";
        labelExpression.text = "${name}";
        labelRuleEditor.text = "return feature.active == true";
        labelLua.text = "return feature.name";
        displayRuleEditor.text = "return feature.visible == true";
        var luaOkButton = luaDialog.standardButton(Dialog.Ok);
        verify(luaOkButton !== null);
        mouseClick(luaOkButton);
        tryCompare(luaDialog, "visible", false);
        labelPlacement.currentIndex = 2;
        labelRotation.text = "angle";
        labelHeight.text = "3";
        labelMinScale.text = "1000";
        labelMaxScale.text = "50000";
        var applyButton = dialog.standardButton(Dialog.Apply);
        verify(applyButton !== null);
        verify(applyButton.enabled);
        mouseClick(applyButton);

        tryCompare(appliedSpy, "count", 1);
        compare(canvas.layerSettingsGeneration, 1);
        var request = JSON.parse(canvas.layerSettingsPayload);
        compare(request.name, "roads");
        compare(request.displayName, "Renamed roads");
        compare(request.sourcePath, "/tmp/roads.shp");
        compare(request.sourceLayerName, "roads_internal");
        compare(request.sourceEncoding, "CP949");
        compare(request.sourceCrs, "EPSG:5179");
        compare(request.visible, false);
        compare(request.style.pointColor, "#aabbcc");
        compare(request.style.pointSizeMm, 3.2);
        compare(request.style.lineColor, "#010203");
        compare(request.style.lineWidthMm, 1.4);
        compare(request.style.polygonColor, "#123456");
        compare(request.style.fillOpacity, 0.6);
        compare(request.labels.enabled, true);
        compare(request.labels.expression, "${name}");
        compare(request.labels.rule, "return feature.active == true");
        compare(request.labels.luaScript, "return feature.name");
        compare(request.displayRule, "return feature.visible == true");
        compare(request.labels.placement, "free-angle");
        compare(request.labels.rotationField, "");
        compare(request.labels.pointPlacement, "SW");
        compare(request.labels.pointOffsetMm, 2);
        compare(request.labels.heightMm, 3);
        compare(request.labels.minScale, 1000);
        compare(request.labels.maxScale, 50000);
        canvas.renderStatus = "Layer settings failed: invalid color";
        tryCompare(renderStatus, "text", "Layer settings failed: invalid color");
        compare(renderStatus.color.toString(), "#b42318");
        canvas.renderStatus = "Layer settings applied";
        tryCompare(renderStatus, "text", "Layer settings applied");
        compare(renderStatus.color.toString(), "#2e7d32");
    }

    function test_layerPropertyLayoutFieldHintsAndOutlineOnly() {
        var canvas = findChild(appWindow, "goGisMapCanvas");
        var dialog = findChild(appWindow, "layerSettingsDialog");
        var viewport = findChild(appWindow, "mapViewport");
        var scroll = findChild(appWindow, "layerSettingsScroll");
        var expression = findChild(appWindow, "labelExpressionField");
        var hintButton = findChild(appWindow, "labelFieldHintButton");
        var hintPopup = findChild(appWindow, "labelFieldHintPopup");
        var outlineOnly = findChild(appWindow, "outlineOnlyField");
        var fillOpacity = findChild(appWindow, "fillOpacityField");
        verify(dialog !== null && scroll !== null && hintPopup !== null);
        canvas.layerTreePayload = JSON.stringify([{name: "parcels", visible: true, geometryType: "POLYGON",
            style: {fillOpacity: 0.45}, labels: {expression: ""}}]);
        var layerModel = findChild(appWindow, "layerModel");
        tryCompare(layerModel, "count", 1);
        tryVerify(function() { return layerModel.get(0).name === "parcels"; });
        viewport.openLayerPropertiesForCategory("parcels", "labels");
        tryCompare(dialog, "visible", true);
        wait(100);
        verify(scroll.contentWidth <= scroll.availableWidth + 1, "property pages must not scroll horizontally");
        var placement = findChild(appWindow, "labelPlacementField");
        var rotation = findChild(appWindow, "labelRotationField");
        var heightField = findChild(appWindow, "labelHeightField");
        compare(placement.count, 4);
        compare(placement.currentIndex, 0);
        compare(rotation.visible, false, "rotation field is only needed for legacy field-angle settings");
        placement.currentIndex = 3;
        tryCompare(rotation, "visible", true);
        var placementX = placement.mapToItem(scroll, 0, 0).x;
        verify(Math.abs(rotation.mapToItem(scroll, 0, 0).x - placementX) < 1,
               "rotation and placement inputs should share a label column");
        verify(Math.abs(heightField.mapToItem(scroll, 0, 0).x - placementX) < 1,
               "numeric label inputs should align with placement");
        var originalWidth = appWindow.width;
        try {
            appWindow.width = 500;
            wait(50);
            verify(scroll.contentWidth <= scroll.availableWidth + 1,
                   "narrow property pages must not require horizontal scrolling");
            verify(expression.width > 100, "label template remains editable in a narrow window");
        } finally {
            appWindow.width = originalWidth;
            wait(50);
        }
        viewport.attributeFieldHints = [{name: "LOT_NO", type: "text"}, {name: "CODE", type: "integer"}];
        expression.text = "Parcel: ";
        expression.cursorPosition = expression.text.length;
        compare(dialog.activeCategory, "labels");
        compare(hintButton.visible, true);
        mouseClick(hintButton);
        tryCompare(hintPopup, "visible", true);
        var fieldList = findChild(hintPopup, "labelFieldList");
        verify(fieldList !== null);
        compare(viewport.attributeFieldHints.length, 2);
        compare(fieldList.count, 2);
        verify(fieldList.height > 0, "field list needs available height");
        tryVerify(function() { return fieldList.itemAtIndex(0) !== null; });
        var fieldRow = fieldList.itemAtIndex(0);
        verify(fieldRow !== null);
        verify(fieldRow.text.indexOf("LOT_NO") >= 0);
        mouseClick(fieldRow);
        tryCompare(hintPopup, "visible", false);
        compare(expression.text, "Parcel: ${LOT_NO}");
        dialog.activeCategory = "symbology";
        wait(50);
        compare(findChild(appWindow, "pointColorField").visible, false);
        compare(findChild(appWindow, "lineColorField").visible, true);
        compare(fillOpacity.text, "0.45");
        compare(outlineOnly.checked, false);
        mouseClick(outlineOnly);
        compare(fillOpacity.text, "0");
        compare(outlineOnly.checked, true);
        mouseClick(outlineOnly);
        compare(fillOpacity.text, "0.45");
        dialog.close();
    }

    function test_labelOrientationChoicesPersistAndClearUnusedRotationField() {
        var canvas = findChild(appWindow, "goGisMapCanvas");
        var viewport = findChild(appWindow, "mapViewport");
        var dialog = findChild(appWindow, "layerSettingsDialog");
        var placement = findChild(appWindow, "labelPlacementField");
        var rotation = findChild(appWindow, "labelRotationField");
        canvas.layerTreePayload = JSON.stringify([{name: "orientation", visible: true,
            labels: {enabled: true, expression: "${name}", placement: "center-rotated", rotationField: "angle", heightMm: 2.5}}]);
        tryVerify(function() { return findChild(appWindow, "layerModel").get(0).name === "orientation"; });
        viewport.openLayerPropertiesForCategory("orientation", "labels");
        tryCompare(dialog, "visible", true);
        compare(placement.currentIndex, 3);
        placement.currentIndex = 1;
        dialog.submitLayer();
        var northSouth = JSON.parse(canvas.layerSettingsPayload);
        compare(northSouth.labels.placement, "vertical");
        compare(northSouth.labels.rotationField, "");
        placement.currentIndex = 2;
        rotation.text = "";
        dialog.submitLayer();
        var free = JSON.parse(canvas.layerSettingsPayload);
        compare(free.labels.placement, "free-angle");
        compare(free.labels.rotationField, "");
        dialog.close();
    }

    function test_englishPlacementAndDXFChoicesContainNoKoreanLabels() {
        var canvas = findChild(appWindow, "goGisMapCanvas");
        var viewport = findChild(appWindow, "mapViewport");
        var dialog = findChild(appWindow, "layerSettingsDialog");
        var labelPlacement = findChild(appWindow, "labelPlacementField");
        var pointPlacement = findChild(appWindow, "pointLabelPlacementField");
        var dxfEncoding = findChild(appWindow, "dxfEncodingChoice");
        appWindow.language = "en";
        canvas.layerTreePayload = JSON.stringify([{name: "controls", visible: true, geometryType: "POINT",
            labels: {enabled: true, expression: "${name}", placement: "free-angle"}}]);
        tryVerify(function() { return findChild(appWindow, "layerModel").get(0).name === "controls"; });
        viewport.openLayerPropertiesForCategory("controls", "labels");
        tryCompare(dialog, "visible", true);
        compare(labelPlacement.model[0], "East–west horizontal");
        compare(labelPlacement.model[2], "Free angle (longest segment)");
        compare(pointPlacement.model[0], "Top");
        var korean = /[가-힣]/;
        for (var index = 0; index < labelPlacement.model.length; ++index)
            verify(!korean.test(String(labelPlacement.model[index])), "English label orientation contains Korean text");
        for (var pointIndex = 0; pointIndex < pointPlacement.model.length; ++pointIndex)
            verify(!korean.test(String(pointPlacement.model[pointIndex])), "English point placement contains Korean text");
        for (var encodingIndex = 0; encodingIndex < dxfEncoding.model.length; ++encodingIndex)
            verify(!korean.test(String(dxfEncoding.model[encodingIndex].label)), "English DXF encoding choice contains Korean text");
        dialog.close();
    }
}
