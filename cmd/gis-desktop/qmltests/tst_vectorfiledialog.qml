import QtQuick
import QtTest
import "../qml" as GoGISApp

TestCase {
    id: testCase
    name: "VectorFileDialog"
    when: windowShown
    property var appWindow

    Component {
        id: appComponent
        GoGISApp.Main {}
    }

    function initTestCase() {
        appWindow = appComponent.createObject(testCase);
        verify(appWindow !== null, "desktop QML should instantiate with the vector file dialog");
        appWindow.show();
        wait(100);
    }

    function cleanupTestCase() {
        if (appWindow)
            appWindow.destroy();
    }

    function test_emptyStateAndHeaderButtonsOpenVectorDialog() {
        var canvas = findChild(appWindow, "goGisMapCanvas");
        var layerModel = findChild(appWindow, "layerModel");
        var headerButton = findChild(appWindow, "addVectorFilesButton");
        var emptyStateButton = findChild(appWindow, "emptyStateAddVectorFilesButton");
        var dialog = findChild(appWindow, "addVectorFilesDialog");
        canvas.layerTreePayload = "[]";
        tryCompare(layerModel, "count", 0);
        verify(headerButton !== null && emptyStateButton !== null && dialog !== null);

        headerButton.clicked();
        tryCompare(dialog, "visible", true);
        dialog.close();
        tryCompare(dialog, "visible", false);
        emptyStateButton.clicked();
        tryCompare(dialog, "visible", true);
        dialog.close();
        tryCompare(dialog, "visible", false);
    }
}
