package postgis

import (
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
