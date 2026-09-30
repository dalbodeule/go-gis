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

    function test_applySubmitsLayerSettings() {
        var canvas = findChild(appWindow, "goGisMapCanvas");
        var renderStatus = findChild(appWindow, "renderStatusLabel");
        var loadIndicator = findChild(appWindow, "loadBusyIndicator");
        var layerModel = findChild(appWindow, "layerModel");
        var dialog = findChild(appWindow, "layerSettingsDialog");
        var displayName = findChild(appWindow, "displayNameField");
        var sourcePath = findChild(appWindow, "sourcePathField");
        var sourceLayer = findChild(appWindow, "sourceLayerField");
        var sourceEncoding = findChild(appWindow, "sourceEncodingField");
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
        var labelPlacement = findChild(appWindow, "labelPlacementField");
        var labelRotation = findChild(appWindow, "labelRotationField");
        var labelHeight = findChild(appWindow, "labelHeightField");
        var labelMinScale = findChild(appWindow, "labelMinScaleField");
        var labelMaxScale = findChild(appWindow, "labelMaxScaleField");
        dialogItem = dialog;
        verify(canvas !== null);
        verify(renderStatus !== null);
        verify(loadIndicator !== null);
        verify(layerModel !== null);
        verify(canvas.logicalPixelsPerMm > 0);
        verify(dialog !== null);
        verify(displayName !== null);
        verify(sourcePath !== null);
        verify(sourceLayer !== null);
        verify(sourceEncoding !== null);
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
        verify(labelPlacement !== null);
        verify(labelRotation !== null);
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
                    heightMm: 2,
                    minScale: 500,
                    maxScale: 25000
                }
            }
        ]);
        tryCompare(layerModel, "count", 1);
        dialog.open();
        tryCompare(dialog, "visible", true);
        wait(300);
        compare(dialog.targetLayerName, "roads");
        compare(displayName.text, "Named roads");
        compare(sourcePath.text, "/data/roads.shp");
        compare(sourceLayer.text, "roads");
        compare(sourceEncoding.editText, "EUC-KR");
        compare(visible.checked, true);
        compare(sourceChangeWarning.visible, false);
        compare(pointColor.text, "#112233");
        compare(pointSize.text, "2.4");
        compare(lineColor.text, "#445566");
        compare(lineWidth.text, "0.8");
        compare(polygonColor.text, "#778899");
        compare(fillOpacity.text, "0.4");
        compare(labelsEnabled.checked, true);
        compare(labelExpression.text, "${label}");
        compare(labelRule.text, "return feature.visible == true");
        compare(labelLua.text, "return feature.label");
        compare(labelPlacement.currentIndex, 1);
        compare(labelRotation.text, "angle");
        compare(labelHeight.text, "2");
        compare(labelMinScale.text, "500");
        compare(labelMaxScale.text, "25000");
        displayName.text = "Renamed roads";
        sourcePath.text = "/tmp/roads.shp";
        sourceLayer.text = "roads_internal";
        sourceEncoding.editText = "CP949";
        compare(sourceChangeWarning.visible, true);
        verify(sourceChangeWarning.text.indexOf("Save unsaved feature edits") >= 0);
        visible.checked = false;
        pointColor.text = "#aabbcc";
        pointSize.text = "3.2";
        lineColor.text = "#010203";
        lineWidth.text = "1.4";
        polygonColor.text = "#123456";
        fillOpacity.text = "0.6";
        labelsEnabled.checked = true;
        labelExpression.text = "${name}";
        labelRule.text = "return feature.active == true";
        labelLua.text = "return feature.name";
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
        compare(request.labels.placement, "free-angle");
        compare(request.labels.rotationField, "angle");
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
}
