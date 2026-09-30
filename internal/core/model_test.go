package core

import (
	"encoding/hex"
	"testing"
)

func TestLayerCloneDetachesLabels(t *testing.T) {
	layer := Layer{
		Name: "labels",
		Features: []Feature{
			{ID: 1, Label: &Label{Text: "one", X: 1, Y: 2}},
			{ID: 2},
			{ID: 3, Label: &Label{Text: "three", X: 3, Y: 4}},
		},
	}

	clone := layer.Clone()
	if clone.Features[0].Label == layer.Features[0].Label {
		t.Fatal("layer clone reused the source label pointer")
	}
	if clone.Features[2].Label == layer.Features[2].Label {
		t.Fatal("layer clone reused the source label pointer")
	}
	if clone.Features[0].Label == clone.Features[2].Label {
		t.Fatal("layer clone reused labels within the clone")
	}

	clone.Features[0].Label.Text = "changed"
	if layer.Features[0].Label.Text != "one" {
		t.Fatal("mutating a cloned label changed the source layer")
	}
}

func TestLayerCloneDetachesWKBGeometryArena(t *testing.T) {
	data, err := hex.DecodeString("0101000000000000000000f03f0000000000000040")
	if err != nil {
		t.Fatal(err)
	}
	layer := Layer{Features: []Feature{
		{ID: 1, Geometry: WKBGeometry{WKB: data}},
		{ID: 2, Geometry: WKBGeometry{WKB: data}},
	}}
	clone := layer.Clone()
	first := clone.Features[0].Geometry.(WKBGeometry)
	second := clone.Features[1].Geometry.(WKBGeometry)
	if &first.WKB[0] == &second.WKB[0] {
		t.Fatal("cloned WKB feature slices overlap")
	}
	first.WKB[0] = 0
	if layer.Features[0].Geometry.(WKBGeometry).WKB[0] != 1 || second.WKB[0] != 1 {
		t.Fatal("mutating cloned WKB changed source or another feature")
	}
}

func BenchmarkLayerClone10KLabels(b *testing.B) {
	layer := Layer{Features: make([]Feature, 10_000)}
	for i := range layer.Features {
		layer.Features[i] = Feature{ID: uint64(i), Label: &Label{Text: "feature"}}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = layer.Clone()
	}
}

func BenchmarkLayerClone10KWKBPoints(b *testing.B) {
	data, err := hex.DecodeString("0101000000000000000000f03f0000000000000040")
	if err != nil {
		b.Fatal(err)
	}
	layer := Layer{Features: make([]Feature, 10_000)}
	for i := range layer.Features {
		layer.Features[i] = Feature{ID: uint64(i), Geometry: WKBGeometry{WKB: data}, Properties: map[string]any{"name": "point"}}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = layer.Clone()
	}
}

func BenchmarkLayerClone10KWKBPointsPerFeatureBaseline(b *testing.B) {
	data, err := hex.DecodeString("0101000000000000000000f03f0000000000000040")
	if err != nil {
		b.Fatal(err)
	}
	layer := Layer{Features: make([]Feature, 10_000)}
	for i := range layer.Features {
		layer.Features[i] = Feature{ID: uint64(i), Geometry: WKBGeometry{WKB: data}, Properties: map[string]any{"name": "point"}}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		clone := layer
		clone.Features = make([]Feature, len(layer.Features))
		for index, feature := range layer.Features {
			clone.Features[index] = cloneFeatureWithoutLabel(feature)
		}
		_ = clone
	}
}
