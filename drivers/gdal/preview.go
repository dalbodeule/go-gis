//go:build native

package gdal

import (
	"context"
	"fmt"
	"math"

	"gogis/internal/core"

	"github.com/airbusgeo/godal"
)

// LayerOverview describes a dataset layer without retaining any feature
// geometry. Bounds are in the layer's source CRS and use minX,minY,maxX,maxY.
type LayerOverview struct {
	Name         string
	CRS          core.CRS
	Bounds       [4]float64
	HasBounds    bool
	FeatureCount int
}

// Inspect returns layer metadata through the retained dataset. Bounds may be
// unavailable for empty or streaming layers; callers must check HasBounds.
func (s *AttributeSession) Inspect(ctx context.Context) ([]LayerOverview, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := s.lockContext(ctx); err != nil {
		return nil, err
	}
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.streamGeoJSON {
		if s.streamIndexReady && s.streamOverviewReady {
			if err := validateGeoJSONSourceStamp(s.source, s.streamIndexStamp); err != nil {
				return nil, err
			}
			return append([]LayerOverview(nil), s.streamOverview...), nil
		}
		result, index, tailBlocks, stamp, err := inspectGeoJSONSourceWithIndexLimitAndTailBlocks(ctx, s.source, s.streamIndexLimit)
		if err != nil {
			return nil, err
		}
		s.streamOverview = append([]LayerOverview(nil), result...)
		s.streamIndex, s.streamTailBlocks, s.streamIndexStamp = index, tailBlocks, stamp
		s.streamOverviewReady, s.streamIndexReady = true, true
		return result, nil
	}
	if s.dataset == nil {
		return nil, fmt.Errorf("attribute session for %q is closed", s.source)
	}
	layers := s.dataset.Layers()
	if len(layers) == 0 {
		return nil, fmt.Errorf("dataset %q contains no vector layers", s.source)
	}
	result := make([]LayerOverview, 0, len(layers))
	for _, layer := range layers {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		header := readLayerHeader(layer)
		overview := LayerOverview{Name: header.Name, CRS: header.CRS, FeatureCount: -1}
		if count, err := layer.FeatureCount(); err == nil {
			overview.FeatureCount = count
		}
		if bounds, err := layer.Bounds(); err == nil && validLayerBounds(bounds) {
			overview.Bounds = bounds
			overview.HasBounds = true
		}
		result = append(result, overview)
	}
	return result, nil
}

func validLayerBounds(bounds [4]float64) bool {
	for _, value := range bounds {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return false
		}
	}
	return bounds[0] <= bounds[2] && bounds[1] <= bounds[3]
}

// OpenGeometryPrefix reads up to limit features from one layer. IDs retain
// the same read-order numbering as a subsequent full snapshot; the returned
// prefix must not be treated as the complete layer.
func (s *AttributeSession) OpenGeometryPrefix(ctx context.Context, layerName string, limit int) (core.Layer, error) {
	if limit <= 0 {
		return core.Layer{}, fmt.Errorf("geometry prefix limit must be positive")
	}
	if err := ctx.Err(); err != nil {
		return core.Layer{}, err
	}
	if err := s.lockContext(ctx); err != nil {
		return core.Layer{}, err
	}
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return core.Layer{}, err
	}
	if s.streamGeoJSON {
		if !s.streamIndexReady {
			overviews, index, tailBlocks, stamp, err := inspectGeoJSONSourceWithIndexLimitAndTailBlocks(ctx, s.source, s.streamIndexLimit)
			if err != nil {
				return core.Layer{}, err
			}
			s.streamOverview, s.streamIndex, s.streamTailBlocks, s.streamIndexStamp = overviews, index, tailBlocks, stamp
			s.streamOverviewReady, s.streamIndexReady = true, true
		}
		if err := validateGeoJSONSourceStamp(s.source, s.streamIndexStamp); err != nil {
			return core.Layer{}, err
		}
		if len(s.streamOverview) == 0 || (layerName != "" && layerName != s.streamOverview[0].Name) {
			return core.Layer{}, fmt.Errorf("layer %q not found in %q", layerName, s.source)
		}
		layer, err := readGeoJSONSourceWindowPrefix(ctx, s.source, layerName, [4]float64{-math.MaxFloat64, -math.MaxFloat64, math.MaxFloat64, math.MaxFloat64}, false, 0, 0, limit)
		if err != nil {
			return core.Layer{}, err
		}
		if err := validateGeoJSONSourceStamp(s.source, s.streamIndexStamp); err != nil {
			return core.Layer{}, err
		}
		layer.Name = s.streamOverview[0].Name
		layer.CRS = s.streamOverview[0].CRS
		return layer, nil
	}
	if s.dataset == nil {
		return core.Layer{}, fmt.Errorf("attribute session for %q is closed", s.source)
	}
	s.featureCursorReady = false
	s.attributeCursorReady = false
	layer, _, err := s.layerForName(layerName)
	if err != nil {
		return core.Layer{}, err
	}
	return readLayerGeometryPrefix(ctx, layer, limit)
}

// OpenAllGeometryPrefix reads a bounded prefix of every layer through the
// same dataset, allowing a renderer to show a preview before the full read.
func (s *AttributeSession) OpenAllGeometryPrefix(ctx context.Context, limit int) ([]core.Layer, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("geometry prefix limit must be positive")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := s.lockContext(ctx); err != nil {
		return nil, err
	}
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.streamGeoJSON {
		if !s.streamIndexReady {
			overviews, index, tailBlocks, stamp, err := inspectGeoJSONSourceWithIndexLimitAndTailBlocks(ctx, s.source, s.streamIndexLimit)
			if err != nil {
				return nil, err
			}
			s.streamOverview, s.streamIndex, s.streamTailBlocks, s.streamIndexStamp = overviews, index, tailBlocks, stamp
			s.streamOverviewReady, s.streamIndexReady = true, true
		}
		if err := validateGeoJSONSourceStamp(s.source, s.streamIndexStamp); err != nil {
			return nil, err
		}
		layer, err := readGeoJSONSourceWindowPrefix(ctx, s.source, "", [4]float64{-math.MaxFloat64, -math.MaxFloat64, math.MaxFloat64, math.MaxFloat64}, false, 0, 0, limit)
		if err != nil {
			return nil, err
		}
		if err := validateGeoJSONSourceStamp(s.source, s.streamIndexStamp); err != nil {
			return nil, err
		}
		if len(s.streamOverview) > 0 {
			layer.Name = s.streamOverview[0].Name
			layer.CRS = s.streamOverview[0].CRS
		}
		return []core.Layer{layer}, nil
	}
	if s.dataset == nil {
		return nil, fmt.Errorf("attribute session for %q is closed", s.source)
	}
	layers := s.dataset.Layers()
	if len(layers) == 0 {
		return nil, fmt.Errorf("dataset %q contains no vector layers", s.source)
	}
	s.featureCursorReady = false
	s.attributeCursorReady = false
	result := make([]core.Layer, 0, len(layers))
	for _, layer := range layers {
		loaded, err := readLayerGeometryPrefix(ctx, layer, limit)
		if err != nil {
			return nil, err
		}
		result = append(result, loaded)
	}
	return result, nil
}

func readLayerGeometryPrefix(ctx context.Context, layer godal.Layer, limit int) (core.Layer, error) {
	result := readLayerHeader(layer)
	capacity := limit
	if count, err := layer.FeatureCount(); err == nil && count >= 0 && count < capacity {
		capacity = count
	}
	result.Features = make([]core.Feature, 0, capacity)
	layer.ResetReading()
	for index := 0; index < limit; index++ {
		if err := ctx.Err(); err != nil {
			return core.Layer{}, err
		}
		feature := layer.NextFeature()
		if feature == nil {
			break
		}
		loaded, err := readFeature(feature, uint64(index+1), false)
		feature.Close()
		if err != nil {
			return core.Layer{}, fmt.Errorf("read feature %d geometry: %w", index+1, err)
		}
		result.Features = append(result.Features, loaded)
	}
	return result, nil
}
