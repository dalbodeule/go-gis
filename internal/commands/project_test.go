package commands

import (
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
