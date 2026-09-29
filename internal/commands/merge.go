package commands

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gogis/internal/core"
)

var (
	ErrMergeNeedsLayers = errors.New("merge requires at least two layers")
	ErrSchemaMismatch   = errors.New("layer schemas do not match")
	ErrCRSMismatch      = errors.New("layer CRS values do not match")
	ErrDuplicateID      = errors.New("duplicate feature ID during merge")
)

// MergeLayers combines layers with an identical field schema and CRS. Every
// feature is cloned, so callers can safely use the result as an edit snapshot.
func MergeLayers(ctx context.Context, layers ...core.Layer) (core.Layer, error) {
	if len(layers) < 2 {
		return core.Layer{}, ErrMergeNeedsLayers
	}
	base := layers[0]
	result := core.Layer{
		Name:     base.Name + "_merged",
		CRS:      base.CRS,
		Fields:   append([]core.Field(nil), base.Fields...),
		Editable: true,
	}
	seenIDs := make(map[uint64]struct{})
	for layerIndex, layer := range layers {
		if err := ctx.Err(); err != nil {
			return core.Layer{}, err
		}
		if layerIndex > 0 {
			if !sameCRS(base.CRS, layer.CRS) {
				return core.Layer{}, fmt.Errorf("%w: %s and %s", ErrCRSMismatch, base.CRS.AuthorityCode, layer.CRS.AuthorityCode)
			}
			if !sameFields(base.Fields, layer.Fields) {
				return core.Layer{}, fmt.Errorf("%w between %q and %q", ErrSchemaMismatch, base.Name, layer.Name)
			}
		}
		for _, feature := range layer.Features {
			if _, exists := seenIDs[feature.ID]; exists {
				return core.Layer{}, fmt.Errorf("%w: %d", ErrDuplicateID, feature.ID)
			}
			seenIDs[feature.ID] = struct{}{}
			result.Features = append(result.Features, feature.Clone())
		}
	}
	return result, nil
}

func sameCRS(left, right core.CRS) bool {
	return left.AuthorityCode == "" || right.AuthorityCode == "" || strings.EqualFold(left.AuthorityCode, right.AuthorityCode)
}

func sameFields(left, right []core.Field) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

// MergeProjectLayers computes and atomically adds a merged result layer to the
// committed project. Source layers are read from one detached snapshot.
func (s *ProjectService) MergeProjectLayers(ctx context.Context, sourceNames []string, resultName string) error {
	if resultName == "" {
		return errors.New("result layer name is required")
	}
	if len(sourceNames) < 2 {
		return ErrMergeNeedsLayers
	}
	project := s.Project()
	layers := make([]core.Layer, 0, len(sourceNames))
	for _, sourceName := range sourceNames {
		found := false
		for _, layer := range project.Layers {
			if layer.Name == sourceName {
				layers = append(layers, layer)
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("%w: %s", ErrLayerMissing, sourceName)
		}
	}
	result, err := MergeLayers(ctx, layers...)
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
