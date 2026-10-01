package commands

import (
	"context"
	"errors"
	"testing"

	"gogis/internal/core"
)

func TestProjectServiceAdoptsLoadedLayerSnapshots(t *testing.T) {
	layers := []core.Layer{{Name: "roads", Editable: true, Features: []core.Feature{{ID: 1, Geometry: core.WKBGeometry{WKB: []byte{1, 2, 3}}}}}}
	service, err := NewProjectServiceWithLayers("loaded", core.CRS{}, layers)
	if err != nil {
		t.Fatal(err)
	}
	if got := service.project.Layers[0].Features; len(got) != 1 || &got[0] != &layers[0].Features[0] {
		t.Fatal("constructor cloned the loader-owned feature slice")
	}

	if err := service.BeginEdit(); err != nil {
		t.Fatal(err)
	}
	if err := service.SetFeatureProperty("roads", 1, "name", "edited"); err != nil {
		t.Fatal(err)
	}
	if err := service.Rollback(); err != nil {
		t.Fatal(err)
	}
	if value := service.project.Layers[0].Features[0].Properties["name"]; value != nil {
		t.Fatalf("rolled-back adopted snapshot property = %v", value)
	}

	if _, err := NewProjectServiceWithLayers("loaded", core.CRS{}, []core.Layer{{Name: "same"}, {Name: "same"}}); !errors.Is(err, ErrLayerExists) {
		t.Fatalf("duplicate layer constructor error = %v, want ErrLayerExists", err)
	}
}

func TestProjectEditCommitAndRollback(t *testing.T) {
	service := NewProjectService("demo", core.CRS{AuthorityCode: "EPSG:4326"})
	layer := core.Layer{
		Name:     "roads",
		Editable: true,
		Features: []core.Feature{{ID: 1, Properties: map[string]any{"name": "길"}}},
	}

	if err := service.BeginEdit(); err != nil {
		t.Fatal(err)
	}
	if err := service.AddLayer(layer); err != nil {
		t.Fatal(err)
	}
	if err := service.SetFeatureProperty("roads", 1, "name", "새 길"); err != nil {
		t.Fatal(err)
	}
	if err := service.Commit(); err != nil {
		t.Fatal(err)
	}
	if got := service.Project().Layers[0].Features[0].Properties["name"]; got != "새 길" {
		t.Fatalf("committed property = %v, want %q", got, "새 길")
	}
	if !service.HasFeature("roads", 1) {
		t.Fatal("committed feature was not indexed")
	}

	if err := service.BeginEdit(); err != nil {
		t.Fatal(err)
	}
	if err := service.SetFeatureProperty("roads", 1, "name", "취소된 값"); err != nil {
		t.Fatal(err)
	}
	if err := service.Rollback(); err != nil {
		t.Fatal(err)
	}
	if got := service.Project().Layers[0].Features[0].Properties["name"]; got != "새 길" {
		t.Fatalf("rolled back property = %v, want %q", got, "새 길")
	}
	if value, ok := service.FeatureProperty("roads", 1, "name"); !ok || value != "새 길" {
		t.Fatalf("indexed property after rollback = %v, %v", value, ok)
	}
}

func TestSetFeatureGeometryCommitAndRollbackAreIsolated(t *testing.T) {
	original := core.WKBGeometry{WKB: []byte{1, 2, 3}}
	service, err := NewProjectServiceWithLayers("loaded", core.CRS{}, []core.Layer{{
		Name: "roads", Editable: true, Features: []core.Feature{{ID: 1, Geometry: original}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	replacement := core.WKTGeometry{WKT: "POINT (4 5)"}
	if err := service.BeginEdit(); err != nil {
		t.Fatal(err)
	}
	if err := service.SetFeatureGeometry("roads", 1, replacement); err != nil {
		t.Fatal(err)
	}
	if err := service.Rollback(); err != nil {
		t.Fatal(err)
	}
	if got := service.project.Layers[0].Features[0].Geometry.GeometryType(); got != "" {
		t.Fatalf("rollback geometry type = %q, want original malformed WKB", got)
	}
	if err := service.BeginEdit(); err != nil {
		t.Fatal(err)
	}
	if err := service.SetFeatureGeometry("roads", 1, replacement); err != nil {
		t.Fatal(err)
	}
	if err := service.Commit(); err != nil {
		t.Fatal(err)
	}
	geometry, ok := service.FeatureGeometry("roads", 1)
	if !ok || geometry.GeometryType() != "POINT" {
		t.Fatalf("committed geometry = %v, %v", geometry, ok)
	}
	if err := service.SetFeatureGeometry("roads", 1, replacement); !errors.Is(err, ErrEditNotActive) {
		t.Fatalf("geometry change outside transaction error = %v", err)
	}
}

func TestRepeatedDraftPropertyEditsRemainRollbackSafe(t *testing.T) {
	service := NewProjectService("demo", core.CRS{})
	if err := service.BeginEdit(); err != nil {
		t.Fatal(err)
	}
	if err := service.AddLayer(core.Layer{
		Name:     "roads",
		Editable: true,
		Features: []core.Feature{{ID: 1, Properties: map[string]any{"name": "original"}}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := service.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := service.BeginEdit(); err != nil {
		t.Fatal(err)
	}
	if err := service.SetFeatureProperty("roads", 1, "name", "first"); err != nil {
		t.Fatal(err)
	}
	if err := service.SetFeatureProperty("roads", 1, "name", "second"); err != nil {
		t.Fatal(err)
	}
	if err := service.Rollback(); err != nil {
		t.Fatal(err)
	}
	if value, _ := service.FeatureProperty("roads", 1, "name"); value != "original" {
		t.Fatalf("rolled back property = %v, want %q", value, "original")
	}
}

func TestLayerNamesAvoidsProjectSnapshot(t *testing.T) {
	service := NewProjectService("demo", core.CRS{})
	if err := service.BeginEdit(); err != nil {
		t.Fatal(err)
	}
	if err := service.AddLayer(core.Layer{
		Name:     "roads",
		Features: []core.Feature{{ID: 1, Geometry: core.WKTGeometry{WKT: "POINT (1 2)"}}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := service.AddLayer(core.Layer{Name: "buildings"}); err != nil {
		t.Fatal(err)
	}
	if err := service.Commit(); err != nil {
		t.Fatal(err)
	}
	if got := service.LayerNames(); len(got) != 2 || got[0] != "roads" || got[1] != "buildings" {
		t.Fatalf("layer names = %#v", got)
	}
}

func TestLayerReturnsDetachedNamedLayer(t *testing.T) {
	service := NewProjectService("demo", core.CRS{AuthorityCode: "EPSG:4326"})
	if err := service.BeginEdit(); err != nil {
		t.Fatal(err)
	}
	if err := service.AddLayer(core.Layer{Name: "roads", Features: []core.Feature{{ID: 1, Properties: map[string]any{"name": "길"}}}}); err != nil {
		t.Fatal(err)
	}
	if err := service.Commit(); err != nil {
		t.Fatal(err)
	}
	layer, ok := service.Layer("roads")
	if !ok {
		t.Fatal("named layer was not found")
	}
	layer.Features[0].Properties["name"] = "변경"
	if got := service.Project().Layers[0].Features[0].Properties["name"]; got != "길" {
		t.Fatalf("service was mutated through layer snapshot: %v", got)
	}
	if _, ok := service.Layer("missing"); ok {
		t.Fatal("missing layer was reported as present")
	}
}

func TestLayerAttributePageOnlyClonesRequestedRows(t *testing.T) {
	service := NewProjectService("demo", core.CRS{AuthorityCode: "EPSG:4326"})
	if err := service.BeginEdit(); err != nil {
		t.Fatal(err)
	}
	if err := service.AddLayer(core.Layer{Name: "roads", Features: []core.Feature{
		{ID: 1, Properties: map[string]any{"name": "first"}},
		{ID: 2, Properties: map[string]any{"name": "second"}},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := service.Commit(); err != nil {
		t.Fatal(err)
	}
	page, total, ok := service.LayerAttributePage("roads", 1, 1)
	if !ok || total != 2 || len(page.Features) != 1 || page.Features[0].ID != 2 {
		t.Fatalf("page=%#v total=%d ok=%v", page, total, ok)
	}
	page.Features[0].Properties["name"] = "changed"
	if value, _ := service.FeatureProperty("roads", 2, "name"); value != "second" {
		t.Fatalf("service was mutated through page: %v", value)
	}
}

func TestLayerAttributePageRejectsOversizedLimit(t *testing.T) {
	service, err := NewProjectServiceWithLayers("test", core.CRS{AuthorityCode: "EPSG:4326"}, []core.Layer{{Name: "roads", Features: []core.Feature{{ID: 1}}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, ok := service.LayerAttributePageOwned("roads", 0, maxAttributePageSize+1); ok {
		t.Fatal("oversized attribute page accepted")
	}
	page, _, ok := service.LayerAttributePageOwned("roads", int(^uint(0)>>1), 200)
	if !ok || len(page.Features) != 0 {
		t.Fatalf("out-of-range attribute page = %#v, valid=%t; want empty page", page, ok)
	}
}

func TestFeatureIndexBuildsOnlyRequestedLayer(t *testing.T) {
	service := NewProjectService("demo", core.CRS{})
	if err := service.BeginEdit(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"roads", "buildings"} {
		if err := service.AddLayer(core.Layer{
			Name:     name,
			Features: []core.Feature{{ID: 1}},
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := service.Commit(); err != nil {
		t.Fatal(err)
	}
	if !service.HasFeature("roads", 1) {
		t.Fatal("requested feature was not found")
	}
	if len(service.featureIndex) != 1 {
		t.Fatalf("feature index layers = %d, want 1", len(service.featureIndex))
	}
	if _, ok := service.featureIndex["buildings"]; ok {
		t.Fatal("unrequested layer was indexed")
	}
}

type recordingLayerWriter struct {
	destination string
	layer       core.Layer
}

func (w *recordingLayerWriter) Write(_ context.Context, destination string, layer core.Layer) error {
	w.destination = destination
	w.layer = layer
	return nil
}

type recordingLayerCollectionWriter struct {
	destination string
	layers      []core.Layer
}

func (w *recordingLayerCollectionWriter) Write(_ context.Context, destination string, layer core.Layer) error {
	w.destination = destination
	w.layers = []core.Layer{layer}
	return nil
}

func (w *recordingLayerCollectionWriter) WriteLayers(_ context.Context, destination string, layers []core.Layer) error {
	w.destination = destination
	w.layers = layers
	return nil
}

func TestProjectSaveAllLayersUsesCollectionWriterAndDetachedSnapshot(t *testing.T) {
	service := NewProjectService("demo", core.CRS{AuthorityCode: "EPSG:4326"})
	if err := service.BeginEdit(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"roads", "buildings"} {
		if err := service.AddLayer(core.Layer{
			Name:     name,
			Features: []core.Feature{{ID: 1, Properties: map[string]any{"name": name}}},
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := service.Commit(); err != nil {
		t.Fatal(err)
	}
	writer := &recordingLayerCollectionWriter{}
	if err := service.SaveAllLayers(context.Background(), writer, "all.gpkg"); err != nil {
		t.Fatal(err)
	}
	if writer.destination != "all.gpkg" || len(writer.layers) != 2 {
		t.Fatalf("write request destination=%q layers=%d", writer.destination, len(writer.layers))
	}
	writer.layers[0].Features[0].Properties["name"] = "mutated"
	if got := service.Project().Layers[0].Features[0].Properties["name"]; got != "roads" {
		t.Fatalf("writer mutated project snapshot: %v", got)
	}
}

func TestProjectSaveLayerUsesCommittedClone(t *testing.T) {
	service := NewProjectService("demo", core.CRS{AuthorityCode: "EPSG:4326"})
	layer := core.Layer{
		Name:     "roads",
		Editable: true,
		Features: []core.Feature{{ID: 1, Properties: map[string]any{"name": "길"}}},
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

	writer := &recordingLayerWriter{}
	if err := service.SaveLayer(context.Background(), writer, "roads.gpkg", "roads"); err != nil {
		t.Fatal(err)
	}
	if writer.destination != "roads.gpkg" || writer.layer.Name != "roads" {
		t.Fatalf("write request = %#v", writer)
	}
	writer.layer.Features[0].Properties["name"] = "writer mutation"
	if got := service.Project().Layers[0].Features[0].Properties["name"]; got != "길" {
		t.Fatalf("service was mutated by writer: %v", got)
	}
}

func TestProjectSaveLayerRequiresNameForMultipleLayers(t *testing.T) {
	service := NewProjectService("demo", core.CRS{})
	if err := service.BeginEdit(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"roads", "buildings"} {
		if err := service.AddLayer(core.Layer{Name: name, Features: []core.Feature{{ID: 1}}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := service.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := service.SaveLayer(context.Background(), &recordingLayerWriter{}, "out.gpkg", ""); err == nil {
		t.Fatal("save without layer name was accepted")
	}
}

func TestRenameLayerPreservesSourceIdentityAndRejectsDuplicates(t *testing.T) {
	service := &ProjectService{project: &core.Project{Name: "field work", Layers: []core.Layer{
		{Name: "roads", SourcePath: "roads.shp", SourceLayerName: "roads"}, {Name: "buildings"},
	}}}
	if err := service.RenameLayer("roads", "  local roads "); err != nil {
		t.Fatal(err)
	}
	layer, ok := service.Layer("roads")
	if !ok || layer.DisplayName != "local roads" || layer.SourcePath != "roads.shp" || layer.SourceLayerName != "roads" {
		t.Fatalf("rename lost source identity: %+v, %v", layer, ok)
	}
	if err := service.RenameLayer("roads", "BUILDINGS"); err == nil {
		t.Fatal("expected case-insensitive duplicate rejection")
	}
}

func TestProjectLayersExceptReturnsDetachedUnrelatedSnapshots(t *testing.T) {
	service := &ProjectService{project: &core.Project{Layers: []core.Layer{
		{Name: "roads", Features: []core.Feature{{ID: 1, Geometry: core.WKTGeometry{WKT: "POINT (1 2)"}, Properties: map[string]any{"name": "edited"}}}},
		{Name: "buildings", Features: []core.Feature{{ID: 2}}},
	}}}

	layers := service.ProjectLayersExcept("buildings")
	if len(layers) != 1 || layers[0].Name != "roads" || layers[0].Features[0].Properties["name"] != "edited" {
		t.Fatalf("unrelated project snapshots = %+v", layers)
	}
	layers[0].Features[0].Properties["name"] = "changed clone"
	layers[0].Features[0].Geometry = core.WKTGeometry{WKT: "POINT (9 9)"}
	if got := service.Project().Layers[0].Features[0].Properties["name"]; got != "edited" {
		t.Fatalf("mutating returned snapshot changed project property to %v", got)
	}
	if got := service.Project().Layers[0].Features[0].Geometry.(core.WKTGeometry).WKT; got != "POINT (1 2)" {
		t.Fatalf("mutating returned snapshot changed project geometry to %q", got)
	}
}

func TestUpdateLayerSettingsValidatesAndPersistsPresentation(t *testing.T) {
	layer := core.Layer{Name: "roads", SourcePath: "roads.shp", SourceLayerName: "roads", Visible: true,
		Style: core.DefaultLayerStyle(), Labels: core.DefaultLabelSettings()}
	service := &ProjectService{project: &core.Project{Name: "field work", Layers: []core.Layer{layer}}}
	layer.SourceEncoding = "CP949"
	layer.Visible = false
	layer.Style.LineColor = "#ff0000"
	layer.Labels = core.LabelSettings{Enabled: true, Expression: "name", Placement: "free-angle", HeightMM: 3}
	if err := service.UpdateLayerSettings("roads", layer); err != nil {
		t.Fatal(err)
	}
	got, _ := service.Layer("roads")
	if got.SourceEncoding != "CP949" || got.Visible || got.Style.LineColor != "#ff0000" || got.Labels != layer.Labels {
		t.Fatalf("settings were not retained: %+v", got)
	}
	layer.Style.LineColor = "blue"
	if err := service.UpdateLayerSettings("roads", layer); err == nil {
		t.Fatal("expected invalid style rejection")
	}
}

func TestProjectAddFeatureIsTransactional(t *testing.T) {
	service := NewProjectService("demo", core.CRS{})
	if err := service.BeginEdit(); err != nil {
		t.Fatal(err)
	}
	if err := service.AddLayer(core.Layer{Name: "roads", Editable: true}); err != nil {
		t.Fatal(err)
	}
	if err := service.AddFeature("roads", core.Feature{ID: 4, Properties: map[string]any{"name": "길"}}); err != nil {
		t.Fatal(err)
	}
	if err := service.Commit(); err != nil {
		t.Fatal(err)
	}
	if got := service.Project().Layers[0].Features[0].ID; got != 4 {
		t.Fatalf("feature ID = %d", got)
	}
}
