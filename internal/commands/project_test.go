package commands

import (
	"context"
	"testing"

	"gogis/internal/core"
)

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
