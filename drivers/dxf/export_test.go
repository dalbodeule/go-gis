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

func TestExportProjectWritesEachLayerAndPreservesDestinationOnFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "project.dxf")
	specs := []LayerSpec{{Name: "연속지적도"}, {Name: "지적도근점"}, {Name: "건물"}}
	layers := []core.Layer{
		{Features: []core.Feature{{ID: 1, Geometry: core.WKTGeometry{WKT: "LINESTRING (0 0, 1 1)"}}}},
		{Features: []core.Feature{{ID: 2, Geometry: core.WKTGeometry{WKT: "POINT (2 2)"}}}},
		{Features: []core.Feature{{ID: 3, Geometry: core.WKTGeometry{WKT: "POLYGON ((3 3, 4 3, 4 4, 3 3))"}}}},
	}
	count := 0
	load := func(index int) (core.Layer, error) { count++; return layers[index], nil }
	if err := (Exporter{}).ExportProject(context.Background(), path, specs, load, "ares-utf8"); err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("loaded %d layers, want 3", count)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"연속지적도", "지적도근점", "건물"} {
		if !strings.Contains(string(data), "\n2\n"+name+"\n") || !strings.Contains(string(data), "\n8\n"+name+"\n") {
			t.Errorf("missing CAD layer %q", name)
		}
	}
	failure := func(index int) (core.Layer, error) {
		if index == 2 {
			return core.Layer{}, fmt.Errorf("source unavailable")
		}
		return layers[index], nil
	}
	if err := (Exporter{}).ExportProject(context.Background(), path, specs, failure, "ares-utf8"); err == nil {
		t.Fatal("expected loader failure")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, after) {
		t.Fatal("failed export replaced the previous complete DXF")
	}
	if err := (Exporter{}).ExportProject(context.Background(), path, []LayerSpec{{Name: "same"}, {Name: "SAME"}}, load, "ares-utf8"); err == nil {
		t.Fatal("expected duplicate CAD layer name error")
	}
}

func TestExportProjectWritesOpeningViewCenteredOnNonPointLayers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "project-view.dxf")
	specs := []LayerSpec{
		{Name: "parcels", GeometryType: "MULTIPOLYGON", Bounds: [4]float64{10, 20, 30, 40}, HasBounds: true},
		{Name: "survey-points", GeometryType: "POINT", Bounds: [4]float64{-500000, -500000, 500000, 500000}, HasBounds: true},
	}
	layers := []core.Layer{{}, {}}
	if err := (Exporter{}).ExportProject(context.Background(), path, specs,
		func(index int) (core.Layer, error) { return layers[index], nil }, "ares-utf8"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "\n9\n$VIEWCTR\n10\n20\n20\n30\n9\n$VIEWSIZE\n40\n23\n"
	if !strings.Contains(string(data), want) {
		t.Fatalf("opening view does not use non-point layer bounds; wanted header sequence %q", want)
	}
	for _, want := range []string{"\n2\nVPORT\n", "\n2\n*ACTIVE\n", "\n12\n20\n22\n30\n", "\n45\n23\n"} {
		if !bytes.Contains(data, []byte(want)) {
			t.Errorf("opening viewport is missing %q", want)
		}
	}
}

func TestExporterWritesSolidTriangleFillWhenTriangulatorIsAvailable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "filled-building.dxf")
	layer := core.Layer{Name: "building", Style: core.LayerStyle{FillOpacity: 1}, Features: []core.Feature{{
		ID: 1, Geometry: core.WKTGeometry{WKT: "POLYGON ((0 0, 2 0, 0 2, 0 0))"},
	}}}
	exporter := Exporter{Triangulate: func(_ context.Context, geometry core.Geometry) ([][3][2]float64, error) {
		if geometry.GeometryType() != "POLYGON" {
			t.Fatalf("triangulator received %s", geometry.GeometryType())
		}
		return [][3][2]float64{{{0, 0}, {2, 0}, {0, 2}}}, nil
	}}
	if err := exporter.Export(context.Background(), path, layer, "ares-utf8"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("\nHATCH\n")) || bytes.Count(data, []byte("\nSOLID\n")) != 1 ||
		!bytes.Contains(data, []byte("\n100\nAcDbTrace\n")) ||
		!bytes.Contains(data, []byte("\n13\n0\n23\n2\n33\n0\n")) ||
		!bytes.Contains(data, []byte("\nLWPOLYLINE\n")) {
		t.Fatal("triangle fill did not preserve a valid SOLID and polygon outline")
	}
}

func TestExportSingleLayerDerivesOpeningViewFromGeometry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "single-layer-view.dxf")
	layer := core.Layer{Name: "parcels", Features: []core.Feature{{
		ID: 1, Geometry: core.WKTGeometry{WKT: "POLYGON ((100 200, 120 200, 120 240, 100 200))"},
	}}}
	if err := (Exporter{}).Export(context.Background(), path, layer, "ares-utf8"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "\n9\n$VIEWCTR\n10\n110\n20\n220\n9\n$VIEWSIZE\n40\n46\n"
	if !strings.Contains(string(data), want) {
		t.Fatalf("single-layer export has no geometry-derived opening view; wanted %q", want)
	}
}

func TestInitialViewFallsBackToPointBoundsAndSkipsInvalidBounds(t *testing.T) {
	specs := []LayerSpec{
		{Name: "bad", GeometryType: "POLYGON", Bounds: [4]float64{math.NaN(), 0, 1, 1}, HasBounds: true},
		{Name: "points", GeometryType: "MULTIPOINT", Bounds: [4]float64{5, 7, 5, 7}, HasBounds: true},
	}
	x, y, size, ok := initialView(specs)
	if !ok || x != 5 || y != 7 || size != 1 {
		t.Fatalf("point-only fallback view = (%v, %v, %v, %t), want (5, 7, 1, true)", x, y, size, ok)
	}
	if _, _, _, ok := initialView([]LayerSpec{{Name: "empty"}}); ok {
		t.Fatal("empty project unexpectedly has an initial view")
	}
}

func TestExportProjectRejectsInvalidCADLayerNames(t *testing.T) {
	for _, name := range []string{"bad/name", "bad,name", "bad:name", " trailing ", strings.Repeat("a", 256)} {
		path := filepath.Join(t.TempDir(), "invalid.dxf")
		err := (Exporter{}).ExportProject(context.Background(), path, []LayerSpec{{Name: name}},
			func(int) (core.Layer, error) { return core.Layer{}, nil }, "ares-utf8")
		if err == nil {
			t.Errorf("accepted invalid CAD layer name %q", name)
		}
		if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
			t.Errorf("invalid name wrote %q: %v", path, statErr)
		}
	}
}

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
	for _, expected := range []string{"$ACADVER", "AC1021", "$DWGCODEPAGE", "UTF-8", "POINT", "한글 도로", "ENDSEC", "EOF"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("DXF does not contain %q", expected)
		}
	}
}

func TestExporterWritesSelfContainedR2000Records(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cad-structure.dxf")
	layer := core.Layer{Name: "parcels", Style: core.LayerStyle{FillOpacity: 1}, Features: []core.Feature{
		{ID: 1, Geometry: core.WKTGeometry{WKT: "POLYGON ((0 0, 2 0, 2 2, 0 0), (0.5 0.5, 0.5 1, 1 1, 0.5 0.5))"},
			Label: &core.Label{X: 1, Y: 1, Text: "한글", Height: 2.5, Rotation: 30, Style: "Korean"}},
		{ID: 2, Geometry: core.WKTGeometry{WKT: "POINT (3 4)"}},
	}}
	if err := (Exporter{}).Export(context.Background(), path, layer, "ares-cp949"); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.Split(bytes.TrimSuffix(contents, []byte("\n")), []byte("\n"))
	if len(lines)%2 != 0 {
		t.Fatal("DXF has an unpaired group code")
	}
	type pair struct{ code, value string }
	pairs := make([]pair, 0, len(lines)/2)
	for index := 0; index < len(lines); index += 2 {
		pairs = append(pairs, pair{string(lines[index]), string(lines[index+1])})
	}
	codePageFound := false
	for index, item := range pairs {
		if item == (pair{"9", "$DWGCODEPAGE"}) {
			codePageFound = true
			if index+1 >= len(pairs) {
				t.Fatal("$DWGCODEPAGE has no value")
			}
			if pairs[index+1] != (pair{"3", "ANSI_949"}) {
				t.Fatalf("$DWGCODEPAGE must use DXF group 3, got %v", pairs[index+1])
			}
		}
	}
	if !codePageFound {
		t.Fatal("missing $DWGCODEPAGE header")
	}
	for _, header := range []pair{{"9", "$PDMODE"}, {"70", "35"}, {"9", "$PDSIZE"}, {"40", "-3"}} {
		if !bytes.Contains(contents, []byte("\n"+header.code+"\n"+header.value+"\n")) {
			t.Errorf("DXF point visibility header missing %v", header)
		}
	}
	var records [][]pair
	for _, item := range pairs {
		if item.code == "0" {
			records = append(records, []pair{item})
		} else if len(records) > 0 {
			records[len(records)-1] = append(records[len(records)-1], item)
		}
	}
	field := func(record []pair, code, value string) bool {
		for _, item := range record {
			if item.code == code && item.value == value {
				return true
			}
		}
		return false
	}
	wantEntities := map[string]string{"LWPOLYLINE": "AcDbPolyline", "HATCH": "AcDbHatch", "TEXT": "AcDbText", "POINT": "AcDbPoint"}
	seenEntities := map[string]bool{}
	seenHandles := map[string]bool{}
	seenLayers := map[string]bool{}
	seenStyles := map[string]bool{}
	modelSpace := false
	for _, record := range records {
		kind := record[0].value
		for _, item := range record {
			if item.code == "5" {
				if seenHandles[item.value] {
					t.Fatalf("duplicate DXF handle %q", item.value)
				}
				seenHandles[item.value] = true
			}
		}
		if kind == "LAYER" {
			for _, item := range record {
				if item.code == "2" {
					seenLayers[item.value] = true
				}
			}
		}
		if kind == "STYLE" {
			for _, item := range record {
				if item.code == "2" {
					seenStyles[item.value] = true
				}
			}
			if field(record, "2", "Standard") && !field(record, "3", "malgun.ttf") {
				t.Error("Korean profiles must use the Windows Korean TrueType font")
			}
		}
		if kind == "BLOCK_RECORD" && field(record, "2", "*Model_Space") && field(record, "5", "1F") {
			modelSpace = true
		}
		if subclass, ok := wantEntities[kind]; ok {
			seenEntities[kind] = true
			for _, required := range []pair{{"330", "1F"}, {"100", "AcDbEntity"}, {"100", subclass}} {
				if !field(record, required.code, required.value) {
					t.Fatalf("%s is missing %v", kind, required)
				}
			}
			if !field(record, "8", "parcels") && !field(record, "8", "LABEL") {
				t.Fatalf("%s references an unexpected layer", kind)
			}
			if kind == "HATCH" {
				if !field(record, "2", "SOLID") || !field(record, "70", "1") || !field(record, "91", "2") {
					t.Fatal("HATCH must be a solid fill with outer and inner boundary paths")
				}
				paths, unassociated := 0, 0
				for _, item := range record {
					if item.code == "92" {
						paths++
						if paths == 1 && item.value != "3" {
							t.Errorf("outer HATCH boundary flag = %q, want 3 (external polyline)", item.value)
						}
						if paths == 2 && item.value != "2" {
							t.Errorf("inner HATCH boundary flag = %q, want 2 (polyline)", item.value)
						}
					}
					if item == (pair{"97", "0"}) {
						unassociated++
					}
				}
				if paths != 2 || unassociated != 2 {
					t.Fatalf("HATCH boundary path counts = %d/%d, want 2/2", paths, unassociated)
				}
			}
			if kind == "TEXT" {
				sections := 0
				textIndex, rotationIndex, styleIndex, finalSubclassIndex := -1, -1, -1, -1
				for index, item := range record {
					if item == (pair{"100", "AcDbText"}) {
						sections++
						finalSubclassIndex = index
					}
					if item.code == "1" {
						textIndex = index
					}
					if item.code == "50" {
						rotationIndex = index
					}
					if item.code == "7" {
						styleIndex = index
					}
				}
				if sections != 2 {
					t.Fatalf("TEXT must contain both AcDbText subclass sections, got %d", sections)
				}
				if textIndex < 0 || rotationIndex <= textIndex || styleIndex <= rotationIndex || finalSubclassIndex <= styleIndex {
					t.Fatalf("TEXT group sequence differs from DXF schema: text=%d rotation=%d style=%d final subclass=%d", textIndex, rotationIndex, styleIndex, finalSubclassIndex)
				}
			}
		}
	}
	for name := range wantEntities {
		if !seenEntities[name] {
			t.Errorf("missing %s entity", name)
		}
	}
	for _, name := range []string{"0", "parcels", "LABEL"} {
		if !seenLayers[name] {
			t.Errorf("missing LAYER %q definition", name)
		}
	}
	for _, name := range []string{"Standard", "Korean"} {
		if !seenStyles[name] {
			t.Errorf("missing STYLE %q definition", name)
		}
	}
	if !modelSpace {
		t.Error("missing model-space BLOCK_RECORD handle 1F")
	}
}

func TestExporterWritesSeparateHatchesForMultipolygonComponents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "multipolygon-hatch.dxf")
	layer := core.Layer{Name: "buildings", Style: core.LayerStyle{FillOpacity: 1}, Features: []core.Feature{{
		ID:       1,
		Geometry: core.WKTGeometry{WKT: "MULTIPOLYGON (((0 0, 4 0, 4 4, 0 0), (1 1, 1 2, 2 1, 1 1)), ((10 10, 14 10, 14 14, 10 10), (11 11, 11 12, 12 11, 11 11)))"},
	}}}
	if err := (Exporter{}).Export(context.Background(), path, layer, "ares-utf8"); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := bytes.Count(contents, []byte("\nHATCH\n")); got != 2 {
		t.Fatalf("HATCH entity count = %d, want one per polygon component (2)", got)
	}
	if got := bytes.Count(contents, []byte("\n92\n3\n")); got != 2 {
		t.Fatalf("external polyline boundary count = %d, want 2", got)
	}
	if got := bytes.Count(contents, []byte("\n92\n2\n")); got != 2 {
		t.Fatalf("inner polyline boundary count = %d, want 2", got)
	}
}

func TestExporterRejectsNonFiniteGeometry(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "invalid.dxf")
	if err := os.WriteFile(path, []byte("existing drawing"), 0o600); err != nil {
		t.Fatal(err)
	}
	wkb := make([]byte, 9+2*16)
	wkb[0] = 1
	binary.LittleEndian.PutUint32(wkb[1:5], 2)
	binary.LittleEndian.PutUint32(wkb[5:9], 2)
	binary.LittleEndian.PutUint64(wkb[9+16:9+24], math.Float64bits(1))
	binary.LittleEndian.PutUint64(wkb[9+24:9+32], math.Float64bits(math.NaN()))
	layer := core.Layer{Name: "invalid", Features: []core.Feature{{
		ID: 1, Geometry: core.WKBGeometry{WKB: wkb},
	}}}
	if err := (Exporter{}).Export(context.Background(), path, layer, "ares-cp949"); err == nil {
		t.Fatal("non-finite geometry should not produce an apparently valid DXF")
	}
	contents, err := os.ReadFile(path)
	if err != nil || string(contents) != "existing drawing" {
		t.Fatalf("failed export replaced previous drawing: contents=%q err=%v", contents, err)
	}
	leftovers, err := filepath.Glob(filepath.Join(directory, ".gogis-dxf-*.tmp"))
	if err != nil || len(leftovers) != 0 {
		t.Fatalf("failed export left temporary files: %v, %v", leftovers, err)
	}
}

func TestExporterReplacesExistingDrawingAfterSuccessfulWrite(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "existing.dxf")
	if err := os.WriteFile(path, []byte("old drawing"), 0o600); err != nil {
		t.Fatal(err)
	}
	layer := core.Layer{Name: "points", Features: []core.Feature{{
		ID: 1, Geometry: core.WKTGeometry{WKT: "POINT (1 2)"},
	}}}
	if err := (Exporter{}).Export(context.Background(), path, layer, "ares-cp949"); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(contents, []byte("\nEOF\n")) || bytes.Contains(contents, []byte("old drawing")) {
		t.Fatal("successful export did not replace the old file with a complete DXF")
	}
	leftovers, err := filepath.Glob(filepath.Join(directory, ".gogis-dxf-*.tmp"))
	if err != nil || len(leftovers) != 0 {
		t.Fatalf("successful export left temporary files: %v, %v", leftovers, err)
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

func TestExporterWritesEightCompassPointLabelOffsetsAndAlignment(t *testing.T) {
	for _, test := range []struct {
		placement            string
		dx, dy               float64
		horizontal, vertical int
	}{
		{"N", 0, 3, 1, 1}, {"NE", 3 / math.Sqrt2, 3 / math.Sqrt2, 0, 1},
		{"E", 3, 0, 0, 2}, {"SE", 3 / math.Sqrt2, -3 / math.Sqrt2, 0, 3},
		{"S", 0, -3, 1, 3}, {"SW", -3 / math.Sqrt2, -3 / math.Sqrt2, 2, 3},
		{"W", -3, 0, 2, 2}, {"NW", -3 / math.Sqrt2, 3 / math.Sqrt2, 2, 1},
	} {
		t.Run(test.placement, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "point-label.dxf")
			layer := core.Layer{
				Name:   "controls",
				Labels: core.LabelSettings{PointPlacement: test.placement, PointOffsetMM: 3, HeightMM: 2.5},
				Features: []core.Feature{{ID: 1, Geometry: core.WKTGeometry{WKT: "POINT (10 20)"},
					Label: &core.Label{Text: "CP-1", X: 10, Y: 20, AnchorSet: true, Height: 2.5}}},
			}
			alignment, aligned := pointTextAlignment(*layer.Features[0].Label, layer.Features[0].Geometry, LayerSpec{
				PointLabelPlacement: test.placement, PointLabelOffsetMM: 3, LabelHeightMM: 2.5,
			})
			if !aligned {
				t.Fatalf("point label alignment was not computed for placement %s", test.placement)
			}
			if math.Abs(alignment.x-(10+test.dx)) > 1e-9 || math.Abs(alignment.y-(20+test.dy)) > 1e-9 {
				t.Fatalf("computed alignment = %+v, want (%g,%g)", alignment, 10+test.dx, 20+test.dy)
			}
			spec := LayerSpec{Name: "controls", PointLabelPlacement: test.placement, PointLabelOffsetMM: 3, LabelHeightMM: 2.5}
			if err := (Exporter{}).ExportProject(context.Background(), path, []LayerSpec{spec}, func(int) (core.Layer, error) {
				return layer, nil
			}, "ares-utf8"); err != nil {
				t.Fatal(err)
			}
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			pairs := bytes.Split(bytes.TrimSuffix(content, []byte("\n")), []byte("\n"))
			fields := make(map[string]string)
			inText := false
			lastTextSubclass, verticalGroup := -1, -1
			for index := 0; index+1 < len(pairs); index += 2 {
				code := string(pairs[index])
				if code == "0" {
					if inText {
						break
					}
					inText = string(pairs[index+1]) == "TEXT"
					continue
				}
				if !inText {
					continue
				}
				if code == "100" && string(pairs[index+1]) == "AcDbText" {
					lastTextSubclass = index
				}
				if code == "10" || code == "20" || code == "11" || code == "21" || code == "72" || code == "73" {
					fields[code] = string(pairs[index+1])
				}
				if code == "73" {
					verticalGroup = index
				}
			}
			for code, want := range map[string]float64{
				"10": 10, "20": 20, "11": 10 + test.dx, "21": 20 + test.dy,
				"72": float64(test.horizontal), "73": float64(test.vertical),
			} {
				got, parseErr := strconv.ParseFloat(fields[code], 64)
				if parseErr != nil || math.Abs(got-want) > 1e-9 {
					t.Errorf("DXF TEXT group %s = %q, want %g (error %v); fields=%v", code, fields[code], want, parseErr, fields)
				}
			}
			if verticalGroup <= lastTextSubclass {
				t.Errorf("DXF TEXT vertical alignment group must follow the second AcDbText subclass marker: marker=%d group73=%d", lastTextSubclass, verticalGroup)
			}
		})
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
