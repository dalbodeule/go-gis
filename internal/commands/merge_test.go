package commands

import (
	"context"
	"errors"
	"testing"

	"gogis/internal/core"
)

func mergeLayer(name string, featureID uint64, crs string, fields []core.Field) core.Layer {
	return core.Layer{
		Name: name, CRS: core.CRS{AuthorityCode: crs}, Fields: fields,
		Features: []core.Feature{{ID: featureID, Geometry: core.WKTGeometry{WKT: "POINT (1 2)"}, Properties: map[string]any{"name": name}}},
	}
}

func TestMergeLayersClonesFeaturesAndPreservesSchema(t *testing.T) {
	fields := []core.Field{{Name: "name", Type: core.FieldTypeText}}
	left := mergeLayer("left", 1, "EPSG:4326", fields)
	right := mergeLayer("right", 2, "EPSG:4326", fields)
	result, err := MergeLayers(context.Background(), left, right)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Features) != 2 || result.Name != "left_merged" || result.Fields[0] != fields[0] {
		t.Fatalf("unexpected merge result: %#v", result)
	}
	result.Features[0].Properties["name"] = "changed"
	if left.Features[0].Properties["name"] != "left" {
		t.Fatal("merge result shares feature properties with source")
	}
}

func TestMergeLayersRejectsSchemaCRSAndDuplicateIDs(t *testing.T) {
	fields := []core.Field{{Name: "name", Type: core.FieldTypeText}}
	otherFields := []core.Field{{Name: "code", Type: core.FieldTypeNumber}}
	if _, err := MergeLayers(context.Background(), mergeLayer("a", 1, "EPSG:4326", fields), mergeLayer("b", 2, "EPSG:4326", otherFields)); !errors.Is(err, ErrSchemaMismatch) {
		t.Fatalf("schema error = %v", err)
	}
	if _, err := MergeLayers(context.Background(), mergeLayer("a", 1, "EPSG:4326", fields), mergeLayer("b", 2, "EPSG:5179", fields)); !errors.Is(err, ErrCRSMismatch) {
		t.Fatalf("CRS error = %v", err)
	}
	if _, err := MergeLayers(context.Background(), mergeLayer("a", 1, "EPSG:4326", fields), mergeLayer("b", 1, "EPSG:4326", fields)); !errors.Is(err, ErrDuplicateID) {
		t.Fatalf("duplicate ID error = %v", err)
	}
}

func TestMergeProjectLayersIsAtomic(t *testing.T) {
	fields := []core.Field{{Name: "name", Type: core.FieldTypeText}}
	service := NewProjectService("demo", core.CRS{})
	if err := service.BeginEdit(); err != nil {
		t.Fatal(err)
	}
	for _, layer := range []core.Layer{mergeLayer("a", 1, "EPSG:4326", fields), mergeLayer("b", 2, "EPSG:4326", fields)} {
		if err := service.AddLayer(layer); err != nil {
			t.Fatal(err)
		}
	}
	if err := service.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := service.MergeProjectLayers(context.Background(), []string{"a", "b"}, "merged"); err != nil {
		t.Fatal(err)
	}
	if len(service.Project().Layers) != 3 {
		t.Fatal("merged layer was not committed")
	}
	if err := service.MergeProjectLayers(context.Background(), []string{"a", "missing"}, "bad"); err == nil {
		t.Fatal("expected missing source error")
	}
	if len(service.Project().Layers) != 3 {
		t.Fatal("failed merge changed project")
	}
}
