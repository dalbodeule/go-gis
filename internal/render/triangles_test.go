package render

import (
	"math"
	"testing"
)

func TestClipTriangleToRectPreservesClippedArea(t *testing.T) {
	input := [3]Point{{X: -0.5, Y: 0.1}, {X: 0.75, Y: 0.1}, {X: 0.25, Y: 1.1}}
	clipped := ClipTriangleToRect(input, 0, 0, 0.5, 0.5)
	area := 0.0
	for _, triangle := range clipped {
		area += math.Abs((triangle[1].X-triangle[0].X)*(triangle[2].Y-triangle[0].Y)-
			(triangle[2].X-triangle[0].X)*(triangle[1].Y-triangle[0].Y)) / 2
		for _, point := range triangle {
			if point.X < -1e-12 || point.X > 0.5+1e-12 || point.Y < -1e-12 || point.Y > 0.5+1e-12 {
				t.Fatalf("clipped vertex outside rectangle: %+v", point)
			}
		}
	}
	if len(clipped) == 0 || math.Abs(area-0.2) > 1e-12 {
		t.Fatalf("clipped triangles cover area %v, want 0.2: %#v", area, clipped)
	}
}

func TestClipTriangleOutsideRectangleReturnsNoGeometry(t *testing.T) {
	input := [3]Point{{X: 2, Y: 2}, {X: 3, Y: 2}, {X: 2, Y: 3}}
	if got := ClipTriangleToRect(input, 0, 0, 1, 1); len(got) != 0 {
		t.Fatalf("outside triangle clip = %#v", got)
	}
}
