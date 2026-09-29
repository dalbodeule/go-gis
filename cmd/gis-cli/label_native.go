//go:build native

package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"gogis/drivers/dxf"
	"gogis/drivers/gdal"
	"gogis/internal/commands"
)

func runLabel(ctx context.Context, options labelOptions) error {
	layer, err := (gdal.Reader{}).Open(ctx, options.input, options.layer)
	if err != nil {
		return err
	}
	result, err := commands.GenerateLabels(ctx, layer, options.field, options.height, options.style)
	if err != nil {
		return err
	}
	result.Name = layer.Name + "_labeled"
	if strings.EqualFold(filepath.Ext(options.output), ".dxf") {
		return (dxf.Exporter{}).Export(ctx, options.output, result, options.profile)
	}
	if ext := strings.ToLower(filepath.Ext(options.output)); ext != ".gpkg" && ext != ".shp" {
		return fmt.Errorf("unsupported label output extension %q; use .dxf, .gpkg, or .shp", ext)
	}
	return (gdal.Writer{}).Write(ctx, options.output, result)
}
