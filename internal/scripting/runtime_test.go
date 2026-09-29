package scripting

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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
	script := `local layers = gogis.layers(); assert(layers[1] == "roads"); gogis.set_property("roads", 1, "name", "한글 도로"); gogis.export_dxf("` + destination + `", "roads", "ares-cp949")`
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
	if !strings.Contains(string(contents), "ANSI_949") {
		t.Fatalf("Lua export profile was not passed through: %q", contents)
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

type recordingExporter struct {
	ctx     context.Context
	profile string
}

type scriptingSpatialOperator struct{}

func (scriptingSpatialOperator) Intersect(context.Context, core.Layer, core.Layer) (core.Layer, error) {
	return core.Layer{Features: []core.Feature{{ID: 9}}}, nil
}
func (scriptingSpatialOperator) Union(context.Context, core.Layer, core.Layer) (core.Layer, error) {
	return core.Layer{Features: []core.Feature{{ID: 9}}}, nil
}
func (scriptingSpatialOperator) Difference(context.Context, core.Layer, core.Layer) (core.Layer, error) {
	return core.Layer{Features: []core.Feature{{ID: 9}}}, nil
}
func (scriptingSpatialOperator) Buffer(context.Context, core.Layer, float64) (core.Layer, error) {
	return core.Layer{Features: []core.Feature{{ID: 10}}}, nil
}

func (e *recordingExporter) Export(ctx context.Context, _ string, _ core.Layer, profile string) error {
	e.ctx = ctx
	e.profile = profile
	return nil
}

func TestRuntimePassesRunContextAndProfileToExporter(t *testing.T) {
	service := commands.NewProjectService("demo", core.CRS{})
	if err := service.BeginEdit(); err != nil {
		t.Fatal(err)
	}
	if err := service.AddLayer(core.Layer{Name: "roads"}); err != nil {
		t.Fatal(err)
	}
	if err := service.Commit(); err != nil {
		t.Fatal(err)
	}
	exporter := &recordingExporter{}
	runtime := NewRuntime(service, exporter)
	defer runtime.Close()
	marker := &struct{}{}
	ctx := context.WithValue(context.Background(), struct{}{}, marker)
	if err := runtime.Run(ctx, `gogis.export_dxf("ignored.dxf", "roads", "ares-cp949")`); err != nil {
		t.Fatal(err)
	}
	if exporter.ctx != ctx {
		t.Fatal("Lua exporter did not receive the Run context")
	}
	if exporter.profile != "ares-cp949" {
		t.Fatalf("profile = %q", exporter.profile)
	}
}

func TestRuntimeSpatialUsesSharedCommandAndAddsResultLayer(t *testing.T) {
	service := commands.NewProjectService("demo", core.CRS{})
	if err := service.BeginEdit(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"left", "right"} {
		if err := service.AddLayer(core.Layer{Name: name}); err != nil {
			t.Fatal(err)
		}
	}
	if err := service.Commit(); err != nil {
		t.Fatal(err)
	}
	runtime := NewRuntime(service, nil, scriptingSpatialOperator{})
	defer runtime.Close()
	if err := runtime.Run(context.Background(), `gogis.spatial("intersect", "left", "right", "intersection")`); err != nil {
		t.Fatal(err)
	}
	project := service.Project()
	if len(project.Layers) != 3 || project.Layers[2].Name != "intersection" || project.Layers[2].Features[0].ID != 9 {
		t.Fatalf("unexpected spatial result: %#v", project.Layers)
	}
}

func TestRuntimeFilterUsesSharedCommandAndAddsResultLayer(t *testing.T) {
	service := commands.NewProjectService("demo", core.CRS{})
	if err := service.BeginEdit(); err != nil {
		t.Fatal(err)
	}
	if err := service.AddLayer(core.Layer{Name: "roads", Fields: []core.Field{{Name: "kind", Type: core.FieldTypeText}}, Features: []core.Feature{
		{ID: 1, Properties: map[string]any{"kind": "road"}},
		{ID: 2, Properties: map[string]any{"kind": "building"}},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := service.Commit(); err != nil {
		t.Fatal(err)
	}
	runtime := NewRuntime(service, nil)
	defer runtime.Close()
	if err := runtime.Run(context.Background(), `gogis.filter("roads", "kind", "road", "roads-only")`); err != nil {
		t.Fatal(err)
	}
	project := service.Project()
	if len(project.Layers) != 2 || len(project.Layers[1].Features) != 1 || project.Layers[1].Features[0].ID != 1 {
		t.Fatalf("unexpected filter result: %#v", project.Layers)
	}
}

func TestRuntimeLabelUsesSharedCommandAndAddsResultLayer(t *testing.T) {
	service := commands.NewProjectService("demo", core.CRS{})
	if err := service.BeginEdit(); err != nil {
		t.Fatal(err)
	}
	if err := service.AddLayer(core.Layer{Name: "roads", Features: []core.Feature{{ID: 1, Geometry: core.WKTGeometry{WKT: "POINT (1 2)"}, Properties: map[string]any{"name": "한글"}}}}); err != nil {
		t.Fatal(err)
	}
	if err := service.Commit(); err != nil {
		t.Fatal(err)
	}
	runtime := NewRuntime(service, nil)
	defer runtime.Close()
	if err := runtime.Run(context.Background(), `gogis.label("roads", "name", "labeled", 2, "Korean")`); err != nil {
		t.Fatal(err)
	}
	label := service.Project().Layers[1].Features[0].Label
	if label == nil || label.Text != "한글" || label.X != 1 || label.Y != 2 || label.Height != 2 || label.Style != "Korean" {
		t.Fatalf("unexpected Lua label: %#v", label)
	}
}
