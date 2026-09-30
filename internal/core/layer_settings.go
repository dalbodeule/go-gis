package core

import (
	"errors"
	"fmt"
	"math"
	"strings"
)

var ErrInvalidLayerSettings = errors.New("invalid layer settings")

// DefaultLayerStyle returns a neutral style that is legible on a light map.
func DefaultLayerStyle() LayerStyle {
	return LayerStyle{
		PointColor: "#d1495b", LineColor: "#2b6cb0", PolygonColor: "#75b798",
		PointSizeMM: 2.2, LineWidthMM: 0.45, FillOpacity: 0.35,
	}
}

func DefaultLabelSettings() LabelSettings {
	return LabelSettings{Placement: "center", HeightMM: 2.5}
}

// WithDefaultPresentation fills unset presentation values after a data driver
// creates a layer. It deliberately does not overwrite saved non-zero options.
func (l Layer) WithDefaultPresentation() Layer {
	if l.Style == (LayerStyle{}) {
		l.Style = DefaultLayerStyle()
	}
	if l.Labels == (LabelSettings{}) {
		l.Labels = DefaultLabelSettings()
	}
	return l
}

// Validate checks values before they are stored or passed to a renderer.
func (s LayerStyle) Validate() error {
	for name, color := range map[string]string{"point": s.PointColor, "line": s.LineColor, "polygon": s.PolygonColor} {
		if !validColor(color) {
			return fmt.Errorf("%w: %s color must be #RRGGBB or #RRGGBBAA", ErrInvalidLayerSettings, name)
		}
	}
	if !finite(s.PointSizeMM) || s.PointSizeMM <= 0 || s.PointSizeMM > 100 ||
		!finite(s.LineWidthMM) || s.LineWidthMM <= 0 || s.LineWidthMM > 100 ||
		!finite(s.FillOpacity) || s.FillOpacity < 0 || s.FillOpacity > 1 {
		return fmt.Errorf("%w: point/line size must be within (0,100] mm and opacity within [0,1]", ErrInvalidLayerSettings)
	}
	return nil
}

// Validate checks label placement, physical size, and scale range.
func (s LabelSettings) Validate() error {
	switch s.Placement {
	case "center", "center-rotated", "free-angle":
	default:
		return fmt.Errorf("%w: unsupported label placement %q", ErrInvalidLayerSettings, s.Placement)
	}
	if s.Enabled && strings.TrimSpace(s.Expression) == "" && strings.TrimSpace(s.LuaScript) == "" {
		return fmt.Errorf("%w: enabled labels need an expression or Lua script", ErrInvalidLayerSettings)
	}
	if !finite(s.HeightMM) || s.HeightMM <= 0 || s.HeightMM > 100 ||
		!finite(s.MinScale) || !finite(s.MaxScale) || s.MinScale < 0 || s.MaxScale < 0 ||
		(s.MinScale > 0 && s.MaxScale > 0 && s.MinScale > s.MaxScale) {
		return fmt.Errorf("%w: invalid label height or scale range", ErrInvalidLayerSettings)
	}
	return nil
}

func validColor(value string) bool {
	if len(value) != 7 && len(value) != 9 || value[0] != '#' {
		return false
	}
	for _, digit := range value[1:] {
		if !(digit >= '0' && digit <= '9' || digit >= 'a' && digit <= 'f' || digit >= 'A' && digit <= 'F') {
			return false
		}
	}
	return true
}

func finite(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }
