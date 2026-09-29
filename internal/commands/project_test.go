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
