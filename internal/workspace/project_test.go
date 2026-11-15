package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gogis/internal/core"
	"golang.org/x/text/unicode/norm"
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
		Labels:      core.LabelSettings{Enabled: true, Expression: "${name}", Rule: `return feature.active == true`, LuaScript: `return feature.name .. " #" .. feature.id`, Placement: "center-rotated", RotationField: "angle", PointPlacement: "SW", PointOffsetMM: 3.25, HeightMM: 2.5, MinScale: 1000, MaxScale: 50000},
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

func TestWorkspaceKeepsUNCSourceForRelinkingOnOtherHosts(t *testing.T) {
	workspacePath := filepath.Join(t.TempDir(), "map.gogis")
	const source = `//server/share/roads.shp`
	doc := Document{Version: CurrentVersion, Layers: []Layer{{Name: "roads", SourcePath: source, Style: core.DefaultLayerStyle(), Labels: core.DefaultLabelSettings()}}}
	if err := Save(workspacePath, doc); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(workspacePath)
	if err != nil {
		t.Fatal(err)
	}
	want := source
	if filepath.Separator == '\\' {
		want = filepath.Clean(source)
	}
	if loaded.Layers[0].SourcePath != want {
		t.Fatalf("UNC source = %q, want %q", loaded.Layers[0].SourcePath, want)
	}
}

func TestWorkspaceLoadRelocatesUniqueNearbySources(t *testing.T) {
	directory := t.TempDir()
	workspacePath := filepath.Join(directory, "공주시.gogis")
	building := filepath.Join(directory, "AL_D010", "AL_D010.shp")
	point := filepath.Join(directory, "point-data", "point.shp")
	for _, source := range []string{building, point} {
		if err := os.MkdirAll(filepath.Dir(source), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(source, []byte("fixture"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	doc := Document{Version: CurrentVersion, Layers: []Layer{
		{Name: "building", SourcePath: "../AL_D010/AL_D010.shp", Style: core.DefaultLayerStyle(), Labels: core.DefaultLabelSettings()},
		{Name: "point", SourcePath: "point.shp", Style: core.DefaultLayerStyle(), Labels: core.DefaultLabelSettings()},
	}}
	if err := Save(workspacePath, doc); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(workspacePath)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Layers[0].SourcePath != building || loaded.Layers[1].SourcePath != point {
		t.Fatalf("relocated paths = %q, %q; want %q, %q", loaded.Layers[0].SourcePath, loaded.Layers[1].SourcePath, building, point)
	}
}

func TestWorkspaceLoadDoesNotGuessBetweenDuplicateSources(t *testing.T) {
	directory := t.TempDir()
	for _, folder := range []string{"first", "second"} {
		path := filepath.Join(directory, folder, "roads.shp")
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(folder), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	workspacePath := filepath.Join(directory, "map.gogis")
	doc := Document{Version: CurrentVersion, Layers: []Layer{{Name: "roads", SourcePath: "old/roads.shp", Style: core.DefaultLayerStyle(), Labels: core.DefaultLabelSettings()}}}
	if err := Save(workspacePath, doc); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(workspacePath)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(directory, "old", "roads.shp"); loaded.Layers[0].SourcePath != want {
		t.Fatalf("ambiguous source = %q, want missing path %q", loaded.Layers[0].SourcePath, want)
	}
}

func TestWorkspaceLoadRelocatesForeignAbsolutePathAndNormalizesUnicode(t *testing.T) {
	directory := t.TempDir()
	filename := norm.NFD.String("건물정보") + ".shp"
	actual := filepath.Join(directory, "data", filename)
	if err := os.MkdirAll(filepath.Dir(actual), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(actual, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	workspacePath := filepath.Join(directory, "map.gogis")
	doc := Document{Version: CurrentVersion, Layers: []Layer{{Name: "building", SourcePath: `C:\GIS\건물정보.shp`, Style: core.DefaultLayerStyle(), Labels: core.DefaultLabelSettings()}}}
	if err := Save(workspacePath, doc); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(workspacePath)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Layers[0].SourcePath != actual {
		t.Fatalf("foreign absolute path = %q, want %q", loaded.Layers[0].SourcePath, actual)
	}
}

func TestWorkspaceSourceURIRoundTrip(t *testing.T) {
	workspacePath := filepath.Join(t.TempDir(), "map.gogis")
	const source = "postgis://host.example/database?table=roads"
	doc := Document{Version: CurrentVersion, Layers: []Layer{{Name: "roads", SourcePath: source, Style: core.DefaultLayerStyle(), Labels: core.DefaultLabelSettings()}}}
	if err := Save(workspacePath, doc); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(workspacePath)
	if err != nil || loaded.Layers[0].SourcePath != source {
		t.Fatalf("source URI = %q, err=%v", loaded.Layers[0].SourcePath, err)
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
