package render

import (
	"context"
	"testing"

	"gogis/internal/core"
)

func TestNewLayerSourceNormalizesGeometryAndBuildsVertices(t *testing.T) {
	source, err := NewLayerSource(core.Layer{Name: "roads", Features: []core.Feature{
		{ID: 7, Geometry: core.WKTGeometry{WKT: "LINESTRING (100 200, 200 400)"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(source.Features) != 1 || source.Features[0].Vertices[0] != (Point{X: 0, Y: 0}) || source.Features[0].Vertices[1] != (Point{X: 1, Y: 1}) {
		t.Fatalf("unexpected normalized features: %#v", source.Features)
	}
	chunk, err := source.Builder(context.Background(), ChunkKey{Layer: "roads"})
	if err != nil || len(chunk.Vertices) != 2 {
		t.Fatalf("unexpected chunk: %#v, %v", chunk, err)
	}
	if chunk.Vertices[1].X != 0.25 || chunk.Vertices[1].Y != 0.25 {
		t.Fatalf("chunk was not clipped to its cell: %#v", chunk.Vertices)
	}
	lastChunk, err := source.Builder(context.Background(), ChunkKey{Layer: "roads", X: 3, Y: 3})
	if err != nil || len(lastChunk.Vertices) != 2 || lastChunk.Vertices[0].X != 0.75 || lastChunk.Vertices[0].Y != 0.75 {
		t.Fatalf("last chunk was not clipped: %#v, %v", lastChunk.Vertices, err)
	}
}

func TestNewLayerSourceSupportsPointsAndCancellation(t *testing.T) {
	source, err := NewLayerSource(core.Layer{Name: "points", Features: []core.Feature{{ID: 1, Geometry: core.WKTGeometry{WKT: "POINT (5 5)"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(source.Features[0].Vertices) != 1 {
		t.Fatalf("unexpected point source: %#v", source.Features)
	}
	chunk, err := source.Builder(context.Background(), ChunkKey{})
	if err != nil || len(chunk.Vertices) != 4 {
		t.Fatalf("unexpected point chunk: %#v, %v", chunk, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := source.Builder(ctx, ChunkKey{}); err == nil {
		t.Fatal("expected canceled source build")
	}
}

func TestNewLayerSourcesUsesCommonExtent(t *testing.T) {
	sources, err := NewLayerSources([]core.Layer{
		{Name: "near", Features: []core.Feature{{ID: 1, Geometry: core.WKTGeometry{WKT: "LINESTRING (0 0, 10 0)"}}}},
		{Name: "far", Features: []core.Feature{{ID: 2, Geometry: core.WKTGeometry{WKT: "LINESTRING (100 0, 110 0)"}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	near := sources["near"].Features[0].Vertices
	far := sources["far"].Features[0].Vertices
	if near[0].X != 0 || near[1].X >= 0.2 {
		t.Fatalf("near layer was not normalized against common extent: %#v", near)
	}
	if far[0].X <= 0.8 || far[1].X != 1 {
		t.Fatalf("far layer was not normalized against common extent: %#v", far)
	}
}

func TestNewLayerSourcesRejectsDuplicateNames(t *testing.T) {
	_, err := NewLayerSources([]core.Layer{{Name: "roads"}, {Name: "roads"}})
	if err == nil {
		t.Fatal("duplicate layer names were accepted")
	}
}

func TestNewLayerSourcePreservesMultiGeometryParts(t *testing.T) {
	source, err := NewLayerSource(core.Layer{Name: "roads", Features: []core.Feature{{
		ID:       1,
		Geometry: core.WKTGeometry{WKT: "MULTILINESTRING ((0 0, 10 0), (100 100, 110 100))"},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(source.Features[0].Parts) != 2 || len(source.Features[0].Parts[0]) != 2 || len(source.Features[0].Parts[1]) != 2 {
		t.Fatalf("multi geometry parts = %#v", source.Features[0].Parts)
	}
	if source.Features[0].Parts[0][1].X >= source.Features[0].Parts[1][0].X {
		t.Fatalf("parts unexpectedly connected: %#v", source.Features[0].Parts)
	}
}

func TestNewLayerSourcePreservesPolygonHoleParts(t *testing.T) {
	source, err := NewLayerSource(core.Layer{Name: "areas", Features: []core.Feature{{
		ID:       1,
		Geometry: core.WKTGeometry{WKT: "POLYGON ((0 0, 10 0, 10 10, 0 0), (2 2, 3 2, 3 3, 2 2))"},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(source.Features[0].Parts) != 2 {
		t.Fatalf("polygon parts = %#v", source.Features[0].Parts)
	}
}
