//go:build qt

package main

import (
	"context"
	"fmt"

	"gogis/internal/core"
)

// installInMemoryAttributeReaders keeps rebuilt layers independent of any
// source path: editable features may have unsaved attribute changes, and
// read-only base layers may never have had a source file at all.
func installInMemoryAttributeReaders(runtime *demoRuntime, _ []core.Layer) {
	runtime.attributePageReader = func(_ context.Context, layerName string, offset, limit int) (core.Layer, int, error) {
		page, total, ok := runtime.service.LayerAttributePageOwned(layerName, offset, limit)
		if !ok {
			return core.Layer{}, 0, fmt.Errorf("layer %q not found", layerName)
		}
		return page, total, nil
	}
	runtime.attributeFeatureReader = func(_ context.Context, layerName string, featureID uint64) (core.Feature, error) {
		if runtime.service != nil {
			if feature, ok := runtime.service.FeatureSnapshot(layerName, featureID); ok {
				return feature, nil
			}
		}
		return core.Feature{}, fmt.Errorf("feature %d not found in layer %q", featureID, layerName)
	}
}
