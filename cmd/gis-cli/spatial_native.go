//go:build native

package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"gogis/drivers/dxf"
	"gogis/drivers/gdal"
	"gogis/drivers/geos"
	"gogis/internal/commands"
	"gogis/internal/core"
)

func runSpatial(ctx context.Context, options spatialOptions) error {
	left, err := (gdal.Reader{}).Open(ctx, options.input, options.layer)
	if err != nil {
		return err
	}
	right := core.Layer{}
	if options.operation != "buffer" {
		right, err = (gdal.Reader{}).Open(ctx, options.rightInput, options.rightLayer)
		if err != nil {
			return err
		}
		if left.CRS.AuthorityCode != "" && right.CRS.AuthorityCode != "" &&
			!strings.EqualFold(left.CRS.AuthorityCode, right.CRS.AuthorityCode) {
			return fmt.Errorf("spatial inputs use different CRS: %s and %s", left.CRS.AuthorityCode, right.CRS.AuthorityCode)
		}
	}
	result, err := commands.ApplySpatialOperation(ctx, geos.NewOperator(), options.operation, left, right, options.distance)
	if err != nil {
		return err
	}
	result.Name = left.Name + "_" + options.operation
	if strings.EqualFold(filepath.Ext(options.output), ".dxf") {
		return (dxf.Exporter{}).Export(ctx, options.output, result, options.profile)
	}
	return (gdal.Writer{}).Write(ctx, options.output, result)
}
