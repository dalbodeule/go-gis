package commands

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"gogis/internal/core"
)

var ErrFilterFieldMissing = errors.New("filter field is required")

// FilterLayerByProperty keeps features whose property equals expected. The
// comparison is typed for common string, boolean, and numeric field values.
func FilterLayerByProperty(ctx context.Context, layer core.Layer, field, expected string) (core.Layer, error) {
	if err := ctx.Err(); err != nil {
		return core.Layer{}, err
	}
	if field == "" {
		return core.Layer{}, ErrFilterFieldMissing
	}
	result := layer.Clone()
	result.Features = result.Features[:0]
	for _, feature := range layer.Features {
		if err := ctx.Err(); err != nil {
			return core.Layer{}, err
		}
		value, exists := feature.Properties[field]
		if exists && propertyEquals(value, expected) {
			result.Features = append(result.Features, feature.Clone())
		}
	}
	return result, nil
}

func propertyEquals(value any, expected string) bool {
	switch typed := value.(type) {
	case string:
		return typed == expected
	case bool:
		parsed, err := strconv.ParseBool(expected)
		return err == nil && typed == parsed
	case int:
		parsed, err := strconv.ParseInt(expected, 10, 64)
		return err == nil && int64(typed) == parsed
	case int8, int16, int32, int64:
		parsed, err := strconv.ParseInt(expected, 10, 64)
		return err == nil && reflect.ValueOf(typed).Int() == parsed
	case uint, uint8, uint16, uint32, uint64:
		parsed, err := strconv.ParseUint(expected, 10, 64)
		return err == nil && reflect.ValueOf(typed).Uint() == parsed
	case float32, float64:
		parsed, err := strconv.ParseFloat(expected, 64)
		return err == nil && reflect.ValueOf(typed).Float() == parsed
	default:
		return strings.TrimSpace(fmt.Sprint(value)) == expected
	}
}

// FilterProjectLayer computes a detached filtered layer and adds it atomically.
func (s *ProjectService) FilterProjectLayer(ctx context.Context, sourceName, field, expected, resultName string) error {
	if resultName == "" {
		return errors.New("result layer name is required")
	}
	project := s.Project()
	for _, layer := range project.Layers {
		if layer.Name != sourceName {
			continue
		}
		result, err := FilterLayerByProperty(ctx, layer, field, expected)
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
