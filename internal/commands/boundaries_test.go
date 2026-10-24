package commands

import (
	"context"
	"strings"
	"testing"

	"gogis/internal/core"
)

func TestBuildBoundaryHullsIgnoresSurveyOrderAndTagsGroups(t *testing.T) {
	input := "block,x,y\nA,1,1\nB,10,10\nA,0,0\nA,1,0\nB,11,10\nA,0,1\nB,11,11\nB,10,11\nA,0.5,0.5\n"
	imported, err := ImportCSVPoints(context.Background(), strings.NewReader(input), CSVOptions{HasHeader: true, XField: "x", YField: "y", CRS: core.CRS{AuthorityCode: "EPSG:5186"}})
	if err != nil {
		t.Fatal(err)
	}
	tagged, boundaries, err := BuildBoundaryHulls(context.Background(), imported.Layer, "x", "y", "block", "outlines")
	if err != nil {
		t.Fatal(err)
	}
	if len(boundaries.Features) != 2 || tagged.Features[0].Properties[BoundaryIdentifierField] != "B000001" || tagged.Features[1].Properties[BoundaryIdentifierField] != "B000002" {
		t.Fatalf("unexpected boundary identifiers: %+v %+v", tagged.Features, boundaries.Features)
	}
	first, err := core.ToWKT(boundaries.Features[0].Geometry)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(first.WKT, "0.5 0.5") || !strings.Contains(first.WKT, "0 0") || !strings.Contains(first.WKT, "1 1") {
		t.Fatalf("convex hull = %q", first.WKT)
	}
	if _, _, err := BuildBoundaryHulls(context.Background(), core.Layer{CRS: core.CRS{AuthorityCode: "EPSG:5186"}, Fields: imported.Layer.Fields, Features: imported.Layer.Features[:2]}, "x", "y", "", "invalid"); err == nil {
		t.Fatal("expected insufficient point error")
	}
	line, err := ImportCSVPoints(context.Background(), strings.NewReader("x,y\n0,0\n1,1\n2,2\n"), CSVOptions{HasHeader: true, XField: "x", YField: "y", CRS: core.CRS{AuthorityCode: "EPSG:5186"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := BuildBoundaryHulls(context.Background(), line.Layer, "x", "y", "", "line_boundary"); err == nil {
		t.Fatal("collinear survey points should not create a polygon")
	}
}
