package scripting

import "testing"

func TestSimpleStringFilter(t *testing.T) {
	for _, test := range []struct {
		rule string
		want StringFilter
		ok   bool
	}{
		{"return feature.COL_ADM_SE == '44150'", StringFilter{Field: "COL_ADM_SE", Value: "44150"}, true},
		{"return string.sub(feature.A3, 1, 5) == '44150'", StringFilter{Field: "A3", Value: "44150", Prefix: true}, true},
		{"return string.sub(feature.A3, 1, 4) == '44150'", StringFilter{}, false},
		{"return feature.A3 == '44150' or feature.B3 == '44150'", StringFilter{}, false},
		{"return feature.A3 == '44%'", StringFilter{}, false},
		{"return feature.A3 == '44150\"", StringFilter{}, false},
		{"return feature.A3 == '44150' -- note", StringFilter{}, false},
	} {
		got, ok := SimpleStringFilter(test.rule)
		if got != test.want || ok != test.ok {
			t.Errorf("rule %q = %+v, %t; want %+v, %t", test.rule, got, ok, test.want, test.ok)
		}
	}
}
