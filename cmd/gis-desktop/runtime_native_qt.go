//go:build qt && native

package main

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"gogis/drivers/gdal"
	"gogis/drivers/proj"
	"gogis/internal/commands"
	"gogis/internal/core"
	"gogis/internal/render"
	"gogis/ui/qt/native"
)

func loadRuntime(args []string) *demoRuntime {
	input, layerName, sourceCRS, targetCRS := desktopInputArgs(args)
	savePath := desktopSavePath(args)
	if input == "" {
		return loadDemoChunk()
	}
	runtime, err := loadDataRuntime(input, layerName, sourceCRS, targetCRS, savePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "GoGIS: unable to load %q: %v\n", input, err)
		return loadDemoChunk()
	}
	runtime.refresh(context.Background(), render.Viewport{Center: render.Point{X: 0.5, Y: 0.5}, Zoom: 1})
	runtime.publishLayerTree()
	if layers := runtime.service.Project().Layers; len(layers) > 0 {
		runtime.publishAttributes(layers[0].Name)
	}
	return runtime
}

func loadDataRuntime(input, layerName, sourceCRS, targetCRS, savePath string) (*demoRuntime, error) {
	layers, err := (gdal.Reader{}).OpenAll(context.Background(), input)
	if err != nil {
		return nil, fmt.Errorf("open dataset: %w", err)
	}
	if layerName != "" {
		filtered := layers[:0]
		for _, layer := range layers {
			if layer.Name == layerName {
				filtered = append(filtered, layer)
			}
		}
		if len(filtered) == 0 {
			return nil, fmt.Errorf("layer %q not found", layerName)
		}
		layers = filtered
	}
	if sourceCRS != "" {
		for index := range layers {
			layers[index].CRS = core.CRS{AuthorityCode: sourceCRS}
		}
	}
	layers, err = alignLayerCRS(context.Background(), layers, targetCRS)
	if err != nil {
		return nil, fmt.Errorf("align CRS: %w", err)
	}
	sources, sourceErr := render.NewLayerSources(layers)
	if sourceErr != nil {
		return nil, fmt.Errorf("prepare layers: %w", sourceErr)
	}
	features := make([]render.HitFeature, 0)
	layerNames := make([]string, 0, len(layers))
	for _, layer := range layers {
		source := sources[layer.Name]
		sources[layer.Name] = source
		features = append(features, source.Features...)
		layerNames = append(layerNames, layer.Name)
	}
	sort.Strings(layerNames)
	planner := render.NewChunkPlanner()
	if len(layers) > 0 {
		planner.ChunkSize = sources[layers[0].Name].ChunkSize
	}
	loadedService := newLoadedProjectService(layers)
	runtime := &demoRuntime{
		scheduler:  render.NewScheduler(),
		batchStore: render.NewBatchStore(),
		planner:    planner,
		visibility: render.NewLayerVisibility(layerNames...),
		features:   features,
		hitIndex:   render.NewHitIndex(features, 0.01),
		service:    loadedService,
		dataMode:   true,
	}
	runtime.builder = func(ctx context.Context, key render.ChunkKey) (render.Chunk, error) {
		source, ok := sources[key.Layer]
		if !ok {
			return render.Chunk{}, fmt.Errorf("render source for layer %q is missing", key.Layer)
		}
		return source.Builder(ctx, key)
	}
	if savePath != "" {
		writer := gdal.Writer{}
		runtime.persist = func(ctx context.Context, layerName string) error {
			return runtime.service.SaveLayer(ctx, writer, savePath, layerName)
		}
	}
	return runtime, nil
}

func (r *demoRuntime) replaceWith(next *demoRuntime) {
	r.cancelCurrentRender()
	r.mu.Lock()
	r.scheduler = next.scheduler
	r.batchStore = next.batchStore
	r.planner = next.planner
	r.visibility = next.visibility
	r.builder = next.builder
	r.features = next.features
	r.hitIndex = next.hitIndex
	r.service = next.service
	r.dataMode = next.dataMode
	r.persist = next.persist
	r.hasSelect = false
	r.mu.Unlock()
	native.SetSelection("", "", "", "Loaded")
	r.refresh(context.Background(), render.Viewport{Center: render.Point{X: 0.5, Y: 0.5}, Zoom: 1})
	r.publishLayerTree()
	if layers := r.service.Project().Layers; len(layers) > 0 {
		r.publishAttributes(layers[0].Name)
	}
}

func alignLayerCRS(ctx context.Context, layers []core.Layer, targetAuthority string) ([]core.Layer, error) {
	target := core.CRS{AuthorityCode: targetAuthority}
	if target.AuthorityCode == "" {
		for _, layer := range layers {
			if layer.CRS.AuthorityCode != "" {
				target = layer.CRS
				break
			}
		}
	}
	if target.AuthorityCode == "" {
		return layers, nil
	}
	transformer := proj.Transformer{}
	aligned := make([]core.Layer, len(layers))
	for index, layer := range layers {
		if layer.CRS.AuthorityCode == "" {
			return nil, fmt.Errorf("layer %q has no CRS while target is %s", layer.Name, target.AuthorityCode)
		}
		if strings.EqualFold(layer.CRS.AuthorityCode, target.AuthorityCode) {
			aligned[index] = layer
			continue
		}
		converted, err := transformer.Transform(ctx, layer.CRS, target, layer)
		if err != nil {
			return nil, fmt.Errorf("transform layer %q from %s to %s: %w", layer.Name, layer.CRS.AuthorityCode, target.AuthorityCode, err)
		}
		aligned[index] = converted
	}
	return aligned, nil
}

func desktopSavePath(args []string) string {
	for index := 1; index < len(args); index++ {
		if args[index] == "--save" && index+1 < len(args) {
			return args[index+1]
		}
	}
	return ""
}

func desktopInputArgs(args []string) (string, string, string, string) {
	input, layer, sourceCRS, targetCRS := "", "", "", ""
	for index := 1; index < len(args); index++ {
		switch args[index] {
		case "--input":
			if index+1 < len(args) {
				input = args[index+1]
				index++
			}
		case "--layer":
			if index+1 < len(args) {
				layer = args[index+1]
				index++
			}
		case "--source-crs":
			if index+1 < len(args) {
				sourceCRS = args[index+1]
				index++
			}
		case "--target-crs":
			if index+1 < len(args) {
				targetCRS = args[index+1]
				index++
			}
		}
	}
	return input, layer, sourceCRS, targetCRS
}

func newLoadedProjectService(layers []core.Layer) *commands.ProjectService {
	if len(layers) == 0 {
		return commands.NewProjectService("loaded", core.CRS{})
	}
	service := commands.NewProjectService("loaded", layers[0].CRS)
	if err := service.BeginEdit(); err != nil {
		return service
	}
	for _, layer := range layers {
		if err := service.AddLayer(layer); err != nil {
			_ = service.Rollback()
			return service
		}
	}
	if err := service.Commit(); err != nil {
		_ = service.Rollback()
	}
	return service
}
