// Package render contains UI-neutral map viewport state.
package render

// Point is a two-dimensional map coordinate.
type Point struct {
	X float64
	Y float64
}

// Viewport is the minimal state needed by a 2D map canvas.
type Viewport struct {
	Center Point
	Zoom   float64
}

// NewViewport returns a usable default viewport.
func NewViewport() Viewport {
	return Viewport{Zoom: 1}
}

// Pan moves the viewport center by map units.
func (v *Viewport) Pan(dx, dy float64) {
	v.Center.X += dx
	v.Center.Y += dy
}

// ZoomBy changes the zoom by a multiplier and prevents invalid values.
func (v *Viewport) ZoomBy(multiplier float64) {
	if multiplier <= 0 {
		return
	}
	v.Zoom *= multiplier
	if v.Zoom < 0.0001 {
		v.Zoom = 0.0001
	}
}
