//go:build !native

package main

import (
	"context"
	"errors"
)

func runScript(context.Context, scriptOptions) error {
	return errors.New("Lua scripting requires the native GDAL/PROJ/GEOS build; run scripts/build.sh native")
}
