//go:build native

package proj

import (
	"context"
	"strings"
	"testing"

	"gogis/internal/core"
)

func TestTransformerUsesPROJForPoint(t *testing.T) {
	layer := core.Layer{Features: []core.Feature{{ID: 1, Geometry: core.WKTGeometry{WKT: "POINT (127 37)"}}}}
	result, err := (Transformer{}).Transform(context.Background(), core.CRS{AuthorityCode: "EPSG:4326"}, core.CRS{AuthorityCode: "EPSG:3857"}, layer)
	if err != nil {
		t.Fatal(err)
	}
	geometry := result.Features[0].Geometry.(core.WKTGeometry)
	if !strings.HasPrefix(geometry.WKT, "POINT") || strings.Contains(geometry.WKT, "127 37") {
		t.Fatalf("unexpected transformed WKT: %s", geometry.WKT)
	}
}
