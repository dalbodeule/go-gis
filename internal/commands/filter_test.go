package commands

import (
	"context"
	"errors"
	"testing"

	"gogis/internal/core"
)

func TestFilterLayerByPropertyUsesTypedComparisonAndClones(t *testing.T) {
	layer := core.Layer{Features: []core.Feature{
		{ID: 1, Properties: map[string]any{"kind": "road", "active": true}},
		{ID: 2, Properties: map[string]any{"kind": "building", "active": false}},
	}}
	result, err := FilterLayerByProperty(context.Background(), layer, "active", "true")
	if err != nil || len(result.Features) != 1 || result.Features[0].ID != 1 {
		t.Fatalf("unexpected filtered result: %#v, %v", result, err)
	}
	result.Features[0].Properties["kind"] = "changed"
	if layer.Features[0].Properties["kind"] != "road" {
		t.Fatal("filter result shares source properties")
	}
}

func TestFilterLayerByPropertyMatchesNumericValues(t *testing.T) {
	layer := core.Layer{Features: []core.Feature{
		{ID: 1, Properties: map[string]any{"rank": int64(7)}},
		{ID: 2, Properties: map[string]any{"rank": float64(8.5)}},
	}}
	for _, test := range []struct {
		value string
		id    uint64
	}{
		{value: "7", id: 1},
		{value: "8.5", id: 2},
	} {
		result, err := FilterLayerByProperty(context.Background(), layer, "rank", test.value)
		if err != nil || len(result.Features) != 1 || result.Features[0].ID != test.id {
			t.Fatalf("value %q result = %#v, err = %v", test.value, result, err)
		}
	}
}

func TestFilterLayerValidatesFieldAndCancellation(t *testing.T) {
	if _, err := FilterLayerByProperty(context.Background(), core.Layer{}, "", "x"); !errors.Is(err, ErrFilterFieldMissing) {
		t.Fatalf("field error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := FilterLayerByProperty(ctx, core.Layer{}, "kind", "road"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel error = %v", err)
	}
}
