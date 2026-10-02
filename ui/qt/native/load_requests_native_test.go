//go:build qt && native

package native

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"testing"
	"time"

	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/qml"
)

func TestRealMapCanvasCapturesRapidLoadRequests(t *testing.T) {
	// Qt permits only one QApplication per process on macOS. Run the real
	// scene-graph check in a fresh process so -count=N remains reliable.
	if os.Getenv("GOGIS_BRIDGE_TEST_CHILD") != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRealMapCanvasCapturesRapidLoadRequests$", "-test.count=1")
		command.Env = append(os.Environ(), "GOGIS_BRIDGE_TEST_CHILD=1", "QT_QPA_PLATFORM=offscreen", "QSG_RENDER_LOOP=basic")
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("real MapCanvas subprocess failed: %v\n%s", err, output)
		}
		return
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	app := qt.NewQApplication([]string{"gogis-bridge-test", "-platform", "offscreen"})
	defer app.Delete()
	RegisterMapCanvas()
	engine := qml.NewQQmlApplicationEngine()
	defer engine.Delete()
	previousGeneration := LoadGeneration()
	engine.LoadData([]byte(fmt.Sprintf(`
import QtQuick
import QtQuick.Window
import GoGIS 1.0
Window {
    visible: true
    width: 320
    height: 240
    property int baseGeneration: %d
    MapCanvas {
        anchors.fill: parent
        property string loadRequestJournal: "[]"
        property int loadGeneration: 0
        property int loadCapturedGeneration: 0
        onLoadCapturedGenerationChanged: {
            var requests = JSON.parse(loadRequestJournal);
            loadRequestJournal = JSON.stringify(requests.filter(function(request) {
                return request.generation > loadCapturedGeneration;
            }));
        }
        Component.onCompleted: {
            loadRequestJournal = JSON.stringify([
                {generation: baseGeneration + 1, paths: ["/tmp/first.shp"]},
                {generation: baseGeneration + 2, paths: ["/tmp/second.shp"]}
            ]);
            loadGeneration = baseGeneration + 2;
        }
    }
}
`, previousGeneration)))
	deadline := time.Now().Add(5 * time.Second)
	for LoadGeneration() < previousGeneration+2 && time.Now().Before(deadline) {
		qt.QCoreApplication_ProcessEvents()
		time.Sleep(10 * time.Millisecond)
	}
	if LoadGeneration() != previousGeneration+2 {
		t.Fatalf("captured load generation = %d, want %d", LoadGeneration(), previousGeneration+2)
	}
	requests, err := CurrentLoadRequests()
	if err != nil {
		t.Fatal(err)
	}
	if len(requests) != 2 || len(requests[0]) != 1 || requests[0][0] != "/tmp/first.shp" ||
		len(requests[1]) != 1 || requests[1][0] != "/tmp/second.shp" {
		t.Fatalf("captured load requests = %v, want both selections in order", requests)
	}
	requests, err = CurrentLoadRequests()
	if err != nil || len(requests) != 0 {
		t.Fatalf("drained load requests = %v, err %v", requests, err)
	}
}
