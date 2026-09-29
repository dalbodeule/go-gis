import QtQuick
import QtQuick.Controls
import QtQuick.Layouts

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
            Label { text: "GPU chunk renderer: pending"; color: "#65717d" }
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
                    Layout.fillWidth: true
                    Layout.fillHeight: true
                    model: ["roads", "buildings", "labels"]
                    delegate: CheckDelegate {
                        width: ListView.view.width
                        text: modelData
                        checked: true
                    }
                }
            }
        }

        Frame {
            SplitView.fillWidth: true
            SplitView.fillHeight: true
            background: Rectangle { color: "#dfe5ea"; radius: 4 }
            Item {
                anchors.fill: parent
                // This is the hand-off point for QSGGeometryNode batches. The
                // Go scheduler already supplies immutable visible chunks; the
                // custom scene-graph item will replace this placeholder next.
                Rectangle {
                    anchors.centerIn: parent
                    width: 380
                    height: 92
                    radius: 6
                    color: "#f8fafb"
                    border.color: "#c6d0d8"
                    Column {
                        anchors.centerIn: parent
                        spacing: 6
                        Label { anchors.horizontalCenter: parent.horizontalCenter; text: "Map canvas"; font.bold: true }
                        Label { anchors.horizontalCenter: parent.horizontalCenter; text: "QSGGeometryNode adapter pending"; color: "#65717d" }
                    }
                }
            }
        }

        Frame {
            SplitView.preferredWidth: 260
            Layout.fillHeight: true
            ColumnLayout {
                anchors.fill: parent
                Label { text: "Properties"; font.bold: true }
                Label { text: "Select a feature to inspect its attributes."; wrapMode: Text.WordWrap; color: "#65717d" }
                Item { Layout.fillHeight: true }
            }
        }
    }

    footer: ToolBar {
        RowLayout {
            anchors.fill: parent
            anchors.leftMargin: 12
            anchors.rightMargin: 12
            Label { text: "Ready"; color: "#2e7d32" }
            Item { Layout.fillWidth: true }
            Label { text: "Chunk cache: 0 visible / generation 0"; color: "#65717d" }
        }
    }
}
