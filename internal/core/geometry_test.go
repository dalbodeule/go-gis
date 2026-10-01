package core

import (
	"encoding/binary"
	"encoding/hex"
	"math"
	"strings"
	"testing"
)

var geometryTypeBenchmarkSink string

func TestWKBGeometryDecodesPointWithoutWKT(t *testing.T) {
	data, err := hex.DecodeString("0101000000000000000000f03f0000000000000040")
	if err != nil {
		t.Fatal(err)
	}
	geometry := WKBGeometry{WKB: data}
	if geometry.GeometryType() != "POINT" {
		t.Fatalf("geometry type = %q", geometry.GeometryType())
	}
	parts, err := geometry.Parts()
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) != 1 || len(parts[0]) != 1 || parts[0][0] != (WKBPoint{X: 1, Y: 2}) {
		t.Fatalf("parts = %#v", parts)
	}
	wkt, err := geometry.WKT()
	if err != nil || wkt != "POINT (1 2)" {
		t.Fatalf("WKT = %q, err = %v", wkt, err)
	}
}

func TestWKBGeometryRejectsTruncatedData(t *testing.T) {
	if _, err := (WKBGeometry{WKB: []byte{1, 1, 0}}).Parts(); err == nil {
		t.Fatal("truncated WKB was accepted")
	}
}

func TestWKTGeometryTypeCanonicalizesStandardTypes(t *testing.T) {
	for _, test := range []struct {
		wkt, want string
	}{
		{wkt: "point (1 2)", want: "POINT"},
		{wkt: "LINESTRING Z (0 0, 1 1)", want: "LINESTRING"},
		{wkt: "geometrycollection EMPTY", want: "GEOMETRYCOLLECTION"},
	} {
		if got := (WKTGeometry{WKT: test.wkt}).GeometryType(); got != test.want {
			t.Errorf("GeometryType(%q) = %q, want %q", test.wkt, got, test.want)
		}
	}
}

func TestMapWKBXYPreservesBinaryGeometryAndMetadata(t *testing.T) {
	data := make([]byte, 57)
	data[0] = 1
	binary.LittleEndian.PutUint32(data[1:5], 2|0x80000000)
	binary.LittleEndian.PutUint32(data[5:9], 2)
	values := []float64{1, 2, 3, 10, 20, 30}
	for index, value := range values {
		binary.LittleEndian.PutUint64(data[9+index*8:], math.Float64bits(value))
	}
	original := append([]byte(nil), data...)
	mapped, err := MapWKBXY(data, func(x, y float64) (float64, float64, error) {
		return x + 1, y + 2, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(original) {
		t.Fatal("MapWKBXY mutated the input")
	}
	geometry := WKBGeometry{WKB: mapped}
	parts, err := geometry.Parts()
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) != 1 || len(parts[0]) != 2 || parts[0][0] != (WKBPoint{X: 2, Y: 4}) || parts[0][1] != (WKBPoint{X: 11, Y: 22}) {
		t.Fatalf("mapped parts = %#v", parts)
	}
}

func TestMoveWKBVertexCopiesGeometryAndValidatesIndex(t *testing.T) {
	data := make([]byte, 1+4+4+6*8)
	data[0] = 1
	binary.LittleEndian.PutUint32(data[1:5], 2)
	binary.LittleEndian.PutUint32(data[5:9], 3)
	for index, value := range []float64{1, 2, 3, 4, 5, 6} {
		binary.LittleEndian.PutUint64(data[9+index*8:], math.Float64bits(value))
	}
	moved, err := MoveWKBVertex(data, 1, 30, 40)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) == string(moved) {
		t.Fatal("moving a vertex did not change the WKB copy")
	}
	if points, err := (WKBGeometry{WKB: moved}).Parts(); err != nil || len(points) != 1 || len(points[0]) != 3 || points[0][1] != (WKBPoint{X: 30, Y: 40}) {
		t.Fatalf("moved geometry parts = %#v, err=%v", points, err)
	}
	if points, err := (WKBGeometry{WKB: data}).Parts(); err != nil || points[0][1] != (WKBPoint{X: 3, Y: 4}) {
		t.Fatalf("MoveWKBVertex mutated source geometry: %#v, err=%v", points, err)
	}
	if _, err := MoveWKBVertex(data, 3, 1, 1); err == nil {
		t.Fatal("out-of-range vertex index was accepted")
	}
	if _, err := MoveWKBVertex(data, 0, math.NaN(), 1); err == nil {
		t.Fatal("non-finite coordinate was accepted")
	}
}

func TestMoveWKBPolygonClosureVertexMovesBothRingEndpoints(t *testing.T) {
	points := []float64{0, 0, 10, 0, 10, 10, 0, 0}
	data := make([]byte, 1+4+4+4+len(points)*8)
	data[0] = 1
	binary.LittleEndian.PutUint32(data[1:5], 3)
	binary.LittleEndian.PutUint32(data[5:9], 1)
	binary.LittleEndian.PutUint32(data[9:13], 4)
	for index, value := range points {
		binary.LittleEndian.PutUint64(data[13+index*8:], math.Float64bits(value))
	}
	moved, err := MoveWKBVertex(data, 0, -2, 3)
	if err != nil {
		t.Fatal(err)
	}
	rings, err := (WKBGeometry{WKB: moved}).Parts()
	if err != nil {
		t.Fatal(err)
	}
	if len(rings) != 1 || rings[0][0] != (WKBPoint{X: -2, Y: 3}) || rings[0][len(rings[0])-1] != rings[0][0] {
		t.Fatalf("moved polygon ring endpoints = %#v", rings)
	}
}

func BenchmarkWKTGeometryType100K(b *testing.B) {
	geometry := WKTGeometry{WKT: "linestring (0 0, 1 1)"}
	b.ReportAllocs()
	for index := 0; index < b.N; index++ {
		for count := 0; count < 100_000; count++ {
			geometryTypeBenchmarkSink = geometry.GeometryType()
		}
	}
}

func BenchmarkWKTGeometryTypeUpperBaseline100K(b *testing.B) {
	geometry := WKTGeometry{WKT: "linestring (0 0, 1 1)"}
	b.ReportAllocs()
	for index := 0; index < b.N; index++ {
		typeName := geometry.WKT
		if cut := strings.IndexAny(typeName, " (\t\r\n"); cut >= 0 {
			typeName = typeName[:cut]
		}
		for count := 0; count < 100_000; count++ {
			geometryTypeBenchmarkSink = strings.ToUpper(typeName)
		}
	}
}
