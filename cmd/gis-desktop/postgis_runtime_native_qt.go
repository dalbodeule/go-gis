//go:build qt && native

package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"gogis/drivers/postgis"
	"gogis/drivers/proj"
	"gogis/internal/core"
	"gogis/internal/render"
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
	info, err := store.Describe(ctx)
	if err != nil {
		_ = store.Close()
		return nil, fmt.Errorf("describe PostGIS table %q: %w", table, err)
	}
	// Large PostGIS tables always use the window runtime. A read-only launch
	// must not fall back to materializing the full table just because editing
	// was already disabled by the caller.
	if info.FeatureCount >= largeDatasetReadOnlyThreshold {
		return loadLargePostGISRuntime(ctx, store, table, targetCRS)
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

func loadLargePostGISRuntime(ctx context.Context, store *postgis.Store, table, targetCRS string) (*demoRuntime, error) {
	info, err := store.Describe(ctx)
	if err != nil {
		_ = store.Close()
		return nil, fmt.Errorf("describe PostGIS table %q: %w", table, err)
	}
	sourceCRS := info.CRS.AuthorityCode
	if sourceCRS == "" {
		_ = store.Close()
		return nil, fmt.Errorf("PostGIS table %q has no usable CRS", table)
	}
	if targetCRS == "" {
		targetCRS = sourceCRS
	}
	targetBounds := info.Bounds
	if !strings.EqualFold(sourceCRS, targetCRS) {
		targetBounds, err = (proj.Transformer{}).TransformBounds(ctx,
			core.CRS{AuthorityCode: sourceCRS}, core.CRS{AuthorityCode: targetCRS}, info.Bounds)
		if err != nil {
			_ = store.Close()
			return nil, fmt.Errorf("transform PostGIS extent %q: %w", table, err)
		}
	}
	page, _, err := store.OpenAttributePage(ctx, table, 0, 1)
	if err != nil {
		_ = store.Close()
		return nil, fmt.Errorf("read PostGIS schema %q: %w", table, err)
	}
	layer := core.Layer{
		Name:            table,
		DisplayName:     table,
		Fields:          page.Fields,
		SourcePath:      "postgis://" + table,
		SourceLayerName: table,
		SourceCRS:       sourceCRS,
		CRS:             core.CRS{AuthorityCode: targetCRS},
		Visible:         true,
		Editable:        false,
		Style:           core.DefaultLayerStyle(),
		Labels:          core.DefaultLabelSettings(),
	}
	runtime, err := buildDataRuntime(ctx, []core.Layer{layer}, "postgis://"+table, "", targetCRS, "", "", true, nil, &targetBounds)
	if err != nil {
		_ = store.Close()
		return nil, fmt.Errorf("prepare large PostGIS table %q: %w", table, err)
	}
	runtime.viewportReadOnly = true
	runtime.preserveReadOnlyFeatureIDs = true
	runtime.layerBounds[layer.Name] = targetBounds
	runtime.layerFeatureCounts[layer.Name] = info.FeatureCount
	runtime.layerGeometryTypes[layer.Name] = info.GeometryType
	runtime.mapFitExtent = targetBounds
	runtime.readOnlyBindings = map[string]readOnlyLayerBinding{
		layer.Name: {session: store, sourceName: table, layer: layer, sourceCRS: sourceCRS, featureCount: info.FeatureCount},
	}
	runtime.readOnlyBaseLayers = []core.Layer{layer}
	runtime.readOnlyDisplayCRS = targetCRS
	runtime.readOnlySources = []vectorSourceSpec{{Path: "postgis://" + table, LayerName: table, SourceCRS: sourceCRS}}
	runtime.scheduler = render.NewSchedulerWithMaxWorkers(readOnlyOverviewWorkerLimit())
	runtime.windowHits = make(map[render.ChunkKey][]render.HitFeature)
	runtime.windowFeatureCounts = make(map[render.ChunkKey]int)
	runtime.windowFeatureIDs = make(map[render.ChunkKey][]uint64)
	runtime.windowPayloadBytes = make(map[render.ChunkKey]int64)
	runtime.windowVisibleKeys = make(map[render.ChunkKey]struct{})
	runtime.windowLabels = make(map[render.ChunkKey][]render.LayerLabel)
	runtime.attributePageReader = func(ctx context.Context, layerName string, offset, limit int) (core.Layer, int, error) {
		if layerName != layer.Name {
			return core.Layer{}, 0, fmt.Errorf("layer %q not found", layerName)
		}
		return store.OpenAttributePage(ctx, table, offset, limit)
	}
	runtime.attributeFeatureReader = func(ctx context.Context, layerName string, featureID uint64) (core.Feature, error) {
		if layerName != layer.Name {
			return core.Feature{}, fmt.Errorf("layer %q not found", layerName)
		}
		return store.OpenFeature(ctx, table, featureID)
	}
	runtime.readOnlyBaseBuilder = runtime.builder
	runtime.builder = runtime.buildReadOnlyWindowChunk
	runtime.closeAttributeSource = func() { _ = store.Close() }
	return runtime, nil
}
