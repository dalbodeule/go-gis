package render

import "testing"

func TestViewportPanAndZoom(t *testing.T) {
	viewport := NewViewport()
	viewport.Pan(10, -5)
	viewport.ZoomBy(2)

	if viewport.Center != (Point{X: 10, Y: -5}) {
		t.Fatalf("center = %+v", viewport.Center)
	}
	if viewport.Zoom != 2 {
		t.Fatalf("zoom = %v, want 2", viewport.Zoom)
	}
}
