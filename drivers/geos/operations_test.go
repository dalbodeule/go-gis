//go:build native

package geos

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"strings"
	"testing"

	geoslib "github.com/twpayne/go-geos"
	"gogis/internal/core"
)

func TestConstrainedTrianglesRespectPolygonHole(t *testing.T) {
	operator := NewOperator()
	geometry := core.WKTGeometry{WKT: "POLYGON ((0 0, 10 0, 10 10, 0 10, 0 0), (3 3, 7 3, 7 7, 3 7, 3 3))"}
	triangles, err := operator.ConstrainedTriangles(context.Background(), geometry)
	if err != nil {
		t.Fatal(err)
	}
	area := 0.0
	for _, triangle := range triangles {
		area += math.Abs((triangle[1][0]-triangle[0][0])*(triangle[2][1]-triangle[0][1])-
			(triangle[2][0]-triangle[0][0])*(triangle[1][1]-triangle[0][1])) / 2
	}
	if len(triangles) == 0 || math.Abs(area-84) > 1e-8 {
		t.Fatalf("triangles=%d, covered area=%v; want 84 (including a 4x4 hole excluded)", len(triangles), area)
	}
}

func TestSimplifyForDisplayReducesGeometryWithoutMutatingSource(t *testing.T) {
	coordinates := make([]string, 0, 44)
	for index := 0; index <= 40; index++ {
		y := 0.0
		if index%2 == 1 {
			y = 0.02
		}
		coordinates = append(coordinates, fmt.Sprintf("%.1f %.2f", float64(index)/10, y))
	}
	coordinates = append(coordinates, "4 4", "0 4", "0 0")
	polygonText := "POLYGON ((" + strings.Join(coordinates, ", ") + "))"
	geosContext := geoslib.NewContext()
	input, err := geosContext.NewGeomFromWKT(polygonText)
	if err != nil {
		t.Fatal(err)
	}
	polygon := core.WKBGeometry{WKB: input.ToWKB()}
	input.Destroy()
	layer := core.Layer{Name: "parcels", Features: []core.Feature{{ID: 7, Geometry: polygon}}}
	result, err := NewOperator().SimplifyForDisplay(context.Background(), layer, 0.1)
	if err != nil {
		t.Fatal(err)
	}
	if got := layer.Features[0].Geometry.(core.WKBGeometry).WKB; string(got) != string(polygon.WKB) {
		t.Fatalf("source geometry mutated: %q", got)
	}
	simplified, ok := result.Features[0].Geometry.(core.WKBGeometry)
	if !ok {
		t.Fatalf("simplified geometry type = %T, want WKBGeometry", result.Features[0].Geometry)
	}
	count, err := simplified.PointCount()
	if err != nil || count >= len(coordinates) || count < 4 {
		t.Fatalf("simplified polygon point count = %d, err=%v; want fewer than %d", count, err, len(coordinates))
	}
}

func TestDissolvePolygonBoundariesForDisplayDropsSharedParcelEdge(t *testing.T) {
	geosContext := geoslib.NewContext()
	left, err := geosContext.NewGeomFromWKT("POLYGON ((0 0, 2 0, 2 2, 0 2, 0 0))")
	if err != nil {
		t.Fatal(err)
	}
	right, err := geosContext.NewGeomFromWKT("POLYGON ((2 0, 4 0, 4 2, 2 2, 2 0))")
	if err != nil {
		t.Fatal(err)
	}
	layer := core.Layer{Name: "parcels", CRS: core.CRS{AuthorityCode: "EPSG:5186"}, Features: []core.Feature{
		{ID: 1, Geometry: core.WKBGeometry{WKB: left.ToWKB()}},
		{ID: 2, Geometry: core.WKBGeometry{WKB: right.ToWKB()}},
	}}
	left.Destroy()
	right.Destroy()
	result, err := NewOperator().DissolvePolygonBoundariesForDisplay(context.Background(), layer)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Features) != 1 || result.Features[0].Geometry == nil {
		t.Fatalf("dissolved display features = %#v, want one coverage-boundary geometry", result.Features)
	}
	boundaryWKB, ok := result.Features[0].Geometry.(core.WKBGeometry)
	if !ok {
		t.Fatalf("boundary geometry type = %T, want WKBGeometry", result.Features[0].Geometry)
	}
	boundary, err := geosContext.NewGeomFromWKB(boundaryWKB.WKB)
	if err != nil {
		t.Fatal(err)
	}
	defer boundary.Destroy()
	if got := boundary.Length(); math.Abs(got-12) > 1e-9 {
		t.Fatalf("dissolved boundary length = %v, want 12 (shared edge excluded)", got)
	}
	if len(layer.Features) != 2 {
		t.Fatalf("source layer was mutated to %d features", len(layer.Features))
	}
}

func TestDissolvePolygonCoverageForDisplayKeepsFillArea(t *testing.T) {
	geosContext := geoslib.NewContext()
	left, err := geosContext.NewGeomFromWKT("POLYGON ((0 0, 2 0, 2 2, 0 2, 0 0))")
	if err != nil {
		t.Fatal(err)
	}
	right, err := geosContext.NewGeomFromWKT("POLYGON ((2 0, 4 0, 4 2, 2 2, 2 0))")
	if err != nil {
		left.Destroy()
		t.Fatal(err)
	}
	layer := core.Layer{Name: "parcels", CRS: core.CRS{AuthorityCode: "EPSG:5186"}, Features: []core.Feature{
		{Geometry: core.WKBGeometry{WKB: left.ToWKB()}},
		{Geometry: core.WKBGeometry{WKB: right.ToWKB()}},
	}}
	left.Destroy()
	right.Destroy()
	result, err := NewOperator().DissolvePolygonCoverageForDisplay(context.Background(), layer)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Features) != 1 || !strings.Contains(result.Features[0].Geometry.GeometryType(), "POLYGON") {
		t.Fatalf("coverage features = %#v, want one fillable polygon", result.Features)
	}
	coverage, err := geosContext.NewGeomFromWKB(result.Features[0].Geometry.(core.WKBGeometry).WKB)
	if err != nil {
		t.Fatal(err)
	}
	defer coverage.Destroy()
	if got := coverage.Area(); math.Abs(got-8) > 1e-9 {
		t.Fatalf("coverage area = %v, want 8", got)
	}
	if len(layer.Features) != 2 {
		t.Fatal("coverage dissolve mutated the source layer")
	}
}

func TestOverviewUnionGridRespectsCRSUnits(t *testing.T) {
	for _, test := range []struct {
		crs  string
		want float64
	}{{"EPSG:5186", 0.1}, {"EPSG:4326", 1e-6}, {"EPSG:4258", 1e-6}, {"LOCAL:unknown", 0}} {
		if got := overviewUnionGridSize(test.crs); got != test.want {
			t.Errorf("overview union grid for %s = %g, want %g", test.crs, got, test.want)
		}
	}
}

func TestDissolvePolygonBoundariesForDisplaySnapsSubDecimeterSeams(t *testing.T) {
	ctx := geoslib.NewContext()
	left, err := ctx.NewGeomFromWKT("POLYGON ((0 0, 2 0, 2 2, 0 2, 0 0))")
	if err != nil {
		t.Fatal(err)
	}
	right, err := ctx.NewGeomFromWKT("POLYGON ((2.04 0, 4 0, 4 2, 2.04 2, 2.04 0))")
	if err != nil {
		left.Destroy()
		t.Fatal(err)
	}
	layer := core.Layer{Name: "parcels", CRS: core.CRS{AuthorityCode: "EPSG:5186"}, Features: []core.Feature{
		{ID: 1, Geometry: core.WKBGeometry{WKB: left.ToWKB()}},
		{ID: 2, Geometry: core.WKBGeometry{WKB: right.ToWKB()}},
	}}
	left.Destroy()
	right.Destroy()

	result, err := NewOperator().DissolvePolygonBoundariesForDisplay(context.Background(), layer)
	if err != nil {
		t.Fatal(err)
	}
	boundaryWKB := result.Features[0].Geometry.(core.WKBGeometry)
	boundary, err := ctx.NewGeomFromWKB(boundaryWKB.WKB)
	if err != nil {
		t.Fatal(err)
	}
	defer boundary.Destroy()
	if got := boundary.Length(); math.Abs(got-12) > 1e-9 {
		t.Fatalf("overview boundary length = %v, want 12 after snapping the 4 cm seam", got)
	}
	if len(layer.Features) != 2 {
		t.Fatal("overview dissolve mutated the source layer")
	}
}

func TestConstrainedTrianglesRejectsOverBudgetWKBBeforeGEOS(t *testing.T) {
	pointCount := maxConstrainedTriangulationCoordinates + 1
	wkb := make([]byte, 13+pointCount*16)
	wkb[0] = 1
	binary.LittleEndian.PutUint32(wkb[1:5], 3) // Polygon
	binary.LittleEndian.PutUint32(wkb[5:9], 1) // one ring
	binary.LittleEndian.PutUint32(wkb[9:13], uint32(pointCount))
	_, err := NewOperator().ConstrainedTriangles(context.Background(), core.WKBGeometry{WKB: wkb})
	if err == nil || !strings.Contains(err.Error(), "coordinate safety limit") {
		t.Fatalf("oversized triangulation WKB error = %v, want coordinate limit", err)
	}
}

func TestConstrainedTrianglesRejectsOverBudgetWKTBeforeGEOS(t *testing.T) {
	wkt := "POLYGON ((" + strings.Repeat("0 0,", maxConstrainedTriangulationCoordinates+1) + "0 0))"
	_, err := NewOperator().ConstrainedTriangles(context.Background(), core.WKTGeometry{WKT: wkt})
	if err == nil || !strings.Contains(err.Error(), "WKT exceeds") {
		t.Fatalf("oversized triangulation WKT error = %v, want byte limit", err)
	}
}

func TestConstrainedTrianglesIgnoresNonPolygonGeometryWithoutGEOSRead(t *testing.T) {
	operator := NewOperator()
	triangles, err := operator.ConstrainedTriangles(context.Background(), core.WKBGeometry{WKB: []byte{1, 1, 0, 0, 0}})
	if err != nil || len(triangles) != 0 {
		t.Fatalf("non-polygon triangulation = %d triangles, err=%v; want empty result", len(triangles), err)
	}
	if _, err := operator.ConstrainedTriangles(context.Background(), core.WKBGeometry{WKB: []byte{1, 99, 0, 0, 0}}); err == nil {
		t.Fatal("unsupported WKB geometry type was silently ignored")
	}
}

func TestPointOnSurfaceFindsInteriorOfConcavePolygon(t *testing.T) {
	geometry := core.WKTGeometry{WKT: "POLYGON ((0 0, 10 0, 10 4, 4 4, 4 10, 0 10, 0 0))"}
	operator := NewOperator()
	anchor, found, err := operator.PointOnSurface(context.Background(), geometry)
	if err != nil || !found {
		t.Fatalf("point on surface = %v, found=%v, err=%v", anchor, found, err)
	}
	// This L-shaped polygon excludes the upper-right notch (x>4 && y>4).
	if anchor[0] < 0 || anchor[1] < 0 || anchor[0] > 10 || anchor[1] > 10 || anchor[0] > 4 && anchor[1] > 4 {
		t.Fatalf("label anchor is outside the concave polygon: %v", anchor)
	}
}

func BenchmarkCloneLayerForOperation10KPoints(b *testing.B) {
	layer := benchmarkGEOSLayer(10_000)
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		benchmarkGEOSSink = cloneLayerForOperation(layer)
	}
}

func BenchmarkCloneLayerForOperationGeometryCloneBaseline10KPoints(b *testing.B) {
	layer := benchmarkGEOSLayer(10_000)
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		benchmarkGEOSSink = layer.Clone()
	}
}

var benchmarkGEOSSink any

func BenchmarkReadWKBPoint(b *testing.B) {
	data, err := hex.DecodeString("0101000000000000000000f03f0000000000000040")
	if err != nil {
		b.Fatal(err)
	}
	operator := NewOperator()
	geometry := core.WKBGeometry{WKB: data}
	b.ReportAllocs()
	for index := 0; index < b.N; index++ {
		result, err := operator.read(geometry)
		if err != nil {
			b.Fatal(err)
		}
		result.Destroy()
	}
}

func BenchmarkReadWKTPoint(b *testing.B) {
	operator := NewOperator()
	geometry := core.WKTGeometry{WKT: "POINT (1 2)"}
	b.ReportAllocs()
	for index := 0; index < b.N; index++ {
		result, err := operator.read(geometry)
		if err != nil {
			b.Fatal(err)
		}
		result.Destroy()
	}
}

func BenchmarkWriteGEOSWKBPoint(b *testing.B) {
	operator := NewOperator()
	geometry, err := operator.read(core.WKTGeometry{WKT: "POLYGON ((0 0, 10 0, 10 10, 0 10, 0 0))"})
	if err != nil {
		b.Fatal(err)
	}
	defer geometry.Destroy()
	b.ReportAllocs()
	for index := 0; index < b.N; index++ {
		benchmarkGEOSSink = geometry.ToWKB()
	}
}

func BenchmarkWriteGEOSWKTPoint(b *testing.B) {
	operator := NewOperator()
	geometry, err := operator.read(core.WKTGeometry{WKT: "POLYGON ((0 0, 10 0, 10 10, 0 10, 0 0))"})
	if err != nil {
		b.Fatal(err)
	}
	defer geometry.Destroy()
	b.ReportAllocs()
	for index := 0; index < b.N; index++ {
		benchmarkGEOSSink = geometry.ToWKT()
	}
}

func benchmarkGEOSLayer(count int) core.Layer {
	features := make([]core.Feature, count)
	for index := range features {
		features[index] = core.Feature{
			ID:         uint64(index + 1),
			Geometry:   core.WKTGeometry{WKT: fmt.Sprintf("POINT (%d %d)", index, index)},
			Properties: map[string]any{"name": fmt.Sprintf("feature-%d", index)},
			Label:      &core.Label{Text: "label"},
		}
	}
	return core.Layer{Fields: []core.Field{{Name: "name", Type: core.FieldTypeText}}, Features: features}
}

func TestOperatorBuffersWithGEOS(t *testing.T) {
	layer := core.Layer{Features: []core.Feature{{ID: 1, Geometry: core.WKTGeometry{WKT: "POINT (0 0)"}, Properties: map[string]any{"name": "source"}, Label: &core.Label{Text: "source"}}}}
	result, err := NewOperator().Buffer(context.Background(), layer, 10)
	if err != nil {
		t.Fatal(err)
	}
	geometry, err := core.ToWKT(result.Features[0].Geometry)
	if err != nil || !strings.HasPrefix(geometry.WKT, "POLYGON") {
		t.Fatalf("buffer WKT = %#v, err=%v", geometry, err)
	}
	result.Features[0].Properties["name"] = "changed"
	result.Features[0].Label.Text = "changed"
	if layer.Features[0].Properties["name"] != "source" || layer.Features[0].Label.Text != "source" {
		t.Fatal("buffer result shares mutable metadata with source")
	}
}

func TestOperatorReadsWKBDirectly(t *testing.T) {
	data, err := hex.DecodeString("0101000000000000000000f03f0000000000000040")
	if err != nil {
		t.Fatal(err)
	}
	result, err := NewOperator().Buffer(context.Background(), core.Layer{
		Features: []core.Feature{{ID: 1, Geometry: core.WKBGeometry{WKB: data}}},
	}, 1)
	if err != nil {
		t.Fatal(err)
	}
	geometry, err := core.ToWKT(result.Features[0].Geometry)
	if err != nil || !strings.HasPrefix(geometry.WKT, "POLYGON") {
		t.Fatalf("WKB buffer result = %#v, err=%v", result.Features[0].Geometry, err)
	}
}

func TestOperatorRunsBinaryOperationsWithGEOS(t *testing.T) {
	left := core.Layer{Features: []core.Feature{{ID: 1, Geometry: core.WKTGeometry{WKT: "POLYGON ((0 0, 10 0, 10 10, 0 10, 0 0))"}}}}
	right := core.Layer{Features: []core.Feature{{ID: 2, Geometry: core.WKTGeometry{WKT: "POLYGON ((5 5, 15 5, 15 15, 5 15, 5 5))"}}}}
	operator := NewOperator()
	for name, operation := range map[string]func(context.Context, core.Layer, core.Layer) (core.Layer, error){
		"intersection": operator.Intersect,
		"union":        operator.Union,
		"difference":   operator.Difference,
	} {
		result, err := operation(context.Background(), left, right)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		geometry, err := core.ToWKT(result.Features[0].Geometry)
		if err != nil || geometry.WKT == "" {
			t.Fatalf("%s returned empty geometry: %#v, err=%v", name, geometry, err)
		}
	}
}
