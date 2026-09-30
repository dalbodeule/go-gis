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
