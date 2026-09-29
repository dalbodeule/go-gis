//go:build !native

package main

import (
	"context"
	"fmt"
)

func runSpatial(context.Context, spatialOptions) error {
	return fmt.Errorf("spatial requires a native build: use scripts/build.sh native")
}
