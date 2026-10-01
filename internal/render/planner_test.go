package render

import (
	"math"
	"testing"
)

func TestChunkPlannerIncludesLookAheadMargin(t *testing.T) {
	planner := ChunkPlanner{ChunkSize: 1, Margin: 1}
	keys := planner.VisibleKeys(Viewport{Zoom: 1}, "roads")
	if len(keys) != 16 {
		t.Fatalf("key count = %d, want 16", len(keys))
	}
	if keys[0] != (ChunkKey{Layer: "roads", ZoomBucket: 0, X: -2, Y: -2}) {
		t.Fatalf("first key = %#v", keys[0])
	}
	if keys[len(keys)-1] != (ChunkKey{Layer: "roads", ZoomBucket: 0, X: 1, Y: 1}) {
		t.Fatalf("last key = %#v", keys[len(keys)-1])
	}
}

func TestChunkPlannerChangesZoomBucketAndExtent(t *testing.T) {
	planner := ChunkPlanner{ChunkSize: 0.5, Margin: 0}
	zoomedOut := planner.VisibleKeys(Viewport{Zoom: 1}, "buildings")
	zoomedIn := planner.VisibleKeys(Viewport{Zoom: 4}, "buildings")
	if len(zoomedOut) <= len(zoomedIn) {
		t.Fatalf("zoomed-out keys = %d, zoomed-in keys = %d", len(zoomedOut), len(zoomedIn))
	}
	for _, key := range zoomedIn {
		if key.ZoomBucket != 2 {
			t.Fatalf("zoom bucket = %d, want 2", key.ZoomBucket)
		}
	}
}

func TestChunkPlannerTracksNegativePan(t *testing.T) {
	planner := ChunkPlanner{ChunkSize: 1, Margin: 0}
	keys := planner.VisibleKeys(Viewport{Center: Point{X: 2.1, Y: -1.2}, Zoom: 2}, "labels")
	if len(keys) == 0 {
		t.Fatal("planner returned no keys")
	}
	for _, key := range keys {
		if key.Layer != "labels" || key.X < 1 || key.Y > 0 {
			t.Fatalf("unexpected panned key = %#v", key)
		}
	}
}

func TestChunkPlannerVisibleKeysIntoReusesDestination(t *testing.T) {
	planner := ChunkPlanner{ChunkSize: 1, Margin: 0}
	viewport := Viewport{Center: Point{X: 0.5, Y: 0.5}, Zoom: 1}
	dst := make([]ChunkKey, 1, 8)
	dst[0] = ChunkKey{Layer: "prefix"}
	got := planner.VisibleKeysInto(dst, viewport, "roads")
	if len(got) != 5 || got[0] != dst[0] {
		t.Fatalf("unexpected destination contents: len=%d keys=%#v", len(got), got)
	}
	want := planner.VisibleKeys(viewport, "roads")
	for index := range want {
		if got[index+1] != want[index] {
			t.Fatalf("key[%d] = %#v, want %#v", index, got[index+1], want[index])
		}
	}
}

func TestChunkPlannerVisibleKeysIntoLimitRejectsAggregateWithoutMutation(t *testing.T) {
	planner := ChunkPlanner{ChunkSize: 0.01, Margin: 0}
	viewport := Viewport{Center: Point{X: 0.5, Y: 0.5}, Zoom: 1}
	prefix := []ChunkKey{{Layer: "existing", X: 7}}
	got, withinLimit := planner.VisibleKeysIntoLimit(prefix, viewport, "roads", 20)
	if withinLimit {
		t.Fatal("oversized viewport plan was accepted")
	}
	if len(got) != len(prefix) || got[0] != prefix[0] {
		t.Fatalf("overflow changed existing destination: %#v", got)
	}

	planner = ChunkPlanner{ChunkSize: 1, Margin: 0}
	viewport.Zoom = 2
	got, withinLimit = planner.VisibleKeysIntoLimit(prefix[:0], viewport, "roads", 1)
	if !withinLimit || len(got) != 1 {
		t.Fatalf("single-key plan = (%#v, %t), want one key within limit", got, withinLimit)
	}
}

func TestChunkPlannerBoundsExtremeZoomToDataDomain(t *testing.T) {
	planner := ChunkPlanner{
		ChunkSize: 0.25,
		Margin:    1,
		Domain:    [4]float64{0, 0, 1, 1},
		HasDomain: true,
	}
	keys := planner.VisibleKeys(Viewport{Zoom: 0.0001}, "roads")
	if len(keys) != 49 {
		t.Fatalf("extreme zoom-out planned %d keys, want 49 bounded keys", len(keys))
	}
	for _, key := range keys {
		if key.X < -1 || key.X > 5 || key.Y < -1 || key.Y > 5 {
			t.Fatalf("key is outside the bounded data domain: %+v", key)
		}
	}
}

func TestChunkPlannerRejectsUnboundedOrInvalidViewports(t *testing.T) {
	planner := ChunkPlanner{ChunkSize: 0.25, Margin: 1}
	viewport := Viewport{Zoom: 0.0001}
	if keys := planner.VisibleKeys(viewport, "roads"); len(keys) != 0 {
		t.Fatalf("unbounded extreme zoom planned %d keys, want safe empty result", len(keys))
	}
	viewport = Viewport{Center: Point{X: math.NaN()}, Zoom: 1}
	if keys := planner.VisibleKeys(viewport, "roads"); len(keys) != 0 {
		t.Fatalf("invalid viewport planned %d keys, want safe empty result", len(keys))
	}
}

func TestChunkPlannerSkipsDataOutsideViewportDomain(t *testing.T) {
	planner := ChunkPlanner{
		ChunkSize: 0.25,
		Domain:    [4]float64{0, 0, 1, 1},
		HasDomain: true,
	}
	keys := planner.VisibleKeys(Viewport{Center: Point{X: 100, Y: 100}, Zoom: 1}, "roads")
	if len(keys) != 0 {
		t.Fatalf("off-domain viewport planned %d keys, want none", len(keys))
	}
}
