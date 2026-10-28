//go:build qt && !native

package main

import "fmt"

func applySpatialRequest(_ *demoRuntime, _ string) error {
	return fmt.Errorf("spatial operations require a native desktop build")
}

func splitLayerByBoundary(_ *demoRuntime, _ layerSettingsRequest) error {
	return fmt.Errorf("boundary splitting requires a native desktop build")
}
