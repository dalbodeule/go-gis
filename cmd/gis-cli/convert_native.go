//go:build native

package main

import (
	"context"
	"fmt"
	"strings"

	"gogis/drivers/dxf"
	"gogis/drivers/gdal"
	"gogis/drivers/proj"
	"gogis/internal/core"
)

func runConvert(ctx context.Context, options convertOptions) error {
	layer, err := (gdal.Reader{}).Open(ctx, options.input, options.layer)
	if err != nil {
		return err
	}
	if options.sourceCRS != "" {
		layer.CRS = core.CRS{AuthorityCode: options.sourceCRS}
	}
	if layer.CRS.AuthorityCode == "" {
		return fmt.Errorf("input CRS is missing; provide --source-crs")
	}
	if options.targetCRS != "" && !strings.EqualFold(options.targetCRS, layer.CRS.AuthorityCode) {
		layer, err = (proj.Transformer{}).Transform(ctx, layer.CRS, core.CRS{AuthorityCode: options.targetCRS}, layer)
		if err != nil {
			return err
		}
	}
	if err := (dxf.Exporter{}).Export(ctx, options.output, layer, options.profile); err != nil {
		return err
	}
	return nil
}
