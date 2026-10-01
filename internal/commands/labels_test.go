package commands

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"math"
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

func TestGenerateLabelsWithRotationField(t *testing.T) {
	layer := core.Layer{Features: []core.Feature{
		{ID: 1, Geometry: core.WKTGeometry{WKT: "POINT (1 2)"}, Properties: map[string]any{"name": "road", "angle": 30}},
		{ID: 2, Geometry: core.WKTGeometry{WKT: "POINT (3 4)"}, Properties: map[string]any{"name": "building", "angle": 0}},
	}}
	result, err := GenerateLabelsWithRotation(context.Background(), layer, "name", "angle", 2.5, "Korean")
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Features[0].Label.Rotation; got != 30 {
		t.Fatalf("first label rotation = %v, want 30", got)
	}
	if got := result.Features[1].Label.Rotation; got != 0 {
		t.Fatalf("second label rotation = %v, want 0", got)
	}
	if layer.Features[0].Label != nil {
		t.Fatal("rotation label generation mutated the source")
	}
}

func TestGenerateLabelsWithRotationRejectsInvalidFieldValues(t *testing.T) {
	for _, value := range []any{"sideways", math.Inf(1), "NaN"} {
		layer := core.Layer{Features: []core.Feature{{
			ID: 7, Geometry: core.WKTGeometry{WKT: "POINT (1 2)"}, Properties: map[string]any{"name": "road", "angle": value},
		}}}
		if _, err := GenerateLabelsWithRotation(context.Background(), layer, "name", "angle", 1, ""); err == nil {
			t.Errorf("invalid rotation %v was accepted", value)
		}
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

func TestGenerateLabelsReadsWKBPointDirectly(t *testing.T) {
	data, err := hex.DecodeString("0101000000000000000000f03f0000000000000040")
	if err != nil {
		t.Fatal(err)
	}
	result, err := GenerateLabels(context.Background(), core.Layer{Features: []core.Feature{{
		ID: 1, Geometry: core.WKBGeometry{WKB: data}, Properties: map[string]any{"name": "point"},
	}}}, "name", 1, "default")
	if err != nil {
		t.Fatal(err)
	}
	if label := result.Features[0].Label; label == nil || label.X != 1 || label.Y != 2 {
		t.Fatalf("WKB label = %#v", label)
	}
}

func TestGenerateLabelsReadsWKBLineDirectly(t *testing.T) {
	data := make([]byte, 57)
	data[0] = 1
	binary.LittleEndian.PutUint32(data[1:5], 2)
	binary.LittleEndian.PutUint32(data[5:9], 3)
	for index, point := range [][2]float64{{0, 0}, {2, 0}, {2, 10}} {
		offset := 9 + index*16
		binary.LittleEndian.PutUint64(data[offset:offset+8], math.Float64bits(point[0]))
		binary.LittleEndian.PutUint64(data[offset+8:offset+16], math.Float64bits(point[1]))
	}
	point, handled, err := representativeStandardWKB(data)
	if err != nil || !handled || point != (labelPoint{X: 2, Y: 4}) {
		t.Fatalf("WKB line representative = %#v, handled=%v, err=%v", point, handled, err)
	}
}

func TestGenerateLabelsReadsWKBPolygonDirectly(t *testing.T) {
	data := make([]byte, 93)
	data[0] = 1
	binary.LittleEndian.PutUint32(data[1:5], 3)
	binary.LittleEndian.PutUint32(data[5:9], 1)
	binary.LittleEndian.PutUint32(data[9:13], 5)
	for index, point := range [][2]float64{{0, 0}, {10, 0}, {10, 10}, {0, 10}, {0, 0}} {
		offset := 13 + index*16
		binary.LittleEndian.PutUint64(data[offset:offset+8], math.Float64bits(point[0]))
		binary.LittleEndian.PutUint64(data[offset+8:offset+16], math.Float64bits(point[1]))
	}
	point, handled, err := representativeStandardWKB(data)
	if err != nil || !handled || point != (labelPoint{X: 5, Y: 5}) {
		t.Fatalf("WKB polygon representative = %#v, handled=%v, err=%v", point, handled, err)
	}
}

func TestCoordinatePairsSupportsExponentAndRejectsOddValues(t *testing.T) {
	pairs, err := coordinatePairs("POINT (1.5e+1 -2.5E-1)")
	if err != nil {
		t.Fatal(err)
	}
	if len(pairs) != 1 || pairs[0] != (labelPoint{X: 15, Y: -0.25}) {
		t.Fatalf("pairs = %#v", pairs)
	}
	if _, err := coordinatePairs("POINT (1 2 3)"); err == nil {
		t.Fatal("expected odd coordinate error")
	}
	if _, err := coordinatePairs("POINT (1e 2)"); err == nil {
		t.Fatal("expected malformed number error")
	}
}

func TestLabelProjectLayerDoesNotMutateSourceFeatures(t *testing.T) {
	service := NewProjectService("demo", core.CRS{})
	if err := service.BeginEdit(); err != nil {
		t.Fatal(err)
	}
	if err := service.AddLayer(core.Layer{
		Name:     "roads",
		Features: []core.Feature{{ID: 1, Geometry: core.WKTGeometry{WKT: "POINT (1 2)"}, Properties: map[string]any{"name": "road"}}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := service.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := service.LabelProjectLayer(context.Background(), "roads", "name", "labeled", 1, "default"); err != nil {
		t.Fatal(err)
	}
	if service.project.Layers[0].Features[0].Label != nil {
		t.Fatal("label command mutated source feature")
	}
	result, ok := service.Layer("labeled")
	if !ok || result.Features[0].Label == nil || result.Features[0].Label.Text != "road" {
		t.Fatalf("labeled result = %#v", result)
	}
}
