//go:build qt && native

package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"gogis/drivers/postgis"
	"gogis/internal/core"
)

const postGISDSNEnvironment = "GOGIS_POSTGIS_DSN"

func loadPostGISRuntime(ctx context.Context, table, targetCRS string, readOnly bool) (*demoRuntime, error) {
	dsn := strings.TrimSpace(os.Getenv(postGISDSNEnvironment))
	if dsn == "" {
		return nil, fmt.Errorf("%s is required for PostGIS desktop loading", postGISDSNEnvironment)
	}
	store, err := postgis.Open(ctx, dsn, table)
	if err != nil {
		return nil, fmt.Errorf("open PostGIS table %q: %w", table, err)
	}
	layer, err := store.ReadLayer(ctx)
	if err != nil {
		_ = store.Close()
		return nil, fmt.Errorf("read PostGIS table %q: %w", table, err)
	}
	layer.Name = table
	layer.DisplayName = table
	layer.SourcePath = "postgis://" + table
	layer.SourceLayerName = table
	layer.Editable = !readOnly
	layer.Visible = true
	runtime, err := buildDataRuntime(ctx, []core.Layer{layer}, "", "", targetCRS, "", "", readOnly, nil, nil)
	if err != nil {
		_ = store.Close()
		return nil, fmt.Errorf("prepare PostGIS table %q: %w", table, err)
	}
	runtime.closeAttributeSource = func() { _ = store.Close() }
	if !readOnly {
		service := runtime.service
		runtime.persist = func(ctx context.Context, layerName string) error {
			current, ok := service.Layer(layerName)
			if !ok {
				return fmt.Errorf("PostGIS layer %q is no longer loaded", layerName)
			}
			return store.SaveLayer(ctx, current)
		}
	}
	return runtime, nil
}
