package scripting

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"gogis/drivers/dxf"
	"gogis/internal/commands"
	"gogis/internal/core"
)

func TestLabelProgramEvaluatesPropertyTemplatesAndRules(t *testing.T) {
	textProgram, err := CompileLabelProgram(`return feature.street .. " " .. feature.number`)
	if err != nil {
		t.Fatal(err)
	}
	defer textProgram.Close()
	text, err := textProgram.EvaluateText(context.Background(), map[string]any{"street": "한강로", "number": 12})
	if err != nil || text != "한강로 12" {
		t.Fatalf("label text = %q, %v", text, err)
	}
	ruleProgram, err := CompileLabelProgram(`return feature.active == true and feature.rank >= 3`)
	if err != nil {
		t.Fatal(err)
	}
	defer ruleProgram.Close()
	matched, err := ruleProgram.EvaluateRule(context.Background(), map[string]any{"active": true, "rank": 4})
	if err != nil || !matched {
		t.Fatalf("label rule = %t, %v", matched, err)
	}
}

func TestValidateLabelComposerScriptsCompilesWithoutFeatures(t *testing.T) {
	if err := ValidateLabelComposerScripts(`return feature.NAME`, `return feature.ACTIVE == true`); err != nil {
		t.Fatalf("valid label scripts rejected: %v", err)
	}
	if err := ValidateLabelComposerScripts(`return (`, ""); err == nil {
		t.Fatal("invalid label syntax was accepted")
	}
	if err := ValidateLabelComposerScripts("", ""); err != nil {
		t.Fatalf("empty optional scripts rejected: %v", err)
	}
}

func TestLabelProgramIsSandboxedAndCancelable(t *testing.T) {
	program, err := CompileLabelProgram(`return os.execute("true")`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := program.EvaluateText(context.Background(), nil); err == nil {
		t.Fatal("label script gained access to os.execute")
	}
	program.Close()
	readOnlyProgram, err := CompileLabelProgram(`feature.name = "changed"; return feature.name`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := readOnlyProgram.EvaluateText(context.Background(), map[string]any{"name": "original"}); err == nil {
		t.Fatal("label script mutated its read-only feature table")
	}
	readOnlyProgram.Close()
	rawWriteProgram, err := CompileLabelProgram(`rawset(feature, "name", "changed"); return feature.name`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rawWriteProgram.EvaluateText(context.Background(), map[string]any{"name": "original"}); err == nil {
		t.Fatal("label script bypassed read-only feature proxy with rawset")
	}
	rawWriteProgram.Close()
	for _, mutation := range []string{
		`table.insert(feature, "changed")`,
		`table.remove(feature)`,
		`table.sort(feature)`,
	} {
		program, err := CompileLabelProgram(mutation + `; return "unexpected"`)
		if err != nil {
			t.Fatalf("compile guarded table operation %q: %v", mutation, err)
		}
		_, evaluateErr := program.EvaluateText(context.Background(), map[string]any{"name": "original"})
		program.Close()
		if evaluateErr == nil || !strings.Contains(evaluateErr.Error(), "read-only") {
			t.Fatalf("table operation %q mutated feature proxy, error=%v", mutation, evaluateErr)
		}
	}
	localTables, err := CompileLabelProgram(`local values = {}; table.insert(values, "ok"); local value = table.remove(values); return value == "ok" and #values == 0`)
	if err != nil {
		t.Fatal(err)
	}
	if matched, err := localTables.EvaluateRule(context.Background(), nil); err != nil || !matched {
		t.Fatalf("table mutators should remain available for local tables: matched=%t err=%v", matched, err)
	}
	localTables.Close()
	loopProgram, err := CompileLabelProgram(`while true do end`)
	if err != nil {
		t.Fatal(err)
	}
	defer loopProgram.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := loopProgram.Evaluate(ctx, nil); err == nil {
		t.Fatal("infinite label script did not observe cancellation")
	}
}

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
	// Lua treats backslashes in string literals as escape characters. Use
	// slash-separated paths so the generated script works on Windows too.
	luaDestination := filepath.ToSlash(destination)
	script := `local layers = gogis.layers(); assert(layers[1] == "roads"); gogis.set_property("roads", 1, "name", "한글 도로"); gogis.export_dxf("` + luaDestination + `", "roads", "ares-cp949")`
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

func TestRuntimeBoundsSource(t *testing.T) {
	runtime := NewRuntime(commands.NewProjectService("limits", core.CRS{}), nil)
	defer runtime.Close()
	if err := runtime.Run(context.Background(), strings.Repeat(" ", maxRuntimeScriptBytes+1)); err == nil {
		t.Fatal("oversized Lua source was accepted")
	}
	scriptPath := filepath.Join(t.TempDir(), "oversized.lua")
	if err := os.WriteFile(scriptPath, []byte(strings.Repeat(" ", maxRuntimeScriptBytes+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := runtime.RunFile(context.Background(), scriptPath); err == nil {
		t.Fatal("oversized Lua script file was accepted")
	}
}

func TestRuntimeLuaFilterChainAndConditionalComposedLabels(t *testing.T) {
	service := commands.NewProjectService("demo", core.CRS{AuthorityCode: "EPSG:4326"})
	features := []core.Feature{
		{ID: 1, Geometry: core.WKTGeometry{WKT: "POINT (1 2)"}, Properties: map[string]any{"kind": "road", "name": "한강로", "lanes": 2, "labelable": true, "enabled": true}, Label: &core.Label{Text: "previous label"}},
		{ID: 2, Geometry: core.WKTGeometry{WKT: "POINT (3 4)"}, Properties: map[string]any{"kind": "road", "name": "세종로", "lanes": 4, "labelable": false, "enabled": true}, Label: &core.Label{Text: "previous label"}},
		{ID: 3, Geometry: core.WKTGeometry{WKT: "POINT (5 6)"}, Properties: map[string]any{"kind": "stream", "name": "금강", "lanes": 0, "labelable": true, "enabled": false}, Label: &core.Label{Text: "previous label"}},
	}
	if err := service.BeginEdit(); err != nil {
		t.Fatal(err)
	}
	if err := service.AddLayer(core.Layer{Name: "roads", Fields: []core.Field{{Name: "kind"}, {Name: "name"}, {Name: "lanes"}}, Features: features}); err != nil {
		t.Fatal(err)
	}
	if err := service.Commit(); err != nil {
		t.Fatal(err)
	}
	runtime := NewRuntime(service, nil)
	defer runtime.Close()
	script := `
gogis.filter_lua("roads", "road_features", [[return feature.kind == "road"]])
gogis.filter_lua("road_features", "two_lane_roads", [[return feature.lanes == 2]])
gogis.label_lua("roads", "annotated", [[
    if feature.labelable then
        return string.format("%s · %d차선", feature.name, feature.lanes)
    end
    return nil
]], [[return feature.enabled == true and feature.labelable == true]], 2.5, "Korean")
`
	if err := runtime.Run(context.Background(), script); err != nil {
		t.Fatal(err)
	}
	project := service.Project()
	byName := make(map[string]core.Layer, len(project.Layers))
	for _, layer := range project.Layers {
		byName[layer.Name] = layer
	}
	if got := len(byName["road_features"].Features); got != 2 {
		t.Fatalf("first Lua filter retained %d features, want 2", got)
	}
	if got := len(byName["two_lane_roads"].Features); got != 1 || byName["two_lane_roads"].Features[0].ID != 1 {
		t.Fatalf("chained Lua filter result = %+v", byName["two_lane_roads"].Features)
	}
	annotated := byName["annotated"]
	if len(annotated.Features) != len(features) {
		t.Fatalf("label rule dropped unlabelled features: got %d, want %d", len(annotated.Features), len(features))
	}
	if label := annotated.Features[0].Label; label == nil || label.Text != "한강로 · 2차선" || label.Height != 2.5 || label.Style != "Korean" {
		t.Fatalf("composed label = %+v", label)
	}
	if annotated.Features[1].Label != nil || annotated.Features[2].Label != nil {
		t.Fatalf("nonmatching feature labels should remain empty: %+v", annotated.Features)
	}
	for index, sourceFeature := range byName["roads"].Features {
		if sourceFeature.Label == nil || sourceFeature.Label.Text != "previous label" {
			t.Fatalf("label composition mutated source feature %d: %+v", index, sourceFeature.Label)
		}
	}
}

func TestLuaFilterAndLabelResultsMatchSharedCommandAPI(t *testing.T) {
	newService := func() *commands.ProjectService {
		service := commands.NewProjectService("parity", core.CRS{AuthorityCode: "EPSG:4326"})
		layer := core.Layer{
			Name: "roads",
			Fields: []core.Field{
				{Name: "kind", Type: core.FieldTypeText},
				{Name: "name", Type: core.FieldTypeText},
				{Name: "lanes", Type: core.FieldTypeNumber},
				{Name: "enabled", Type: core.FieldTypeBool},
			},
			Features: []core.Feature{
				{ID: 1, Geometry: core.WKTGeometry{WKT: "POINT (1 2)"}, Properties: map[string]any{"kind": "road", "name": "한강로", "lanes": 2, "enabled": true}},
				{ID: 2, Geometry: core.WKTGeometry{WKT: "POINT (3 4)"}, Properties: map[string]any{"kind": "road", "name": "세종로", "lanes": 4, "enabled": true}},
				{ID: 3, Geometry: core.WKTGeometry{WKT: "POINT (5 6)"}, Properties: map[string]any{"kind": "stream", "name": "금강", "lanes": 0, "enabled": false}},
			},
		}
		if err := service.BeginEdit(); err != nil {
			t.Fatal(err)
		}
		if err := service.AddLayer(layer); err != nil {
			t.Fatal(err)
		}
		if err := service.Commit(); err != nil {
			t.Fatal(err)
		}
		return service
	}

	commandService := newService()
	if err := commandService.FilterProjectLayerBy(context.Background(), "roads", "road_features", func(_ context.Context, feature core.Feature) (bool, error) {
		return feature.Properties["kind"] == "road", nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := commandService.FilterProjectLayerBy(context.Background(), "road_features", "two_lane_roads", func(_ context.Context, feature core.Feature) (bool, error) {
		return feature.Properties["lanes"] == 2, nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := commandService.LabelProjectLayerWith(context.Background(), "roads", "annotated", 2.5, "Korean", func(_ context.Context, feature core.Feature) (string, error) {
		properties := feature.Properties
		if properties["enabled"] != true {
			return "", nil
		}
		return fmt.Sprintf("%s · %d차선", properties["name"], properties["lanes"]), nil
	}); err != nil {
		t.Fatal(err)
	}

	luaService := newService()
	runtime := NewRuntime(luaService, nil)
	defer runtime.Close()
	script := `
gogis.filter_lua("roads", "road_features", [[return feature.kind == "road"]])
gogis.filter_lua("road_features", "two_lane_roads", [[return feature.lanes == 2]])
gogis.label_lua("roads", "annotated", [[return string.format("%s · %d차선", feature.name, feature.lanes)]], [[return feature.enabled == true]], 2.5, "Korean")
`
	if err := runtime.Run(context.Background(), script); err != nil {
		t.Fatal(err)
	}
	if got, want := luaService.Project().Layers, commandService.Project().Layers; !reflect.DeepEqual(got, want) {
		t.Fatalf("Lua project differs from shared command API\nLua:     %#v\nCommand: %#v", got, want)
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
		`assert(rawset == nil)`,
		`assert(not pcall(function() return string.rep("x", 1000000000) end))`,
		`assert(not pcall(function() return string.format("%1000000000s", "x") end))`,
		`assert(string.byte == nil and string.find == nil and string.gmatch == nil and string.gsub == nil and string.match == nil)`,
		`assert(not pcall(function() return table.concat({string.rep("x", 600000), string.rep("y", 600000)}) end))`,
	} {
		if err := runtime.Run(context.Background(), expression); err != nil {
			t.Fatalf("sandbox expression %q failed: %v", expression, err)
		}
	}
	if err := runtime.Run(context.Background(), `assert(string.upper("gogis") == "GOGIS"); assert(math.floor(2.9) == 2)`); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Run(context.Background(), `assert(table.concat({"geo", "gis"}, "-") == "geo-gis")`); err != nil {
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

func TestRuntimeExecutionLimitStopsUnboundedScript(t *testing.T) {
	runtime := NewRuntime(commands.NewProjectService("demo", core.CRS{}), nil)
	defer runtime.Close()
	started := time.Now()
	err := runtime.runWithMaximumDuration(context.Background(), `while true do end`, 25*time.Millisecond)
	if err == nil {
		t.Fatal("unbounded Lua script completed without reaching its execution limit")
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("Lua execution limit took %s to stop the script", elapsed)
	}
}

func TestRuntimeExecutionLimitHonorsShorterCallerDeadline(t *testing.T) {
	runtime := NewRuntime(commands.NewProjectService("demo", core.CRS{}), nil)
	defer runtime.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	started := time.Now()
	err := runtime.runWithMaximumDuration(ctx, `while true do end`, time.Second)
	if err == nil {
		t.Fatal("Lua script completed without honoring the caller deadline")
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("caller deadline took %s to stop the script", elapsed)
	}
}

func TestRuntimeCloseIsIdempotentAndClosedRunReturnsError(t *testing.T) {
	runtime := NewRuntime(commands.NewProjectService("demo", core.CRS{}), nil)
	if err := runtime.Run(nil, `assert(true)`); err != nil {
		t.Fatalf("Run with nil context = %v", err)
	}
	runtime.Close()
	runtime.Close()
	if err := runtime.Run(context.Background(), `assert(true)`); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("Run after Close error = %v", err)
	}
}

func TestLabelEvaluationAcceptsNilContext(t *testing.T) {
	program, err := CompileLabelProgram(`return feature.name`)
	if err != nil {
		t.Fatal(err)
	}
	defer program.Close()
	if got, err := program.EvaluateText(nil, map[string]any{"name": "roads"}); err != nil || got != "roads" {
		t.Fatalf("label evaluation = %q, %v", got, err)
	}
	composer, err := CompileLabelComposerProgram(`return feature.name`, ``)
	if err != nil {
		t.Fatal(err)
	}
	defer composer.Close()
	if got, matched, err := composer.Evaluate(nil, map[string]any{"name": "roads"}); err != nil || !matched || got != "roads" {
		t.Fatalf("composer evaluation = %q, matched=%t, err=%v", got, matched, err)
	}
}

func TestLabelEvaluationHasDefaultExecutionLimit(t *testing.T) {
	program, err := CompileLabelComposerProgram(`while true do end`, "")
	if err != nil {
		t.Fatal(err)
	}
	defer program.Close()
	started := time.Now()
	_, _, err = program.Evaluate(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "deadline exceeded") {
		t.Fatalf("non-terminating label evaluation error = %v; want deadline exceeded", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("label evaluation limit took %s", elapsed)
	}
}

func TestLabelStringRepeatRejectsOversizedAllocation(t *testing.T) {
	program, err := CompileLabelProgram(`return string.rep("x", 1000000000)`)
	if err != nil {
		t.Fatal(err)
	}
	defer program.Close()
	_, err = program.EvaluateText(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "sandbox limit") {
		t.Fatalf("oversized string.rep error = %v; want sandbox allocation limit", err)
	}
}

func TestLabelSandboxDisablesUnboundedPatternLibraries(t *testing.T) {
	program, err := CompileLabelProgram(`return string.byte == nil and string.find == nil and string.gmatch == nil and string.gsub == nil and string.match == nil and table.concat({"geo", "gis"}, "-") == "geo-gis"`)
	if err != nil {
		t.Fatal(err)
	}
	defer program.Close()
	matched, err := program.EvaluateRule(context.Background(), nil)
	if err != nil || !matched {
		t.Fatalf("bounded library availability = %t, err=%v", matched, err)
	}
	oversized, err := CompileLabelProgram(`return table.concat({string.rep("x", 600000), string.rep("y", 600000)})`)
	if err != nil {
		t.Fatal(err)
	}
	defer oversized.Close()
	if _, err := oversized.EvaluateText(context.Background(), nil); err == nil || !strings.Contains(err.Error(), "table.concat result exceeds") {
		t.Fatalf("oversized table.concat error = %v; want pre-allocation sandbox limit", err)
	}
}

func TestLabelEvaluationRejectsExcessiveFeatureProperties(t *testing.T) {
	program, err := CompileLabelProgram(`return true`)
	if err != nil {
		t.Fatal(err)
	}
	defer program.Close()
	properties := make(map[string]any, maxLabelFeatureProperties+1)
	for index := range maxLabelFeatureProperties + 1 {
		properties[fmt.Sprintf("field_%d", index)] = index
	}
	_, err = program.Evaluate(context.Background(), properties)
	if err == nil || !strings.Contains(err.Error(), "label sandbox limit") {
		t.Fatalf("excessive feature properties error = %v; want property-count guard", err)
	}
}

func TestLabelStringFormatRejectsOversizedOutput(t *testing.T) {
	for _, script := range []string{
		`return string.format("%1000000000s", "x")`,
		`return string.format("%s%s", feature.name, feature.name)`,
	} {
		program, err := CompileLabelProgram(script)
		if err != nil {
			t.Fatal(err)
		}
		_, err = program.EvaluateText(context.Background(), map[string]any{"name": strings.Repeat("x", 300_000)})
		program.Close()
		if err == nil || !strings.Contains(err.Error(), "sandbox limit") {
			t.Errorf("script %q error = %v; want bounded-format error", script, err)
		}
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
	if got := exporter.ctx.Value(struct{}{}); got != marker {
		t.Fatal("Lua exporter did not receive the Run context values")
	}
	if _, ok := exporter.ctx.Deadline(); !ok {
		t.Fatal("Lua exporter context has no maximum execution deadline")
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
