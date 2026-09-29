//go:build native

package gdal

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestReaderOpensGeoJSONFixtureThroughGDAL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "roads.geojson")
	fixture := `{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"name":"한글 도로","speed":40},"geometry":{"type":"Point","coordinates":[127.1,37.4]}}]}`
	if err := os.WriteFile(path, []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}

	layer, err := (Reader{}).Open(context.Background(), path, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(layer.Features) != 1 {
		t.Fatalf("features = %d, want 1", len(layer.Features))
	}
	if got := layer.Features[0].Properties["name"]; got != "한글 도로" {
		t.Fatalf("name = %v", got)
	}
}
