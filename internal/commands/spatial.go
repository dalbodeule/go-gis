package commands

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gogis/internal/core"
)

// SpatialOperator is the command-layer boundary for GEOS-like operations.
// Keeping this interface here lets GUI, CLI, and Lua use the same dispatch
// without importing a native geometry package.
type SpatialOperator interface {
	Intersect(ctx context.Context, left, right core.Layer) (core.Layer, error)
	Union(ctx context.Context, left, right core.Layer) (core.Layer, error)
	Difference(ctx context.Context, left, right core.Layer) (core.Layer, error)
	Buffer(ctx context.Context, layer core.Layer, distance float64) (core.Layer, error)
}

var (
	ErrSpatialOperatorMissing  = errors.New("spatial operator is required")
	ErrUnknownSpatialOperation = errors.New("unknown spatial operation")
	ErrSpatialCRSMismatch      = errors.New("spatial operation requires matching CRS")
)

// ApplySpatialOperation dispatches one format-independent spatial command.
// Binary operations require both layers; buffer uses only left and ignores
// right. The returned layer is detached from the inputs by the operator
// contract.
func ApplySpatialOperation(ctx context.Context, operator SpatialOperator, operation string, left, right core.Layer, distance float64) (core.Layer, error) {
	if err := ctx.Err(); err != nil {
		return core.Layer{}, err
	}
	if operator == nil {
		return core.Layer{}, ErrSpatialOperatorMissing
	}
	if operation != "buffer" && left.CRS.AuthorityCode != "" && right.CRS.AuthorityCode != "" &&
		!strings.EqualFold(left.CRS.AuthorityCode, right.CRS.AuthorityCode) {
		return core.Layer{}, fmt.Errorf("%w: %s and %s", ErrSpatialCRSMismatch, left.CRS.AuthorityCode, right.CRS.AuthorityCode)
	}
	switch operation {
	case "intersect":
		return operator.Intersect(ctx, left, right)
	case "union":
		return operator.Union(ctx, left, right)
	case "difference":
		return operator.Difference(ctx, left, right)
	case "buffer":
		return operator.Buffer(ctx, left, distance)
	default:
		return core.Layer{}, fmt.Errorf("%w: %s", ErrUnknownSpatialOperation, operation)
	}
}

// ApplySpatialOperation computes a result and adds it atomically to the
// committed project under resultName. Source layers are read from the
// committed snapshot, so a failed operation cannot alter project state.
func (s *ProjectService) ApplySpatialOperation(ctx context.Context, operator SpatialOperator, operation, leftName, rightName, resultName string, distance float64) error {
	if resultName == "" {
		return errors.New("result layer name is required")
	}
	project := s.Project()
	var left, right core.Layer
	var foundLeft, foundRight bool
	for _, layer := range project.Layers {
		if layer.Name == leftName {
			left, foundLeft = layer, true
		}
		if layer.Name == rightName {
			right, foundRight = layer, true
		}
	}
	if !foundLeft {
		return fmt.Errorf("%w: %s", ErrLayerMissing, leftName)
	}
	if operation != "buffer" && !foundRight {
		return fmt.Errorf("%w: %s", ErrLayerMissing, rightName)
	}
	result, err := ApplySpatialOperation(ctx, operator, operation, left, right, distance)
	if err != nil {
		return err
	}
	result.Name = resultName
	result.Editable = true
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
