package commands

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"gogis/internal/core"
)

func BenchmarkImportCSVPoints100k(b *testing.B) {
	benchmarkImportCSVPoints(b, 100000)
}

func BenchmarkBuildBoundaryHulls100k(b *testing.B) {
	layer := core.Layer{Name: "survey", CRS: core.CRS{AuthorityCode: "EPSG:5186"}, GeometryType: "POINT", Fields: []core.Field{{Name: "x", Type: core.FieldTypeNumber}, {Name: "y", Type: core.FieldTypeNumber}, {Name: "block", Type: core.FieldTypeText}}, Features: make([]core.Feature, 100000)}
	for index := range layer.Features {
		x, y := float64(index%1000), float64(index/1000)
		layer.Features[index] = core.Feature{ID: uint64(index + 1), Geometry: core.WKTGeometry{WKT: fmt.Sprintf("POINT (%g %g)", x, y)}, Properties: map[string]any{"x": x, "y": y, "block": fmt.Sprintf("B%d", index%16)}}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		_, boundaries, err := BuildBoundaryHulls(context.Background(), layer, "x", "y", "block", "blocks")
		if err != nil || len(boundaries.Features) != 16 {
			b.Fatalf("boundary build: %d, %v", len(boundaries.Features), err)
		}
	}
}

func BenchmarkBuildBoundaryHullsConvex10k(b *testing.B) {
	layer := core.Layer{Name: "survey", CRS: core.CRS{AuthorityCode: "EPSG:5186"}, GeometryType: "POINT", Fields: []core.Field{{Name: "x", Type: core.FieldTypeNumber}, {Name: "y", Type: core.FieldTypeNumber}}, Features: make([]core.Feature, 10000)}
	for index := range layer.Features {
		angle := 2 * math.Pi * float64(index) / float64(len(layer.Features))
		x, y := math.Cos(angle)*1000, math.Sin(angle)*1000
		layer.Features[index] = core.Feature{ID: uint64(index + 1), Geometry: csvPointWKT(x, y), Properties: map[string]any{"x": x, "y": y}}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		_, boundaries, err := BuildBoundaryHulls(context.Background(), layer, "x", "y", "", "blocks")
		if err != nil || len(boundaries.Features) != 1 {
			b.Fatalf("boundary build: %d, %v", len(boundaries.Features), err)
		}
	}
}

func BenchmarkConvexBoundary100k(b *testing.B) {
	points := make([]boundaryPoint, 100000)
	for index := range points {
		points[index] = boundaryPoint{x: float64(index % 1000), y: float64(index / 1000)}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		if hull := convexBoundary(points); len(hull) < 3 {
			b.Fatalf("invalid hull: %d points", len(hull))
		}
	}
}

var boundaryCrossSink float64

func BenchmarkBoundaryCrossArithmetic100k(b *testing.B) {
	points := make([]boundaryPoint, 100002)
	for index := range points {
		points[index] = boundaryPoint{x: float64(index % 1000), y: float64(index / 1000)}
	}
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		sum := 0.0
		for index := 0; index < 100000; index++ {
			a, p, c := points[index], points[index+1], points[index+2]
			sum += (p.x-a.x)*(c.y-a.y) - (p.y-a.y)*(c.x-a.x)
		}
		boundaryCrossSink = sum
	}
}

func BenchmarkImportCSVPoints1M(b *testing.B) {
	benchmarkImportCSVPoints(b, 1000000)
}

func BenchmarkValidateCSVText1MRows(b *testing.B) {
	record := []string{"000123", "225359.85", "440025.25", "parcel name"}
	b.ReportAllocs()
	for iteration := 0; iteration < b.N; iteration++ {
		for row := 0; row < 1000000; row++ {
			if err := validateCSVText(record); err != nil {
				b.Fatal(err)
			}
		}
	}
}

func BenchmarkReplacementSearchLongField(b *testing.B) {
	value := strings.Repeat("survey note ", 32)
	b.Run("byte-search", func(b *testing.B) {
		for iteration := 0; iteration < b.N; iteration++ {
			if containsReplacementRune(value) {
				b.Fatal("unexpected replacement rune")
			}
		}
	})
	b.Run("rune-scan", func(b *testing.B) {
		for iteration := 0; iteration < b.N; iteration++ {
			if strings.ContainsRune(value, utf8.RuneError) {
				b.Fatal("unexpected replacement rune")
			}
		}
	})
}

func benchmarkImportCSVPoints(b *testing.B, rows int) {
	var csv strings.Builder
	csv.WriteString("id,x,y,name\n")
	for i := 1; i <= rows; i++ {
		csv.WriteString(strconv.Itoa(i))
		csv.WriteByte(',')
		csv.WriteString(strconv.Itoa(i))
		csv.WriteString(",")
		csv.WriteString(strconv.Itoa(i + 1))
		csv.WriteString(",시설-")
		csv.WriteString(strconv.Itoa(i))
		csv.WriteByte('\n')
	}
	data := csv.String()
	b.ReportMetric(float64(len(data)), "bytes/input")
	b.ReportMetric(float64(rows), "rows/input")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result, err := ImportCSVPoints(context.Background(), strings.NewReader(data), CSVOptions{
			Delimiter: ',', HasHeader: true, XField: "x", YField: "y",
			CRS: core.CRS{AuthorityCode: "EPSG:4326"}, LayerName: "bench",
		})
		if err != nil || result.Accepted != rows {
			b.Fatalf("import failed: accepted=%d err=%v", result.Accepted, err)
		}
	}
}
