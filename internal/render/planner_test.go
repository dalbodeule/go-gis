package render

import "testing"

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
