package render

import "testing"

func TestLayerVisibilityFiltersUnknownAndHiddenLayers(t *testing.T) {
	state := NewLayerVisibility("roads", "buildings")
	keys := []ChunkKey{
		{Layer: "roads", X: 1},
		{Layer: "buildings", X: 2},
		{Layer: "labels", X: 3},
	}
	if !state.Set("buildings", false) {
		t.Fatal("registered layer was not updated")
	}
	filtered := state.FilterChunkKeys(keys)
	if len(filtered) != 1 || filtered[0].Layer != "roads" {
		t.Fatalf("filtered keys = %#v", filtered)
	}
	if state.IsVisible("labels") {
		t.Fatal("unknown layer reported visible")
	}
}

func TestLayerVisibilityPreservesLayerTreeOrder(t *testing.T) {
	state := NewLayerVisibility("roads", "buildings", "labels")
	state.Set("buildings", false)
	state.Set("labels", false)
	state.Set("labels", true)
	visible := state.VisibleLayers()
	want := []string{"roads", "labels"}
	if len(visible) != len(want) || visible[0] != want[0] || visible[1] != want[1] {
		t.Fatalf("visible layers = %#v, want %#v", visible, want)
	}
}
