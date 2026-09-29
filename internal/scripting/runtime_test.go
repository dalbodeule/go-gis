package scripting

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gogis/drivers/dxf"
	"gogis/internal/commands"
	"gogis/internal/core"
)

func TestRuntimeChangesPropertyAndExports(t *testing.T) {
	service := commands.NewProjectService("demo", core.CRS{AuthorityCode: "EPSG:4326"})
	if err := service.BeginEdit(); err != nil {
		t.Fatal(err)
	}
	if err := service.AddLayer(core.Layer{Name: "roads", Editable: true, Features: []core.Feature{{ID: 1, Geometry: core.WKTGeometry{WKT: "POINT (1 2)"}, Properties: map[string]any{"name": "old"}}}}); err != nil {
		t.Fatal(err)
	}
	if err := service.Commit(); err != nil {
		t.Fatal(err)
	}

	runtime := NewRuntime(service, dxf.Exporter{})
	defer runtime.Close()
	destination := filepath.Join(t.TempDir(), "roads.dxf")
	script := `local layers = gogis.layers(); assert(layers[1] == "roads"); gogis.set_property("roads", 1, "name", "한글 도로"); gogis.export_dxf("` + destination + `", "roads")`
	if err := runtime.Run(context.Background(), script); err != nil {
		t.Fatal(err)
	}
	if got := service.Project().Layers[0].Features[0].Properties["name"]; got != "한글 도로" {
		t.Fatalf("property = %v", got)
	}
	contents, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if len(contents) == 0 {
		t.Fatal("DXF output is empty")
	}
}

func TestRuntimeSandboxBlocksHostCapabilities(t *testing.T) {
	service := commands.NewProjectService("demo", core.CRS{AuthorityCode: "EPSG:4326"})
	runtime := NewRuntime(service, nil)
	defer runtime.Close()
	for _, expression := range []string{
		`assert(io == nil)`,
		`assert(os == nil)`,
		`assert(package == nil)`,
		`assert(debug == nil)`,
		`assert(require == nil)`,
		`assert(dofile == nil)`,
		`assert(loadfile == nil)`,
		`assert(loadstring == nil)`,
	} {
		if err := runtime.Run(context.Background(), expression); err != nil {
			t.Fatalf("sandbox expression %q failed: %v", expression, err)
		}
	}
	if err := runtime.Run(context.Background(), `assert(string.upper("gogis") == "GOGIS"); assert(math.floor(2.9) == 2)`); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeCancellationStopsLongScript(t *testing.T) {
	service := commands.NewProjectService("demo", core.CRS{})
	runtime := NewRuntime(service, nil)
	defer runtime.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- runtime.Run(ctx, `while true do end`)
	}()
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("long script completed without cancellation")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("long script did not stop after cancellation")
	}
}
