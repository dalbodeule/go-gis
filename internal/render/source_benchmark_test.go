package render

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"strings"
	"testing"

	"gogis/internal/core"
)

func benchmarkLineLayer(featureCount int) core.Layer {
	features := make([]core.Feature, featureCount)
	for index := range features {
		x := float64(index % 1000)
		y := float64(index / 1000)
		features[index] = core.Feature{
			ID:       uint64(index + 1),
			Geometry: core.WKTGeometry{WKT: fmt.Sprintf("LINESTRING (%g %g, %g %g)", x, y, x+0.75, y+0.5)},
		}
	}
	return core.Layer{Name: "benchmark", Features: features}
}

func BenchmarkLayerSource100KLines(b *testing.B) {
	layer := benchmarkLineLayer(100_000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := NewLayerSource(layer); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkWKTLayerSource10KPolygons(b *testing.B) {
	layer := benchmarkWKTPolygonLayer(10_000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := NewLayerSource(layer); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParseLayerPoints10KPolygons(b *testing.B) {
	layer := benchmarkWKTPolygonLayer(10_000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, _, _, _, _, err := parseLayerPoints(layer); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkWKTLayerSource10KPolygonsGenericBaseline(b *testing.B) {
	layer := benchmarkWKTPolygonLayer(10_000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		parsed := make([]parsedFeaturePoints, len(layer.Features))
		minX, minY := math.Inf(1), math.Inf(1)
		maxX, maxY := math.Inf(-1), math.Inf(-1)
		for index, feature := range layer.Features {
			parts, err := wktParts(feature.Geometry)
			if err != nil {
				b.Fatal(err)
			}
			parsed[index] = parsedFeaturePoints{parts: parts}
			for _, part := range parts {
				updateExtent(part, &minX, &minY, &maxX, &maxY)
			}
		}
		_ = newLayerSource(layer, parsed, nil, minX, minY, maxX, maxY, nil)
	}
}

func BenchmarkWKTGroups100KComponents(b *testing.B) {
	wkt := "MULTILINESTRING (" + strings.TrimSuffix(strings.Repeat("(0 0, 1 1),", 50_000), ",") + ")"
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		groups, err := wktGroups(wkt)
		if err != nil || len(groups) != 50_000 {
			b.Fatalf("groups = %d, err=%v", len(groups), err)
		}
	}
}

func BenchmarkWKTLayerSource10KMultiLines(b *testing.B) {
	features := make([]core.Feature, 10_000)
	for index := range features {
		features[index] = core.Feature{
			ID:       uint64(index + 1),
			Geometry: core.WKTGeometry{WKT: "MULTILINESTRING ((0 0, 1 1), (2 2, 3 3))"},
		}
	}
	layer := core.Layer{Name: "multi-lines", Features: features}
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, err := NewLayerSource(layer); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParseLayerPoints10KMultiLines(b *testing.B) {
	features := make([]core.Feature, 10_000)
	for index := range features {
		features[index] = core.Feature{
			ID:       uint64(index + 1),
			Geometry: core.WKTGeometry{WKT: "MULTILINESTRING ((0 0, 1 1), (2 2, 3 3))"},
		}
	}
	layer := core.Layer{Name: "multi-lines", Features: features}
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, _, _, _, _, _, err := parseLayerPoints(layer); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkWKTLayerSource10KMultiPoints(b *testing.B) {
	features := make([]core.Feature, 10_000)
	for index := range features {
		features[index] = core.Feature{
			ID:       uint64(index + 1),
			Geometry: core.WKTGeometry{WKT: "MULTIPOINT ((0 0), (1 1))"},
		}
	}
	layer := core.Layer{Name: "multi-points", Features: features}
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, err := NewLayerSource(layer); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkWKTLayerSource10KMultiPolygons(b *testing.B) {
	features := make([]core.Feature, 10_000)
	for index := range features {
		features[index] = core.Feature{
			ID:       uint64(index + 1),
			Geometry: core.WKTGeometry{WKT: "MULTIPOLYGON (((0 0, 1 0, 1 1, 0 0)), ((2 2, 3 2, 3 3, 2 2)))"},
		}
	}
	layer := core.Layer{Name: "multi-polygons", Features: features}
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, err := NewLayerSource(layer); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkWKTLayerSource10KGeometryCollections(b *testing.B) {
	features := make([]core.Feature, 10_000)
	for index := range features {
		features[index] = core.Feature{
			ID:       uint64(index + 1),
			Geometry: core.WKTGeometry{WKT: "GEOMETRYCOLLECTION (POINT (0 0), LINESTRING (1 1, 2 2))"},
		}
	}
	layer := core.Layer{Name: "collections", Features: features}
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, err := NewLayerSource(layer); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkWKTLayerSource10KGeometryCollectionsWithPolygon(b *testing.B) {
	features := make([]core.Feature, 10_000)
	for index := range features {
		features[index] = core.Feature{
			ID:       uint64(index + 1),
			Geometry: core.WKTGeometry{WKT: "GEOMETRYCOLLECTION (POLYGON ((0 0, 10 0, 10 10, 0 0)), POINT (20 20))"},
		}
	}
	layer := core.Layer{Name: "collection-polygons", Features: features}
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, err := NewLayerSource(layer); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkWKBLayerSource10KGeometryCollections(b *testing.B) {
	data := benchmarkWKBGeometryCollection()
	features := make([]core.Feature, 10_000)
	for index := range features {
		features[index] = core.Feature{ID: uint64(index + 1), Geometry: core.WKBGeometry{WKB: data}}
	}
	layer := core.Layer{Name: "wkb-collections", Features: features}
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, err := NewLayerSource(layer); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkWKBLayerSource10KGeometryCollectionsWithPolygon(b *testing.B) {
	data := benchmarkWKBGeometryCollectionWithPolygon()
	features := make([]core.Feature, 10_000)
	for index := range features {
		features[index] = core.Feature{ID: uint64(index + 1), Geometry: core.WKBGeometry{WKB: data}}
	}
	layer := core.Layer{Name: "wkb-collections-polygon", Features: features}
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, err := NewLayerSource(layer); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkWKBLayerSource10KGeometryCollectionsWithPolygonHole(b *testing.B) {
	data := benchmarkWKBGeometryCollectionWithPolygonHole()
	features := make([]core.Feature, 10_000)
	for index := range features {
		features[index] = core.Feature{ID: uint64(index + 1), Geometry: core.WKBGeometry{WKB: data}}
	}
	layer := core.Layer{Name: "wkb-collections-polygon-hole", Features: features}
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, err := NewLayerSource(layer); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkWKBLayerSource10KMultiLines(b *testing.B) {
	data := benchmarkWKBMultiLine()
	features := make([]core.Feature, 10_000)
	for index := range features {
		features[index] = core.Feature{ID: uint64(index + 1), Geometry: core.WKBGeometry{WKB: data}}
	}
	layer := core.Layer{Name: "wkb-multilines", Features: features}
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, err := NewLayerSource(layer); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkWKBLayerSource10KMultiPoints(b *testing.B) {
	data := benchmarkWKBMultiPoint()
	features := make([]core.Feature, 10_000)
	for index := range features {
		features[index] = core.Feature{ID: uint64(index + 1), Geometry: core.WKBGeometry{WKB: data}}
	}
	layer := core.Layer{Name: "wkb-multipoints", Features: features}
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, err := NewLayerSource(layer); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkWKBLayerSource10KMultiPolygons(b *testing.B) {
	data := benchmarkWKBMultiPolygon()
	features := make([]core.Feature, 10_000)
	for index := range features {
		features[index] = core.Feature{ID: uint64(index + 1), Geometry: core.WKBGeometry{WKB: data}}
	}
	layer := core.Layer{Name: "wkb-multipolygons", Features: features}
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, err := NewLayerSource(layer); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkWKBLayerSource100KLines(b *testing.B) {
	layer := benchmarkWKBLineLayer(100_000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := NewLayerSource(layer); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkWKBLayerSource10KPolygons(b *testing.B) {
	layer := benchmarkWKBPolygonLayer(10_000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := NewLayerSource(layer); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkWKBLayerSource10KPolygonsGenericBaseline(b *testing.B) {
	layer := benchmarkWKBPolygonLayer(10_000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		parsed := make([]parsedFeaturePoints, len(layer.Features))
		minX, minY := math.Inf(1), math.Inf(1)
		maxX, maxY := math.Inf(-1), math.Inf(-1)
		for index, feature := range layer.Features {
			parts, err := parseWKBParts(feature.Geometry.(core.WKBGeometry).WKB)
			if err != nil {
				b.Fatal(err)
			}
			parsed[index] = parsedFeaturePoints{parts: parts}
			for _, part := range parts {
				updateExtent(part, &minX, &minY, &maxX, &maxY)
			}
		}
		_ = newLayerSource(layer, parsed, nil, minX, minY, maxX, maxY, nil)
	}
}

func BenchmarkLayerSource100KPoints(b *testing.B) {
	layer := benchmarkPointLayer(100_000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := NewLayerSource(layer); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParseLayerPoints100KLines(b *testing.B) {
	layer := benchmarkLineLayer(100_000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, _, _, _, _, err := parseLayerPoints(layer); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParseLayerPoints100KPoints(b *testing.B) {
	layer := benchmarkPointLayer(100_000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, _, _, _, _, err := parseLayerPoints(layer); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkNormalizeLayerSource100KLines(b *testing.B) {
	layer := benchmarkLineLayer(100_000)
	parsed, lineFlags, minX, minY, maxX, maxY, err := parseLayerPoints(layer)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = newLayerSource(layer, parsed, lineFlags, minX, minY, maxX, maxY, nil)
		b.StopTimer()
		benchmarkRestoreParsedCoordinates(parsed, minX, minY, maxX, maxY)
		b.StartTimer()
	}
}

func BenchmarkNormalizeLayerSource100KPoints(b *testing.B) {
	layer := benchmarkPointLayer(100_000)
	parsed, lineFlags, minX, minY, maxX, maxY, err := parseLayerPoints(layer)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = newLayerSource(layer, parsed, lineFlags, minX, minY, maxX, maxY, nil)
		b.StopTimer()
		benchmarkRestoreParsedCoordinates(parsed, minX, minY, maxX, maxY)
		b.StartTimer()
	}
}

func benchmarkRestoreParsedCoordinates(parsed []parsedFeaturePoints, minX, minY, maxX, maxY float64) {
	spanX, spanY := maxX-minX, maxY-minY
	if spanX == 0 {
		spanX = 1
	}
	if spanY == 0 {
		spanY = 1
	}
	restore := func(points []Point) {
		for index := range points {
			points[index].X = points[index].X*spanX + minX
			points[index].Y = points[index].Y*spanY + minY
		}
	}
	for index := range parsed {
		if parsed[index].parts == nil {
			restore(parsed[index].points)
			continue
		}
		for _, part := range parsed[index].parts {
			restore(part)
		}
	}
}

func benchmarkWKBLineLayer(featureCount int) core.Layer {
	features := make([]core.Feature, featureCount)
	for index := range features {
		x := float64(index % 1000)
		y := float64(index / 1000)
		wkb := make([]byte, 41)
		wkb[0] = 1
		binary.LittleEndian.PutUint32(wkb[1:5], 2)
		binary.LittleEndian.PutUint32(wkb[5:9], 2)
		binary.LittleEndian.PutUint64(wkb[9:17], math.Float64bits(x))
		binary.LittleEndian.PutUint64(wkb[17:25], math.Float64bits(y))
		binary.LittleEndian.PutUint64(wkb[25:33], math.Float64bits(x+0.75))
		binary.LittleEndian.PutUint64(wkb[33:41], math.Float64bits(y+0.5))
		features[index] = core.Feature{ID: uint64(index + 1), Geometry: core.WKBGeometry{WKB: wkb}}
	}
	return core.Layer{Name: "benchmark-wkb", Features: features}
}

func benchmarkWKBPolygonLayer(featureCount int) core.Layer {
	data := make([]byte, 93)
	data[0] = 1
	binary.LittleEndian.PutUint32(data[1:5], 3)
	binary.LittleEndian.PutUint32(data[5:9], 1)
	binary.LittleEndian.PutUint32(data[9:13], 5)
	for index, point := range [][2]float64{{0, 0}, {10, 0}, {10, 10}, {0, 10}, {0, 0}} {
		offset := 13 + index*16
		binary.LittleEndian.PutUint64(data[offset:offset+8], math.Float64bits(point[0]))
		binary.LittleEndian.PutUint64(data[offset+8:offset+16], math.Float64bits(point[1]))
	}
	features := make([]core.Feature, featureCount)
	for index := range features {
		features[index] = core.Feature{ID: uint64(index + 1), Geometry: core.WKBGeometry{WKB: data}}
	}
	return core.Layer{Name: "benchmark-wkb-polygons", Features: features}
}

func benchmarkWKBGeometryCollection() []byte {
	data := make([]byte, 71)
	data[0] = 1
	binary.LittleEndian.PutUint32(data[1:5], 7)
	binary.LittleEndian.PutUint32(data[5:9], 2)
	data[9] = 1
	binary.LittleEndian.PutUint32(data[10:14], 1)
	binary.LittleEndian.PutUint64(data[14:22], math.Float64bits(0))
	binary.LittleEndian.PutUint64(data[22:30], math.Float64bits(0))
	offset := 30
	data[offset] = 1
	binary.LittleEndian.PutUint32(data[offset+1:offset+5], 2)
	binary.LittleEndian.PutUint32(data[offset+5:offset+9], 2)
	for index, point := range [][2]float64{{1, 1}, {2, 2}} {
		pointOffset := offset + 9 + index*16
		binary.LittleEndian.PutUint64(data[pointOffset:pointOffset+8], math.Float64bits(point[0]))
		binary.LittleEndian.PutUint64(data[pointOffset+8:pointOffset+16], math.Float64bits(point[1]))
	}
	return data
}

func benchmarkWKBGeometryCollectionWithPolygon() []byte {
	data := make([]byte, 123)
	data[0] = 1
	binary.LittleEndian.PutUint32(data[1:5], 7)
	binary.LittleEndian.PutUint32(data[5:9], 2)
	polygonOffset := 9
	data[polygonOffset] = 1
	binary.LittleEndian.PutUint32(data[polygonOffset+1:polygonOffset+5], 3)
	binary.LittleEndian.PutUint32(data[polygonOffset+5:polygonOffset+9], 1)
	binary.LittleEndian.PutUint32(data[polygonOffset+9:polygonOffset+13], 5)
	for index, point := range [][2]float64{{0, 0}, {10, 0}, {10, 10}, {0, 10}, {0, 0}} {
		pointOffset := polygonOffset + 13 + index*16
		binary.LittleEndian.PutUint64(data[pointOffset:pointOffset+8], math.Float64bits(point[0]))
		binary.LittleEndian.PutUint64(data[pointOffset+8:pointOffset+16], math.Float64bits(point[1]))
	}
	pointOffset := polygonOffset + 93
	data[pointOffset] = 1
	binary.LittleEndian.PutUint32(data[pointOffset+1:pointOffset+5], 1)
	binary.LittleEndian.PutUint64(data[pointOffset+5:pointOffset+13], math.Float64bits(20))
	binary.LittleEndian.PutUint64(data[pointOffset+13:pointOffset+21], math.Float64bits(20))
	return data
}

func benchmarkWKBGeometryCollectionWithPolygonHole() []byte {
	data := make([]byte, 207)
	data[0] = 1
	binary.LittleEndian.PutUint32(data[1:5], 7)
	binary.LittleEndian.PutUint32(data[5:9], 2)
	polygonOffset := 9
	data[polygonOffset] = 1
	binary.LittleEndian.PutUint32(data[polygonOffset+1:polygonOffset+5], 3)
	binary.LittleEndian.PutUint32(data[polygonOffset+5:polygonOffset+9], 2)
	rings := [][5][2]float64{
		{{0, 0}, {10, 0}, {10, 10}, {0, 10}, {0, 0}},
		{{2, 2}, {2, 8}, {8, 8}, {8, 2}, {2, 2}},
	}
	offset := polygonOffset + 9
	for _, ring := range rings {
		binary.LittleEndian.PutUint32(data[offset:offset+4], 5)
		offset += 4
		for _, point := range ring {
			binary.LittleEndian.PutUint64(data[offset:offset+8], math.Float64bits(point[0]))
			binary.LittleEndian.PutUint64(data[offset+8:offset+16], math.Float64bits(point[1]))
			offset += 16
		}
	}
	data[offset] = 1
	binary.LittleEndian.PutUint32(data[offset+1:offset+5], 1)
	binary.LittleEndian.PutUint64(data[offset+5:offset+13], math.Float64bits(20))
	binary.LittleEndian.PutUint64(data[offset+13:offset+21], math.Float64bits(20))
	return data
}

func benchmarkWKBMultiLine() []byte {
	data := make([]byte, 91)
	data[0] = 1
	binary.LittleEndian.PutUint32(data[1:5], 5)
	binary.LittleEndian.PutUint32(data[5:9], 2)
	for line, points := range [][2][2]float64{
		{{0, 0}, {10, 10}},
		{{20, 20}, {30, 30}},
	} {
		offset := 9 + line*41
		data[offset] = 1
		binary.LittleEndian.PutUint32(data[offset+1:offset+5], 2)
		binary.LittleEndian.PutUint32(data[offset+5:offset+9], 2)
		for index, point := range points {
			pointOffset := offset + 9 + index*16
			binary.LittleEndian.PutUint64(data[pointOffset:pointOffset+8], math.Float64bits(point[0]))
			binary.LittleEndian.PutUint64(data[pointOffset+8:pointOffset+16], math.Float64bits(point[1]))
		}
	}
	return data
}

func benchmarkWKBMultiPoint() []byte {
	data := make([]byte, 51)
	data[0] = 1
	binary.LittleEndian.PutUint32(data[1:5], 4)
	binary.LittleEndian.PutUint32(data[5:9], 2)
	for index, point := range [][2]float64{{1, 1}, {2, 2}} {
		offset := 9 + index*21
		data[offset] = 1
		binary.LittleEndian.PutUint32(data[offset+1:offset+5], 1)
		binary.LittleEndian.PutUint64(data[offset+5:offset+13], math.Float64bits(point[0]))
		binary.LittleEndian.PutUint64(data[offset+13:offset+21], math.Float64bits(point[1]))
	}
	return data
}

func benchmarkWKBMultiPolygon() []byte {
	data := make([]byte, 195)
	data[0] = 1
	binary.LittleEndian.PutUint32(data[1:5], 6)
	binary.LittleEndian.PutUint32(data[5:9], 2)
	for polygon, points := range [][5][2]float64{
		{{0, 0}, {10, 0}, {10, 10}, {0, 10}, {0, 0}},
		{{20, 20}, {30, 20}, {30, 30}, {20, 30}, {20, 20}},
	} {
		offset := 9 + polygon*93
		data[offset] = 1
		binary.LittleEndian.PutUint32(data[offset+1:offset+5], 3)
		binary.LittleEndian.PutUint32(data[offset+5:offset+9], 1)
		binary.LittleEndian.PutUint32(data[offset+9:offset+13], 5)
		for index, point := range points {
			pointOffset := offset + 13 + index*16
			binary.LittleEndian.PutUint64(data[pointOffset:pointOffset+8], math.Float64bits(point[0]))
			binary.LittleEndian.PutUint64(data[pointOffset+8:pointOffset+16], math.Float64bits(point[1]))
		}
	}
	return data
}

func benchmarkWKTPolygonLayer(featureCount int) core.Layer {
	features := make([]core.Feature, featureCount)
	for index := range features {
		features[index] = core.Feature{
			ID:       uint64(index + 1),
			Geometry: core.WKTGeometry{WKT: "POLYGON ((0 0, 10 0, 10 10, 0 10, 0 0))"},
		}
	}
	return core.Layer{Name: "benchmark-wkt-polygons", Features: features}
}

func benchmarkPointLayer(featureCount int) core.Layer {
	features := make([]core.Feature, featureCount)
	for index := range features {
		x := float64(index % 1000)
		y := float64(index / 1000)
		features[index] = core.Feature{
			ID:       uint64(index + 1),
			Geometry: core.WKTGeometry{WKT: fmt.Sprintf("POINT (%g %g)", x, y)},
		}
	}
	return core.Layer{Name: "benchmark-points", Features: features}
}

func BenchmarkLayerSourceScale(b *testing.B) {
	for _, featureCount := range []int{10_000, 100_000, 1_000_000} {
		featureCount := featureCount
		b.Run(fmt.Sprintf("%d_lines", featureCount), func(b *testing.B) {
			layer := benchmarkLineLayer(featureCount)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := NewLayerSource(layer); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkLayerSource100KChunkBuild(b *testing.B) {
	source, err := NewLayerSource(benchmarkLineLayer(100_000))
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	key := ChunkKey{Layer: "benchmark", ZoomBucket: 0}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := source.Builder(ctx, key); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkHitIndex100KLines(b *testing.B) {
	source, err := NewLayerSource(benchmarkWKBLineLayer(100_000))
	if err != nil {
		b.Fatal(err)
	}
	initial := NewHitIndex(source.Features, 0.01)
	b.ReportAllocs()
	b.ResetTimer()
	b.ReportMetric(float64(len(initial.cells)), "cells/op")
	for i := 0; i < b.N; i++ {
		_ = NewHitIndex(source.Features, 0.01)
	}
}

func BenchmarkHitIndex100LongDiagonalLines(b *testing.B) {
	features := make([]HitFeature, 100)
	for index := range features {
		features[index] = HitFeature{
			Layer:     "diagonal",
			FeatureID: uint64(index + 1),
			Vertices:  []Point{{X: 0, Y: 0}, {X: 1, Y: 1}},
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		_ = NewHitIndex(features, 0.01)
	}
}

func BenchmarkHitIndex100LongHorizontalLines(b *testing.B) {
	features := make([]HitFeature, 100)
	for index := range features {
		features[index] = HitFeature{
			Layer:     "horizontal",
			FeatureID: uint64(index + 1),
			Vertices:  []Point{{X: 0, Y: float64(index)}, {X: 100, Y: float64(index)}},
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		_ = NewHitIndex(features, 0.01)
	}
}

func BenchmarkMultiLayerChunkBuild(b *testing.B) {
	layers := make([]core.Layer, 4)
	for index := range layers {
		layers[index] = benchmarkLineLayer(25_000)
		layers[index].Name = fmt.Sprintf("layer-%d", index)
	}
	sources, err := NewLayerSources(layers)
	if err != nil {
		b.Fatal(err)
	}
	keys := make([]ChunkKey, 0, len(sources))
	for name := range sources {
		keys = append(keys, ChunkKey{Layer: name, X: 1, Y: 1})
	}
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, key := range keys {
			if _, err := sources[key.Layer].Builder(ctx, key); err != nil {
				b.Fatal(err)
			}
		}
	}
}

func BenchmarkHitTestLinear100K(b *testing.B) {
	source, err := NewLayerSource(benchmarkLineLayer(100_000))
	if err != nil {
		b.Fatal(err)
	}
	point := Point{X: 0.5, Y: 0.5}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = HitTest(source.Features, point, 0.01)
	}
}

func BenchmarkHitTestIndexed100K(b *testing.B) {
	source, err := NewLayerSource(benchmarkLineLayer(100_000))
	if err != nil {
		b.Fatal(err)
	}
	index := NewHitIndex(source.Features, 0.01)
	point := Point{X: 0.5, Y: 0.5}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = index.HitTest(point, 0.01)
	}
}

func BenchmarkHitTestIndexedDense100K(b *testing.B) {
	source, err := NewLayerSource(benchmarkLineLayer(100_000))
	if err != nil {
		b.Fatal(err)
	}
	index := NewHitIndex(source.Features, 0.01)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = index.HitTest(Point{X: 0.5, Y: 0.5}, 0.5)
	}
}

func BenchmarkHitTestIndexedVisible100K(b *testing.B) {
	source, err := NewLayerSource(benchmarkLineLayer(100_000))
	if err != nil {
		b.Fatal(err)
	}
	index := NewHitIndex(source.Features, 0.01)
	point := Point{X: 0.5, Y: 0.5}
	visible := map[string]bool{"benchmark": true}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = index.HitTestVisible(point, 0.01, visible)
	}
}

func BenchmarkHitTestIndexedVisibleFunc100K(b *testing.B) {
	source, err := NewLayerSource(benchmarkLineLayer(100_000))
	if err != nil {
		b.Fatal(err)
	}
	index := NewHitIndex(source.Features, 0.01)
	point := Point{X: 0.5, Y: 0.5}
	visible := func(layer string) bool { return layer == "benchmark" }
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = index.HitTestVisibleFunc(point, 0.01, visible)
	}
}

func BenchmarkHitTestIndexedVisibleState100K(b *testing.B) {
	source, err := NewLayerSource(benchmarkLineLayer(100_000))
	if err != nil {
		b.Fatal(err)
	}
	index := NewHitIndex(source.Features, 0.01)
	visibility := NewLayerVisibility("benchmark")
	point := Point{X: 0.5, Y: 0.5}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = index.HitTestVisibleFunc(point, 0.01, visibility.IsVisible)
	}
}
