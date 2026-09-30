package core

import (
	"fmt"
	"strings"
)

const maxLabelTextBytes = 4096

// EvaluateLabelTemplate resolves ${field} placeholders. A bare expression
// matching a property name returns that property's value. Non-placeholder
// text is preserved so templates can combine literal and data content.
func EvaluateLabelTemplate(expression string, properties map[string]any) (string, error) {
	if value, ok := properties[expression]; ok {
		return formatLabelValue(value), nil
	}
	var result strings.Builder
	for len(expression) > 0 {
		start := strings.Index(expression, "${")
		if start < 0 {
			result.WriteString(expression)
			break
		}
		result.WriteString(expression[:start])
		expression = expression[start+2:]
		end := strings.IndexByte(expression, '}')
		if end < 0 {
			return "", fmt.Errorf("unterminated label field placeholder")
		}
		field := strings.TrimSpace(expression[:end])
		if field == "" {
			return "", fmt.Errorf("label field placeholder is empty")
		}
		result.WriteString(formatLabelValue(properties[field]))
		expression = expression[end+1:]
		if result.Len() > maxLabelTextBytes {
			return "", fmt.Errorf("label text exceeds %d bytes", maxLabelTextBytes)
		}
	}
	if result.Len() > maxLabelTextBytes {
		return "", fmt.Errorf("label text exceeds %d bytes", maxLabelTextBytes)
	}
	return result.String(), nil
}

func formatLabelValue(value any) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}
