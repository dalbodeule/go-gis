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

func runFilter(ctx context.Context, options filterOptions) error {
	layer, err := (gdal.Reader{}).Open(ctx, options.input, options.layer)
	if err != nil {
		return err
	}
	result, err := commands.FilterLayerByProperty(ctx, layer, options.field, options.value)
	if err != nil {
		return err
	}
	result.Name = layer.Name + "_filtered"
	mappings, err := parseFieldMappings(options.fieldMap)
	if err != nil {
		return err
	}
	result, err = commands.RemapFields(result, mappings)
	if err != nil {
		return err
	}
	if strings.EqualFold(filepath.Ext(options.output), ".dxf") {
		return (dxf.Exporter{}).Export(ctx, options.output, result, options.profile)
	}
	if strings.EqualFold(filepath.Ext(options.output), ".csv") {
		return writeLayerCSV(options.output, result)
	}
	if ext := strings.ToLower(filepath.Ext(options.output)); ext != ".gpkg" && ext != ".shp" {
		return fmt.Errorf("unsupported filter output extension %q; use .csv, .dxf, .gpkg, or .shp", ext)
	}
	return (gdal.Writer{}).Write(ctx, options.output, result)
}
