//go:build native

package gdal

import (
	"context"
	"fmt"
	"sync"

	"gogis/internal/core"

	"github.com/airbusgeo/godal"
)

// GeometrySession keeps one GDAL dataset open for repeated viewport-window
// requests. OGR layer reading state is mutable, so calls are serialized just
// like AttributeSession; callers can still issue requests from UI callbacks
// without racing the native dataset.
type GeometrySession struct {
	mu            sync.Mutex
	source        string
	dataset       *godal.Dataset
	streamSession *AttributeSession
	layer         godal.Layer
	layerName     string
	layerReady    bool
}

// OpenGeometrySession opens a reusable read-only window session. GeoJSON and
// GeoJSONSeq sources use the bounded streaming reader rather than GDAL's raw
// JSON driver to avoid dataset-sized native materialization.
func OpenGeometrySession(source string, encoding ...string) (*GeometrySession, error) {
	registerDrivers()
	selectedEncoding := ""
	if len(encoding) > 0 {
		selectedEncoding = encoding[0]
	}
	if isGeoJSONStreamSource(source) {
		streamSession, err := OpenAttributeSession(source, selectedEncoding)
		if err != nil {
			return nil, fmt.Errorf("open %q: %w", source, err)
		}
		return &GeometrySession{source: source, streamSession: streamSession}, nil
	}
	dataset, err := openDataset(source, selectedEncoding)
	if err != nil {
		return nil, fmt.Errorf("open %q: %w", source, err)
	}
	return &GeometrySession{source: source, dataset: dataset}, nil
}

// OpenWindow reads a window using the retained dataset.
func (s *GeometrySession) OpenWindow(ctx context.Context, layerName string, bounds [4]float64) (core.Layer, error) {
	return s.openWindow(ctx, layerName, bounds, true)
}

// OpenWindowGeometryOnly reads a window without materializing properties.
func (s *GeometrySession) OpenWindowGeometryOnly(ctx context.Context, layerName string, bounds [4]float64) (core.Layer, error) {
	return s.openWindow(ctx, layerName, bounds, false)
}

func (s *GeometrySession) openWindow(ctx context.Context, layerName string, bounds [4]float64, includeProperties bool) (core.Layer, error) {
	if err := validateSpatialWindow(bounds); err != nil {
		return core.Layer{}, err
	}
	if err := ctx.Err(); err != nil {
		return core.Layer{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.streamSession != nil {
		return s.streamSession.OpenWindowWithLimits(ctx, layerName, bounds, includeProperties,
			maxMaterializedSnapshotFeatures, maxMaterializedSnapshotBytes)
	}
	if s.dataset == nil {
		return core.Layer{}, fmt.Errorf("geometry session for %q is closed", s.source)
	}
	requestedLayer := layerName
	if !s.layerReady || s.layerName != requestedLayer {
		layer, err := selectLayer(s.dataset, s.source, requestedLayer)
		if err != nil {
			return core.Layer{}, err
		}
		s.layer = layer
		s.layerName = requestedLayer
		s.layerReady = true
	}
	return openWindowLayer(ctx, s.dataset, s.source, s.layer, requestedLayer, bounds, includeProperties)
}

// Close releases the retained GDAL dataset. It is safe to call repeatedly.
func (s *GeometrySession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.streamSession != nil {
		session := s.streamSession
		s.streamSession = nil
		return session.Close()
	}
	if s.dataset == nil {
		return nil
	}
	dataset := s.dataset
	s.dataset = nil
	return dataset.Close()
}
