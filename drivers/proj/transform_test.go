//go:build native

package proj

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"testing"

	projlib "github.com/twpayne/go-proj/v11"
	"gogis/internal/core"
)

func TestTransformerTransformsBoundsWithVisualizationAxisOrder(t *testing.T) {
	transformer := Transformer{}
	got, err := transformer.TransformBounds(context.Background(), core.CRS{AuthorityCode: "EPSG:4326"}, core.CRS{AuthorityCode: "EPSG:3857"}, [4]float64{-1, -1, 1, 1})
	if err != nil {
		t.Fatal(err)
	}
	if got[0] > -111_000 || got[1] > -111_000 || got[2] < 111_000 || got[3] < 111_000 {
		t.Fatalf("forward bounds = %v", got)
	}
	back, err := transformer.TransformBounds(context.Background(), core.CRS{AuthorityCode: "EPSG:3857"}, core.CRS{AuthorityCode: "EPSG:4326"}, got)
	if err != nil {
		t.Fatal(err)
	}
	if back[0] > -0.99 || back[1] > -0.99 || back[2] < 0.99 || back[3] < 0.99 {
		t.Fatalf("inverse bounds = %v", back)
	}
}

var numberPattern = regexp.MustCompile(`[-+]?(?:\d+(?:\.\d*)?|\.\d+)(?:[eE][-+]?\d+)?`)

func TestTransformerUsesPROJForPoint(t *testing.T) {
	layer := core.Layer{Features: []core.Feature{{ID: 1, Geometry: core.WKTGeometry{WKT: "POINT (127 37)"}}}}
	result, err := (Transformer{}).Transform(context.Background(), core.CRS{AuthorityCode: "EPSG:4326"}, core.CRS{AuthorityCode: "EPSG:3857"}, layer)
	if err != nil {
		t.Fatal(err)
	}
	geometry := result.Features[0].Geometry.(core.WKTGeometry)
	if !strings.HasPrefix(geometry.WKT, "POINT") || strings.Contains(geometry.WKT, "127 37") {
		t.Fatalf("unexpected transformed WKT: %s", geometry.WKT)
	}
}

func TestTransformerUsesDirectWKBPath(t *testing.T) {
	data, err := hex.DecodeString("0101000000000000000000f03f0000000000000040")
	if err != nil {
		t.Fatal(err)
	}
	result, err := (Transformer{}).Transform(context.Background(), core.CRS{AuthorityCode: "EPSG:4326"}, core.CRS{AuthorityCode: "EPSG:3857"}, core.Layer{
		Features: []core.Feature{{ID: 1, Geometry: core.WKBGeometry{WKB: data}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	geometry, ok := result.Features[0].Geometry.(core.WKBGeometry)
	if !ok {
		t.Fatalf("geometry type = %T, want core.WKBGeometry", result.Features[0].Geometry)
	}
	parts, err := geometry.Parts()
	if err != nil {
		t.Fatal(err)
	}
	assertNear(t, parts[0][0].X, 111319.490793, 0.02)
	assertNear(t, parts[0][0].Y, 222684.208506, 0.02)
}

func TestTransformerBatchesWKBPointsWithoutMutatingInput(t *testing.T) {
	first, err := hex.DecodeString("0101000000000000000000f03f0000000000000040")
	if err != nil {
		t.Fatal(err)
	}
	second, err := hex.DecodeString("010100000000000000000000400000000000000840")
	if err != nil {
		t.Fatal(err)
	}
	input := core.Layer{Features: []core.Feature{
		{ID: 1, Geometry: core.WKBGeometry{WKB: first}},
		{ID: 2, Geometry: core.WKBGeometry{WKB: second}},
	}}
	result, err := (Transformer{}).Transform(context.Background(), core.CRS{AuthorityCode: "EPSG:4326"}, core.CRS{AuthorityCode: "EPSG:3857"}, input)
	if err != nil {
		t.Fatal(err)
	}
	if string(input.Features[0].Geometry.(core.WKBGeometry).WKB) != string(first) ||
		string(input.Features[1].Geometry.(core.WKBGeometry).WKB) != string(second) {
		t.Fatal("input WKB was mutated")
	}
	for index, feature := range result.Features {
		parts, err := feature.Geometry.(core.WKBGeometry).Parts()
		if err != nil {
			t.Fatal(err)
		}
		if len(parts) != 1 || len(parts[0]) != 1 {
			t.Fatalf("feature %d parts = %#v", index, parts)
		}
		if parts[0][0].X == 1 || parts[0][0].Y == 2 {
			t.Fatalf("feature %d was not transformed: %#v", index, parts[0][0])
		}
	}
}

func TestTransformerBatchesWKBLinesWithoutMutatingInput(t *testing.T) {
	data := make([]byte, 41)
	data[0] = 1
	binary.LittleEndian.PutUint32(data[1:5], 2)
	binary.LittleEndian.PutUint32(data[5:9], 2)
	binary.LittleEndian.PutUint64(data[9:17], math.Float64bits(127))
	binary.LittleEndian.PutUint64(data[17:25], math.Float64bits(37))
	binary.LittleEndian.PutUint64(data[25:33], math.Float64bits(128))
	binary.LittleEndian.PutUint64(data[33:41], math.Float64bits(38))
	original := append([]byte(nil), data...)
	result, err := (Transformer{}).Transform(context.Background(), core.CRS{AuthorityCode: "EPSG:4326"}, core.CRS{AuthorityCode: "EPSG:3857"}, core.Layer{
		Features: []core.Feature{{ID: 1, Geometry: core.WKBGeometry{WKB: data}}, {ID: 2, Geometry: core.WKBGeometry{WKB: data}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(original) {
		t.Fatal("input WKB line was mutated")
	}
	for index, feature := range result.Features {
		parts, err := feature.Geometry.(core.WKBGeometry).Parts()
		if err != nil || len(parts) != 1 || len(parts[0]) != 2 {
			t.Fatalf("feature %d parts = %#v, err=%v", index, parts, err)
		}
		if parts[0][0].X == 127 || parts[0][0].Y == 37 {
			t.Fatalf("feature %d was not transformed: %#v", index, parts[0][0])
		}
	}
}

func TestTransformerBatchesWKBSimplePolygonsWithoutMutatingInput(t *testing.T) {
	layer := benchmarkWKBPolygonLayer10K()
	input := core.Layer{Features: layer.Features[:2]}
	original := append([]byte(nil), input.Features[0].Geometry.(core.WKBGeometry).WKB...)
	result, err := (Transformer{}).Transform(context.Background(), core.CRS{AuthorityCode: "EPSG:4326"}, core.CRS{AuthorityCode: "EPSG:3857"}, input)
	if err != nil {
		t.Fatal(err)
	}
	if string(input.Features[0].Geometry.(core.WKBGeometry).WKB) != string(original) {
		t.Fatal("input WKB polygon was mutated")
	}
	for index, feature := range result.Features {
		parts, err := feature.Geometry.(core.WKBGeometry).Parts()
		if err != nil || len(parts) != 1 || len(parts[0]) != 5 {
			t.Fatalf("feature %d parts = %#v, err=%v", index, parts, err)
		}
		if parts[0][0].X == 127 || parts[0][0].Y == 37 {
			t.Fatalf("feature %d was not transformed: %#v", index, parts[0][0])
		}
	}
}

func TestTransformerBatchesWKBPreservesNaNCoordinates(t *testing.T) {
	data := make([]byte, 41)
	data[0] = 1
	binary.LittleEndian.PutUint32(data[1:5], 2)
	binary.LittleEndian.PutUint32(data[5:9], 2)
	binary.LittleEndian.PutUint64(data[9:17], math.Float64bits(math.NaN()))
	binary.LittleEndian.PutUint64(data[17:25], math.Float64bits(math.NaN()))
	binary.LittleEndian.PutUint64(data[25:33], math.Float64bits(128))
	binary.LittleEndian.PutUint64(data[33:41], math.Float64bits(38))
	result, err := (Transformer{}).Transform(context.Background(), core.CRS{AuthorityCode: "EPSG:4326"}, core.CRS{AuthorityCode: "EPSG:3857"}, core.Layer{
		Features: []core.Feature{{ID: 1, Geometry: core.WKBGeometry{WKB: data}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	parts, err := result.Features[0].Geometry.(core.WKBGeometry).Parts()
	if err != nil || len(parts) != 1 || len(parts[0]) != 2 {
		t.Fatalf("parts = %#v, err=%v", parts, err)
	}
	if !math.IsNaN(parts[0][0].X) || !math.IsNaN(parts[0][0].Y) {
		t.Fatalf("NaN coordinate changed: %#v", parts[0][0])
	}
	if parts[0][1].X == 128 || parts[0][1].Y == 38 {
		t.Fatalf("valid coordinate was not transformed: %#v", parts[0][1])
	}
}

func TestTransformerBatchesWKBMultiLinesWithoutMutatingInput(t *testing.T) {
	layer := benchmarkWKBMultiLineLayer10K()
	input := core.Layer{Features: layer.Features[:1]}
	original := append([]byte(nil), input.Features[0].Geometry.(core.WKBGeometry).WKB...)
	result, err := (Transformer{}).Transform(context.Background(), core.CRS{AuthorityCode: "EPSG:4326"}, core.CRS{AuthorityCode: "EPSG:3857"}, input)
	if err != nil {
		t.Fatal(err)
	}
	if string(input.Features[0].Geometry.(core.WKBGeometry).WKB) != string(original) {
		t.Fatal("input WKB multi line was mutated")
	}
	parts, err := result.Features[0].Geometry.(core.WKBGeometry).Parts()
	if err != nil || len(parts) != 2 || len(parts[0]) != 2 || len(parts[1]) != 2 {
		t.Fatalf("parts = %#v, err=%v", parts, err)
	}
	if parts[0][0].X == 127 || parts[0][0].Y == 37 || parts[1][1].X == 130 || parts[1][1].Y == 40 {
		t.Fatalf("multi line coordinates were not transformed: %#v", parts)
	}
}

func TestTransformerBatchesWKBMultiPolygonsWithoutMutatingInput(t *testing.T) {
	layer := benchmarkWKBMultiPolygonLayer10K()
	input := core.Layer{Features: layer.Features[:1]}
	original := append([]byte(nil), input.Features[0].Geometry.(core.WKBGeometry).WKB...)
	result, err := (Transformer{}).Transform(context.Background(), core.CRS{AuthorityCode: "EPSG:4326"}, core.CRS{AuthorityCode: "EPSG:3857"}, input)
	if err != nil {
		t.Fatal(err)
	}
	if string(input.Features[0].Geometry.(core.WKBGeometry).WKB) != string(original) {
		t.Fatal("input WKB multi polygon was mutated")
	}
	parts, err := result.Features[0].Geometry.(core.WKBGeometry).Parts()
	if err != nil || len(parts) != 2 || len(parts[0]) != 5 || len(parts[1]) != 5 {
		t.Fatalf("parts = %#v, err=%v", parts, err)
	}
	if parts[0][0].X == 127 || parts[0][0].Y == 37 || parts[1][1].X == 130 || parts[1][1].Y == 39 {
		t.Fatalf("multi polygon coordinates were not transformed: %#v", parts)
	}
}

func TestTransformerBatchesWKBMultiPointsWithoutMutatingInput(t *testing.T) {
	layer := benchmarkWKBMultiPointLayer10K()
	input := core.Layer{Features: layer.Features[:1]}
	original := append([]byte(nil), input.Features[0].Geometry.(core.WKBGeometry).WKB...)
	result, err := (Transformer{}).Transform(context.Background(), core.CRS{AuthorityCode: "EPSG:4326"}, core.CRS{AuthorityCode: "EPSG:3857"}, input)
	if err != nil {
		t.Fatal(err)
	}
	if string(input.Features[0].Geometry.(core.WKBGeometry).WKB) != string(original) {
		t.Fatal("input WKB multi point was mutated")
	}
	parts, err := result.Features[0].Geometry.(core.WKBGeometry).Parts()
	if err != nil || len(parts) != 2 || len(parts[0]) != 1 || len(parts[1]) != 1 {
		t.Fatalf("parts = %#v, err=%v", parts, err)
	}
	if parts[0][0].X == 127 || parts[0][0].Y == 37 || parts[1][0].X == 128 || parts[1][0].Y == 38 {
		t.Fatalf("multi point coordinates were not transformed: %#v", parts)
	}
}

func TestTransformerBatchesWKTPointsWithoutMutatingInput(t *testing.T) {
	input := core.Layer{Features: []core.Feature{
		{ID: 1, Geometry: core.WKTGeometry{WKT: "POINT (127 37)"}},
		{ID: 2, Geometry: core.WKTGeometry{WKT: "POINT (128 38)"}},
	}}
	result, err := (Transformer{}).Transform(context.Background(), core.CRS{AuthorityCode: "EPSG:4326"}, core.CRS{AuthorityCode: "EPSG:3857"}, input)
	if err != nil {
		t.Fatal(err)
	}
	if input.Features[0].Geometry.(core.WKTGeometry).WKT != "POINT (127 37)" {
		t.Fatal("input WKT was mutated")
	}
	for index, feature := range result.Features {
		geometry, ok := feature.Geometry.(core.WKTGeometry)
		if !ok || !strings.HasPrefix(geometry.WKT, "POINT (") || strings.Contains(geometry.WKT, "127 37") || strings.Contains(geometry.WKT, "128 38") {
			t.Fatalf("feature %d was not transformed: %#v", index, feature.Geometry)
		}
	}
}

func TestTransformerTransformsLayersWithSharedPipeline(t *testing.T) {
	layers := []core.Layer{
		{Name: "roads", Features: []core.Feature{{ID: 1, Geometry: core.WKTGeometry{WKT: "POINT (127 37)"}}}},
		{Name: "buildings", Features: []core.Feature{{ID: 2, Geometry: core.WKTGeometry{WKT: "POINT (128 38)"}}}},
	}
	result, err := (Transformer{}).TransformLayers(context.Background(), core.CRS{AuthorityCode: "EPSG:4326"}, core.CRS{AuthorityCode: "EPSG:3857"}, layers)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 2 || result[0].CRS.AuthorityCode != "EPSG:3857" || result[1].CRS.AuthorityCode != "EPSG:3857" {
		t.Fatalf("transformed layers = %#v", result)
	}
	if result[0].Features[0].Geometry.(core.WKTGeometry).WKT == "POINT (127 37)" || result[1].Features[0].Geometry.(core.WKTGeometry).WKT == "POINT (128 38)" {
		t.Fatal("shared-pipeline transform did not change geometry")
	}
}

func TestTransformerWKTScannerPreservesCoordinatePairs(t *testing.T) {
	result, err := (Transformer{}).Transform(context.Background(), core.CRS{AuthorityCode: "EPSG:4326"}, core.CRS{AuthorityCode: "EPSG:3857"}, core.Layer{
		Features: []core.Feature{{ID: 1, Geometry: core.WKTGeometry{WKT: "LINESTRING (127 37, 128 38)"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	geometry := result.Features[0].Geometry.(core.WKTGeometry)
	if !strings.HasPrefix(geometry.WKT, "LINESTRING (") || !strings.Contains(geometry.WKT, ",") {
		t.Fatalf("geometry delimiters were not preserved: %q", geometry.WKT)
	}
	if len(numberPattern.FindAllString(geometry.WKT, -1)) != 4 || strings.Contains(geometry.WKT, "127 37") {
		t.Fatalf("unexpected transformed line: %q", geometry.WKT)
	}
	if _, err := (Transformer{}).Transform(context.Background(), core.CRS{AuthorityCode: "EPSG:4326"}, core.CRS{AuthorityCode: "EPSG:3857"}, core.Layer{
		Features: []core.Feature{{ID: 2, Geometry: core.WKTGeometry{WKT: "POINT (127)"}}},
	}); err == nil {
		t.Fatal("expected odd-coordinate WKT error")
	}
}

func TestTransformerWKTPolygonWithHoleUsesFallback(t *testing.T) {
	input := `POLYGON (((127 37, 128 37, 128 38, 127 38, 127 37)), ((127.2 37.2, 127.8 37.2, 127.8 37.8, 127.2 37.8, 127.2 37.2)))`
	result, err := (Transformer{}).Transform(context.Background(), core.CRS{AuthorityCode: "EPSG:4326"}, core.CRS{AuthorityCode: "EPSG:3857"}, core.Layer{
		Features: []core.Feature{{ID: 1, Geometry: core.WKTGeometry{WKT: input}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	geometry := result.Features[0].Geometry.(core.WKTGeometry)
	if strings.Count(geometry.WKT, "(") != 5 || strings.Contains(geometry.WKT, "127 37") {
		t.Fatalf("polygon hole fallback changed structure or coordinates: %q", geometry.WKT)
	}
}

func TestTransformerKoreanCRSRegression(t *testing.T) {
	input := core.Layer{Features: []core.Feature{{ID: 1, Geometry: core.WKTGeometry{WKT: "POINT (127 37)"}}}}
	transformer := Transformer{}

	to5179, err := transformer.Transform(context.Background(), core.CRS{AuthorityCode: "EPSG:4326"}, core.CRS{AuthorityCode: "EPSG:5179"}, input)
	if err != nil {
		t.Fatal(err)
	}
	point5179 := pointFromWKT(t, to5179.Features[0])
	assertNear(t, point5179[0], 955511.809285, 0.02)
	assertNear(t, point5179[1], 1889174.174347, 0.02)
	assertProjectedBoundsMatchPoint(t, transformer, "EPSG:5179", point5179)
	if to5179.CRS.AuthorityCode != "EPSG:5179" {
		t.Fatalf("target CRS = %q", to5179.CRS.AuthorityCode)
	}

	to5186, err := transformer.Transform(context.Background(), core.CRS{AuthorityCode: "EPSG:4326"}, core.CRS{AuthorityCode: "EPSG:5186"}, input)
	if err != nil {
		t.Fatal(err)
	}
	point5186 := pointFromWKT(t, to5186.Features[0])
	assertNear(t, point5186[0], 200000.0, 0.02)
	assertNear(t, point5186[1], 489012.955691, 0.02)
	assertProjectedBoundsMatchPoint(t, transformer, "EPSG:5186", point5186)

	from5179, err := transformer.Transform(context.Background(), core.CRS{AuthorityCode: "EPSG:5179"}, core.CRS{AuthorityCode: "EPSG:5186"}, to5179)
	if err != nil {
		t.Fatal(err)
	}
	pointFrom5179 := pointFromWKT(t, from5179.Features[0])
	assertNear(t, pointFrom5179[0], point5186[0], 0.02)
	assertNear(t, pointFrom5179[1], point5186[1], 0.02)
}

func assertProjectedBoundsMatchPoint(t *testing.T, transformer Transformer, target string, point [2]float64) {
	t.Helper()
	bounds, err := transformer.TransformBounds(context.Background(), core.CRS{AuthorityCode: "EPSG:4326"}, core.CRS{AuthorityCode: target}, [4]float64{127, 37, 127, 37})
	if err != nil {
		t.Fatalf("transform point bounds to %s: %v", target, err)
	}
	assertNear(t, bounds[0], point[0], 0.02)
	assertNear(t, bounds[1], point[1], 0.02)
	assertNear(t, bounds[2], point[0], 0.02)
	assertNear(t, bounds[3], point[1], 0.02)
}

func pointFromWKT(t *testing.T, feature core.Feature) [2]float64 {
	t.Helper()
	geometry, ok := feature.Geometry.(core.WKTGeometry)
	if !ok {
		t.Fatalf("geometry type = %T", feature.Geometry)
	}
	values := numberPattern.FindAllString(geometry.WKT, -1)
	if len(values) != 2 {
		t.Fatalf("WKT = %q, parsed %v", geometry.WKT, values)
	}
	x, err := strconv.ParseFloat(values[0], 64)
	if err != nil {
		t.Fatal(err)
	}
	y, err := strconv.ParseFloat(values[1], 64)
	if err != nil {
		t.Fatal(err)
	}
	return [2]float64{x, y}
}

func assertNear(t *testing.T, got, want, tolerance float64) {
	t.Helper()
	if math.Abs(got-want) > tolerance {
		t.Fatalf("value = %.9f, want %.9f ± %.9f", got, want, tolerance)
	}
}

func BenchmarkTransformWKB10KPoints(b *testing.B) {
	layer := benchmarkTransformLayer(10_000, true)
	benchmarkTransform(b, layer)
}

func BenchmarkTransformWKB10KLines(b *testing.B) {
	benchmarkTransform(b, benchmarkWKBLineLayer10K())
}

func BenchmarkTransformWKB10KLinesGenericBaseline(b *testing.B) {
	layer := benchmarkWKBLineLayer10K()
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		pj, err := projlib.NewCRSToCRS("EPSG:4326", "EPSG:3857", nil)
		if err != nil {
			b.Fatal(err)
		}
		result := cloneLayerForTransform(layer)
		for featureIndex := range result.Features {
			geometry := result.Features[featureIndex].Geometry.(core.WKBGeometry)
			mapped, err := core.MapWKBXY(geometry.WKB, func(x, y float64) (float64, float64, error) {
				return transformCoordinate(x, y, pj, true, false)
			})
			if err != nil {
				b.Fatal(err)
			}
			result.Features[featureIndex].Geometry = core.WKBGeometry{WKB: mapped}
		}
	}
}

func BenchmarkTransformWKB10KMultiLines(b *testing.B) {
	benchmarkTransform(b, benchmarkWKBMultiLineLayer10K())
}

func BenchmarkTransformWKB10KMultiPolygons(b *testing.B) {
	benchmarkTransform(b, benchmarkWKBMultiPolygonLayer10K())
}

func BenchmarkTransformWKB10KMultiPoints(b *testing.B) {
	benchmarkTransform(b, benchmarkWKBMultiPointLayer10K())
}

func BenchmarkTransformWKB10KPolygons(b *testing.B) {
	benchmarkTransform(b, benchmarkWKBPolygonLayer10K())
}

func BenchmarkTransformWKB10KPolygonsGenericBaseline(b *testing.B) {
	layer := benchmarkWKBPolygonLayer10K()
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		pj, err := projlib.NewCRSToCRS("EPSG:4326", "EPSG:3857", nil)
		if err != nil {
			b.Fatal(err)
		}
		result := cloneLayerForTransform(layer)
		for featureIndex := range result.Features {
			geometry := result.Features[featureIndex].Geometry.(core.WKBGeometry)
			mapped, err := core.MapWKBXY(geometry.WKB, func(x, y float64) (float64, float64, error) {
				return transformCoordinate(x, y, pj, true, false)
			})
			if err != nil {
				b.Fatal(err)
			}
			result.Features[featureIndex].Geometry = core.WKBGeometry{WKB: mapped}
		}
	}
}

func BenchmarkTransformWKBPoint(b *testing.B) {
	layer := benchmarkTransformLayer(1, true)
	benchmarkTransform(b, layer)
}

func BenchmarkTransformWKT10KPoints(b *testing.B) {
	layer := benchmarkTransformLayer(10_000, false)
	benchmarkTransform(b, layer)
}

func BenchmarkTransformWKT10KLines(b *testing.B) {
	benchmarkTransform(b, benchmarkWKTLineLayer10K())
}

func BenchmarkTransformWKT10KLinesGenericBaseline(b *testing.B) {
	layer := benchmarkWKTLineLayer10K()
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		pj, err := projlib.NewCRSToCRS("EPSG:4326", "EPSG:3857", nil)
		if err != nil {
			b.Fatal(err)
		}
		result := cloneLayerForTransform(layer)
		for featureIndex := range result.Features {
			geometry := result.Features[featureIndex].Geometry.(core.WKTGeometry)
			wkt, err := transformXY(geometry.WKT, pj, true, false)
			if err != nil {
				b.Fatal(err)
			}
			result.Features[featureIndex].Geometry = core.WKTGeometry{WKT: wkt}
		}
	}
}

func BenchmarkTransformWKT10KPolygons(b *testing.B) {
	benchmarkTransform(b, benchmarkWKTPolygonLayer10K())
}

func BenchmarkTransformWKT10KPolygonsGenericBaseline(b *testing.B) {
	layer := benchmarkWKTPolygonLayer10K()
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		pj, err := projlib.NewCRSToCRS("EPSG:4326", "EPSG:3857", nil)
		if err != nil {
			b.Fatal(err)
		}
		result := cloneLayerForTransform(layer)
		for featureIndex := range result.Features {
			geometry := result.Features[featureIndex].Geometry.(core.WKTGeometry)
			wkt, err := transformXY(geometry.WKT, pj, true, false)
			if err != nil {
				b.Fatal(err)
			}
			result.Features[featureIndex].Geometry = core.WKTGeometry{WKT: wkt}
		}
	}
}

func benchmarkWKTLineLayer10K() core.Layer {
	features := make([]core.Feature, 10_000)
	for index := range features {
		x := 126.0 + float64(index%1000)*0.001
		y := 36.0 + float64(index/1000)*0.001
		features[index] = core.Feature{ID: uint64(index + 1), Geometry: core.WKTGeometry{WKT: fmt.Sprintf("LINESTRING (%g %g, %g %g)", x, y, x+0.001, y+0.001)}}
	}
	return core.Layer{Features: features}
}

func benchmarkWKTPolygonLayer10K() core.Layer {
	features := make([]core.Feature, 10_000)
	for index := range features {
		x := 126.0 + float64(index%1000)*0.001
		y := 36.0 + float64(index/1000)*0.001
		features[index] = core.Feature{ID: uint64(index + 1), Geometry: core.WKTGeometry{WKT: fmt.Sprintf("POLYGON ((%g %g, %g %g, %g %g, %g %g, %g %g))", x, y, x+0.001, y, x+0.001, y+0.001, x, y+0.001, x, y)}}
	}
	return core.Layer{Features: features}
}

func BenchmarkTransformFourLayersIndividually10KPoints(b *testing.B) {
	layers := benchmarkTransformLayers(4, 2_500)
	transformer := Transformer{}
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		for _, layer := range layers {
			if _, err := transformer.Transform(context.Background(), core.CRS{AuthorityCode: "EPSG:4326"}, core.CRS{AuthorityCode: "EPSG:3857"}, layer); err != nil {
				b.Fatal(err)
			}
		}
	}
}

func BenchmarkTransformFourLayersSharedPipeline10KPoints(b *testing.B) {
	layers := benchmarkTransformLayers(4, 2_500)
	transformer := Transformer{}
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, err := transformer.TransformLayers(context.Background(), core.CRS{AuthorityCode: "EPSG:4326"}, core.CRS{AuthorityCode: "EPSG:3857"}, layers); err != nil {
			b.Fatal(err)
		}
	}
}

func benchmarkTransform(b *testing.B, layer core.Layer) {
	b.Helper()
	transformer := Transformer{}
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, err := transformer.Transform(context.Background(), core.CRS{AuthorityCode: "EPSG:4326"}, core.CRS{AuthorityCode: "EPSG:3857"}, layer); err != nil {
			b.Fatal(err)
		}
	}
}

func benchmarkTransformLayer(count int, wkb bool) core.Layer {
	features := make([]core.Feature, count)
	for index := range features {
		x, y := 126.0+float64(index%1000)*0.001, 36.0+float64(index/1000)*0.001
		if wkb {
			data := make([]byte, 21)
			data[0] = 1
			binary.LittleEndian.PutUint32(data[1:5], 1)
			binary.LittleEndian.PutUint64(data[5:13], math.Float64bits(x))
			binary.LittleEndian.PutUint64(data[13:21], math.Float64bits(y))
			features[index] = core.Feature{ID: uint64(index + 1), Geometry: core.WKBGeometry{WKB: data}}
			continue
		}
		features[index] = core.Feature{ID: uint64(index + 1), Geometry: core.WKTGeometry{WKT: fmt.Sprintf("POINT (%g %g)", x, y)}}
	}
	return core.Layer{Features: features}
}

func benchmarkWKBLineLayer10K() core.Layer {
	data := make([]byte, 41)
	data[0] = 1
	binary.LittleEndian.PutUint32(data[1:5], 2)
	binary.LittleEndian.PutUint32(data[5:9], 2)
	binary.LittleEndian.PutUint64(data[9:17], math.Float64bits(127))
	binary.LittleEndian.PutUint64(data[17:25], math.Float64bits(37))
	binary.LittleEndian.PutUint64(data[25:33], math.Float64bits(128))
	binary.LittleEndian.PutUint64(data[33:41], math.Float64bits(38))
	features := make([]core.Feature, 10_000)
	for index := range features {
		features[index] = core.Feature{ID: uint64(index + 1), Geometry: core.WKBGeometry{WKB: data}}
	}
	return core.Layer{Features: features}
}

func benchmarkWKBPolygonLayer10K() core.Layer {
	data := make([]byte, 93)
	data[0] = 1
	binary.LittleEndian.PutUint32(data[1:5], 3)
	binary.LittleEndian.PutUint32(data[5:9], 1)
	binary.LittleEndian.PutUint32(data[9:13], 5)
	for index, point := range [][2]float64{{127, 37}, {128, 37}, {128, 38}, {127, 38}, {127, 37}} {
		offset := 13 + index*16
		binary.LittleEndian.PutUint64(data[offset:offset+8], math.Float64bits(point[0]))
		binary.LittleEndian.PutUint64(data[offset+8:offset+16], math.Float64bits(point[1]))
	}
	features := make([]core.Feature, 10_000)
	for index := range features {
		features[index] = core.Feature{ID: uint64(index + 1), Geometry: core.WKBGeometry{WKB: data}}
	}
	return core.Layer{Features: features}
}

func benchmarkWKBMultiLineLayer10K() core.Layer {
	data := make([]byte, 91)
	data[0] = 1
	binary.LittleEndian.PutUint32(data[1:5], 5)
	binary.LittleEndian.PutUint32(data[5:9], 2)
	for lineIndex, points := range [][2][2]float64{
		{{127, 37}, {128, 38}},
		{{129, 39}, {130, 40}},
	} {
		offset := 9 + lineIndex*41
		data[offset] = 1
		binary.LittleEndian.PutUint32(data[offset+1:offset+5], 2)
		binary.LittleEndian.PutUint32(data[offset+5:offset+9], 2)
		for pointIndex, point := range points {
			pointOffset := offset + 9 + pointIndex*16
			binary.LittleEndian.PutUint64(data[pointOffset:pointOffset+8], math.Float64bits(point[0]))
			binary.LittleEndian.PutUint64(data[pointOffset+8:pointOffset+16], math.Float64bits(point[1]))
		}
	}
	features := make([]core.Feature, 10_000)
	for index := range features {
		features[index] = core.Feature{ID: uint64(index + 1), Geometry: core.WKBGeometry{WKB: data}}
	}
	return core.Layer{Features: features}
}

func benchmarkWKBMultiPolygonLayer10K() core.Layer {
	data := make([]byte, 195)
	data[0] = 1
	binary.LittleEndian.PutUint32(data[1:5], 6)
	binary.LittleEndian.PutUint32(data[5:9], 2)
	for polygonIndex, points := range [][5][2]float64{
		{{127, 37}, {128, 37}, {128, 38}, {127, 38}, {127, 37}},
		{{129, 39}, {130, 39}, {130, 40}, {129, 40}, {129, 39}},
	} {
		offset := 9 + polygonIndex*93
		data[offset] = 1
		binary.LittleEndian.PutUint32(data[offset+1:offset+5], 3)
		binary.LittleEndian.PutUint32(data[offset+5:offset+9], 1)
		binary.LittleEndian.PutUint32(data[offset+9:offset+13], 5)
		for pointIndex, point := range points {
			pointOffset := offset + 13 + pointIndex*16
			binary.LittleEndian.PutUint64(data[pointOffset:pointOffset+8], math.Float64bits(point[0]))
			binary.LittleEndian.PutUint64(data[pointOffset+8:pointOffset+16], math.Float64bits(point[1]))
		}
	}
	features := make([]core.Feature, 10_000)
	for index := range features {
		features[index] = core.Feature{ID: uint64(index + 1), Geometry: core.WKBGeometry{WKB: data}}
	}
	return core.Layer{Features: features}
}

func benchmarkWKBMultiPointLayer10K() core.Layer {
	data := make([]byte, 51)
	data[0] = 1
	binary.LittleEndian.PutUint32(data[1:5], 4)
	binary.LittleEndian.PutUint32(data[5:9], 2)
	for pointIndex, point := range [][2]float64{{127, 37}, {128, 38}} {
		offset := 9 + pointIndex*21
		data[offset] = 1
		binary.LittleEndian.PutUint32(data[offset+1:offset+5], 1)
		binary.LittleEndian.PutUint64(data[offset+5:offset+13], math.Float64bits(point[0]))
		binary.LittleEndian.PutUint64(data[offset+13:offset+21], math.Float64bits(point[1]))
	}
	features := make([]core.Feature, 10_000)
	for index := range features {
		features[index] = core.Feature{ID: uint64(index + 1), Geometry: core.WKBGeometry{WKB: data}}
	}
	return core.Layer{Features: features}
}

func benchmarkTransformLayers(layerCount, featuresPerLayer int) []core.Layer {
	layers := make([]core.Layer, layerCount)
	for layerIndex := range layers {
		layer := benchmarkTransformLayer(featuresPerLayer, true)
		layer.Name = fmt.Sprintf("layer-%d", layerIndex)
		layers[layerIndex] = layer
	}
	return layers
}
