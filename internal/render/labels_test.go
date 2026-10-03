package render

import (
	"encoding/binary"
	"math"
	"testing"

	"gogis/internal/core"
)

func TestLongestSegmentPlacementUsesMidpointAndUprightAngle(t *testing.T) {
	for _, test := range []struct {
		name, geometry string
		x, y, angle    float64
		found          bool
	}{
		{name: "longest vertical segment", geometry: "LINESTRING (0 0, 2 0, 2 10)", x: 2, y: 5, angle: 90, found: true},
		{name: "reverse digitizing remains upright", geometry: "LINESTRING (10 0, 0 0)", x: 5, y: 0, angle: 0, found: true},
		{name: "polygon boundary", geometry: "POLYGON ((0 0, 10 0, 10 2, 0 2, 0 0))", x: 5, y: 0, angle: 0, found: true},
		{name: "point has no segment", geometry: "POINT (3 4)", found: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			anchor, angle, found, err := LongestSegmentPlacement(core.WKTGeometry{WKT: test.geometry})
			if err != nil || found != test.found || angle != test.angle || (found && (anchor.X != test.x || anchor.Y != test.y)) {
				t.Fatalf("placement = %v %g %t, %v", anchor, angle, found, err)
			}
		})
	}
}

func TestLongestSegmentPlacementWKB(t *testing.T) {
	wkb := make([]byte, 9+3*16)
	wkb[0] = 1
	binary.LittleEndian.PutUint32(wkb[1:5], 2)
	binary.LittleEndian.PutUint32(wkb[5:9], 3)
	for index, point := range [][2]float64{{0, 0}, {2, 0}, {2, 10}} {
		offset := 9 + index*16
		binary.LittleEndian.PutUint64(wkb[offset:offset+8], math.Float64bits(point[0]))
		binary.LittleEndian.PutUint64(wkb[offset+8:offset+16], math.Float64bits(point[1]))
	}
	anchor, angle, found, err := LongestSegmentPlacement(core.WKBGeometry{WKB: wkb})
	if err != nil || !found || anchor != (Point{X: 2, Y: 5}) || angle != 90 {
		t.Fatalf("WKB placement = %v %g %t, %v", anchor, angle, found, err)
	}
}
