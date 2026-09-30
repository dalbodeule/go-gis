//go:build qt && native

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"gogis/drivers/gdal"
	"gogis/internal/core"
	"gogis/internal/render"
	"gogis/internal/workspace"
	"gogis/ui/qt/native"

	"github.com/airbusgeo/godal"
)

func TestUniqueSourcePathsRemovesRepeatedFiles(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "roads.gpkg")
	got := uniqueSourcePaths([]string{path, filepath.Join(directory, ".", "roads.gpkg"), "", "   "})
	if len(got) != 1 || normalizedSourcePath(got[0]) != normalizedSourcePath(path) {
		t.Fatalf("unique source paths = %v, want one normalized %q", got, path)
	}
}

func TestLargeDatasetReadOnlyThresholdAndOverride(t *testing.T) {
	if !shouldOpenLargeDatasetReadOnly(largeDatasetReadOnlyThreshold, false, false) {
		t.Fatal("dataset at threshold was not selected for read-only mode")
	}
	if !shouldOpenLargeDatasetReadOnly(0, true, false) {
		t.Fatal("dataset with unknown feature count was not selected for read-only mode")
	}
	if shouldOpenLargeDatasetReadOnly(largeDatasetReadOnlyThreshold, false, true) {
		t.Fatal("--editable-large override did not preserve editable mode")
	}
	if !desktopAllowLargeEditable([]string{"gogis-desktop-native", "--editable-large"}) {
		t.Fatal("--editable-large was not recognized")
	}
}

func TestDatasetPreflightDetectsLargeFeatureCount(t *testing.T) {
	path := largeGeoJSONFixture(t, largeDatasetReadOnlyThreshold+3)
	count, large, err := inspectSourceFeatureCount(context.Background(), []vectorSourceSpec{{Path: path}})
	if err != nil {
		t.Fatal(err)
	}
	if !large || count != largeDatasetReadOnlyThreshold {
		t.Fatalf("preflight count/large = %d/%t, want threshold/%t", count, large, true)
	}

	runtime, autoReadOnly, count, err := loadDataRuntimeFilesWithLargePolicy(
		context.Background(), []string{path}, nil, "", false)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.closeAttributeSource()
	if !autoReadOnly || !runtime.readOnly || count != largeDatasetReadOnlyThreshold {
		t.Fatalf("large load policy = read-only:%t runtime:%t count:%d", autoReadOnly, runtime.readOnly, count)
	}
	if names := runtime.service.LayerNames(); len(names) != 1 {
		t.Fatalf("loaded layer names = %v", names)
	} else {
		page, total, pageErr := runtime.attributePageReader(context.Background(), names[0], 0, 1)
		if pageErr != nil {
			t.Fatal(pageErr)
		}
		if total != largeDatasetReadOnlyThreshold+3 || len(page.Features) != 1 || page.Features[0].Properties["name"] != "point-0" {
			t.Fatalf("lazy attribute page total/features = %d/%v", total, page.Features)
		}
	}

	workspacePath := filepath.Join(t.TempDir(), "large.gogis")
	doc := workspace.FromProject(core.Project{
		Name: "large", CRS: core.CRS{AuthorityCode: "EPSG:4326"},
		Layers: []core.Layer{{Name: "large", SourcePath: path, SourceLayerName: "large",
			SourceCRS: "EPSG:4326", CRS: core.CRS{AuthorityCode: "EPSG:4326"}, Visible: true,
			Style: core.DefaultLayerStyle(), Labels: core.DefaultLabelSettings()}},
	})
	if err := workspace.Save(workspacePath, doc); err != nil {
		t.Fatal(err)
	}
	workspaceRuntime, workspaceAutoReadOnly, workspaceCount, err := loadWorkspaceRuntimeWithLargePolicy(
		context.Background(), workspacePath, false, "", false)
	if err != nil {
		t.Fatal(err)
	}
	defer workspaceRuntime.closeAttributeSource()
	if !workspaceAutoReadOnly || !workspaceRuntime.readOnly || workspaceCount != largeDatasetReadOnlyThreshold {
		t.Fatalf("large workspace policy = read-only:%t runtime:%t count:%d", workspaceAutoReadOnly, workspaceRuntime.readOnly, workspaceCount)
	}
}

func TestCancelCurrentOperationCancelsLoadAndInvalidatesResult(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	runtime := &demoRuntime{loadCancel: cancel, loadGeneration: 4, previewLoading: true}
	runtime.cancelCurrentRender()
	if ctx.Err() == nil {
		t.Fatal("load context was not cancelled")
	}
	if runtime.loadCancel != nil || runtime.loadGeneration != 5 || runtime.previewLoading {
		t.Fatalf("cancelled load state = cancel:%v generation:%d preview:%v", runtime.loadCancel != nil, runtime.loadGeneration, runtime.previewLoading)
	}
}

func TestDesktopLoadsOnlySelectedGeoPackageLayer(t *testing.T) {
	godal.RegisterAll()
	path := filepath.Join(t.TempDir(), "layers.gpkg")
	dataset, err := godal.CreateVector(godal.GeoPackage, path)
	if err != nil {
		t.Fatal(err)
	}
	spatialRef, err := godal.NewSpatialRef("EPSG:4326")
	if err != nil {
		_ = dataset.Close()
		t.Fatal(err)
	}
	defer spatialRef.Close()
	for _, name := range []string{"first", "selected"} {
		layer, err := dataset.CreateLayer(name, spatialRef, godal.GTPoint)
		if err != nil {
			_ = dataset.Close()
			t.Fatal(err)
		}
		geometry, err := godal.NewGeometryFromWKT("POINT (127 37)", spatialRef)
		if err != nil {
			_ = dataset.Close()
			t.Fatal(err)
		}
		feature, err := layer.NewFeature(geometry)
		geometry.Close()
		if err != nil {
			_ = dataset.Close()
			t.Fatal(err)
		}
		if err := layer.UpdateFeature(feature); err != nil {
			feature.Close()
			_ = dataset.Close()
			t.Fatal(err)
		}
		feature.Close()
	}
	if err := dataset.Close(); err != nil {
		t.Fatal(err)
	}
	allLayers, err := loadDataRuntimeModeContext(context.Background(), path, "", "", "", "", false)
	if err != nil {
		t.Fatalf("load every dataset layer: %v", err)
	}
	if names := allLayers.service.LayerNames(); len(names) != 2 || names[0] != "first" || names[1] != "selected" {
		t.Fatalf("all layer names = %v", names)
	}
	if len(allLayers.features) != 2 {
		t.Fatalf("all layer feature count = %d, want 2", len(allLayers.features))
	}
	if allLayers.mapExtent != [4]float64{126.995, 36.995, 127.005, 37.005} {
		t.Fatalf("common map extent = %v", allLayers.mapExtent)
	}
	for _, readOnly := range []bool{true, false} {
		runtime, err := loadDataRuntimeModeContext(context.Background(), path, "selected", "", "", "", readOnly)
		if err != nil {
			t.Fatalf("readOnly=%t: %v", readOnly, err)
		}
		if names := runtime.service.LayerNames(); len(names) != 1 || names[0] != "selected" {
			t.Fatalf("readOnly=%t: layer names = %v", readOnly, names)
		}
		if len(runtime.features) != 1 {
			t.Fatalf("readOnly=%t: features = %d", readOnly, len(runtime.features))
		}
		if runtime.closeAttributeSource != nil {
			runtime.closeAttributeSource()
		}
	}
}

func TestDesktopSourceEncodingAppliesToLazyAttributeReads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "roads.shp")
	layer := core.Layer{
		Name: "roads", CRS: core.CRS{AuthorityCode: "EPSG:4326"},
		Fields: []core.Field{{Name: "name", Type: core.FieldTypeText}},
		Features: []core.Feature{{
			ID: 1, Geometry: core.WKTGeometry{WKT: "POINT (127 37)"},
			Properties: map[string]any{"name": "한글 도로"},
		}},
	}
	if err := (gdal.Writer{}).Write(context.Background(), path, layer); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(strings.TrimSuffix(path, filepath.Ext(path))+".cpg", []byte("CP949\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	runtime, err := loadDataRuntimeModeContextWithEncoding(context.Background(), path, "roads", "", "", "", "UTF-8", false, nil)
	if err != nil {
		t.Fatal(err)
	}
	page, total, err := runtime.attributePageReader(context.Background(), "roads", 0, 10)
	if err != nil || total != 1 || page.Features[0].Properties["name"] != "한글 도로" {
		t.Fatalf("encoded lazy attribute page=%#v total=%d err=%v", page, total, err)
	}
	feature, err := runtime.attributeFeatureReader(context.Background(), "roads", 1)
	if err != nil || feature.Properties["name"] != "한글 도로" {
		t.Fatalf("encoded lazy feature=%#v err=%v", feature, err)
	}
}

func TestDesktopAddsMultipleVectorFilesAsLayers(t *testing.T) {
	root := t.TempDir()
	firstDirectory := filepath.Join(root, "first")
	secondDirectory := filepath.Join(root, "second")
	for _, directory := range []string{firstDirectory, secondDirectory} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	firstPath := filepath.Join(firstDirectory, "roads.shp")
	secondPath := filepath.Join(secondDirectory, "roads.shp")
	firstLayer := core.Layer{
		Name:   "roads",
		CRS:    core.CRS{AuthorityCode: "EPSG:4326"},
		Fields: []core.Field{{Name: "name", Type: core.FieldTypeText}},
		Features: []core.Feature{{
			ID: 1, Geometry: core.WKTGeometry{WKT: "POINT (127 37)"},
			Properties: map[string]any{"name": "first"},
		}},
	}
	secondLayer := firstLayer.Clone()
	secondLayer.Features[0].Geometry = core.WKTGeometry{WKT: "POINT (128 38)"}
	secondLayer.Features[0].Properties["name"] = "second"
	for _, item := range []struct {
		path  string
		layer core.Layer
	}{{firstPath, firstLayer}, {secondPath, secondLayer}} {
		if err := (gdal.Writer{}).Write(context.Background(), item.path, item.layer); err != nil {
			t.Fatal(err)
		}
	}
	first, err := loadDataRuntimeFiles(context.Background(), []string{firstPath}, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	added, err := loadDataRuntimeFiles(context.Background(), []string{secondPath}, first.service.Project().Layers, "")
	if err != nil {
		t.Fatal(err)
	}
	if names := added.service.LayerNames(); len(names) != 2 || names[0] != "roads" || names[1] != "roads_roads" {
		t.Fatalf("combined layer names = %v", names)
	}
	if len(added.features) != 2 {
		t.Fatalf("combined feature count = %d, want 2", len(added.features))
	}
	roads, ok := added.service.Layer("roads")
	if !ok || roads.SourcePath != firstPath || roads.SourceLayerName != "roads" || roads.Style != core.DefaultLayerStyle() {
		t.Fatalf("first layer source/presentation = %+v, found=%t", roads, ok)
	}
	addedRoads, ok := added.service.Layer("roads_roads")
	if !ok || addedRoads.SourcePath != secondPath || addedRoads.SourceLayerName != "roads" {
		t.Fatalf("added layer source identity = %+v, found=%t", addedRoads, ok)
	}
	if added.mapExtent != [4]float64{127, 37, 128, 38} {
		t.Fatalf("combined extent = %v", added.mapExtent)
	}
	page, total, err := added.attributePageReader(context.Background(), "roads_roads", 0, 10)
	if err != nil || total != 1 || page.Features[0].Properties["name"] != "second" {
		t.Fatalf("added layer attributes page=%#v total=%d err=%v", page, total, err)
	}
	readOnlyFirst, err := loadReadOnlyDataRuntime(context.Background(), []vectorSourceSpec{{Path: firstPath}}, "")
	if err != nil {
		t.Fatal(err)
	}
	defer readOnlyFirst.closeAttributeSource()
	readOnlyAdded, err := loadReadOnlyDataRuntime(context.Background(), []vectorSourceSpec{
		{Path: firstPath}, {Path: secondPath},
	}, readOnlyFirst.mapCRS)
	if err != nil {
		t.Fatal(err)
	}
	defer readOnlyAdded.closeAttributeSource()
	if !readOnlyAdded.readOnly || len(readOnlyAdded.features) != 2 {
		t.Fatalf("read-only combined runtime mode=%t features=%d", readOnlyAdded.readOnly, len(readOnlyAdded.features))
	}
	page, total, err = readOnlyAdded.attributePageReader(context.Background(), "roads_roads", 0, 10)
	if err != nil || total != 1 || page.Features[0].Properties["name"] != "second" {
		t.Fatalf("read-only added layer page=%#v total=%d err=%v", page, total, err)
	}
}

func TestDesktopReloadingOneSourcePreservesOtherEditedLayersAndOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "roads.gpkg")
	reloaded := core.Layer{
		Name: "roads", CRS: core.CRS{AuthorityCode: "EPSG:4326"},
		Fields: []core.Field{{Name: "name", Type: core.FieldTypeText}},
		Features: []core.Feature{{
			ID: 1, Geometry: core.WKTGeometry{WKT: "LINESTRING (127 37, 127.1 37.1)"},
			Properties: map[string]any{"name": "reloaded source"},
		}},
	}
	if err := (gdal.Writer{}).Write(context.Background(), path, reloaded); err != nil {
		t.Fatal(err)
	}
	baseLayers := []core.Layer{{
		Name: "buildings", DisplayName: "Edited buildings", SourcePath: "buildings.gpkg",
		SourceLayerName: "buildings", CRS: core.CRS{AuthorityCode: "EPSG:4326"},
		Visible: true, Style: core.DefaultLayerStyle(), Labels: core.DefaultLabelSettings(),
		Fields: []core.Field{{Name: "status", Type: core.FieldTypeText}},
		Features: []core.Feature{{
			ID: 9, Geometry: core.WKTGeometry{WKT: "POINT (127.2 37.2)"},
			Properties: map[string]any{"status": "unsaved edit"},
		}},
	}}
	visible := true
	runtime, err := loadDataRuntimeSourcesWithCRS(context.Background(), []vectorSourceSpec{{
		Path: path, LayerName: "roads", Name: "roads", DisplayName: "Relinked roads",
		Visible: &visible, Style: core.DefaultLayerStyle(), Labels: core.DefaultLabelSettings(),
		InsertAt: 0, InsertAtSet: true,
	}}, baseLayers, "", "EPSG:4326")
	if err != nil {
		t.Fatal(err)
	}
	project := runtime.service.Project()
	if len(project.Layers) != 2 || project.Layers[0].Name != "roads" || project.Layers[1].Name != "buildings" {
		t.Fatalf("reloaded project layer order = %+v", runtime.service.LayerNames())
	}
	if project.Layers[0].DisplayName != "Relinked roads" || project.Layers[0].Features[0].Properties["name"] != "reloaded source" {
		t.Fatalf("replacement source layer = %+v", project.Layers[0])
	}
	if project.Layers[1].DisplayName != "Edited buildings" || project.Layers[1].Features[0].Properties["status"] != "unsaved edit" {
		t.Fatalf("unrelated edited layer was not preserved: %+v", project.Layers[1])
	}
}

func TestWorkspaceReopensOriginalSourcesAndLayerSettings(t *testing.T) {
	directory := t.TempDir()
	sourcePath := filepath.Join(directory, "roads.gpkg")
	layer := core.Layer{
		Name: "roads", CRS: core.CRS{AuthorityCode: "EPSG:4326"},
		Fields:   []core.Field{{Name: "name", Type: core.FieldTypeText}},
		Features: []core.Feature{{ID: 1, Geometry: core.WKTGeometry{WKT: "LINESTRING (127 37, 127.1 37.1)"}, Properties: map[string]any{"name": "길"}}},
	}
	if err := (gdal.Writer{}).Write(context.Background(), sourcePath, layer); err != nil {
		t.Fatal(err)
	}
	runtime, err := loadDataRuntimeFiles(context.Background(), []string{sourcePath}, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	settings, _ := runtime.service.LayerProperties("roads")
	settings.DisplayName = "Local roads"
	settings.SourceEncoding = "UTF-8"
	settings.Visible = false
	settings.Style.PointColor = "#aabbcc"
	settings.Style.PointSizeMM = 3.2
	settings.Style.LineColor = "#0066cc"
	settings.Style.LineWidthMM = 1.4
	settings.Style.PolygonColor = "#123456"
	settings.Style.FillOpacity = 0.6
	settings.Labels = core.LabelSettings{
		Enabled: true, Expression: "${name}", Rule: `return feature.kind == "primary"`,
		LuaScript: `return feature.name .. " (" .. feature.kind .. ")"`, Placement: "free-angle",
		RotationField: "angle", HeightMM: 2.5, MinScale: 1000, MaxScale: 50000,
	}
	if err := runtime.service.UpdateLayerSettings("roads", settings); err != nil {
		t.Fatal(err)
	}
	projectName, projectCRS := runtime.service.ProjectInfo()
	workspacePath := filepath.Join(directory, "field.gogis")
	projectLayers := runtime.service.ProjectLayerProperties()
	projectLayers = append(projectLayers, core.Layer{
		Name: "buildings", DisplayName: "Buildings", SourcePath: filepath.Join(directory, "moved", "buildings.gpkg"),
		SourceLayerName: "buildings", SourceCRS: "EPSG:4326", CRS: projectCRS,
		Visible: true, Style: core.DefaultLayerStyle(), Labels: core.DefaultLabelSettings(),
	})
	doc := workspace.FromProject(core.Project{Name: projectName, CRS: projectCRS, Layers: projectLayers})
	doc.View = &workspace.ViewState{CenterX: 0.35, CenterY: 0.72, Zoom: 2.5, ActiveLayer: "roads"}
	if err := workspace.Save(workspacePath, doc); err != nil {
		t.Fatal(err)
	}
	reopened, err := loadWorkspaceRuntime(context.Background(), workspacePath, false, "")
	if err != nil {
		t.Fatal(err)
	}
	properties, ok := reopened.service.LayerProperties("roads")
	if !ok || properties.SourcePath != sourcePath || properties.DisplayName != "Local roads" || properties.SourceEncoding != "UTF-8" ||
		properties.Visible || properties.Style != settings.Style || properties.Labels != settings.Labels {
		t.Fatalf("workspace layer settings were not restored: %+v, found=%t", properties, ok)
	}
	if reopened.workspaceView == nil || *reopened.workspaceView != *doc.View {
		t.Fatalf("workspace view was not restored: %+v, want %+v", reopened.workspaceView, doc.View)
	}
	missing, ok := reopened.service.LayerProperties("buildings")
	if !ok || missing.SourcePath != filepath.Join(directory, "moved", "buildings.gpkg") || reopened.unavailableSources["buildings"].Reason == "" {
		t.Fatalf("missing source was not retained for relinking: layer=%+v unavailable=%+v", missing, reopened.unavailableSources)
	}
	if got := len(reopened.service.LayerNames()); got != 2 {
		t.Fatalf("workspace layer count = %d, want both loaded and missing layers", got)
	}
	if reopened.readOnly {
		t.Fatal("an unavailable, empty workspace source incorrectly forced read-only mode")
	}
	readOnly, err := loadWorkspaceRuntime(context.Background(), workspacePath, true, "")
	if err != nil {
		t.Fatal(err)
	}
	if readOnly.closeAttributeSource != nil {
		defer readOnly.closeAttributeSource()
	}
	if _, ok := readOnly.service.LayerProperties("buildings"); !ok || readOnly.unavailableSources["buildings"].Reason == "" {
		t.Fatalf("read-only workspace did not retain missing source: %+v", readOnly.unavailableSources)
	}
}

func TestWorkspaceViewNormalizesPanAndZoom(t *testing.T) {
	got := workspaceViewFromViewport(native.Viewport{PanX: 120, PanY: -60, Zoom: 2, Width: 800, Height: 400}, "roads")
	want := workspace.ViewState{CenterX: 0.425, CenterY: 0.425, Zoom: 2, ActiveLayer: "roads"}
	if got != want {
		t.Fatalf("saved view state = %+v, want %+v", got, want)
	}
	runtime := &demoRuntime{workspaceView: &got}
	if viewport := runtimeInitialViewport(runtime); viewport.Center != (render.Point{X: want.CenterX, Y: want.CenterY}) || viewport.Zoom != want.Zoom {
		t.Fatalf("restored initial viewport = %+v, want center (%v, %v) zoom %v", viewport, want.CenterX, want.CenterY, want.Zoom)
	}
}

func TestIsWorkspacePathUsesExtensionCaseInsensitively(t *testing.T) {
	for path, want := range map[string]bool{
		"field.gogis": true, "FIELD.GOGIS": true, "field.gpkg": false, "folder.gogis/data.gpkg": false,
	} {
		if got := isWorkspacePath(path); got != want {
			t.Errorf("isWorkspacePath(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestApplyLayerSettingsRejectsEmptyDisplayName(t *testing.T) {
	err := applyLayerSettings(&demoRuntime{}, `{"name":"roads","displayName":"   "}`)
	if err == nil || !strings.Contains(err.Error(), "display name") {
		t.Fatalf("empty display name error = %v", err)
	}
}

func TestApplyLayerSettingsUpdatesDisplayNameAndVisibility(t *testing.T) {
	style := core.DefaultLayerStyle()
	labels := core.DefaultLabelSettings()
	labels.Enabled = true
	labels.Expression = "${street} ${number}"
	labels.Rule = `return feature.active == true`
	labels.LuaScript = `return feature.name`
	labels.Placement = "free-angle"
	labels.RotationField = "angle"
	labels.HeightMM = 2.5
	labels.MinScale = 1000
	labels.MaxScale = 50000
	service, err := newLoadedProjectService([]core.Layer{{
		Name: "roads", DisplayName: "roads", SourcePath: "roads.shp", SourceLayerName: "roads",
		Visible: true, Style: style, Labels: core.DefaultLabelSettings(),
	}})
	if err != nil {
		t.Fatal(err)
	}
	runtime := &demoRuntime{
		service: service, visibleLayers: map[string]bool{"roads": true},
		visibility:  render.NewLayerVisibility("roads"),
		layerStyles: map[string]core.LayerStyle{}, layerStyleMu: &sync.RWMutex{},
	}
	payload, err := json.Marshal(layerSettingsRequest{
		Name: "roads", DisplayName: "Cadastral Roads", SourcePath: "roads.shp",
		SourceLayerName: "roads", Visible: false, Style: style, Labels: labels,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := applyLayerSettings(runtime, string(payload)); err != nil {
		t.Fatal(err)
	}
	layer, ok := service.LayerProperties("roads")
	if !ok || layer.DisplayName != "Cadastral Roads" || layer.Visible || layer.Labels != labels {
		t.Fatalf("updated layer properties = %+v, found=%t", layer, ok)
	}
	if runtime.loadGeneration != 0 {
		t.Fatalf("non-source layer settings triggered a source reload (generation %d)", runtime.loadGeneration)
	}
	if runtime.visibleLayers["roads"] || runtime.visibility.IsVisible("roads") {
		t.Fatal("layer visibility was not updated in render state")
	}
}

func TestPolygonRuntimeBuilderPublishesClippedFillMeshWithOpacity(t *testing.T) {
	style := core.DefaultLayerStyle()
	layers := []core.Layer{{
		Name: "areas", Style: style,
		Features: []core.Feature{{ID: 7, Geometry: core.WKTGeometry{WKT: "POLYGON ((0 0, 10 0, 10 10, 0 10, 0 0))"}}},
	}}
	runtime, err := buildDataRuntime(context.Background(), layers, "", "", "", "", "", false, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	chunk, err := runtime.builder(context.Background(), render.ChunkKey{Layer: "areas", X: 0, Y: 0})
	if err != nil {
		t.Fatal(err)
	}
	fills := 0
	for _, vertex := range chunk.Vertices {
		if vertex.Kind == render.VertexFill {
			fills++
			if vertex.Color != render.ColorForPolygonFill(style) {
				t.Fatalf("polygon fill color/opacity = %#08x, want %#08x", vertex.Color, render.ColorForPolygonFill(style))
			}
		}
	}
	if fills == 0 || fills%3 != 0 {
		t.Fatalf("polygon fill mesh has %d vertices, expected triangle groups", fills)
	}
}

func TestPolygonFillCanBeEnabledAfterLoadingWithZeroOpacity(t *testing.T) {
	style := core.DefaultLayerStyle()
	style.FillOpacity = 0
	runtime, err := buildDataRuntime(context.Background(), []core.Layer{{
		Name: "areas", Style: style,
		Features: []core.Feature{{ID: 1, Geometry: core.WKTGeometry{WKT: "POLYGON ((0 0, 4 0, 4 4, 0 4, 0 0))"}}},
	}}, "", "", "", "", "", false, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	updated := style
	updated.FillOpacity = 0.6
	runtime.layerStyleMu.Lock()
	runtime.layerStyles["areas"] = updated
	runtime.layerStyleMu.Unlock()
	chunk, err := runtime.builder(context.Background(), render.ChunkKey{Layer: "areas", X: 0, Y: 0})
	if err != nil {
		t.Fatal(err)
	}
	want := render.ColorForPolygonFill(updated)
	for _, vertex := range chunk.Vertices {
		if vertex.Kind == render.VertexFill && vertex.Color == want {
			return
		}
	}
	t.Fatalf("fill mesh did not become visible after opacity update; expected fill color %#08x", want)
}

func TestConfiguredLabelsUseTemplateLuaRuleAndRenderPlacement(t *testing.T) {
	layers := []core.Layer{{
		Name: "roads",
		Labels: core.LabelSettings{
			Enabled: true, Expression: "${street} ${number}", Rule: `return feature.kind == "primary"`,
			Placement: "center-rotated", RotationField: "angle", HeightMM: 2.5, MinScale: 1000, MaxScale: 50000,
		},
		Features: []core.Feature{{ID: 7, Geometry: core.WKTGeometry{WKT: "LINESTRING (0 0, 2 0)"}, Properties: map[string]any{
			"street": "한강로", "number": 12, "kind": "primary", "angle": 30,
		}}},
	}}
	if err := prepareLayerLabels(context.Background(), layers); err != nil {
		t.Fatal(err)
	}
	source, err := render.NewLayerSource(layers[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(source.Labels) != 1 {
		t.Fatalf("layer labels = %#v", source.Labels)
	}
	label := source.Labels[0]
	if label.Text != "한강로 12" || label.FeatureID != 7 || label.Rotation != 30 || label.HeightMM != 2.5 || label.MinScale != 1000 || label.MaxScale != 50000 {
		t.Fatalf("configured label = %#v", label)
	}
}

func TestConfiguredLabelRotationRejectsMissingOrNonFiniteValues(t *testing.T) {
	tests := []struct {
		name       string
		properties map[string]any
		wantError  string
	}{
		{name: "missing field", properties: map[string]any{"name": "Road"}, wantError: `rotation field "angle" is missing`},
		{name: "non-numeric", properties: map[string]any{"name": "Road", "angle": "north"}, wantError: `rotation field "angle" must contain a finite number`},
		{name: "not finite", properties: map[string]any{"name": "Road", "angle": "NaN"}, wantError: `rotation field "angle" must contain a finite number`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			layers := []core.Layer{{
				Name: "roads", Labels: core.LabelSettings{Enabled: true, Expression: "${name}", Placement: "center-rotated", RotationField: "angle"},
				Features: []core.Feature{{ID: 9, Geometry: core.WKTGeometry{WKT: "POINT (1 2)"}, Properties: test.properties}},
			}}
			err := prepareLayerLabels(context.Background(), layers)
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("prepareLayerLabels error = %v, want substring %q", err, test.wantError)
			}
		})
	}
}

func TestPolygonLabelIsAnchoredOnConcaveInterior(t *testing.T) {
	layers := []core.Layer{{
		Name:   "areas",
		Labels: core.LabelSettings{Enabled: true, Expression: "${name}", Placement: "center", HeightMM: 2.5},
		Features: []core.Feature{{ID: 3, Geometry: core.WKTGeometry{WKT: "POLYGON ((0 0, 10 0, 10 4, 4 4, 4 10, 0 10, 0 0))"},
			Properties: map[string]any{"name": "L area"}}},
	}}
	if err := prepareLayerLabels(context.Background(), layers); err != nil {
		t.Fatal(err)
	}
	feature := layers[0].Features[0]
	if feature.Label == nil || !feature.Label.AnchorSet {
		t.Fatalf("polygon label has no explicit interior anchor: %+v", feature.Label)
	}
	anchorX, anchorY := feature.Label.X, feature.Label.Y
	if anchorX < 0 || anchorY < 0 || anchorX > 10 || anchorY > 10 || anchorX > 4 && anchorY > 4 {
		t.Fatalf("polygon label lies outside the concave polygon: (%v, %v)", anchorX, anchorY)
	}
	source, err := render.NewLayerSource(layers[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(source.Labels) != 1 || math.Abs(source.Labels[0].X-anchorX/10) > 1e-12 || math.Abs(source.Labels[0].Y-anchorY/10) > 1e-12 {
		t.Fatalf("normalized polygon label = %+v, expected (%v, %v)", source.Labels, anchorX/10, anchorY/10)
	}
}

func TestPreviewBoundsRequiresLargeCommonCRSDataset(t *testing.T) {
	overviews := []gdal.LayerOverview{
		{Name: "west", CRS: core.CRS{AuthorityCode: "EPSG:4326"}, Bounds: [4]float64{0, 0, 10, 10}, HasBounds: true, FeatureCount: previewMinimumFeatures - 1},
		{Name: "east", CRS: core.CRS{AuthorityCode: "EPSG:4326"}, Bounds: [4]float64{20, -5, 30, 5}, HasBounds: true, FeatureCount: 1},
	}
	bounds, ok := previewBounds(overviews, "", "", "")
	if !ok || bounds != ([4]float64{0, -5, 30, 10}) {
		t.Fatalf("combined preview bounds = %v, %t", bounds, ok)
	}
	if _, ok := previewBounds(overviews, "west", "", ""); ok {
		t.Fatal("small selected layer was previewed")
	}
	if _, ok := previewBounds(overviews, "missing", "", ""); ok {
		t.Fatal("missing selected layer was previewed")
	}
	if _, ok := previewBounds(overviews, "", "", "EPSG:3857"); ok {
		t.Fatal("preview accepted a target CRS requiring transformation")
	}
	overviews[1].CRS.AuthorityCode = "EPSG:3857"
	if _, ok := previewBounds(overviews, "", "", ""); ok {
		t.Fatal("preview accepted mixed source CRSs")
	}
	if _, ok := previewBounds(overviews, "", "EPSG:4326", ""); !ok {
		t.Fatal("explicit source CRS should allow a common coordinate space")
	}
	overviews[1].HasBounds = false
	if _, ok := previewBounds(overviews, "", "EPSG:4326", ""); ok {
		t.Fatal("preview accepted a layer without bounds")
	}
	manySmall := make([]gdal.LayerOverview, 100)
	for index := range manySmall {
		manySmall[index] = gdal.LayerOverview{Name: fmt.Sprintf("layer-%d", index), CRS: core.CRS{AuthorityCode: "EPSG:4326"}, Bounds: [4]float64{0, 0, 1, 1}, HasBounds: true, FeatureCount: 500}
	}
	if _, ok := previewBounds(manySmall, "", "", ""); ok {
		t.Fatal("preview would duplicate a full read across many small layers")
	}
}

func TestStalePreviewDoesNotReplaceRuntime(t *testing.T) {
	current := &demoRuntime{loadGeneration: 2}
	stale := &demoRuntime{}
	current.replaceWithPreview(stale, 1)
	if current.previewLoading || current.scheduler != nil || current.loadGeneration != 2 {
		t.Fatal("stale preview changed current runtime")
	}
}

func TestLargeReadOnlyLoadPublishesStablePreview(t *testing.T) {
	path := largeReadOnlyFixture(t)
	var preview *demoRuntime
	full, err := loadDataRuntimeModeContextWithPreview(context.Background(), path, "", "", "", "", true, func(next *demoRuntime) {
		preview = next
	})
	if err != nil {
		t.Fatal(err)
	}
	defer full.closeAttributeSource()
	if preview == nil || len(preview.features) != previewFeatureLimit || len(full.features) != previewMinimumFeatures {
		t.Fatalf("preview/full feature counts = %d/%d", len(preview.features), len(full.features))
	}
	if preview.features[previewFeatureLimit-1].Vertices[0] != full.features[previewFeatureLimit-1].Vertices[0] {
		t.Fatalf("preview/full coordinates differ: %v / %v", preview.features[previewFeatureLimit-1].Vertices[0], full.features[previewFeatureLimit-1].Vertices[0])
	}
	if preview.closeAttributeSource != nil || preview.attributeFeatureReader != nil {
		t.Fatal("preview retained the full loader's attribute session")
	}
}

func largeReadOnlyFixture(tb testing.TB) string {
	return largeGeoJSONFixture(tb, previewMinimumFeatures)
}

func largeGeoJSONFixture(tb testing.TB, featureCount int) string {
	tb.Helper()
	path := filepath.Join(tb.TempDir(), "large.geojson")
	var fixture strings.Builder
	fixture.Grow(featureCount * 100)
	fixture.WriteString(`{"type":"FeatureCollection","features":[`)
	for index := 0; index < featureCount; index++ {
		if index > 0 {
			fixture.WriteByte(',')
		}
		fmt.Fprintf(&fixture, `{"type":"Feature","properties":{"name":"point-%d"},"geometry":{"type":"Point","coordinates":[%d,%d]}}`, index, index, index)
	}
	fixture.WriteString(`]}`)
	if err := os.WriteFile(path, []byte(fixture.String()), 0o600); err != nil {
		tb.Fatal(err)
	}
	return path
}

func TestDesktopInputArgsIncludesCRSOverrides(t *testing.T) {
	input, layer, source, target := desktopInputArgs([]string{
		"gogis-desktop", "--input", "roads.gpkg", "--layer", "roads",
		"--source-crs", "EPSG:4326", "--target-crs", "EPSG:5179",
	})
	if input != "roads.gpkg" || layer != "roads" || source != "EPSG:4326" || target != "EPSG:5179" {
		t.Fatalf("parsed desktop args = %q, %q, %q, %q", input, layer, source, target)
	}
}

func TestDesktopReadOnlyFlag(t *testing.T) {
	if !desktopReadOnly([]string{"gogis-desktop", "--read-only"}) {
		t.Fatal("read-only flag was not detected")
	}
	if desktopReadOnly([]string{"gogis-desktop", "--input", "roads.gpkg"}) {
		t.Fatal("read-only flag was detected unexpectedly")
	}
}

func TestLayerMetadataOnlyDropsLargeGeometrySnapshot(t *testing.T) {
	layers := layerMetadataOnly([]core.Layer{{
		Name: "roads", CRS: core.CRS{AuthorityCode: "EPSG:4326"},
		Fields:   []core.Field{{Name: "name"}},
		Features: []core.Feature{{ID: 1, Geometry: core.WKTGeometry{WKT: "POINT (1 2)"}, Properties: map[string]any{"name": "road"}}},
		Editable: true,
	}})
	if len(layers) != 1 || layers[0].Name != "roads" || layers[0].CRS.AuthorityCode != "EPSG:4326" {
		t.Fatalf("metadata layers = %#v", layers)
	}
	if layers[0].Fields != nil || layers[0].Features != nil || layers[0].Editable {
		t.Fatalf("large snapshot was retained: %#v", layers[0])
	}
}

func TestAlignLayerCRSTransformsToExplicitTarget(t *testing.T) {
	layers, err := alignLayerCRS(context.Background(), []core.Layer{{
		Name: "roads", CRS: core.CRS{AuthorityCode: "EPSG:4326"},
		Features: []core.Feature{{ID: 1, Geometry: core.WKTGeometry{WKT: "POINT (127 37)"}}},
	}}, "EPSG:3857")
	if err != nil {
		t.Fatal(err)
	}
	if layers[0].CRS.AuthorityCode != "EPSG:3857" {
		t.Fatalf("aligned CRS = %q", layers[0].CRS.AuthorityCode)
	}
	geometry := layers[0].Features[0].Geometry.(core.WKTGeometry)
	if geometry.WKT == "POINT (127 37)" {
		t.Fatalf("geometry was not transformed: %s", geometry.WKT)
	}
}

func TestReadOnlyFeatureNameCacheAvoidsRepeatedLookup(t *testing.T) {
	lookups := 0
	runtime := &demoRuntime{
		readOnly: true,
		attributeFeatureReader: func(context.Context, string, uint64) (core.Feature, error) {
			lookups++
			return core.Feature{Properties: map[string]any{"name": "road"}}, nil
		},
	}
	if got := runtime.featureName("roads", 7); got != "road" {
		t.Fatalf("first feature name = %q", got)
	}
	if got := runtime.featureName("roads", 7); got != "road" {
		t.Fatalf("cached feature name = %q", got)
	}
	if lookups != 1 {
		t.Fatalf("feature lookups = %d, want 1", lookups)
	}
	if got := runtime.featureName("roads", 8); got != "road" {
		t.Fatalf("second feature name = %q", got)
	}
	if lookups != 2 {
		t.Fatalf("feature lookups after different ID = %d, want 2", lookups)
	}
}
