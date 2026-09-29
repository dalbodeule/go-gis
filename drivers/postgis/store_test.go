package postgis

import "testing"

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
