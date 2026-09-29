//go:build native

package geos

import (
	"context"
	"fmt"

	"gogis/internal/core"

	geoslib "github.com/twpayne/go-geos"
)

// Operator uses one GEOS context per operator instance. It is safe to use
// from one worker; create one operator per concurrent worker.
type Operator struct {
	context *geoslib.Context
}

// NewOperator creates a GEOS operator with a dedicated context.
func NewOperator() *Operator {
	return &Operator{context: geoslib.NewContext()}
}

func (o *Operator) Intersect(ctx context.Context, left, right core.Layer) (core.Layer, error) {
	return o.binary(ctx, left, right, func(a, b *geoslib.Geom) *geoslib.Geom { return a.Intersection(b) })
}

func (o *Operator) Union(ctx context.Context, left, right core.Layer) (core.Layer, error) {
	return o.binary(ctx, left, right, func(a, b *geoslib.Geom) *geoslib.Geom { return a.Union(b) })
}

func (o *Operator) Difference(ctx context.Context, left, right core.Layer) (core.Layer, error) {
	return o.binary(ctx, left, right, func(a, b *geoslib.Geom) *geoslib.Geom { return a.Difference(b) })
}

func (o *Operator) Buffer(ctx context.Context, layer core.Layer, distance float64) (core.Layer, error) {
	result := layer.Clone()
	for i := range result.Features {
		if err := ctx.Err(); err != nil {
			return core.Layer{}, err
		}
		input, err := o.read(result.Features[i].Geometry)
		if err != nil {
			return core.Layer{}, err
		}
		output := input.Buffer(distance, 8)
		input.Destroy()
		result.Features[i].Geometry = core.WKTGeometry{WKT: output.ToWKT()}
		output.Destroy()
	}
	return result, nil
}

func (o *Operator) binary(ctx context.Context, left, right core.Layer, operation func(*geoslib.Geom, *geoslib.Geom) *geoslib.Geom) (core.Layer, error) {
	if len(left.Features) != len(right.Features) {
		return core.Layer{}, fmt.Errorf("binary GEOS operation requires equal feature counts")
	}
	result := left.Clone()
	for i := range result.Features {
		if err := ctx.Err(); err != nil {
			return core.Layer{}, err
		}
		a, err := o.read(left.Features[i].Geometry)
		if err != nil {
			return core.Layer{}, err
		}
		b, err := o.read(right.Features[i].Geometry)
		if err != nil {
			a.Destroy()
			return core.Layer{}, err
		}
		output := operation(a, b)
		a.Destroy()
		b.Destroy()
		result.Features[i].Geometry = core.WKTGeometry{WKT: output.ToWKT()}
		output.Destroy()
	}
	return result, nil
}

func (o *Operator) read(geometry core.Geometry) (*geoslib.Geom, error) {
	wkt, ok := geometry.(core.WKTGeometry)
	if !ok {
		return nil, fmt.Errorf("geometry is not core.WKTGeometry")
	}
	return o.context.NewGeomFromWKT(wkt.WKT)
}
