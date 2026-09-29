package commands

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"

	"gogis/internal/core"
)

var ErrLabelFieldMissing = errors.New("label field is required")

// GenerateLabels creates detached labels from one feature property. The
// representative position is the point itself, the midpoint vertex for a
// line, or the area centroid for a simple polygon ring.
func GenerateLabels(ctx context.Context, layer core.Layer, field string, height float64, style string) (core.Layer, error) {
	if field == "" {
		return core.Layer{}, ErrLabelFieldMissing
	}
	result := layer.Clone()
	for index := range result.Features {
		if err := ctx.Err(); err != nil {
			return core.Layer{}, err
		}
		feature := &result.Features[index]
		value, ok := feature.Properties[field]
		if !ok || value == nil {
			feature.Label = nil
			continue
		}
		text := strings.TrimSpace(fmt.Sprint(value))
		if text == "" {
			feature.Label = nil
			continue
		}
		point, err := representativePoint(feature.Geometry)
		if err != nil {
			return core.Layer{}, fmt.Errorf("feature %d: %w", feature.ID, err)
		}
		feature.Label = &core.Label{Text: text, X: point.X, Y: point.Y, Height: height, Style: style}
	}
	return result, nil
}

// LabelProjectLayer generates labels and adds the result atomically.
func (s *ProjectService) LabelProjectLayer(ctx context.Context, sourceName, field, resultName string, height float64, style string) error {
	if resultName == "" {
		return errors.New("result layer name is required")
	}
	for _, layer := range s.Project().Layers {
		if layer.Name != sourceName {
			continue
		}
		result, err := GenerateLabels(ctx, layer, field, height, style)
		if err != nil {
			return err
		}
		result.Name = resultName
		if err := s.BeginEdit(); err != nil {
			return err
		}
		if err := s.AddLayer(result); err != nil {
			_ = s.Rollback()
			return err
		}
		if err := s.Commit(); err != nil {
			_ = s.Rollback()
			return err
		}
		return nil
	}
	return fmt.Errorf("%w: %s", ErrLayerMissing, sourceName)
}

type labelPoint struct{ X, Y float64 }

func representativePoint(geometry core.Geometry) (labelPoint, error) {
	wktGeometry, ok := geometry.(core.WKTGeometry)
	if !ok {
		return labelPoint{}, errors.New("geometry is not core.WKTGeometry")
	}
	typeName := strings.ToUpper(strings.TrimSpace(wktGeometry.GeometryType()))
	values, err := coordinatePairs(wktGeometry.WKT)
	if err != nil {
		return labelPoint{}, err
	}
	if len(values) == 0 {
		return labelPoint{}, errors.New("geometry has no coordinates")
	}
	switch typeName {
	case "POINT":
		return values[0], nil
	case "LINESTRING":
		return lineMidpoint(values), nil
	case "POLYGON":
		return polygonCentroid(values), nil
	default:
		return labelPoint{}, fmt.Errorf("unsupported label geometry type %q", typeName)
	}
}

func lineMidpoint(points []labelPoint) labelPoint {
	if len(points) == 1 {
		return points[0]
	}
	total := 0.0
	for index := 1; index < len(points); index++ {
		total += distance(points[index-1], points[index])
	}
	if total == 0 {
		return points[0]
	}
	target := total / 2
	traversed := 0.0
	for index := 1; index < len(points); index++ {
		segment := distance(points[index-1], points[index])
		if traversed+segment >= target {
			ratio := (target - traversed) / segment
			return labelPoint{
				X: points[index-1].X + ratio*(points[index].X-points[index-1].X),
				Y: points[index-1].Y + ratio*(points[index].Y-points[index-1].Y),
			}
		}
		traversed += segment
	}
	return points[len(points)-1]
}

func distance(left, right labelPoint) float64 {
	deltaX := right.X - left.X
	deltaY := right.Y - left.Y
	return math.Hypot(deltaX, deltaY)
}

func coordinatePairs(wkt string) ([]labelPoint, error) {
	values := make([]float64, 0)
	token := strings.Builder{}
	flush := func() error {
		if token.Len() == 0 {
			return nil
		}
		value, err := strconv.ParseFloat(token.String(), 64)
		if err != nil {
			return err
		}
		values = append(values, value)
		token.Reset()
		return nil
	}
	for _, character := range wkt {
		if unicode.IsDigit(character) || character == '-' || character == '+' || character == '.' {
			token.WriteRune(character)
			continue
		}
		if (character == 'e' || character == 'E') && token.Len() > 0 {
			token.WriteRune(character)
			continue
		}
		if err := flush(); err != nil {
			return nil, err
		}
	}
	if err := flush(); err != nil {
		return nil, err
	}
	if len(values)%2 != 0 {
		return nil, errors.New("coordinate list has an odd number of values")
	}
	pairs := make([]labelPoint, len(values)/2)
	for index := range pairs {
		pairs[index] = labelPoint{X: values[index*2], Y: values[index*2+1]}
	}
	return pairs, nil
}

func polygonCentroid(points []labelPoint) labelPoint {
	if len(points) < 3 {
		return points[0]
	}
	areaTwice := 0.0
	cx := 0.0
	cy := 0.0
	for index, current := range points {
		next := points[(index+1)%len(points)]
		cross := current.X*next.Y - next.X*current.Y
		areaTwice += cross
		cx += (current.X + next.X) * cross
		cy += (current.Y + next.Y) * cross
	}
	if areaTwice == 0 {
		return points[len(points)/2]
	}
	return labelPoint{X: cx / (3 * areaTwice), Y: cy / (3 * areaTwice)}
}
