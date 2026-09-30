package commands

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"gogis/internal/core"
)

var ErrFilterFieldMissing = errors.New("filter field is required")

// FilterLayerByProperty keeps features whose property equals expected. The
// comparison is typed for common string, boolean, and numeric field values.
func FilterLayerByProperty(ctx context.Context, layer core.Layer, field, expected string) (core.Layer, error) {
	return filterLayerByProperty(ctx, layer, field, expected, true)
}

func filterLayerByProperty(ctx context.Context, layer core.Layer, field, expected string, cloneFeatures bool) (core.Layer, error) {
	if err := ctx.Err(); err != nil {
		return core.Layer{}, err
	}
	if field == "" {
		return core.Layer{}, ErrFilterFieldMissing
	}
	matcher := newPropertyMatcher(expected)
	result := core.Layer{
		Name:     layer.Name,
		CRS:      layer.CRS,
		Editable: layer.Editable,
		Fields:   append([]core.Field(nil), layer.Fields...),
		Features: make([]core.Feature, 0),
	}
	for _, feature := range layer.Features {
		if err := ctx.Err(); err != nil {
			return core.Layer{}, err
		}
		value, exists := feature.Properties[field]
		if exists && matcher.matches(value) {
			if cloneFeatures {
				result.Features = append(result.Features, feature.Clone())
			} else {
				result.Features = append(result.Features, feature)
			}
		}
	}
	return result, nil
}

func propertyEquals(value any, expected string) bool {
	return newPropertyMatcher(expected).matches(value)
}

type propertyMatcher struct {
	expected   string
	boolValue  bool
	boolOK     bool
	intValue   int64
	intOK      bool
	uintValue  uint64
	uintOK     bool
	floatValue float64
	floatOK    bool
}

func newPropertyMatcher(expected string) propertyMatcher {
	matcher := propertyMatcher{expected: expected}
	var err error
	matcher.boolValue, err = strconv.ParseBool(expected)
	matcher.boolOK = err == nil
	matcher.intValue, err = strconv.ParseInt(expected, 10, 64)
	matcher.intOK = err == nil
	matcher.uintValue, err = strconv.ParseUint(expected, 10, 64)
	matcher.uintOK = err == nil
	matcher.floatValue, err = strconv.ParseFloat(expected, 64)
	matcher.floatOK = err == nil
	return matcher
}

func (matcher propertyMatcher) matches(value any) bool {
	switch typed := value.(type) {
	case string:
		return typed == matcher.expected
	case bool:
		return matcher.boolOK && typed == matcher.boolValue
	case int:
		return matcher.intOK && int64(typed) == matcher.intValue
	case int8:
		return matcher.intOK && int64(typed) == matcher.intValue
	case int16:
		return matcher.intOK && int64(typed) == matcher.intValue
	case int32:
		return matcher.intOK && int64(typed) == matcher.intValue
	case int64:
		return matcher.intOK && typed == matcher.intValue
	case uint:
		return matcher.uintOK && uint64(typed) == matcher.uintValue
	case uint8:
		return matcher.uintOK && uint64(typed) == matcher.uintValue
	case uint16:
		return matcher.uintOK && uint64(typed) == matcher.uintValue
	case uint32:
		return matcher.uintOK && uint64(typed) == matcher.uintValue
	case uint64:
		return matcher.uintOK && typed == matcher.uintValue
	case float32:
		return matcher.floatOK && float64(typed) == matcher.floatValue
	case float64:
		return matcher.floatOK && typed == matcher.floatValue
	default:
		return strings.TrimSpace(fmt.Sprint(value)) == matcher.expected
	}
}

// FilterProjectLayer computes a detached filtered layer and adds it atomically.
func (s *ProjectService) FilterProjectLayer(ctx context.Context, sourceName, field, expected, resultName string) error {
	if resultName == "" {
		return errors.New("result layer name is required")
	}
	var layer core.Layer
	found := false
	for _, candidate := range s.project.Layers {
		if candidate.Name == sourceName {
			layer = candidate
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("%w: %s", ErrLayerMissing, sourceName)
	}
	// AddLayer performs the ownership copy. The committed source remains
	// untouched, so cloning only matching features here avoids a full input
	// snapshot and a second copy at the edit boundary.
	result, err := filterLayerByProperty(ctx, layer, field, expected, false)
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
