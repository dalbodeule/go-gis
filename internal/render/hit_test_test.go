package render

import "testing"

func TestInitialHitMembershipCapacityIsBounded(t *testing.T) {
	if got := initialHitMembershipCapacity(10); got != 20 {
		t.Fatalf("small membership capacity = %d, want 20", got)
	}
	if got := initialHitMembershipCapacity(maxInitialHitMembershipCapacity * 100); got != maxInitialHitMembershipCapacity {
		t.Fatalf("large membership capacity = %d, want %d", got, maxInitialHitMembershipCapacity)
	}
}

func TestHitTestReturnsClosestLineFeature(t *testing.T) {
	features := []HitFeature{
		{Layer: "roads", FeatureID: 10, Vertices: []Point{{X: 0, Y: 0}, {X: 10, Y: 0}}},
		{Layer: "roads", FeatureID: 20, Vertices: []Point{{X: 0, Y: 3}, {X: 10, Y: 3}}},
	}
	result, ok := HitTest(features, Point{X: 4, Y: 0.2}, 0.5)
	if !ok || result.FeatureID != 10 || result.Layer != "roads" {
		t.Fatalf("hit = %#v, ok = %v", result, ok)
	}
	if result.Distance < 0.19 || result.Distance > 0.21 {
		t.Fatalf("distance = %f, want 0.2", result.Distance)
	}
}

func TestHitTestRejectsOutsideToleranceAndHandlesPoints(t *testing.T) {
	features := []HitFeature{{Layer: "labels", FeatureID: 7, Vertices: []Point{{X: 2, Y: 2}}}}
	if _, ok := HitTest(features, Point{X: 2.5, Y: 2}, 0.25); ok {
		t.Fatal("hit outside tolerance")
	}
	if result, ok := HitTest(features, Point{X: 2.1, Y: 2}, 0.25); !ok || result.FeatureID != 7 {
		t.Fatalf("point hit = %#v, ok = %v", result, ok)
	}
}

func TestHitTestRejectsDegenerateInput(t *testing.T) {
	if _, ok := HitTest(nil, Point{}, 1); ok {
		t.Fatal("empty feature list reported a hit")
	}
	if _, ok := HitTest([]HitFeature{{Vertices: []Point{{X: 0, Y: 0}}}}, Point{}, -1); ok {
		t.Fatal("negative tolerance reported a hit")
	}
}

func TestScreenPointToWorldUsesCenteredViewportAndInvertsY(t *testing.T) {
	viewport := Viewport{Center: Point{X: 10, Y: 20}, Zoom: 2}
	world, ok := ScreenPointToWorld(Point{X: 100, Y: 50}, viewport, 200, 100)
	if !ok {
		t.Fatal("screen point was rejected")
	}
	if world != (Point{X: 10, Y: 20}) {
		t.Fatalf("center world = %#v, want (10,20)", world)
	}

	world, ok = ScreenPointToWorld(Point{X: 120, Y: 40}, viewport, 200, 100)
	if !ok || world.X <= 10 || world.Y <= 20 {
		t.Fatalf("offset world = %#v, ok = %v", world, ok)
	}
}

func TestHitTestScreenUsesPixelTolerance(t *testing.T) {
	viewport := Viewport{Center: Point{X: 0.5, Y: 0.5}, Zoom: 1}
	features := []HitFeature{{Layer: "roads", FeatureID: 42, Vertices: []Point{{X: 0.4, Y: 0.5}, {X: 0.6, Y: 0.5}}}}
	result, ok := HitTestScreen(features, Point{X: 100, Y: 101}, viewport, 200, 200, 2)
	if !ok || result.FeatureID != 42 {
		t.Fatalf("screen hit = %#v, ok = %v", result, ok)
	}
}

func TestHitIndexMatchesLinearHitTest(t *testing.T) {
	features := []HitFeature{
		{Layer: "roads", FeatureID: 10, Vertices: []Point{{X: 0, Y: 0}, {X: 1, Y: 0}}},
		{Layer: "roads", FeatureID: 20, Vertices: []Point{{X: 0, Y: 0.4}, {X: 1, Y: 0.4}}},
		{Layer: "labels", FeatureID: 30, Vertices: []Point{{X: 0.9, Y: 0.9}}},
	}
	index := NewHitIndex(features, 0.25)
	linear, linearOK := HitTest(features, Point{X: 0.6, Y: 0.03}, 0.1)
	indexed, indexedOK := index.HitTest(Point{X: 0.6, Y: 0.03}, 0.1)
	if linearOK != indexedOK || linear != indexed {
		t.Fatalf("linear = %#v/%v, indexed = %#v/%v", linear, linearOK, indexed, indexedOK)
	}
	if _, ok := index.HitTest(Point{X: 0.6, Y: 0.2}, 0.05); ok {
		t.Fatal("indexed hit outside tolerance")
	}
}

func TestHitIndexDensePathMatchesLinearHitTest(t *testing.T) {
	features := []HitFeature{
		{Layer: "roads", FeatureID: 10, Vertices: []Point{{X: 0, Y: 0}, {X: 1, Y: 0}}},
		{Layer: "roads", FeatureID: 20, Vertices: []Point{{X: 0, Y: 0.4}, {X: 1, Y: 0.4}}},
		{Layer: "labels", FeatureID: 30, Vertices: []Point{{X: 0.9, Y: 0.9}}},
	}
	index := NewHitIndex(features, 0.25)
	point := Point{X: 0.6, Y: 0.2}
	linear, linearOK := HitTest(features, point, 0.5)
	indexed, indexedOK := index.HitTest(point, 0.5)
	if linearOK != indexedOK || linear != indexed {
		t.Fatalf("linear = %#v/%v, indexed = %#v/%v", linear, linearOK, indexed, indexedOK)
	}
}

func TestHitIndexLongSegmentUsesTraversedCells(t *testing.T) {
	features := []HitFeature{{
		Layer:     "roads",
		FeatureID: 1,
		Vertices:  []Point{{X: 0, Y: 0}, {X: 1, Y: 1}},
	}}
	index := NewHitIndex(features, 0.1)
	if len(index.cells) >= 121 {
		t.Fatalf("long segment indexed %d cells, want fewer than bounding box", len(index.cells))
	}
	if result, ok := index.HitTest(Point{X: 0.5, Y: 0.5}, 0.02); !ok || result.FeatureID != 1 {
		t.Fatalf("long segment hit = %#v, ok = %v", result, ok)
	}
}

func TestHitIndexAxisAlignedSegmentsUseAllCrossedCells(t *testing.T) {
	features := []HitFeature{
		{Layer: "roads", FeatureID: 1, Vertices: []Point{{X: 0.01, Y: 0.25}, {X: 0.99, Y: 0.25}}},
		{Layer: "roads", FeatureID: 2, Vertices: []Point{{X: 0.5, Y: 0.01}, {X: 0.5, Y: 0.99}}},
	}
	index := NewHitIndex(features, 0.1)
	if result, ok := index.HitTest(Point{X: 0.75, Y: 0.25}, 0.02); !ok || result.FeatureID != 1 {
		t.Fatalf("horizontal segment hit = %#v, ok = %v", result, ok)
	}
	if result, ok := index.HitTest(Point{X: 0.5, Y: 0.75}, 0.02); !ok || result.FeatureID != 2 {
		t.Fatalf("vertical segment hit = %#v, ok = %v", result, ok)
	}
	longIndex := NewHitIndex([]HitFeature{{
		Layer: "roads", FeatureID: 3,
		Vertices: []Point{{X: 0, Y: 0.5}, {X: 1, Y: 0.5}},
	}}, 0.01)
	if len(longIndex.spans) != 1 || len(longIndex.cells) != 0 {
		t.Fatalf("long horizontal index = spans=%d cells=%d", len(longIndex.spans), len(longIndex.cells))
	}
	if result, ok := longIndex.HitTest(Point{X: 0.5, Y: 0.5}, 0.02); !ok || result.FeatureID != 3 {
		t.Fatalf("long horizontal span hit = %#v, ok = %v", result, ok)
	}
}

func TestHitIndexScreenUsesPixelTolerance(t *testing.T) {
	features := []HitFeature{{Layer: "roads", FeatureID: 42, Vertices: []Point{{X: 0.4, Y: 0.5}, {X: 0.6, Y: 0.5}}}}
	index := NewHitIndex(features, 0.25)
	result, ok := index.HitTestScreen(Point{X: 100, Y: 101}, Viewport{Center: Point{X: 0.5, Y: 0.5}, Zoom: 1}, 200, 200, 2)
	if !ok || result.FeatureID != 42 {
		t.Fatalf("indexed screen hit = %#v, ok = %v", result, ok)
	}
}

func TestHitIndexHonorsLayerVisibility(t *testing.T) {
	features := []HitFeature{
		{Layer: "hidden", FeatureID: 1, Vertices: []Point{{X: 0, Y: 0}, {X: 1, Y: 0}}},
		{Layer: "visible", FeatureID: 2, Vertices: []Point{{X: 0, Y: 0.5}, {X: 1, Y: 0.5}}},
	}
	index := NewHitIndex(features, 0.25)
	if result, ok := index.HitTestVisible(Point{X: 0.5, Y: 0}, 0.1, map[string]bool{"visible": true}); ok || result.FeatureID != 0 {
		t.Fatalf("hidden layer was selectable: %#v, ok = %v", result, ok)
	}
	if result, ok := index.HitTestVisible(Point{X: 0.5, Y: 0}, 0.1, map[string]bool{"hidden": true}); !ok || result.FeatureID != 1 {
		t.Fatalf("visible hidden-layer hit = %#v, ok = %v", result, ok)
	}
}

func TestScreenPointToWorldRejectsInvalidViewport(t *testing.T) {
	if _, ok := ScreenPointToWorld(Point{}, Viewport{Zoom: 0}, 100, 100); ok {
		t.Fatal("invalid zoom was accepted")
	}
	if _, ok := HitTestScreen(nil, Point{}, Viewport{Zoom: 1}, 0, 100, 2); ok {
		t.Fatal("invalid dimensions were accepted")
	}
}
