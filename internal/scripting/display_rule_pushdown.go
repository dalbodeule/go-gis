package scripting

import (
	"regexp"
	"strconv"
	"strings"
)

// StringFilter is an optional source-side prefilter for a display rule.
// Callers must still evaluate the original Lua rule on returned features.
type StringFilter struct {
	Field  string
	Value  string
	Prefix bool
}

var (
	stringEqualityRule = regexp.MustCompile(`^return\s+feature\.([A-Za-z_][A-Za-z0-9_]*)\s*==\s*(['"])([0-9]+)(['"])\s*$`)
	stringPrefixRule   = regexp.MustCompile(`^return\s+string\.sub\s*\(\s*feature\.([A-Za-z_][A-Za-z0-9_]*)\s*,\s*1\s*,\s*([0-9]+)\s*\)\s*==\s*(['"])([0-9]+)(['"])\s*$`)
)

// SimpleStringFilter recognizes only literal, numeric-string comparisons.
// Complex or stateful Lua stays on the existing per-feature path.
func SimpleStringFilter(rule string) (StringFilter, bool) {
	rule = strings.TrimSpace(rule)
	if match := stringEqualityRule.FindStringSubmatch(rule); match != nil && match[2] == match[4] {
		return StringFilter{Field: match[1], Value: match[3]}, true
	}
	if match := stringPrefixRule.FindStringSubmatch(rule); match != nil && match[3] == match[5] {
		length, err := strconv.Atoi(match[2])
		if err == nil && length == len(match[4]) {
			return StringFilter{Field: match[1], Value: match[4], Prefix: true}, true
		}
	}
	return StringFilter{}, false
}
