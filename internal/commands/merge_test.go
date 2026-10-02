package commands

import (
	"context"
	"errors"
	"runtime"
	"testing"

	"gogis/internal/core"
)

type cancelAfterErrChecksContext struct {
	context.Context
	checks int
	after  int
}

func (ctx *cancelAfterErrChecksContext) Err() error {
	if ctx.checks >= ctx.after {
		return context.Canceled
	}
	ctx.checks++
	return nil
}

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

func TestMergeLayersRejectsMetadataBeforeLargeFeatureAllocations(t *testing.T) {
	largeFeatures := make([]core.Feature, 250_000)
	left := core.Layer{Name: "left", Fields: []core.Field{{Name: "a"}}, Features: largeFeatures}
	right := core.Layer{Name: "right", Fields: []core.Field{{Name: "b"}}, Features: largeFeatures}
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	if _, err := MergeLayers(context.Background(), left, right); !errors.Is(err, ErrSchemaMismatch) {
		t.Fatalf("large incompatible merge error = %v, want schema mismatch", err)
	}
	runtime.ReadMemStats(&after)
	allocated := after.TotalAlloc - before.TotalAlloc
	if allocated > 4<<20 {
		t.Fatalf("incompatible merge allocated %d bytes before rejecting metadata; want under 4 MiB", allocated)
	}
}

func TestMergeLayersRejectsLateDuplicateBeforeCloningFeatures(t *testing.T) {
	const featureCount = 10_000
	geometry := make([]byte, 1<<10)
	features := make([]core.Feature, featureCount)
	for index := range features {
		features[index] = core.Feature{ID: uint64(index + 1), Geometry: core.WKBGeometry{WKB: geometry}}
	}
	left := core.Layer{Name: "left", Features: features}
	right := core.Layer{Name: "right", Features: []core.Feature{{ID: featureCount}}}
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	if _, err := MergeLayers(context.Background(), left, right); !errors.Is(err, ErrDuplicateID) {
		t.Fatalf("late duplicate merge error = %v, want duplicate ID", err)
	}
	runtime.ReadMemStats(&after)
	allocated := after.TotalAlloc - before.TotalAlloc
	if allocated > 4<<20 {
		t.Fatalf("late-duplicate merge allocated %d bytes before rejection; want under 4 MiB", allocated)
	}
}

func TestAddMergeFeatureCountRejectsOverflow(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	if got, ok := addMergeFeatureCount(maxInt-1, 1); !ok || got != maxInt {
		t.Fatalf("valid boundary sum = %d, %t", got, ok)
	}
	if _, ok := addMergeFeatureCount(maxInt, 1); ok {
		t.Fatal("overflowing merge feature count was accepted")
	}
}

func TestMergeLayersChecksCancellationDuringLargeLayer(t *testing.T) {
	features := make([]core.Feature, mergeCancellationCheckInterval*4)
	for index := range features {
		features[index].ID = uint64(index)
	}
	left := core.Layer{Name: "left", Features: features}
	right := core.Layer{Name: "right", Features: []core.Feature{{ID: uint64(len(features))}}}
	ctx := &cancelAfterErrChecksContext{Context: context.Background(), after: 2}

	if _, err := MergeLayers(ctx, left, right); !errors.Is(err, context.Canceled) {
		t.Fatalf("merge cancellation error = %v, want context.Canceled", err)
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
	service.project.Layers[0].Features[0].Properties["name"] = "source-mutated"
	merged, ok := service.Layer("merged")
	if !ok || merged.Features[0].Properties["name"] != "a" {
		t.Fatal("merged layer shares feature data with its source")
	}
	if err := service.MergeProjectLayers(context.Background(), []string{"a", "missing"}, "bad"); err == nil {
		t.Fatal("expected missing source error")
	}
	if len(service.Project().Layers) != 3 {
		t.Fatal("failed merge changed project")
	}
}
