package postgis

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"strings"
	"testing"

	"gogis/internal/core"
)

func TestSRIDRequiresPositiveEPSGAuthority(t *testing.T) {
	for _, test := range []struct {
		input string
		want  int
		valid bool
	}{
		{input: "EPSG:4326", want: 4326, valid: true},
		{input: "epsg:5179", want: 5179, valid: true},
		{input: "", valid: false},
		{input: "EPSG:0", valid: false},
		{input: "OGC:CRS84", valid: false},
	} {
		got, err := srid(test.input)
		if test.valid {
			if err != nil || got != test.want {
				t.Fatalf("srid(%q) = %d, %v", test.input, got, err)
			}
		} else if err == nil {
			t.Fatalf("srid(%q) should fail", test.input)
		}
	}
}

func TestPostGISIntegrationSaveLayerRollbackPreservesSnapshot(t *testing.T) {
	dsn := os.Getenv("GOGIS_TEST_POSTGIS_DSN")
	if dsn == "" {
		t.Skip("set GOGIS_TEST_POSTGIS_DSN to run the live PostGIS transaction test")
	}

	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatal(err)
	}
	table := "gogis_integration_" + hex.EncodeToString(suffix[:])
	ctx := context.Background()
	store, err := Open(ctx, dsn, table)
	if err != nil {
		t.Fatalf("open integration PostGIS database: %v", err)
	}
	t.Cleanup(func() {
		_, _ = store.DB.ExecContext(context.Background(), "DROP TABLE IF EXISTS "+quoteIdentifier(table))
		_ = store.Close()
	})
	if _, err := store.DB.ExecContext(ctx, "CREATE EXTENSION IF NOT EXISTS postgis"); err != nil {
		t.Fatalf("enable PostGIS extension: %v", err)
	}

	initial := core.Layer{
		Name: "roads", CRS: core.CRS{AuthorityCode: "EPSG:4326"},
		Features: []core.Feature{{
			ID: 1, Geometry: core.WKTGeometry{WKT: "POINT (127 37)"},
			Properties: map[string]any{"version": "original"},
		}},
	}
	if err := store.SaveLayer(ctx, initial); err != nil {
		t.Fatalf("save initial snapshot: %v", err)
	}

	// The first replacement insert succeeds, then malformed WKT makes the next
	// PostGIS insert fail after the previous snapshot has been deleted. The
	// failed transaction must restore the original row and discard the partial
	// replacement row.
	replacement := core.Layer{
		Name: "roads", CRS: core.CRS{AuthorityCode: "EPSG:4326"},
		Features: []core.Feature{
			{ID: 2, Geometry: core.WKTGeometry{WKT: "POINT (128 38)"}, Properties: map[string]any{"version": "partial"}},
			{ID: 3, Geometry: core.WKTGeometry{WKT: "NOT A GEOMETRY"}, Properties: map[string]any{"version": "invalid"}},
		},
	}
	if err := store.SaveLayer(ctx, replacement); err == nil {
		t.Fatal("malformed geometry unexpectedly replaced the snapshot")
	}

	got, err := store.ReadLayer(ctx)
	if err != nil {
		t.Fatalf("read snapshot after failed replacement: %v", err)
	}
	if len(got.Features) != 1 || got.Features[0].ID != 1 || got.Features[0].Properties["version"] != "original" {
		t.Fatalf("failed replacement did not roll back atomically: %+v", got.Features)
	}
}

func TestQuoteIdentifierQuotesEachPart(t *testing.T) {
	if got := quoteIdentifier("gis.roads"); got != `"gis"."roads"` {
		t.Fatalf("quoted identifier = %s", got)
	}
}

func TestPostGISQueriesUseQuotedTableAndStableOrdering(t *testing.T) {
	if got := schemaQuery(`gis.roads`); !strings.Contains(got, `"gis"."roads"`) || !strings.Contains(got, "JSONB") {
		t.Fatalf("schema query = %s", got)
	}
	if got := readLayerQuery(`gis.roads`); got != `SELECT id, ST_AsText(geom), ST_SRID(geom), properties FROM "gis"."roads" ORDER BY id` {
		t.Fatalf("read query = %s", got)
	}
	if got := clearLayerQuery(`gis.roads`); got != `DELETE FROM "gis"."roads"` {
		t.Fatalf("clear query = %s", got)
	}
}

func TestInferFieldsSortsNamesAndMapsJSONTypes(t *testing.T) {
	features := []core.Feature{{Properties: map[string]any{
		"title":   "한글",
		"count":   float64(2),
		"enabled": true,
	}}}
	fields := inferFields(features)
	if len(fields) != 3 || fields[0].Name != "count" || fields[1].Name != "enabled" || fields[2].Name != "title" {
		t.Fatalf("fields = %#v", fields)
	}
	if fields[0].Type != core.FieldTypeNumber || fields[1].Type != core.FieldTypeBool || fields[2].Type != core.FieldTypeText {
		t.Fatalf("field types = %#v", fields)
	}
}

func TestTableNameUsesFinalIdentifierPart(t *testing.T) {
	if got := tableName("gis.roads"); got != "roads" {
		t.Fatalf("table name = %q", got)
	}
}
