//go:build native

package proj

import (
	"context"
	"math"
	"strconv"
	"strings"
	"testing"

	"gogis/internal/core"
)

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

func TestTransformerKoreanCRSRegression(t *testing.T) {
	input := core.Layer{Features: []core.Feature{{ID: 1, Geometry: core.WKTGeometry{WKT: "POINT (127 37)"}}}}
	transformer := Transformer{}

	to5179, err := transformer.Transform(context.Background(), core.CRS{AuthorityCode: "EPSG:4326"}, core.CRS{AuthorityCode: "EPSG:5179"}, input)
	if err != nil {
		t.Fatal(err)
	}
	point5179 := pointFromWKT(t, to5179.Features[0])
	assertNear(t, point5179[0], 1889174.174347, 0.02)
	assertNear(t, point5179[1], 955511.809285, 0.02)
	if to5179.CRS.AuthorityCode != "EPSG:5179" {
		t.Fatalf("target CRS = %q", to5179.CRS.AuthorityCode)
	}

	to5186, err := transformer.Transform(context.Background(), core.CRS{AuthorityCode: "EPSG:4326"}, core.CRS{AuthorityCode: "EPSG:5186"}, input)
	if err != nil {
		t.Fatal(err)
	}
	point5186 := pointFromWKT(t, to5186.Features[0])
	assertNear(t, point5186[0], 489012.955691, 0.02)
	assertNear(t, point5186[1], 200000.0, 0.02)

	from5179, err := transformer.Transform(context.Background(), core.CRS{AuthorityCode: "EPSG:5179"}, core.CRS{AuthorityCode: "EPSG:5186"}, to5179)
	if err != nil {
		t.Fatal(err)
	}
	pointFrom5179 := pointFromWKT(t, from5179.Features[0])
	assertNear(t, pointFrom5179[0], point5186[0], 0.02)
	assertNear(t, pointFrom5179[1], point5186[1], 0.02)
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
