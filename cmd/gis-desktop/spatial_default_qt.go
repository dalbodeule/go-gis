//go:build qt && !native

package main

import "fmt"

func applySpatialRequest(_ *demoRuntime, _ string) error {
	return fmt.Errorf("spatial operations require a native desktop build")
}
