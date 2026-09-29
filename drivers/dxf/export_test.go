package dxf

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gogis/internal/core"
	"golang.org/x/text/encoding/korean"
)

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
