//go:build qt && !native

package main

import (
	"fmt"

	"gogis/ui/qt/native"
)

func loadRuntime(_ []string) *demoRuntime {
	return loadEmptyProject()
}

func startInitialDataLoad(_ *demoRuntime, _ []string) {}

func (r *demoRuntime) startDataLoad(_, _, _, _, _ string, _ bool) {}

func (r *demoRuntime) startDataLoadPaths(_ []string) {
	native.SetRenderStatus("Add files requires the native GDAL build")
}

func (r *demoRuntime) startRemoveLayer(_ string) error {
	return fmt.Errorf("removing layers requires the native GDAL build")
}

func (r *demoRuntime) saveDataset(destination string) {
	native.SetRenderStatus("Save failed: build desktop with native GDAL support")
}

func (r *demoRuntime) reloadLayerWithSettings(_ layerSettingsRequest, _ bool, _, _ string) error {
	return fmt.Errorf("relinking layer sources requires the native GDAL build")
}

func (r *demoRuntime) rebuildEditedLayer(_ string) error {
	return fmt.Errorf("geometry editing requires the native GDAL build")
}
