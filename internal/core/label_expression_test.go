package core

import "testing"

func TestEvaluateLabelTemplate(t *testing.T) {
	properties := map[string]any{"road": "한강로", "number": 12}
	for expression, want := range map[string]string{
		"road":                       "한강로",
		"${road} ${number}":          "한강로 12",
		"Route ${road} / ${missing}": "Route 한강로 / ",
	} {
		got, err := EvaluateLabelTemplate(expression, properties)
		if err != nil || got != want {
			t.Errorf("EvaluateLabelTemplate(%q) = %q, %v; want %q", expression, got, err, want)
		}
	}
	if _, err := EvaluateLabelTemplate("${broken", properties); err == nil {
		t.Fatal("unterminated field placeholder accepted")
	}
}
