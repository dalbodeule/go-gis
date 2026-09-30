//go:build qt && !native

package main

import "gogis/ui/qt/native"

func loadRuntime(_ []string) *demoRuntime {
	return loadDemoChunk()
}

func startInitialDataLoad(_ *demoRuntime, _ []string) {}

func (r *demoRuntime) startDataLoad(_, _, _, _, _ string, _ bool) {}

func (r *demoRuntime) startDataLoadPaths(_ []string) {
	native.SetRenderStatus("Add files requires the native GDAL build")
}

func (r *demoRuntime) saveDataset(destination string) {
	native.SetRenderStatus("Save failed: build desktop with native GDAL support")
}
