package postgis

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"gogis/internal/core"

	"golang.org/x/text/unicode/norm"
)

func TestNormIterMakesProgressOnMalformedUTF8(t *testing.T) {
	inputs := [][]byte{
		{0xff},
		{0xe2, 0x82},
		{0xe2, 0x28, 0xa1},
		{0xc0, 0xaf},
	}
	done := make(chan error, 1)
	go func() {
		for _, input := range inputs {
			var iterator norm.Iter
			iterator.Init(norm.NFC, input)
			for !iterator.Done() {
				previous := iterator.Pos()
				iterator.Next()
				if iterator.Pos() <= previous {
					done <- fmt.Errorf("normalizer did not advance on % x", input)
					return
				}
			}
		}
		done <- nil
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("normalizer did not finish malformed UTF-8 input within 2 seconds")
	}
}

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

func TestValidTableIdentifierEnforcesPostgreSQLPartLimits(t *testing.T) {
	for _, table := range []string{"roads", "gis.roads", strings.Repeat("r", maxPostgreSQLIdentifierBytes)} {
		if !validTableIdentifier(table) {
			t.Errorf("valid identifier %q was rejected", table)
		}
	}
	for _, table := range []string{"", ".roads", "gis.", "gis..roads", "db.gis.roads", strings.Repeat("r", maxPostgreSQLIdentifierBytes+1), `roads; DROP TABLE users`} {
		if validTableIdentifier(table) {
			t.Errorf("invalid identifier %q was accepted", table)
		}
	}
}

func TestPostGISFeatureIDRejectsNegativeBigint(t *testing.T) {
	if got, err := postGISFeatureID(0); err != nil || got != 0 {
		t.Fatalf("zero feature ID = %d, %v", got, err)
	}
	if got, err := postGISFeatureID(1<<63 - 1); err != nil || got != uint64(1<<63-1) {
		t.Fatalf("maximum feature ID = %d, %v", got, err)
	}
	if got, err := postGISFeatureID(-1); err == nil || got != 0 {
		t.Fatalf("negative feature ID = %d, %v; want error", got, err)
	}
}

func TestPostGISQueriesUseQuotedTableAndStableOrdering(t *testing.T) {
	if got := schemaQuery(`gis.roads`); !strings.Contains(got, `"gis"."roads"`) || !strings.Contains(got, "JSONB") {
		t.Fatalf("schema query = %s", got)
	}
	got := readLayerQuery(`gis.roads`)
	for _, expected := range []string{
		`"gis"."roads"`, `ST_NPoints(geom) <= 50000`, `pg_column_size(geom) <= 2097152`,
		`octet_length(properties::text) <= 8388608`, `ORDER BY id LIMIT 100001`,
	} {
		if !strings.Contains(got, expected) {
			t.Fatalf("read query %q does not contain %q", got, expected)
		}
	}
	if !strings.HasPrefix(got, "SELECT id,") {
		t.Fatalf("read query = %s", got)
	}
	if got := clearLayerQuery(`gis.roads`); got != `DELETE FROM "gis"."roads"` {
		t.Fatalf("clear query = %s", got)
	}
}

func TestEstimatePostGISPropertyBytesBoundsComplexJSON(t *testing.T) {
	estimate, err := estimatePostGISPropertyBytes([]byte(`{"name":"road","nested":[1,true,null]}`))
	if err != nil || estimate <= 0 || estimate > maxPostGISFeatureBytes {
		t.Fatalf("bounded JSON estimate = %d, %v", estimate, err)
	}
	deep := strings.Repeat("[", maxPostGISPropertyDepth+1) + "null" + strings.Repeat("]", maxPostGISPropertyDepth+1)
	if _, err := estimatePostGISPropertyBytes([]byte(deep)); err == nil || !strings.Contains(err.Error(), "nesting") {
		t.Fatalf("over-deep JSON error = %v, want nesting limit", err)
	}
	wide := `{"values":[` + strings.TrimSuffix(strings.Repeat("0,", maxPostGISPropertyNodes), ",") + "]}"
	if _, err := estimatePostGISPropertyBytes([]byte(wide)); err == nil || !strings.Contains(err.Error(), "node limit") {
		t.Fatalf("over-complex JSON error = %v, want node limit", err)
	}
	if _, err := estimatePostGISPropertyBytes([]byte(`{"value":`)); err == nil {
		t.Fatal("malformed JSON was accepted")
	}
}

func FuzzEstimatePostGISPropertyBytesNoPanic(f *testing.F) {
	f.Add([]byte(`{"name":"road","nested":[1,true,null]}`))
	f.Add([]byte(`null`))
	f.Add([]byte(`{"deep":{"items":[{"value":"text"}]}}`))
	f.Add([]byte(`{"broken":`))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			t.Skip()
		}
		_, _ = estimatePostGISPropertyBytes(data)
	})
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
