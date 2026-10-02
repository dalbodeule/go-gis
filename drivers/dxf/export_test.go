package dxf

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"gogis/internal/core"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/korean"
)

func BenchmarkWriteCodeLine100K(b *testing.B) {
	var output bytes.Buffer
	writer := bufio.NewWriter(&output)
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		output.Reset()
		for count := 0; count < 100_000; count++ {
			if err := writeCodeLine(writer, count%90); err != nil {
				b.Fatal(err)
			}
		}
		if err := writer.Flush(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkWriteCodeLineFmtBaseline100K(b *testing.B) {
	var output bytes.Buffer
	writer := bufio.NewWriter(&output)
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		output.Reset()
		for count := 0; count < 100_000; count++ {
			if _, err := fmt.Fprintf(writer, "%d\n", count%90); err != nil {
				b.Fatal(err)
			}
		}
		if err := writer.Flush(); err != nil {
			b.Fatal(err)
		}
	}
}

func TestNumbersSupportsScientificNotation(t *testing.T) {
	values, err := numbers("LINESTRING (1.25e2 -3.5E-1, 1.35e2 6.5e-1)")
	if err != nil {
		t.Fatal(err)
	}
	want := []float64{125, -0.35, 135, 0.65}
	if len(values) != len(want) {
		t.Fatalf("values = %#v", values)
	}
	for index := range want {
		if values[index] != want[index] {
			t.Fatalf("values[%d] = %v, want %v", index, values[index], want[index])
		}
	}
}

func BenchmarkNumbers100KCoordinates(b *testing.B) {
	wkt := benchmarkNumbersWKT()
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		values, err := numbers(wkt)
		if err != nil || len(values) != 100_000 {
			b.Fatalf("numbers = %d, err=%v", len(values), err)
		}
	}
}

var benchmarkNumberPattern = regexp.MustCompile(`[-+]?(?:\d+(?:\.\d*)?|\.\d+)(?:[eE][-+]?\d+)?`)

func BenchmarkNumbersRegexBaseline100KCoordinates(b *testing.B) {
	wkt := benchmarkNumbersWKT()
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		values := make([]float64, 0, 8)
		for _, token := range benchmarkNumberPattern.FindAllString(wkt, -1) {
			value, err := strconv.ParseFloat(token, 64)
			if err != nil {
				b.Fatal(err)
			}
			values = append(values, value)
		}
		if len(values) != 100_000 {
			b.Fatalf("baseline numbers = %d", len(values))
		}
	}
}

func BenchmarkEncodeTextCP949100K(b *testing.B) {
	encode, err := newTextEncoder("ANSI_949")
	if err != nil {
		b.Fatal(err)
	}
	values := make([]string, 100_000)
	for index := range values {
		values[index] = "한글 도로"
	}
	b.ReportAllocs()
	b.ResetTimer()
	var buffer []byte
	for index := 0; index < b.N; index++ {
		for _, value := range values {
			var err error
			buffer, err = encode(value, buffer[:0])
			if err != nil {
				b.Fatal(err)
			}
		}
	}
}

func BenchmarkEncodeTextCP949NewEncoderBaseline100K(b *testing.B) {
	values := make([]string, 100_000)
	for index := range values {
		values[index] = "한글 도로"
	}
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		for _, value := range values {
			if _, err := encodeText(value, "ANSI_949"); err != nil {
				b.Fatal(err)
			}
		}
	}
}

func BenchmarkWriteWKT100KCoordinates(b *testing.B) {
	wkt := benchmarkNumbersWKT()
	write := func(int, string) error { return nil }
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if err := writeWKT(write, wkt, "roads"); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkWriteWKTWithFloat100KCoordinates(b *testing.B) {
	wkt := benchmarkNumbersWKT()
	write := func(int, string) error { return nil }
	writeFloat := func(int, float64) error { return nil }
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if err := writeWKTWithFloat(write, writeFloat, wkt, "roads"); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkWriteText100KLabels(b *testing.B) {
	labels := make([]core.Label, 100_000)
	for index := range labels {
		labels[index] = core.Label{X: float64(index), Y: float64(index) * 0.5, Height: 1.5, Text: "road"}
	}
	write := func(int, string) error { return nil }
	writeFloat := func(int, float64) error { return nil }
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		for _, label := range labels {
			if err := writeTextWithFloat(write, writeFloat, label); err != nil {
				b.Fatal(err)
			}
		}
	}
}

func BenchmarkWriteText100KLabelsStringFloats(b *testing.B) {
	labels := make([]core.Label, 100_000)
	for index := range labels {
		labels[index] = core.Label{X: float64(index), Y: float64(index) * 0.5, Height: 1.5, Text: "road"}
	}
	write := func(int, string) error { return nil }
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		for _, label := range labels {
			if err := writeText(write, label); err != nil {
				b.Fatal(err)
			}
		}
	}
}

func BenchmarkWriteWKBPointDirect(b *testing.B) {
	data, err := hex.DecodeString("0101000000000000000000f03f0000000000000040")
	if err != nil {
		b.Fatal(err)
	}
	geometry := core.WKBGeometry{WKB: data}
	write := func(int, string) error { return nil }
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, err := writeWKBGeometry(write, geometry, "roads"); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkWriteWKBPointWKTBaseline(b *testing.B) {
	data, err := hex.DecodeString("0101000000000000000000f03f0000000000000040")
	if err != nil {
		b.Fatal(err)
	}
	geometry := core.WKBGeometry{WKB: data}
	write := func(int, string) error { return nil }
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		wktGeometry, err := geometry.WKT()
		if err != nil {
			b.Fatal(err)
		}
		if err := writeWKT(write, wktGeometry, "roads"); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkWriteWKB10KLines(b *testing.B) {
	data := benchmarkWKBLineData()
	geometry := core.WKBGeometry{WKB: data}
	write := func(int, string) error { return nil }
	writeFloat := func(int, float64) error { return nil }
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		for count := 0; count < 10_000; count++ {
			if _, err := writeWKBGeometryWithFloat(write, writeFloat, geometry, "roads"); err != nil {
				b.Fatal(err)
			}
		}
	}
}

func BenchmarkWriteWKB10KLinesFormatBaseline(b *testing.B) {
	data := benchmarkWKBLineData()
	geometry := core.WKBGeometry{WKB: data}
	write := func(int, string) error { return nil }
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		for count := 0; count < 10_000; count++ {
			if _, err := writeWKBGeometry(write, geometry, "roads"); err != nil {
				b.Fatal(err)
			}
		}
	}
}

func BenchmarkWriteWKB10KPolygons(b *testing.B) {
	data := benchmarkWKBPolygonData()
	geometry := core.WKBGeometry{WKB: data}
	write := func(int, string) error { return nil }
	writeFloat := func(int, float64) error { return nil }
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		for count := 0; count < 10_000; count++ {
			if _, err := writeWKBGeometryWithFloat(write, writeFloat, geometry, "roads"); err != nil {
				b.Fatal(err)
			}
		}
	}
}

func BenchmarkWriteWKB10KPolygonsFormatBaseline(b *testing.B) {
	data := benchmarkWKBPolygonData()
	geometry := core.WKBGeometry{WKB: data}
	write := func(int, string) error { return nil }
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		for count := 0; count < 10_000; count++ {
			if _, err := writeWKBGeometry(write, geometry, "roads"); err != nil {
				b.Fatal(err)
			}
		}
	}
}

func BenchmarkExport10KLines(b *testing.B) {
	data := benchmarkWKBLineData()
	features := make([]core.Feature, 10_000)
	for index := range features {
		features[index] = core.Feature{ID: uint64(index + 1), Geometry: core.WKBGeometry{WKB: data}}
	}
	layer := core.Layer{Name: "roads", Features: features}
	path := filepath.Join(b.TempDir(), "lines.dxf")
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if err := (Exporter{}).Export(context.Background(), path, layer, "ares-utf8"); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkGeometryGroups100KComponents(b *testing.B) {
	wkt := "MULTILINESTRING (" + strings.TrimSuffix(strings.Repeat("(0 0, 1 1),", 50_000), ",") + ")"
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		groups, err := geometryGroups(wkt)
		if err != nil || len(groups) != 50_000 {
			b.Fatalf("groups = %d, err=%v", len(groups), err)
		}
	}
}

func benchmarkNumbersWKT() string {
	wkt := "LINESTRING ("
	for index := 0; index < 50_000; index++ {
		if index > 0 {
			wkt += ", "
		}
		wkt += "127.12345 37.54321"
	}
	wkt += ")"
	return wkt
}

func benchmarkWKBLineData() []byte {
	data := make([]byte, 41)
	data[0] = 1
	binary.LittleEndian.PutUint32(data[1:5], 2)
	binary.LittleEndian.PutUint32(data[5:9], 2)
	binary.LittleEndian.PutUint64(data[9:17], math.Float64bits(127.12345))
	binary.LittleEndian.PutUint64(data[17:25], math.Float64bits(37.54321))
	binary.LittleEndian.PutUint64(data[25:33], math.Float64bits(128.12345))
	binary.LittleEndian.PutUint64(data[33:41], math.Float64bits(38.54321))
	return data
}

func benchmarkWKBPolygonData() []byte {
	data := make([]byte, 93)
	data[0] = 1
	binary.LittleEndian.PutUint32(data[1:5], 3)
	binary.LittleEndian.PutUint32(data[5:9], 1)
	binary.LittleEndian.PutUint32(data[9:13], 5)
	points := [][2]float64{{127, 37}, {128, 37}, {128, 38}, {127, 38}, {127, 37}}
	for index, point := range points {
		offset := 13 + index*16
		binary.LittleEndian.PutUint64(data[offset:offset+8], math.Float64bits(point[0]))
		binary.LittleEndian.PutUint64(data[offset+8:offset+16], math.Float64bits(point[1]))
	}
	return data
}

func TestExporterWritesHeaderGeometryAndKoreanLabel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sample.dxf")
	layer := core.Layer{Name: "roads", Features: []core.Feature{{
		ID:         1,
		Geometry:   core.WKTGeometry{WKT: "POINT (127.1 37.4)"},
		Properties: map[string]any{"label": "한글 도로"},
	}}}
	if err := (Exporter{}).Export(context.Background(), path, layer, "ares-utf8"); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(contents)
	for _, expected := range []string{"$ACADVER", "AC1015", "$DWGCODEPAGE", "UTF-8", "POINT", "한글 도로", "ENDSEC", "EOF"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("DXF does not contain %q", expected)
		}
	}
}

func TestExporterWritesWKBPointDirectly(t *testing.T) {
	data, err := hex.DecodeString("0101000000000000000000f03f0000000000000040")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "wkb.dxf")
	layer := core.Layer{Name: "roads", Features: []core.Feature{{
		ID: 1, Geometry: core.WKBGeometry{WKB: data},
	}}}
	if err := (Exporter{}).Export(context.Background(), path, layer, "ares-utf8"); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(contents)
	if !strings.Contains(text, "POINT") || !strings.Contains(text, "1") || !strings.Contains(text, "2") {
		t.Fatalf("direct WKB point output = %q", text)
	}
}

func TestExporterSkipsNaNWKBPoint(t *testing.T) {
	data := make([]byte, 21)
	data[0] = 1
	binary.LittleEndian.PutUint32(data[1:5], 1)
	binary.LittleEndian.PutUint64(data[5:13], math.Float64bits(math.NaN()))
	binary.LittleEndian.PutUint64(data[13:21], math.Float64bits(math.NaN()))
	called := false
	handled, err := writeWKBGeometry(func(int, string) error {
		called = true
		return nil
	}, core.WKBGeometry{WKB: data}, "roads")
	if err != nil || !handled || called {
		t.Fatalf("NaN point handled=%v called=%v err=%v", handled, called, err)
	}
}

func TestExporterWritesSimpleWKBLineWithoutPartsDecode(t *testing.T) {
	data := benchmarkWKBLineData()
	var output []string
	handled, err := writeWKBGeometry(func(_ int, value string) error {
		output = append(output, value)
		return nil
	}, core.WKBGeometry{WKB: data}, "roads")
	if err != nil || !handled {
		t.Fatalf("simple WKB line handled=%v err=%v", handled, err)
	}
	joined := strings.Join(output, "\n")
	for _, expected := range []string{"LWPOLYLINE", "127.12345", "37.54321", "128.12345", "38.54321"} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("simple WKB line output missing %q: %s", expected, joined)
		}
	}
}

func TestExporterWritesSimpleWKBPolygonWithoutPartsDecode(t *testing.T) {
	var output []string
	handled, err := writeWKBGeometry(func(_ int, value string) error {
		output = append(output, value)
		return nil
	}, core.WKBGeometry{WKB: benchmarkWKBPolygonData()}, "roads")
	if err != nil || !handled {
		t.Fatalf("simple WKB polygon handled=%v err=%v", handled, err)
	}
	joined := strings.Join(output, "\n")
	for _, expected := range []string{"LWPOLYLINE", "1", "127", "37", "128", "38"} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("simple WKB polygon output missing %q: %s", expected, joined)
		}
	}
}

func TestExporterSkipsEmptyWKBLine(t *testing.T) {
	data, err := hex.DecodeString("010200000000000000")
	if err != nil {
		t.Fatal(err)
	}
	var output []string
	handled, err := writeWKBGeometry(func(_ int, value string) error {
		output = append(output, value)
		return nil
	}, core.WKBGeometry{WKB: data}, "roads")
	if err != nil || !handled || len(output) != 0 {
		t.Fatalf("empty WKB line = handled %v, output %#v, err %v", handled, output, err)
	}
}

func TestExporterWritesCP949Profile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sample-cp949.dxf")
	layer := core.Layer{Name: "roads", Features: []core.Feature{{
		ID:         1,
		Geometry:   core.WKTGeometry{WKT: "POINT (127.1 37.4)"},
		Properties: map[string]any{"label": "한글 도로"},
	}}}
	if err := (Exporter{}).Export(context.Background(), path, layer, "ares-cp949"); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(contents), "ANSI_949") {
		t.Fatalf("missing CP949 header: %q", contents)
	}
	decoded, err := korean.EUCKR.NewDecoder().Bytes(contents)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(decoded), "한글 도로") {
		t.Fatalf("CP949 label did not decode: %q", decoded)
	}
	if strings.Contains(string(contents), "한글 도로") {
		t.Fatal("CP949 output unexpectedly contains UTF-8 label bytes")
	}
}

func TestExporterWritesShiftJISProfile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sample-shift-jis.dxf")
	layer := core.Layer{Name: "roads", Features: []core.Feature{{
		ID: 1, Geometry: core.WKTGeometry{WKT: "POINT (139.7 35.6)"},
		Properties: map[string]any{"label": "地図"},
	}}}
	if err := (Exporter{}).Export(context.Background(), path, layer, "ares-shift-jis"); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(contents), "ANSI_932") {
		t.Fatalf("missing Shift-JIS header: %q", contents)
	}
	decoded, err := japanese.ShiftJIS.NewDecoder().Bytes(contents)
	if err != nil || !strings.Contains(string(decoded), "地図") {
		t.Fatalf("Shift-JIS label did not round trip: %q, err=%v", decoded, err)
	}
}

func TestExporterRejectsUnrepresentableCP949Text(t *testing.T) {
	path := filepath.Join(t.TempDir(), "unsupported-cp949.dxf")
	layer := core.Layer{Name: "roads", Features: []core.Feature{{
		ID:         1,
		Geometry:   core.WKTGeometry{WKT: "POINT (0 0)"},
		Properties: map[string]any{"label": "𠀀"},
	}}}
	if err := (Exporter{}).Export(context.Background(), path, layer, "ares-cp949"); err == nil {
		t.Fatal("unrepresentable CP949 text was accepted")
	}
}

func TestExporterWritesLabelPlacementRotationHeightAndStyle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "placed-label.dxf")
	layer := core.Layer{Name: "roads", Features: []core.Feature{{
		ID:       1,
		Geometry: core.WKTGeometry{WKT: "POINT (1 2)"},
		Label:    &core.Label{Text: "도로", X: 10.5, Y: 20.25, Rotation: 45, Height: 3.5, Style: "Korean"},
	}}}
	if err := (Exporter{}).Export(context.Background(), path, layer, "ares-utf8"); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(contents)
	for _, expected := range []string{"10.5", "20.25", "3.5", "45", "Korean", "도로"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("placed label output does not contain %q", expected)
		}
	}
}

func TestExporterWritesLineStringAndPolygon(t *testing.T) {
	path := filepath.Join(t.TempDir(), "geometry.dxf")
	layer := core.Layer{Name: "roads", Features: []core.Feature{
		{ID: 1, Geometry: core.WKTGeometry{WKT: "LINESTRING (126.9 37.4, 127.1 37.5)"}},
		{ID: 2, Geometry: core.WKTGeometry{WKT: "POLYGON ((127 37.4, 127.1 37.4, 127.1 37.5, 127 37.4))"}},
	}}
	if err := (Exporter{}).Export(context.Background(), path, layer, "ares-utf8"); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(contents)
	if !strings.Contains(text, "LWPOLYLINE") || !strings.Contains(text, "126.9") || !strings.Contains(text, "127.1") {
		t.Fatalf("line/polygon geometry was not exported: %s", text)
	}
	if !strings.Contains(text, "90\n2") || !strings.Contains(text, "90\n4") {
		t.Fatalf("unexpected vertex counts: %s", text)
	}
}

func TestExporterWritesMultiGeometriesAsSeparatePolylines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "multi.dxf")
	layer := core.Layer{Name: "areas", Features: []core.Feature{
		{ID: 1, Geometry: core.WKTGeometry{WKT: "MULTILINESTRING ((0 0, 1 1), (2 2, 3 3))"}},
		{ID: 2, Geometry: core.WKTGeometry{WKT: "MULTIPOLYGON (((0 0, 1 0, 1 1, 0 0)), ((2 2, 3 2, 3 3, 2 2)))"}},
	}}
	if err := (Exporter{}).Export(context.Background(), path, layer, "ares-utf8"); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(contents), "LWPOLYLINE"); got != 4 {
		t.Fatalf("LWPOLYLINE count = %d, want 4", got)
	}
}

func TestExporterWritesMultiPointAndGeometryCollection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "collection.dxf")
	layer := core.Layer{Name: "mixed", Features: []core.Feature{
		{ID: 1, Geometry: core.WKTGeometry{WKT: "MULTIPOINT ((1 2), (3 4))"}},
		{ID: 2, Geometry: core.WKTGeometry{WKT: "GEOMETRYCOLLECTION (POINT (5 6), LINESTRING (7 8, 9 10))"}},
	}}
	if err := (Exporter{}).Export(context.Background(), path, layer, "ares-utf8"); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(contents)
	if got := strings.Count(text, "POINT"); got != 3 {
		t.Fatalf("POINT count = %d, want 3", got)
	}
	if got := strings.Count(text, "LWPOLYLINE"); got != 1 {
		t.Fatalf("LWPOLYLINE count = %d, want 1", got)
	}
}

func TestExporterSkipsEmptyAndSupportsUnparenthesizedMultiPoint(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.dxf")
	layer := core.Layer{Name: "mixed", Features: []core.Feature{
		{ID: 1, Geometry: core.WKTGeometry{WKT: "POLYGON EMPTY"}},
		{ID: 2, Geometry: core.WKTGeometry{WKT: "MULTIPOINT (1 2, 3 4)"}},
	}}
	if err := (Exporter{}).Export(context.Background(), path, layer, "ares-utf8"); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(contents)
	if strings.Contains(text, "POLYGON EMPTY") || strings.Count(text, "POINT") != 2 {
		t.Fatalf("unexpected empty/multipoint output: %s", text)
	}
}
