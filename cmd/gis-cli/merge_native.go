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
	"gogis/internal/core"
)

func runMerge(ctx context.Context, options mergeOptions) error {
	layers := make([]core.Layer, 0, len(options.inputs))
	var nextID uint64 = 1
	for index, input := range options.inputs {
		layer, err := (gdal.Reader{}).Open(ctx, input, options.layers[index])
		if err != nil {
			return err
		}
		// Reader IDs are local to each dataset because the format-independent
		// reader intentionally assigns IDs in read order. Make them unique
		// across the CLI merge inputs before applying the strict core merge.
		for featureIndex := range layer.Features {
			layer.Features[featureIndex].ID = nextID
			nextID++
		}
		layers = append(layers, layer)
	}
	result, err := commands.MergeLayers(ctx, layers...)
	if err != nil {
		return err
	}
	result.Name = "merged"
	if strings.EqualFold(filepath.Ext(options.output), ".dxf") {
		return (dxf.Exporter{}).Export(ctx, options.output, result, options.profile)
	}
	if ext := strings.ToLower(filepath.Ext(options.output)); ext != ".gpkg" && ext != ".shp" {
		return fmt.Errorf("unsupported merge output extension %q; use .dxf, .gpkg, or .shp", ext)
	}
	return (gdal.Writer{}).Write(ctx, options.output, result)
}
