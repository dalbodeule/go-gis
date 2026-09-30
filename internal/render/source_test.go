package render

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"math"
	"testing"

	"gogis/internal/core"
)

func TestLayerSourceCarriesConfiguredGeometryColor(t *testing.T) {
	layer := core.Layer{
		Name: "roads", Style: core.LayerStyle{LineColor: "#aabbcc", PointColor: "#010203", PolygonColor: "#ffffff", LineWidthMM: 1.4, PointSizeMM: 3.2},
		Features: []core.Feature{{ID: 1, Geometry: core.WKTGeometry{WKT: "LINESTRING (0 0, 1 1)"}}},
	}
	source, err := NewLayerSource(layer)
	if err != nil {
		t.Fatal(err)
	}
	chunk, err := source.Builder(context.Background(), ChunkKey{Layer: "roads", X: 0, Y: 0})
	if err != nil {
		t.Fatal(err)
	}
	if len(chunk.Vertices) == 0 || chunk.Vertices[0].Color != 0xaabbccff {
		t.Fatalf("configured line color not carried by render vertices: %#v", chunk.Vertices)
	}
	if chunk.Vertices[0].Kind != VertexLine || chunk.Vertices[0].SizeMM != 1.4 {
		t.Fatalf("configured physical line width not carried by render vertices: %#v", chunk.Vertices[0])
	}
}

func TestPointRenderVerticesCarryPhysicalSymbolSize(t *testing.T) {
	style := core.LayerStyle{LineColor: "#111111", PointColor: "#010203", PolygonColor: "#ffffff", LineWidthMM: 0.5, PointSizeMM: 4.25}
	source, err := NewLayerSource(core.Layer{Name: "places", Style: style, Features: []core.Feature{{
		ID: 1, Geometry: core.WKTGeometry{WKT: "POINT (3 7)"},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	chunk, err := source.Builder(context.Background(), ChunkKey{Layer: "places", X: 2, Y: 2})
	if err != nil || len(chunk.Vertices) != 2 {
		t.Fatalf("point render chunk = %#v, %v", chunk, err)
	}
	for _, vertex := range chunk.Vertices {
		if vertex.Kind != VertexPoint || vertex.SizeMM != 4.25 || vertex.Color != 0x010203ff {
			t.Fatalf("point symbol style not carried by vertex: %+v", vertex)
		}
	}
	if chunk.Vertices[0].X != chunk.Vertices[1].X || chunk.Vertices[0].Y != chunk.Vertices[1].Y {
		t.Fatalf("point symbol should be represented by a coincident pair: %+v", chunk.Vertices)
	}
}

func TestPolygonFillColorMultipliesColorAlphaByFillOpacity(t *testing.T) {
	style := core.DefaultLayerStyle()
	style.PolygonColor = "#12345680"
	style.FillOpacity = 0.5
	if got, want := ColorForPolygonFill(style), uint32(0x12345640); got != want {
		t.Fatalf("polygon fill color = %#08x, want %#08x", got, want)
	}
}

func TestLineLabelUsesHalfLengthAndPolygonLabelUsesExplicitInteriorAnchor(t *testing.T) {
	settings := core.LabelSettings{Enabled: true, Expression: "name", Placement: "free-angle", HeightMM: 2.5}
	line, err := NewLayerSource(core.Layer{Name: "routes", Labels: settings, Features: []core.Feature{{
		ID: 1, Geometry: core.WKTGeometry{WKT: "LINESTRING (0 0, 1 1, 101 101)"},
		Label: &core.Label{Text: "Route"},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(line.Labels) != 1 || math.Abs(line.Labels[0].X-0.5) > 1e-12 || math.Abs(line.Labels[0].Y-0.5) > 1e-12 || line.Labels[0].Rotation != 45 {
		t.Fatalf("line label placement = %+v, want geometric midpoint and 45-degree map tangent", line.Labels)
	}

	polygon, err := NewLayerSource(core.Layer{Name: "areas", Labels: settings, Features: []core.Feature{{
		ID: 2, Geometry: core.WKTGeometry{WKT: "POLYGON ((0 0, 10 0, 10 10, 0 10, 0 0))"},
		Label: &core.Label{Text: "Area", X: 2, Y: 5, AnchorSet: true},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(polygon.Labels) != 1 || math.Abs(polygon.Labels[0].X-0.2) > 1e-12 || math.Abs(polygon.Labels[0].Y-0.5) > 1e-12 {
		t.Fatalf("explicit polygon label anchor = %+v, want normalized interior point (0.2, 0.5)", polygon.Labels)
	}
}

func TestParseWKBStandardXYFastPath(t *testing.T) {
	data, err := hex.DecodeString("0000000002000000023ff0000000000000400000000000000040080000000000004010000000000000")
	if err != nil {
		t.Fatal(err)
	}
	points, simple, arena, err := parseWKBSimple(data, nil)
	if err != nil || !simple || len(arena) != 2 || len(points) != 2 {
		t.Fatalf("parsed WKB = %#v, simple=%v, arena=%d, err=%v", points, simple, len(arena), err)
	}
	if points[0] != (Point{X: 1, Y: 2}) || points[1] != (Point{X: 3, Y: 4}) {
		t.Fatalf("parsed WKB points = %#v", points)
	}
}

func TestParseWKTNumberFastAndFallbackPaths(t *testing.T) {
	tests := []struct {
		token string
		want  float64
	}{
		{token: "127.25", want: 127.25},
		{token: "-0.5", want: -0.5},
		{token: "+42", want: 42},
		{token: ".125", want: 0.125},
		{token: "1.25e2", want: 125},
		{token: "6.5E-1", want: 0.65},
	}
	for _, test := range tests {
		got, err := parseWKTNumber(test.token)
		if err != nil || got != test.want {
			t.Errorf("parseWKTNumber(%q) = %v, %v; want %v", test.token, got, err, test.want)
		}
	}
	for _, token := range []string{"", "+", "-", "1.2.3", "not-a-number"} {
		if _, err := parseWKTNumber(token); err == nil {
			t.Errorf("parseWKTNumber(%q) unexpectedly succeeded", token)
		}
	}
	if value, err := parseWKTNumber("1e400"); !math.IsInf(value, 1) || err == nil {
		t.Fatalf("overflow fallback = %v, %v; want +Inf with range error", value, err)
	}
}

func TestWKTPointsKeepsNumericPrefixValidation(t *testing.T) {
	if _, _, err := wktPointsInto(core.WKTGeometry{WKT: "LINESTRING 1 (2 3)"}, nil); err == nil {
		t.Fatal("numeric prefix was silently ignored")
	}
}

func TestNewLayerSourceNormalizesGeometryAndBuildsVertices(t *testing.T) {
	source, err := NewLayerSource(core.Layer{Name: "roads", Features: []core.Feature{
		{ID: 7, Geometry: core.WKTGeometry{WKT: "LINESTRING (100 200, 200 400)"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(source.Features) != 1 || source.Features[0].Vertices[0] != (Point{X: 0, Y: 0}) || source.Features[0].Vertices[1] != (Point{X: 1, Y: 1}) {
		t.Fatalf("unexpected normalized features: %#v", source.Features)
	}
	chunk, err := source.Builder(context.Background(), ChunkKey{Layer: "roads"})
	if err != nil || len(chunk.Vertices) != 2 {
		t.Fatalf("unexpected chunk: %#v, %v", chunk, err)
	}
	if chunk.Vertices[1].X != 0.25 || chunk.Vertices[1].Y != 0.25 {
		t.Fatalf("chunk was not clipped to its cell: %#v", chunk.Vertices)
	}
	lastChunk, err := source.Builder(context.Background(), ChunkKey{Layer: "roads", X: 3, Y: 3})
	if err != nil || len(lastChunk.Vertices) != 2 || lastChunk.Vertices[0].X != 0.75 || lastChunk.Vertices[0].Y != 0.75 {
		t.Fatalf("last chunk was not clipped: %#v, %v", lastChunk.Vertices, err)
	}
}

func TestNewLayerSourceBuildsReversedAxisAlignedPolygonSegments(t *testing.T) {
	source, err := NewLayerSource(core.Layer{Name: "areas", Features: []core.Feature{{
		ID:       1,
		Geometry: core.WKTGeometry{WKT: "POLYGON ((0 0, 10 0, 10 10, 0 10, 0 0))"},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	totalVertices := 0
	for y := -1; y <= 5; y++ {
		for x := -1; x <= 5; x++ {
			chunk, buildErr := source.Builder(context.Background(), ChunkKey{Layer: "areas", X: x, Y: y})
			if buildErr != nil {
				t.Fatal(buildErr)
			}
			totalVertices += len(chunk.Vertices)
		}
	}
	if totalVertices != 40 {
		t.Fatalf("axis-aligned polygon vertices = %d, want 40", totalVertices)
	}
}

func TestNewLayerSourceSupportsPointsAndCancellation(t *testing.T) {
	source, err := NewLayerSource(core.Layer{Name: "points", Features: []core.Feature{{ID: 1, Geometry: core.WKTGeometry{WKT: "POINT (5 5)"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(source.Features[0].Vertices) != 1 {
		t.Fatalf("unexpected point source: %#v", source.Features)
	}
	if source.Features[0].Parts != nil {
		t.Fatalf("point source retained unnecessary parts wrapper: %#v", source.Features[0].Parts)
	}
	if got := source.Features[0].Vertices[0]; got != (Point{X: 0.5, Y: 0.5}) {
		t.Fatalf("point was not centered in padded extent: %v", got)
	}
	if source.Extent != [4]float64{4.5, 4.5, 5.5, 5.5} {
		t.Fatalf("point extent = %v", source.Extent)
	}
	chunk, err := source.Builder(context.Background(), ChunkKey{X: 2, Y: 2})
	if err != nil || len(chunk.Vertices) != 2 || chunk.Vertices[0].Kind != VertexPoint || chunk.Vertices[1].Kind != VertexPoint {
		t.Fatalf("unexpected point chunk: %#v, %v", chunk, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := source.Builder(ctx, ChunkKey{}); err == nil {
		t.Fatal("expected canceled source build")
	}
}

func TestNewLayerSourcePadsGeographicPointForCoordinateAndScale(t *testing.T) {
	source, err := NewLayerSource(core.Layer{
		Name: "points", CRS: core.CRS{AuthorityCode: "EPSG:4326"},
		Features: []core.Feature{{ID: 1, Geometry: core.WKTGeometry{WKT: "POINT (127 37)"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if source.Extent != [4]float64{126.995, 36.995, 127.005, 37.005} {
		t.Fatalf("geographic point extent = %v", source.Extent)
	}
	if source.Features[0].Vertices[0] != (Point{X: 0.5, Y: 0.5}) {
		t.Fatalf("geographic point position = %v", source.Features[0].Vertices[0])
	}
}

func TestNewLayerSourceClipsPointMarkerAtChunkBoundary(t *testing.T) {
	source, err := NewLayerSource(core.Layer{Name: "points", Features: []core.Feature{
		{ID: 1, Geometry: core.WKTGeometry{WKT: "POINT (0 0)"}},
		{ID: 2, Geometry: core.WKTGeometry{WKT: "POINT (1 0)"}},
		{ID: 3, Geometry: core.WKTGeometry{WKT: "POINT (0.25 0)"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	left, err := source.Builder(context.Background(), ChunkKey{Layer: "points", X: 0, Y: 2})
	if err != nil {
		t.Fatal(err)
	}
	right, err := source.Builder(context.Background(), ChunkKey{Layer: "points", X: 1, Y: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(left.Vertices) == 0 || len(right.Vertices) == 0 {
		t.Fatalf("boundary marker chunks = left %d, right %d", len(left.Vertices), len(right.Vertices))
	}
}

func TestNewLayerSourceKeepsEmptyPartsDistinct(t *testing.T) {
	source, err := NewLayerSource(core.Layer{Name: "empty", Features: []core.Feature{{
		ID: 1, Geometry: core.WKTGeometry{WKT: "POINT EMPTY"},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if source.Features[0].Parts == nil || len(source.Features[0].Parts) != 0 {
		t.Fatalf("empty parts = %#v", source.Features[0].Parts)
	}
}

func TestNewLayerSourceParsesScientificNotation(t *testing.T) {
	source, err := NewLayerSource(core.Layer{Name: "scientific", Features: []core.Feature{{
		ID: 1, Geometry: core.WKTGeometry{WKT: "LINESTRING (1.25e2 -3.5E-1, 1.35e2 6.5e-1)"},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	points := source.Features[0].Parts[0]
	if len(points) != 2 || points[0] != (Point{X: 0, Y: 0}) || points[1] != (Point{X: 1, Y: 1}) {
		t.Fatalf("scientific notation points = %#v", points)
	}
}

func TestNewLayerSourceAcceptsCaseInsensitiveWKTType(t *testing.T) {
	source, err := NewLayerSource(core.Layer{Name: "lowercase", Features: []core.Feature{{
		ID: 1, Geometry: core.WKTGeometry{WKT: "multilinestring ((0 0, 1 1), (2 2, 3 3))"},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(source.Features) != 1 || len(source.Features[0].Parts) != 2 {
		t.Fatalf("lowercase WKT parts = %#v", source.Features)
	}
}

func TestNewLayerSourcesUsesCommonExtent(t *testing.T) {
	sources, err := NewLayerSources([]core.Layer{
		{Name: "near", Features: []core.Feature{{ID: 1, Geometry: core.WKTGeometry{WKT: "LINESTRING (0 0, 10 0)"}}}},
		{Name: "far", Features: []core.Feature{{ID: 2, Geometry: core.WKTGeometry{WKT: "LINESTRING (100 0, 110 0)"}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	near := sources["near"].Features[0].Vertices
	far := sources["far"].Features[0].Vertices
	if near[0].X != 0 || near[1].X >= 0.2 {
		t.Fatalf("near layer was not normalized against common extent: %#v", near)
	}
	if far[0].X <= 0.8 || far[1].X != 1 {
		t.Fatalf("far layer was not normalized against common extent: %#v", far)
	}
}

func TestNewLayerSourcesWithExtentKeepsPreviewCoordinatesStable(t *testing.T) {
	preview := []core.Layer{{Name: "roads", Features: []core.Feature{{ID: 1, Geometry: core.WKTGeometry{WKT: "LINESTRING (0 0, 50 50)"}}}}}
	full := []core.Layer{{Name: "roads", Features: []core.Feature{
		{ID: 1, Geometry: core.WKTGeometry{WKT: "LINESTRING (0 0, 50 50)"}},
		{ID: 2, Geometry: core.WKTGeometry{WKT: "POINT (100 100)"}},
	}}}
	previewSources, previewFeatures, err := NewLayerSourcesWithExtent(preview, [4]float64{0, 0, 100, 100})
	if err != nil {
		t.Fatal(err)
	}
	fullSources, _, err := NewLayerSourcesWithFeatures(full)
	if err != nil {
		t.Fatal(err)
	}
	got := previewSources["roads"].Features[0].Vertices
	want := fullSources["roads"].Features[0].Vertices
	if len(previewFeatures) != 1 || len(got) != len(want) || got[1] != want[1] || got[1] != (Point{X: 0.5, Y: 0.5}) {
		t.Fatalf("preview coordinates = %#v, full = %#v", got, want)
	}
}

func TestNewLayerSourcesWithExtentRejectsInvalidBounds(t *testing.T) {
	for _, bounds := range [][4]float64{{1, 0, 0, 1}, {0, 1, 1, 0}, {math.NaN(), 0, 1, 1}, {0, 0, math.Inf(1), 1}} {
		if _, _, err := NewLayerSourcesWithExtent(nil, bounds); err == nil {
			t.Errorf("accepted invalid bounds %v", bounds)
		}
	}
}

func TestNewLayerSourcesRejectsDuplicateNames(t *testing.T) {
	_, err := NewLayerSources([]core.Layer{{Name: "roads"}, {Name: "roads"}})
	if err == nil {
		t.Fatal("duplicate layer names were accepted")
	}
}

func TestNewLayerSourcesWithFeaturesSharesOrderedImmutableView(t *testing.T) {
	sources, features, err := NewLayerSourcesWithFeatures([]core.Layer{
		{Name: "first", Features: []core.Feature{{ID: 1, Geometry: core.WKTGeometry{WKT: "POINT (0 0)"}}}},
		{Name: "empty"},
		{Name: "last", Features: []core.Feature{{ID: 2, Geometry: core.WKTGeometry{WKT: "POINT (10 10)"}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(features) != 2 || features[0].Layer != "first" || features[1].Layer != "last" {
		t.Fatalf("ordered features = %#v", features)
	}
	if &features[0] != &sources["first"].Features[0] || &features[1] != &sources["last"].Features[0] {
		t.Fatal("source feature views do not share the returned backing array")
	}
	if cap(sources["first"].Features) != 1 || cap(sources["last"].Features) != 1 {
		t.Fatal("source feature views can append into another layer")
	}
	if len(sources["empty"].Features) != 0 {
		t.Fatal("empty layer acquired a feature")
	}
}

func TestNewLayerSourcePreservesMultiGeometryParts(t *testing.T) {
	source, err := NewLayerSource(core.Layer{Name: "roads", Features: []core.Feature{{
		ID:       1,
		Geometry: core.WKTGeometry{WKT: "MULTILINESTRING ((0 0, 10 0), (100 100, 110 100))"},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(source.Features[0].Parts) != 2 || len(source.Features[0].Parts[0]) != 2 || len(source.Features[0].Parts[1]) != 2 {
		t.Fatalf("multi geometry parts = %#v", source.Features[0].Parts)
	}
	if source.Features[0].Parts[0][1].X >= source.Features[0].Parts[1][0].X {
		t.Fatalf("parts unexpectedly connected: %#v", source.Features[0].Parts)
	}
}

func TestNewLayerSourceKeepsEmptyMultiLineGeometry(t *testing.T) {
	source, err := NewLayerSource(core.Layer{Name: "empty", Features: []core.Feature{{
		ID:       1,
		Geometry: core.WKTGeometry{WKT: "MULTILINESTRING EMPTY"},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if source.Features[0].Parts == nil || len(source.Features[0].Parts) != 0 {
		t.Fatalf("empty multi line parts = %#v", source.Features[0].Parts)
	}
}

func TestNewLayerSourcePreservesMultiPointParts(t *testing.T) {
	source, err := NewLayerSource(core.Layer{Name: "points", Features: []core.Feature{{
		ID:       1,
		Geometry: core.WKTGeometry{WKT: "MULTIPOINT ((0 0), (10 10))"},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(source.Features[0].Parts) != 2 || len(source.Features[0].Parts[0]) != 1 || len(source.Features[0].Parts[1]) != 1 {
		t.Fatalf("multi point parts = %#v", source.Features[0].Parts)
	}
}

func TestNewLayerSourcePreservesUnparenthesizedMultiPointParts(t *testing.T) {
	source, err := NewLayerSource(core.Layer{Name: "points", Features: []core.Feature{{
		ID:       1,
		Geometry: core.WKTGeometry{WKT: "MULTIPOINT (0 0, 10 10)"},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(source.Features[0].Parts) != 2 || source.Features[0].Parts[0][0].X != 0 || source.Features[0].Parts[1][0].X != 1 {
		t.Fatalf("unparenthesized multi point parts = %#v", source.Features[0].Parts)
	}
}

func TestNewLayerSourcePreservesMultiPolygonRingParts(t *testing.T) {
	source, err := NewLayerSource(core.Layer{Name: "areas", Features: []core.Feature{{
		ID:       1,
		Geometry: core.WKTGeometry{WKT: "MULTIPOLYGON (((0 0, 10 0, 10 10, 0 0)), ((20 20, 30 20, 30 30, 20 20)))"},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(source.Features[0].Parts) != 2 || len(source.Features[0].Parts[0]) != 4 || len(source.Features[0].Parts[1]) != 4 {
		t.Fatalf("multi polygon parts = %#v", source.Features[0].Parts)
	}
}

func TestNewLayerSourcePreservesSimpleGeometryCollectionParts(t *testing.T) {
	source, err := NewLayerSource(core.Layer{Name: "collections", Features: []core.Feature{{
		ID:       1,
		Geometry: core.WKTGeometry{WKT: "GEOMETRYCOLLECTION (POINT (0 0), LINESTRING (10 10, 20 20))"},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(source.Features[0].Parts) != 2 || len(source.Features[0].Parts[0]) != 1 || len(source.Features[0].Parts[1]) != 2 {
		t.Fatalf("geometry collection parts = %#v", source.Features[0].Parts)
	}
}

func TestNewLayerSourcePreservesPolygonGeometryCollectionParts(t *testing.T) {
	source, err := NewLayerSource(core.Layer{Name: "collections", Features: []core.Feature{{
		ID:       1,
		Geometry: core.WKTGeometry{WKT: "GEOMETRYCOLLECTION (POLYGON ((0 0, 10 0, 10 10, 0 0)), POINT (20 20))"},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(source.Features[0].Parts) != 2 || len(source.Features[0].Parts[0]) != 4 || len(source.Features[0].Parts[1]) != 1 {
		t.Fatalf("polygon geometry collection parts = %#v", source.Features[0].Parts)
	}
}

func TestNewLayerSourceReadsSimpleWKBGeometryCollection(t *testing.T) {
	source, err := NewLayerSource(core.Layer{Name: "wkb-collections", Features: []core.Feature{{
		ID:       1,
		Geometry: core.WKBGeometry{WKB: benchmarkWKBGeometryCollection()},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(source.Features[0].Parts) != 2 || len(source.Features[0].Parts[0]) != 1 || len(source.Features[0].Parts[1]) != 2 {
		t.Fatalf("WKB geometry collection parts = %#v", source.Features[0].Parts)
	}
}

func TestNewLayerSourceReadsPolygonWKBGeometryCollection(t *testing.T) {
	source, err := NewLayerSource(core.Layer{Name: "wkb-collections", Features: []core.Feature{{
		ID:       1,
		Geometry: core.WKBGeometry{WKB: benchmarkWKBGeometryCollectionWithPolygon()},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(source.Features[0].Parts) != 2 || len(source.Features[0].Parts[0]) != 5 || len(source.Features[0].Parts[1]) != 1 {
		t.Fatalf("WKB polygon geometry collection parts = %#v", source.Features[0].Parts)
	}
}

func TestNewLayerSourceReadsPolygonHoleWKBGeometryCollection(t *testing.T) {
	source, err := NewLayerSource(core.Layer{Name: "wkb-collections", Features: []core.Feature{{
		ID:       1,
		Geometry: core.WKBGeometry{WKB: benchmarkWKBGeometryCollectionWithPolygonHole()},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(source.Features[0].Parts) != 3 || len(source.Features[0].Parts[0]) != 5 || len(source.Features[0].Parts[1]) != 5 || len(source.Features[0].Parts[2]) != 1 {
		t.Fatalf("WKB polygon hole geometry collection parts = %#v", source.Features[0].Parts)
	}
}

func TestNewLayerSourceReadsWKBMultiLine(t *testing.T) {
	source, err := NewLayerSource(core.Layer{Name: "wkb-lines", Features: []core.Feature{{
		ID:       1,
		Geometry: core.WKBGeometry{WKB: benchmarkWKBMultiLine()},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(source.Features[0].Parts) != 2 || len(source.Features[0].Parts[0]) != 2 || len(source.Features[0].Parts[1]) != 2 {
		t.Fatalf("WKB multi line parts = %#v", source.Features[0].Parts)
	}
}

func TestNewLayerSourceReadsWKBMultiPoint(t *testing.T) {
	source, err := NewLayerSource(core.Layer{Name: "wkb-points", Features: []core.Feature{{
		ID:       1,
		Geometry: core.WKBGeometry{WKB: benchmarkWKBMultiPoint()},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(source.Features[0].Parts) != 2 || len(source.Features[0].Parts[0]) != 1 || len(source.Features[0].Parts[1]) != 1 {
		t.Fatalf("WKB multi point parts = %#v", source.Features[0].Parts)
	}
}

func TestNewLayerSourceReadsWKBMultiPolygon(t *testing.T) {
	source, err := NewLayerSource(core.Layer{Name: "wkb-polygons", Features: []core.Feature{{
		ID:       1,
		Geometry: core.WKBGeometry{WKB: benchmarkWKBMultiPolygon()},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(source.Features[0].Parts) != 2 || len(source.Features[0].Parts[0]) != 5 || len(source.Features[0].Parts[1]) != 5 {
		t.Fatalf("WKB multi polygon parts = %#v", source.Features[0].Parts)
	}
}

func FuzzWKBFastPathDoesNotPanic(f *testing.F) {
	f.Add(benchmarkWKBMultiPoint())
	f.Add(benchmarkWKBMultiLine())
	f.Add(benchmarkWKBMultiPolygon())
	f.Add(benchmarkWKBGeometryCollectionWithPolygonHole())
	f.Add([]byte{1, 4, 0, 0, 0, 255})
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = NewLayerSource(core.Layer{Name: "fuzz", Features: []core.Feature{{
			ID:       1,
			Geometry: core.WKBGeometry{WKB: data},
		}}})
	})
}

func TestNewLayerSourcePreservesPolygonHoleParts(t *testing.T) {
	source, err := NewLayerSource(core.Layer{Name: "areas", Features: []core.Feature{{
		ID:       1,
		Geometry: core.WKTGeometry{WKT: "POLYGON ((0 0, 10 0, 10 10, 0 0), (2 2, 3 2, 3 3, 2 2))"},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(source.Features[0].Parts) != 2 {
		t.Fatalf("polygon parts = %#v", source.Features[0].Parts)
	}
}

func TestNewLayerSourceReadsStandardWKBPolygonIntoParts(t *testing.T) {
	data := make([]byte, 93)
	data[0] = 1
	binary.LittleEndian.PutUint32(data[1:5], 3)
	binary.LittleEndian.PutUint32(data[5:9], 2)
	binary.LittleEndian.PutUint32(data[9:13], 5)
	for index, point := range [][2]float64{{0, 0}, {10, 0}, {10, 10}, {0, 10}, {0, 0}} {
		offset := 13 + index*16
		binary.LittleEndian.PutUint64(data[offset:offset+8], math.Float64bits(point[0]))
		binary.LittleEndian.PutUint64(data[offset+8:offset+16], math.Float64bits(point[1]))
	}
	data = append(data, make([]byte, 4+5*16)...)
	binary.LittleEndian.PutUint32(data[93:97], 5)
	for index, point := range [][2]float64{{2, 2}, {2, 8}, {8, 8}, {8, 2}, {2, 2}} {
		offset := 97 + index*16
		binary.LittleEndian.PutUint64(data[offset:offset+8], math.Float64bits(point[0]))
		binary.LittleEndian.PutUint64(data[offset+8:offset+16], math.Float64bits(point[1]))
	}
	parsed, _, _, _, _, _, err := parseLayerPoints(core.Layer{Features: []core.Feature{{Geometry: core.WKBGeometry{WKB: data}}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed) != 1 || len(parsed[0].parts) != 2 || len(parsed[0].parts[0]) != 5 || len(parsed[0].parts[1]) != 5 {
		t.Fatalf("parsed polygon parts = %#v", parsed)
	}
	if parsed[0].parts[1][2] != (Point{X: 8, Y: 8}) {
		t.Fatalf("hole point = %#v", parsed[0].parts[1][2])
	}
}

func TestNewLayerSourceReadsSingleRingWKTPolygonFlat(t *testing.T) {
	source, err := NewLayerSource(core.Layer{Name: "areas", Features: []core.Feature{{
		ID: 1, Geometry: core.WKTGeometry{WKT: "POLYGON ((0 0, 10 0, 10 10, 0 10, 0 0))"},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	feature := source.Features[0]
	if feature.Parts != nil || len(feature.Vertices) != 5 {
		t.Fatalf("single-ring polygon representation = %#v", feature)
	}
}
