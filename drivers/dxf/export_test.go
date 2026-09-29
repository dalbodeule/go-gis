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
