package dxf

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gogis/internal/core"
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
