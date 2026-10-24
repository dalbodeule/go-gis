package commands

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"gogis/internal/core"
)

func TestImportCSVPointFilesCombinesAndRejectsSchemaMismatch(t *testing.T) {
	options := CSVOptions{HasHeader: true, XField: "x", YField: "y", CRS: core.CRS{AuthorityCode: "EPSG:4326"}, LayerName: "combined"}
	inputs := []io.Reader{strings.NewReader("id,x,y\na,1,2\nb,bad,3\nc,4,5\n"), strings.NewReader("id,x,y\nd,6,7\n")}
	result, err := ImportCSVPointFiles(context.Background(), inputs, options)
	if err != nil {
		t.Fatal(err)
	}
	if result.Accepted != 3 || result.Rejected != 1 || len(result.Layer.Features) != 3 {
		t.Fatalf("combined result = %+v", result)
	}
	for i, feature := range result.Layer.Features {
		if feature.ID != []uint64{1, 3, 4}[i] {
			t.Fatalf("feature IDs must preserve input row gaps and remain unique: %+v", result.Layer.Features)
		}
	}
	_, err = ImportCSVPointFiles(context.Background(), []io.Reader{strings.NewReader("id,x,y\na,1,2\n"), strings.NewReader("x,id,y\n3,b,4\n")}, options)
	if err == nil || !strings.Contains(err.Error(), "schema") && !strings.Contains(err.Error(), "differs") {
		t.Fatalf("schema mismatch error = %v", err)
	}
	options.MaxRows = 1
	if _, err := ImportCSVPointFiles(context.Background(), []io.Reader{strings.NewReader("id,x,y\na,1,2\n"), strings.NewReader("id,x,y\nb,3,4\n")}, options); err == nil {
		t.Fatal("expected cumulative row limit")
	}
	if _, err := ImportCSVPointFiles(context.Background(), []io.Reader{strings.NewReader("id,x,y\na,1,2\n"), strings.NewReader("id,x,y\n")}, options); err != nil {
		t.Fatalf("header-only file should not exceed row limit: %v", err)
	}
}

func TestImportCSVPointsPreservesIdentifiersAndReportsBadRows(t *testing.T) {
	result, err := ImportCSVPoints(context.Background(), strings.NewReader("관리번호,X,Y\n0012,127.1,37.4\n0034,nope,37.5\n"), CSVOptions{HasHeader: true, XField: "X", YField: "Y", CRS: core.CRS{AuthorityCode: "EPSG:4326"}, LayerName: "survey"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Accepted != 1 || result.Rejected != 1 || result.Layer.Features[0].Properties["관리번호"] != "0012" {
		t.Fatalf("unexpected import result: %+v", result)
	}
}

func TestImportCSVPointsCancellation(t *testing.T) {
	var input strings.Builder
	input.WriteString("id,x,y\n")
	for i := 0; i < 10000; i++ {
		fmt.Fprintf(&input, "%d,%d,%d\n", i, i, i)
	}
	ctx := &cancelAfterContext{Context: context.Background(), checksUntilCancel: 100}
	_, err := ImportCSVPoints(ctx, strings.NewReader(input.String()), CSVOptions{HasHeader: true, XField: "x", YField: "y"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error = %v, want context.Canceled", err)
	}
}

type cancelAfterContext struct {
	context.Context
	checksUntilCancel int
}

func (c *cancelAfterContext) Err() error {
	if c.checksUntilCancel <= 0 {
		return context.Canceled
	}
	c.checksUntilCancel--
	return nil
}

func TestCSVPreviewHandlesUTF8BOMAndQuotedDelimiters(t *testing.T) {
	preview, err := PreviewCSV(context.Background(), strings.NewReader("\ufeffid,X,Y,note\n001,1,2,\"a,b\"\n"), ',', "UTF-8", 10)
	if err != nil || preview.Headers[0] != "id" || len(preview.Rows) != 1 || preview.Rows[0][3] != "a,b" {
		t.Fatalf("unexpected preview: %#v %v", preview, err)
	}
	result, err := ImportCSVPoints(context.Background(), strings.NewReader("\ufeffid,X,Y,active\n001,1,2,true\n"), CSVOptions{HasHeader: true, XField: "X", YField: "Y", FieldTypes: map[string]core.FieldType{"active": core.FieldTypeBool}})
	if err != nil || result.Layer.Features[0].Properties["active"] != true {
		t.Fatalf("typed import failed: %#v %v", result, err)
	}
}

func TestCSVErrorUsesPhysicalLineAfterQuotedNewline(t *testing.T) {
	input := "id,x,y,note\n001,1,2,\"first\nsecond\"\n002,bad,3,ok\n"
	result, err := ImportCSVPoints(context.Background(), strings.NewReader(input), CSVOptions{HasHeader: true, XField: "x", YField: "y"})
	if err != nil || result.Rejected != 1 || result.Errors[0].Row != 4 {
		t.Fatalf("physical row diagnostic = %+v, %v", result.Errors, err)
	}
}

func TestCSVImportReportsUndecodableText(t *testing.T) {
	input := append([]byte("id,x,y,note\n001,1,2,"), 0xff)
	input = append(input, '\n')
	result, err := ImportCSVPoints(context.Background(), strings.NewReader(string(input)), CSVOptions{HasHeader: true, XField: "x", YField: "y", Encoding: "UTF-8"})
	if err != nil || result.Rejected != 1 || len(result.Errors) != 1 || !strings.Contains(result.Errors[0].Reason, "encoding") {
		t.Fatalf("undecodable row = %+v, %v", result, err)
	}
}

func TestCSVImportRejectsInvalidTypedFieldsAndBoundsDiagnostics(t *testing.T) {
	var input strings.Builder
	input.WriteString("id,x,y,count\n")
	for i := 0; i < 120; i++ {
		fmt.Fprintf(&input, "%d,1,2,not-a-number\n", i)
	}
	result, err := ImportCSVPoints(context.Background(), strings.NewReader(input.String()), CSVOptions{HasHeader: true, XField: "x", YField: "y", FieldTypes: map[string]core.FieldType{"count": core.FieldTypeNumber}})
	if err != nil || result.Accepted != 0 || result.Rejected != 120 || len(result.Errors) != maxCSVRowDiagnostics {
		t.Fatalf("typed import diagnostics = %+v, %v", result, err)
	}
	if _, err := ImportCSVPoints(context.Background(), strings.NewReader("id,x,y\n1,1,2\n2,3,4\n"), CSVOptions{HasHeader: true, XField: "x", YField: "y", MaxRows: 1}); err == nil {
		t.Fatal("interactive row limit was ignored")
	}
}

func TestInteractiveTableLimitDoesNotReturnPartialRows(t *testing.T) {
	rows, _, err := ReadCSVTableLimited(context.Background(), strings.NewReader("id,name\n1,A\n2,B\n"), ',', "UTF-8", 1)
	if err == nil || rows != nil {
		t.Fatalf("limited table returned partial data: rows=%v err=%v", rows, err)
	}
}

func TestJoinManyFeaturesToOneRowAndRejectsDuplicates(t *testing.T) {
	layer := core.Layer{Fields: []core.Field{{Name: "key", Type: core.FieldTypeText}}, Features: []core.Feature{
		{ID: 1, Properties: map[string]any{"key": "001"}},
		{ID: 2, Properties: map[string]any{"key": "001"}},
		{ID: 3, Properties: map[string]any{"key": "missing"}},
	}}
	table := []map[string]any{{"id": "001", "grade": "A"}, {"id": "unused", "grade": "B"}, {"id": "", "grade": "C"}}
	joined, report, err := JoinLayerTable(layer, table, "key", "id")
	if err != nil || report.Matched != 2 || report.UnmatchedFeatures != 1 || report.UnmatchedRows != 2 {
		t.Fatalf("join = %+v, %+v, %v", joined, report, err)
	}
	if joined.Features[1].Properties["grade"] != "A" || layer.Features[0].Properties["grade"] != nil {
		t.Fatal("join did not preserve source or apply all matching features")
	}
	_, report, err = JoinLayerTable(layer, append(table, map[string]any{"id": "001", "grade": "different"}), "key", "id")
	if err == nil || report.DuplicateRows != 1 {
		t.Fatalf("duplicate key was not blocked: %+v, %v", report, err)
	}
}

func TestRemapFieldsPreservesValuesWhenNamesSwap(t *testing.T) {
	layer := core.Layer{Fields: []core.Field{{Name: "left", Type: core.FieldTypeText}, {Name: "right", Type: core.FieldTypeText}}, Features: []core.Feature{{ID: 1, Properties: map[string]any{"left": "L", "right": "R"}}}}
	result, err := RemapFields(layer, map[string]string{"left": "right", "right": "left"})
	if err != nil || result.Features[0].Properties["left"] != "R" || result.Features[0].Properties["right"] != "L" {
		t.Fatalf("swapped values lost: %+v, %v", result, err)
	}
	if layer.Features[0].Properties["left"] != "L" {
		t.Fatal("source mutated")
	}
}

func TestPolygonFromOrderedPointsClosesRing(t *testing.T) {
	points := []core.Feature{{ID: 1, Geometry: core.WKTGeometry{WKT: "POINT (0 0)"}}, {ID: 2, Geometry: core.WKTGeometry{WKT: "POINT (1 0)"}}, {ID: 3, Geometry: core.WKTGeometry{WKT: "POINT (0 1)"}}}
	layer, err := PolygonFromOrderedPoints(points, "area")
	if err != nil {
		t.Fatal(err)
	}
	if got := layer.Features[0].Geometry.(core.WKTGeometry).WKT; got != "POLYGON ((0 0, 1 0, 0 1, 0 0))" {
		t.Fatalf("unexpected polygon: %s", got)
	}
	if !layer.GeometryLocked {
		t.Fatal("generated data must start locked")
	}
}

func TestPolygonRejectsCrossingEdgesAndAcceptsWKBPoints(t *testing.T) {
	wkb, err := hex.DecodeString("0101000000000000000000f03f0000000000000040")
	if err != nil {
		t.Fatal(err)
	}
	_, err = PolygonFromOrderedPoints([]core.Feature{
		{ID: 1, Geometry: core.WKTGeometry{WKT: "POINT (0 0)"}},
		{ID: 2, Geometry: core.WKTGeometry{WKT: "POINT (3 3)"}},
		{ID: 3, Geometry: core.WKTGeometry{WKT: "POINT (0 4)"}},
		{ID: 4, Geometry: core.WKTGeometry{WKT: "POINT (4 0)"}},
	}, "crossed")
	if err == nil {
		t.Fatal("crossing polygon edges were accepted")
	}
	layer, err := PolygonFromOrderedPoints([]core.Feature{
		{ID: 1, Geometry: core.WKBGeometry{WKB: wkb}},
		{ID: 2, Geometry: core.WKTGeometry{WKT: "POINT (2 2)"}},
		{ID: 3, Geometry: core.WKTGeometry{WKT: "POINT (1 3)"}},
	}, "area")
	if err != nil || layer.Features[0].Geometry.GeometryType() != "POLYGON" {
		t.Fatalf("WKB points failed: %+v, %v", layer, err)
	}
}

func TestPlanarMetricsDoNotReturnAngularAreaOrLineArea(t *testing.T) {
	geographic := core.Layer{CRS: core.CRS{AuthorityCode: "EPSG:4326"}, Features: []core.Feature{{ID: 1, Geometry: core.WKTGeometry{WKT: "POLYGON ((0 0, 1 0, 1 1, 0 0))"}}}}
	if _, err := CalculatePlanarMetrics(geographic); err == nil {
		t.Fatal("geographic coordinates were measured as planar area")
	}
	projected := core.Layer{CRS: core.CRS{AuthorityCode: "EPSG:5186"}, Features: []core.Feature{{ID: 1, Geometry: core.WKTGeometry{WKT: "LINESTRING (0 0, 10 0, 10 10)"}, Properties: map[string]any{}}}}
	measured, err := CalculatePlanarMetrics(projected)
	if err != nil || measured.Features[0].Properties["area"] != 0.0 || measured.Features[0].Properties["length"] != 20.0 {
		t.Fatalf("line measurement = %+v, %v", measured, err)
	}
}

func TestWorkflowsAcceptFeaturesWithoutPropertyMaps(t *testing.T) {
	layer := core.Layer{CRS: core.CRS{AuthorityCode: "EPSG:5186"}, Fields: []core.Field{{Name: "key", Type: core.FieldTypeText}}, Features: []core.Feature{{ID: 1, Geometry: core.WKTGeometry{WKT: "POINT (0 0)"}}}}
	joined, _, err := JoinLayerTable(layer, []map[string]any{{"id": "<nil>", "grade": "A"}}, "key", "id")
	if err != nil {
		t.Fatal(err)
	}
	if joined.Features[0].Properties != nil {
		t.Fatal("missing key should not match a text sentinel")
	}
	measured, err := CalculatePlanarMetrics(layer)
	if err != nil || measured.Features[0].Properties["length"] != 0.0 {
		t.Fatalf("measure nil properties: %+v, %v", measured, err)
	}
	labeled, err := GenerateLabelField(layer, "label", "{id}")
	if err != nil || labeled.Features[0].Properties["label"] != "1" {
		t.Fatalf("label nil properties: %+v, %v", labeled, err)
	}
}

func TestGeometryLockBlocksGeometryButNotAttributes(t *testing.T) {
	service, err := NewProjectServiceWithLayers("p", core.CRS{}, []core.Layer{{Name: "roads", Editable: true, GeometryLocked: true, Features: []core.Feature{{ID: 1, Geometry: core.WKTGeometry{WKT: "POINT (0 0)"}}}}})
	if err != nil {
		t.Fatal(err)
	}
	if err = service.BeginEdit(); err != nil {
		t.Fatal(err)
	}
	if err = service.SetFeatureGeometry("roads", 1, core.WKTGeometry{WKT: "POINT (1 1)"}); err == nil {
		t.Fatal("locked geometry edit accepted")
	}
	if err = service.SetFeatureProperty("roads", 1, "status", "ok"); err != nil {
		t.Fatalf("attribute edit should remain available: %v", err)
	}
}

func TestFeatureSnapshotTracksEditsWithoutSharingProperties(t *testing.T) {
	service, err := NewProjectServiceWithLayers("p", core.CRS{}, []core.Layer{{Name: "assets", Editable: true, Features: []core.Feature{{ID: 1, Geometry: core.WKTGeometry{WKT: "POINT (0 0)"}, Properties: map[string]any{"grade": "A"}}}}})
	if err != nil {
		t.Fatal(err)
	}
	before, ok := service.FeatureSnapshot("assets", 1)
	if !ok {
		t.Fatal("feature not found")
	}
	if err := service.BeginEdit(); err != nil {
		t.Fatal(err)
	}
	if err := service.SetFeatureProperty("assets", 1, "grade", "B"); err != nil {
		t.Fatal(err)
	}
	if err := service.Commit(); err != nil {
		t.Fatal(err)
	}
	after, ok := service.FeatureSnapshot("assets", 1)
	if !ok || after.Properties["grade"] != "B" || before.Properties["grade"] != "A" {
		t.Fatalf("snapshots: before=%+v after=%+v", before, after)
	}
	after.Properties["grade"] = "C"
	if value, _ := service.FeatureProperty("assets", 1, "grade"); value != "B" {
		t.Fatal("returned snapshot mutated the project")
	}
}

func TestJoinValidationMeasureAndLabels(t *testing.T) {
	layer := core.Layer{Name: "assets", GeometryType: "POLYGON", CRS: core.CRS{AuthorityCode: "EPSG:5186"}, Fields: []core.Field{{Name: "관리번호", Type: core.FieldTypeText}}, Features: []core.Feature{{ID: 1, Geometry: core.WKTGeometry{WKT: "POLYGON ((0 0, 10 0, 10 10, 0 0))"}, Properties: map[string]any{"관리번호": "001"}}}}
	joined, report, err := JoinLayerTable(layer, []map[string]any{{"번호": "001", "등급": "A"}, {"번호": "002", "등급": "B"}}, "관리번호", "번호")
	if err != nil || report.Matched != 1 || joined.Features[0].Properties["등급"] != "A" {
		t.Fatalf("join failed: %#v %+v %v", joined, report, err)
	}
	measured, err := CalculatePlanarMetrics(joined)
	if err != nil || measured.Features[0].Properties["area"] != 50.0 {
		t.Fatalf("measure failed: %#v %v", measured, err)
	}
	labeled, err := GenerateLabelField(measured, "label_text", "시설-{관리번호}")
	if err != nil || labeled.Features[0].Properties["label_text"] != "시설-001" || !labeled.Labels.Enabled {
		t.Fatalf("label failed: %#v %v", labeled, err)
	}
	issues := ValidateLayer(core.Layer{Features: []core.Feature{{ID: 1, Geometry: core.WKTGeometry{WKT: "POINT (0 0)"}, Properties: map[string]any{"key": "x"}}, {ID: 2, Geometry: core.WKTGeometry{WKT: "POINT (1 1)"}, Properties: map[string]any{"key": "x"}}}}, []string{"required"}, "key")
	if len(issues) != 3 {
		t.Fatalf("validation issues = %d, want 3: %#v", len(issues), issues)
	}
}
