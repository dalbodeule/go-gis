//go:build !native

package main

import (
	"context"
	"fmt"
)

func runPostGIS(context.Context, string, postGISOptions) error {
	return fmt.Errorf("PostGIS CLI requires a native build: use scripts/build.sh native")
}
