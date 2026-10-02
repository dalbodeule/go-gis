package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gogis/internal/core"
)

func TestWorkspaceRoundTripPreservesLayerSettingsWithoutFeatures(t *testing.T) {
	path := filepath.Join(t.TempDir(), "map.gogis")
	style := core.DefaultLayerStyle()
	style.PointColor = "#aabbcc"
	style.PointSizeMM = 3.2
	style.LineColor = "#010203"
	style.LineWidthMM = 1.4
	style.PolygonColor = "#123456"
	style.FillOpacity = 0.6
	project := core.Project{Name: "field work", CRS: core.CRS{AuthorityCode: "EPSG:5179"}, Layers: []core.Layer{{
		Name: "roads", DisplayName: "Road centerlines", SourcePath: "../data/roads.shp", SourceLayerName: "roads", SourceEncoding: "CP949",
		CRS: core.CRS{AuthorityCode: "EPSG:5179"}, Visible: false, Style: core.DefaultLayerStyle(),
		Labels:      core.LabelSettings{Enabled: true, Expression: "${name}", Rule: `return feature.active == true`, LuaScript: `return feature.name .. " #" .. feature.id`, Placement: "center-rotated", RotationField: "angle", HeightMM: 2.5, MinScale: 1000, MaxScale: 50000},
		DisplayRule: `return feature.active == true`,
		Features:    []core.Feature{{ID: 1}},
	}}}
	project.Layers[0].Style = style
	if err := Save(path, FromProject(project)); err != nil {
		t.Fatal(err)
	}
	doc, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := doc.Project()
	if err != nil {
		t.Fatal(err)
	}
	expectedSource, err := filepath.Abs(filepath.Join(filepath.Dir(path), "../data/roads.shp"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Layers) != 1 || got.Layers[0].DisplayName != "Road centerlines" || got.Layers[0].SourcePath != expectedSource ||
		got.Layers[0].SourceLayerName != "roads" || got.Layers[0].SourceEncoding != "CP949" || got.Layers[0].Visible {
		t.Fatalf("workspace did not preserve source and visibility: %+v", got.Layers)
	}
	if got.Layers[0].Labels != project.Layers[0].Labels || got.Layers[0].Style != project.Layers[0].Style ||
		got.Layers[0].DisplayRule != project.Layers[0].DisplayRule {
		t.Fatalf("workspace did not preserve style, labels, or display rule: %+v", got.Layers[0])
	}
	if len(got.Layers[0].Features) != 0 {
		t.Fatal("workspace unexpectedly embedded feature data")
	}
}

func TestWorkspaceRoundTripPreservesViewState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "view.gogis")
	doc := Document{
		Version: CurrentVersion,
		Name:    "view",
		View:    &ViewState{CenterX: 0.35, CenterY: 0.72, Zoom: 2.5, ActiveLayer: "roads"},
	}
	if err := Save(path, doc); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.View == nil || *loaded.View != *doc.View {
		t.Fatalf("workspace view state = %+v, want %+v", loaded.View, doc.View)
	}
}

func TestLoadNormalizesWindowsRelativeSourcePath(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "map.gogis")
	doc := Document{
		Version: CurrentVersion,
		Layers: []Layer{{
			Name: "roads", SourcePath: `..\data\roads.shp`,
			Style: core.DefaultLayerStyle(), Labels: core.DefaultLabelSettings(),
		}},
	}
	if err := Save(path, doc); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(contents), `"sourcePath": "../data/roads.shp"`) {
		t.Fatalf("workspace did not store a portable relative path: %s", contents)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Clean(filepath.Join(directory, "..", "data", "roads.shp"))
	if got := loaded.Layers[0].SourcePath; got != want {
		t.Fatalf("resolved Windows-style relative source = %q, want %q", got, want)
	}
}

func TestWindowsAbsolutePathIsRecognizedOnEveryHost(t *testing.T) {
	for _, path := range []string{`C:\GIS\roads.shp`, `D:/data/roads.shp`, `\\server\share\roads.shp`} {
		if !isWindowsAbsolutePath(path) {
			t.Errorf("isWindowsAbsolutePath(%q) = false", path)
		}
	}
	for _, path := range []string{`C:roads.shp`, `data\roads.shp`, `/data/roads.shp`} {
		if isWindowsAbsolutePath(path) {
			t.Errorf("isWindowsAbsolutePath(%q) = true", path)
		}
	}
}

func TestWorkspaceLoadDoesNotRebaseForeignWindowsAbsolutePath(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "map.gogis")
	source := `C:\GIS\roads.shp`
	doc := Document{
		Version: CurrentVersion,
		Layers: []Layer{{
			Name: "roads", SourcePath: source,
			Style: core.DefaultLayerStyle(), Labels: core.DefaultLabelSettings(),
		}},
	}
	if err := Save(path, doc); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	want := source
	if filepath.IsAbs(source) {
		want = filepath.Clean(source)
	}
	if got := loaded.Layers[0].SourcePath; got != want {
		t.Fatalf("resolved Windows absolute source = %q, want %q", got, want)
	}
}

func TestWorkspaceRejectsInvalidViewState(t *testing.T) {
	doc := Document{Version: CurrentVersion, View: &ViewState{CenterX: 0.5, CenterY: 0.5, Zoom: 0}}
	if err := Save(filepath.Join(t.TempDir(), "invalid-view.gogis"), doc); err == nil {
		t.Fatal("expected invalid view state to be rejected")
	}
}

func TestLoadRejectsUnknownVersionAndInvalidLayerSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.gogis")
	if err := os.WriteFile(path, []byte(`{"version":99,"layers":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected unsupported version error")
	}
	doc := Document{Version: CurrentVersion, Layers: []Layer{{Name: "bad", SourcePath: "x", Style: core.LayerStyle{}}}}
	if err := Save(filepath.Join(t.TempDir(), "bad.gogis"), doc); err == nil {
		t.Fatal("expected invalid default style error")
	}
}

func TestSaveAtomicallyReplacesExistingWorkspace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "map.gogis")
	doc := Document{Version: CurrentVersion, Name: "first"}
	if err := Save(path, doc); err != nil {
		t.Fatal(err)
	}
	doc.Name = "second"
	if err := Save(path, doc); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil || loaded.Name != "second" {
		t.Fatalf("overwritten workspace = %+v, %v", loaded, err)
	}
}
