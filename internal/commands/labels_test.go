package commands

import (
	"context"
	"errors"
	"testing"

	"gogis/internal/core"
)

func TestGenerateLabelsUsesRepresentativeGeometryPositions(t *testing.T) {
	layer := core.Layer{Features: []core.Feature{
		{ID: 1, Geometry: core.WKTGeometry{WKT: "POINT (1 2)"}, Properties: map[string]any{"name": "점"}},
		{ID: 2, Geometry: core.WKTGeometry{WKT: "LINESTRING (0 0, 10 0, 20 0)"}, Properties: map[string]any{"name": "선"}},
		{ID: 3, Geometry: core.WKTGeometry{WKT: "POLYGON ((0 0, 10 0, 10 10, 0 10, 0 0))"}, Properties: map[string]any{"name": "면"}},
	}}
	result, err := GenerateLabels(context.Background(), layer, "name", 2.5, "Korean")
	if err != nil {
		t.Fatal(err)
	}
	expected := []core.Label{{Text: "점", X: 1, Y: 2, Height: 2.5, Style: "Korean"}, {Text: "선", X: 10, Y: 0, Height: 2.5, Style: "Korean"}, {Text: "면", X: 5, Y: 5, Height: 2.5, Style: "Korean"}}
	for index, label := range expected {
		if *result.Features[index].Label != label {
			t.Fatalf("label %d = %#v, want %#v", index, result.Features[index].Label, label)
		}
	}
	result.Features[0].Label.Text = "changed"
	if layer.Features[0].Label != nil {
		t.Fatal("label generation mutated source layer")
	}
}

func TestGenerateLabelsHandlesMissingFieldAndCancellation(t *testing.T) {
	layer := core.Layer{Features: []core.Feature{{ID: 1, Geometry: core.WKTGeometry{WKT: "POINT (0 0)"}}}}
	if _, err := GenerateLabels(context.Background(), layer, "", 1, ""); !errors.Is(err, ErrLabelFieldMissing) {
		t.Fatalf("field error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := GenerateLabels(ctx, layer, "name", 1, ""); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel error = %v", err)
	}
}

func TestGenerateLabelsUsesDistanceMidpointForLine(t *testing.T) {
	layer := core.Layer{Features: []core.Feature{{
		ID: 1, Geometry: core.WKTGeometry{WKT: "LINESTRING (0 0, 2 0, 2 10)"},
		Properties: map[string]any{"name": "uneven"},
	}}}
	result, err := GenerateLabels(context.Background(), layer, "name", 1, "")
	if err != nil {
		t.Fatal(err)
	}
	label := result.Features[0].Label
	if label == nil || label.X != 2 || label.Y != 4 {
		t.Fatalf("label midpoint = %#v, want (2,4)", label)
	}
}
