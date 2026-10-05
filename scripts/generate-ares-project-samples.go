//go:build native

// Command generate-ares-project-samples creates tiny synthetic three-layer DXF
// fixtures for manual CAD compatibility checks. No user GIS data is included.
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"gogis/drivers/dxf"
	"gogis/drivers/geos"
	"gogis/internal/core"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: go run ./scripts/generate-ares-project-samples.go OUTPUT_DIRECTORY")
		os.Exit(2)
	}
	directory := os.Args[1]
	if err := os.MkdirAll(directory, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	specs := []dxf.LayerSpec{
		{Name: "0-연속지적도", GeometryType: "POLYGON", Bounds: [4]float64{200000, 440000, 200020, 440020}, HasBounds: true},
		{Name: "0-지적도근점", GeometryType: "POINT", Bounds: [4]float64{200005, 440005, 200005, 440005}, HasBounds: true},
		{Name: "0-건물", GeometryType: "MULTIPOLYGON", Bounds: [4]float64{200030, 440000, 200045, 440015}, HasBounds: true, FillPolygons: true},
	}
	layers := []core.Layer{
		{Features: []core.Feature{{ID: 1, Geometry: core.WKTGeometry{WKT: "POLYGON ((200000 440000, 200020 440000, 200020 440020, 200000 440020, 200000 440000))"}, Label: &core.Label{Text: "123-4", X: 200010, Y: 440010, Height: 2.5}}}},
		{Features: []core.Feature{{ID: 2, Geometry: core.WKTGeometry{WKT: "POINT (200005 440005)"}, Label: &core.Label{Text: "도근점 1", X: 200005, Y: 440005, Height: 2.5}}}},
		{Features: []core.Feature{{ID: 3, Geometry: core.WKTGeometry{WKT: "MULTIPOLYGON (((200030 440000, 200036 440000, 200036 440006, 200030 440006, 200030 440000), (200032 440002, 200034 440002, 200034 440004, 200032 440004, 200032 440002)), ((200039 440000, 200045 440000, 200045 440006, 200039 440006, 200039 440000)))"}}}},
	}
	load := func(index int) (core.Layer, error) { return layers[index], nil }
	triangulator := geos.NewOperator()
	for _, sample := range []struct{ name, profile string }{
		{"sample-project-utf8.dxf", "ares-utf8"},
		{"sample-project-cp949.dxf", "ares-cp949"},
	} {
		if err := (dxf.Exporter{Triangulate: triangulator.ConstrainedTriangles}).ExportProject(context.Background(), filepath.Join(directory, sample.name), specs, load, sample.profile); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
}
