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

func TestWKBGeometryPointCountDoesNotMaterializeCoordinates(t *testing.T) {
	point := make([]byte, 21)
	point[0] = 1
	binary.LittleEndian.PutUint32(point[1:5], 1)
	if count, err := (WKBGeometry{WKB: point}).PointCount(); err != nil || count != 1 {
		t.Fatalf("point count = %d, err=%v; want 1", count, err)
	}

	line3D := make([]byte, 9+24)
	line3D[0] = 1
	binary.LittleEndian.PutUint32(line3D[1:5], 1002) // ISO WKB LineString Z
	binary.LittleEndian.PutUint32(line3D[5:9], 1)
	if count, err := (WKBGeometry{WKB: line3D}).PointCount(); err != nil || count != 1 {
		t.Fatalf("3D line point count = %d, err=%v; want 1", count, err)
	}

	collection := make([]byte, 9+len(point))
	collection[0] = 1
	binary.LittleEndian.PutUint32(collection[1:5], 7)
	binary.LittleEndian.PutUint32(collection[5:9], 1)
	copy(collection[9:], point)
	if count, err := (WKBGeometry{WKB: collection}).PointCount(); err != nil || count != 1 {
		t.Fatalf("collection point count = %d, err=%v; want 1", count, err)
	}
}

func TestWKBDecodersRejectImpossibleCountsBeforeAllocating(t *testing.T) {
	for _, test := range []struct {
		name     string
		typeCode uint32
	}{
		{name: "linestring points", typeCode: 2},
		{name: "polygon rings", typeCode: 3},
		{name: "multipoint children", typeCode: 4},
	} {
		t.Run(test.name, func(t *testing.T) {
			data := make([]byte, 9)
			data[0] = 1
			binary.LittleEndian.PutUint32(data[1:5], test.typeCode)
			binary.LittleEndian.PutUint32(data[5:9], ^uint32(0))
			if _, err := (WKBGeometry{WKB: data}).Parts(); err == nil {
				t.Fatal("Parts accepted a count that cannot fit in the input")
			}
		})
	}
}

func TestWKBDecodersRejectExcessiveCollectionDepth(t *testing.T) {
	data := []byte{1, 1, 0, 0, 0}
	for range 64 {
		collection := []byte{1, 7, 0, 0, 0, 1, 0, 0, 0}
		data = append(collection, data...)
	}
	geometry := WKBGeometry{WKB: data}
	if _, err := geometry.Parts(); err == nil || !strings.Contains(err.Error(), "nesting exceeds 64") {
		t.Fatalf("Parts error = %v; want nesting-depth error", err)
	}
	if _, err := MapWKBXY(data, func(x, y float64) (float64, float64, error) { return x, y, nil }); err == nil || !strings.Contains(err.Error(), "nesting exceeds 64") {
		t.Fatalf("MapWKBXY error = %v; want nesting-depth error", err)
	}
}

func TestWKBDecoderCapsEmptyCollectionElements(t *testing.T) {
	const rings = maxWKBDecodeElements + 1
	data := make([]byte, 9+4*rings)
	data[0] = 1
	binary.LittleEndian.PutUint32(data[1:5], 3) // Polygon
	binary.LittleEndian.PutUint32(data[5:9], rings)
	if _, err := (WKBGeometry{WKB: data}).Parts(); err == nil || !strings.Contains(err.Error(), "decode safety limit") {
		t.Fatalf("Parts error = %v; want decode-element limit", err)
	}
	if _, err := (WKBGeometry{WKB: data}).PointCount(); err == nil || !strings.Contains(err.Error(), "decode safety limit") {
		t.Fatalf("PointCount error = %v; want decode-element limit", err)
	}
}

func FuzzWKBDecodersNoPanic(f *testing.F) {
	point := []byte{1, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	f.Add(point)
	for _, typeCode := range []uint32{2, 3, 4, 7} {
		malformed := make([]byte, 9)
		malformed[0] = 1
		binary.LittleEndian.PutUint32(malformed[1:5], typeCode)
		binary.LittleEndian.PutUint32(malformed[5:9], ^uint32(0))
		f.Add(malformed)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			t.Skip()
		}
		geometry := WKBGeometry{WKB: data}
		_, _ = geometry.PointCount()
		_, _ = geometry.Parts()
		_, _ = geometry.WKT()
		_, _ = MapWKBXY(data, func(x, y float64) (float64, float64, error) { return x, y, nil })
	})
}

func TestWKBGeometryPointCountRejectsInvalidLengths(t *testing.T) {
	data := make([]byte, 9)
	data[0] = 1
	binary.LittleEndian.PutUint32(data[1:5], 2)
	binary.LittleEndian.PutUint32(data[5:9], ^uint32(0))
	if _, err := (WKBGeometry{WKB: data}).PointCount(); err == nil {
		t.Fatal("WKB count larger than available coordinates was accepted")
	}
	if _, err := (WKBGeometry{WKB: append(data[:5], 1, 2)}).PointCount(); err == nil {
		t.Fatal("trailing/truncated WKB was accepted")
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
