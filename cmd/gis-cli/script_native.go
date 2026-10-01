//go:build native

package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"gogis/drivers/dxf"
	"gogis/drivers/gdal"
	geosdriver "gogis/drivers/geos"
	"gogis/internal/commands"
	"gogis/internal/core"
	"gogis/internal/scripting"
)

func runScript(ctx context.Context, options scriptOptions) error {
	reader := gdal.Reader{}
	layers := make([]core.Layer, 0, len(options.inputs))
	for index, input := range options.inputs {
		var loaded []core.Layer
		var err error
		if len(options.layers) == 0 {
			loaded, err = reader.OpenAll(ctx, input)
		} else {
			var layer core.Layer
			layer, err = reader.Open(ctx, input, options.layers[index])
			if err == nil {
				loaded = []core.Layer{layer}
			}
		}
		if err != nil {
			return fmt.Errorf("open script input %q: %w", input, err)
		}
		layers = append(layers, loaded...)
	}
	if len(layers) == 0 {
		return fmt.Errorf("script inputs contain no vector layers")
	}
	service, err := commands.NewProjectServiceWithLayers("Lua Script", layers[0].CRS, layers)
	if err != nil {
		return err
	}
	runtime := scripting.NewRuntime(service, dxf.Exporter{}, geosdriver.NewOperator())
	defer runtime.Close()
	if err := runtime.RunFile(ctx, options.script); err != nil {
		return err
	}
	if options.output == "" {
		return nil
	}
	project := service.Project()
	if options.outputLayer != "" {
		for _, layer := range project.Layers {
			if layer.Name == options.outputLayer {
				return (gdal.Writer{}).Write(ctx, options.output, layer)
			}
		}
		return fmt.Errorf("output layer %q not found", options.outputLayer)
	}
	switch strings.ToLower(filepath.Ext(options.output)) {
	case ".gpkg":
		return (gdal.Writer{}).WriteLayers(ctx, options.output, project.Layers)
	case ".shp":
		return fmt.Errorf("Shapefile output requires --output-layer")
	default:
		return fmt.Errorf("unsupported script output extension %q; use .gpkg, .shp, or omit --output", filepath.Ext(options.output))
	}
}
