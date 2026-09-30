package render

import "math"

// ClipTriangleToRect clips a triangle to an axis-aligned rectangle and returns
// the resulting convex polygon as non-overlapping triangle fans.
func ClipTriangleToRect(triangle [3]Point, minX, minY, maxX, maxY float64) [][3]Point {
	polygon := []Point{triangle[0], triangle[1], triangle[2]}
	polygon = clipPolygonEdge(polygon, func(p Point) bool { return p.X >= minX }, func(a, b Point) Point {
		t := (minX - a.X) / (b.X - a.X)
		return Point{X: minX, Y: a.Y + t*(b.Y-a.Y)}
	})
	polygon = clipPolygonEdge(polygon, func(p Point) bool { return p.X <= maxX }, func(a, b Point) Point {
		t := (maxX - a.X) / (b.X - a.X)
		return Point{X: maxX, Y: a.Y + t*(b.Y-a.Y)}
	})
	polygon = clipPolygonEdge(polygon, func(p Point) bool { return p.Y >= minY }, func(a, b Point) Point {
		t := (minY - a.Y) / (b.Y - a.Y)
		return Point{X: a.X + t*(b.X-a.X), Y: minY}
	})
	polygon = clipPolygonEdge(polygon, func(p Point) bool { return p.Y <= maxY }, func(a, b Point) Point {
		t := (maxY - a.Y) / (b.Y - a.Y)
		return Point{X: a.X + t*(b.X-a.X), Y: maxY}
	})
	if len(polygon) < 3 {
		return nil
	}
	triangles := make([][3]Point, 0, len(polygon)-2)
	for index := 1; index+1 < len(polygon); index++ {
		candidate := [3]Point{polygon[0], polygon[index], polygon[index+1]}
		area2 := (candidate[1].X-candidate[0].X)*(candidate[2].Y-candidate[0].Y) -
			(candidate[2].X-candidate[0].X)*(candidate[1].Y-candidate[0].Y)
		if math.Abs(area2) > 1e-15 {
			triangles = append(triangles, candidate)
		}
	}
	return triangles
}

func clipPolygonEdge(polygon []Point, inside func(Point) bool, intersection func(Point, Point) Point) []Point {
	if len(polygon) == 0 {
		return nil
	}
	clipped := make([]Point, 0, len(polygon)+1)
	previous := polygon[len(polygon)-1]
	previousInside := inside(previous)
	for _, current := range polygon {
		currentInside := inside(current)
		if currentInside != previousInside {
			clipped = append(clipped, intersection(previous, current))
		}
		if currentInside {
			clipped = append(clipped, current)
		}
		previous, previousInside = current, currentInside
	}
	return clipped
}
