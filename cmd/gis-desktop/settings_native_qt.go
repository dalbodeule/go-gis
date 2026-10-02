//go:build qt && native

package main

import (
	"context"
	"fmt"
	"strings"

	"gogis/internal/commands"
	"gogis/internal/core"
	"gogis/ui/qt/native"
)

func (r *demoRuntime) reloadLayerWithSettings(request layerSettingsRequest, readOnly bool, displayCRS, saveDestination string) error {
	r.mu.Lock()
	if r.service == nil {
		r.mu.Unlock()
		return fmt.Errorf("no project is loaded")
	}
	layers := r.service.ProjectLayerProperties()
	baseLayers := []core.Layer(nil)
	if !readOnly {
		baseLayers = r.service.ProjectLayersExcept(request.Name)
	}
	unavailable := make(map[string]unavailableSource, len(r.unavailableSources))
	for name, source := range r.unavailableSources {
		unavailable[name] = source
	}
	found := false
	sources := make([]vectorSourceSpec, 0, len(layers))
	newDisplayName := strings.TrimSpace(request.DisplayName)
	if newDisplayName == "" {
		r.mu.Unlock()
		return fmt.Errorf("display name must not be empty")
	}
	for index, layer := range layers {
		targetLayer := layer.Name == request.Name
		if targetLayer {
			layer.SourceCRS = request.SourceCRS
			layer.SourcePath = request.SourcePath
			layer.SourceLayerName = request.SourceLayerName
			layer.SourceEncoding = request.SourceEncoding
			layer.DisplayName = request.DisplayName
			layer.Visible = request.Visible
			layer.Style = request.Style
			layer.Labels = request.Labels
			layer.DisplayRule = request.DisplayRule
			found = true
		} else {
			otherName := layer.DisplayName
			if otherName == "" {
				otherName = layer.Name
			}
			if strings.EqualFold(otherName, newDisplayName) {
				r.mu.Unlock()
				return fmt.Errorf("%w: %s", commands.ErrLayerExists, newDisplayName)
			}
		}
		visible := layer.Visible
		missing, wasUnavailable := unavailable[layer.Name]
		allowUnavailable := wasUnavailable && missing.Path == layer.SourcePath &&
			missing.LayerName == layer.SourceLayerName && missing.Encoding == layer.SourceEncoding
		source := vectorSourceSpec{
			Path: layer.SourcePath, LayerName: layer.SourceLayerName, SourceCRS: layer.SourceCRS,
			FallbackCRS: layer.CRS.AuthorityCode, AllowUnavailable: allowUnavailable,
			Encoding: layer.SourceEncoding, Name: layer.Name, DisplayName: layer.DisplayName,
			Visible: &visible, Style: layer.Style, Labels: layer.Labels, DisplayRule: layer.DisplayRule,
		}
		if !readOnly {
			source.InsertAt = index
			source.InsertAtSet = true
		}
		if readOnly || targetLayer {
			sources = append(sources, source)
		}
	}
	if !found {
		r.mu.Unlock()
		return fmt.Errorf("layer %q not found", request.Name)
	}
	previousCancel := r.loadCancel
	ctx, cancel := context.WithCancel(context.Background())
	r.loadCancel = cancel
	r.loadGeneration++
	generation := r.loadGeneration
	r.mu.Unlock()
	if previousCancel != nil {
		previousCancel()
	}
	native.SetRenderStatus("Reopening layer source with selected encoding")
	go func() {
		var next *demoRuntime
		var err error
		if readOnly {
			next, err = loadReadOnlyDataRuntime(ctx, sources, displayCRS)
		} else {
			next, err = loadDataRuntimeSourcesWithCRS(ctx, sources, baseLayers, saveDestination, displayCRS)
			if err == nil {
				next.unavailableSources = make(map[string]unavailableSource, len(unavailable))
				for name, source := range unavailable {
					if name != request.Name {
						next.unavailableSources[name] = source
					}
				}
			}
		}
		r.mu.Lock()
		current := generation == r.loadGeneration
		if current {
			r.loadCancel = nil
		}
		r.mu.Unlock()
		if !current {
			if next != nil && next.closeAttributeSource != nil {
				next.closeAttributeSource()
			}
			return
		}
		if err != nil {
			native.SetRenderStatus("Layer reload failed: " + err.Error())
			r.publishLayerTree()
			return
		}
		view := workspaceViewFromViewport(native.CurrentViewport(), native.CurrentActiveLayer())
		next.workspaceView = &view
		r.replaceWithLoaded(next, generation)
		native.SetRenderStatus("Layer source reloaded")
	}()
	return nil
}
