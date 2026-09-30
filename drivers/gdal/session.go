//go:build native

package gdal

import (
	"context"
	"fmt"
	"sync"

	"gogis/internal/core"

	"github.com/airbusgeo/godal"
)

// AttributeSession keeps one GDAL dataset open for repeated attribute-page
// requests. Calls are serialized because OGR layer reading state is mutable;
// this avoids reopening and reparsing the source for every page while keeping
// the session safe when a UI callback and a selection request overlap.
type AttributeSession struct {
	mu                   sync.Mutex
	source               string
	dataset              *godal.Dataset
	featureCursorLayer   string
	featureNextID        uint64
	featureCursorReady   bool
	attributeCursorLayer string
	attributeNextID      uint64
	attributeCursorReady bool
	attributeTotals      map[string]int
	attributeSchemas     map[string][]core.Field
	layer                godal.Layer
	layerRequest         string
	layerName            string
	layerReady           bool
}

// OpenAttributeSession opens a reusable read session for attribute requests.
func OpenAttributeSession(source string) (*AttributeSession, error) {
	registerDrivers()
	dataset, err := godal.Open(source)
	if err != nil {
		return nil, fmt.Errorf("open %q: %w", source, err)
	}
	return &AttributeSession{
		source:           source,
		dataset:          dataset,
		attributeTotals:  make(map[string]int),
		attributeSchemas: make(map[string][]core.Field),
	}, nil
}

// OpenAllGeometryOnly reads all layers through the retained dataset. Callers
// can use the same session for the initial render snapshot and later
// attribute requests, avoiding a second GDAL dataset open for the source.
func (s *AttributeSession) OpenAllGeometryOnly(ctx context.Context) ([]core.Layer, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dataset == nil {
		return nil, fmt.Errorf("attribute session for %q is closed", s.source)
	}
	s.featureCursorReady = false
	s.attributeCursorReady = false
	return openAllDataset(ctx, s.dataset, s.source, false)
}

// OpenGeometryOnly reads just the requested layer from the retained dataset.
// This avoids materializing unrelated layers when the desktop input names one
// layer, while leaving the dataset available for later attribute requests.
func (s *AttributeSession) OpenGeometryOnly(ctx context.Context, layerName string) (core.Layer, error) {
	if err := ctx.Err(); err != nil {
		return core.Layer{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dataset == nil {
		return core.Layer{}, fmt.Errorf("attribute session for %q is closed", s.source)
	}
	s.featureCursorReady = false
	s.attributeCursorReady = false
	layer, _, err := s.layerForName(layerName)
	if err != nil {
		return core.Layer{}, err
	}
	return readLayerOptions(ctx, layer, false)
}

// OpenAttributePage reads one property page using the retained dataset.
func (s *AttributeSession) OpenAttributePage(ctx context.Context, layerName string, offset, limit int) (core.Layer, int, error) {
	if offset < 0 || limit <= 0 {
		return core.Layer{}, 0, fmt.Errorf("invalid attribute page: offset=%d limit=%d", offset, limit)
	}
	if err := ctx.Err(); err != nil {
		return core.Layer{}, 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return core.Layer{}, 0, err
	}
	if s.dataset == nil {
		return core.Layer{}, 0, fmt.Errorf("attribute session for %q is closed", s.source)
	}
	return s.openAttributePageCursor(ctx, layerName, offset, limit)
}

func (s *AttributeSession) openAttributePageCursor(ctx context.Context, layerName string, offset, limit int) (core.Layer, int, error) {
	// A failed or canceled page request may have advanced OGR's mutable layer
	// cursor. Do not let a later feature request reuse that stale position.
	s.featureCursorReady = false
	layer, resolvedLayerName, err := s.layerForName(layerName)
	if err != nil {
		s.attributeCursorReady = false
		return core.Layer{}, 0, err
	}
	layerName = resolvedLayerName
	sequential := s.attributeCursorReady && s.attributeCursorLayer == layerName &&
		uint64(offset)+1 == s.attributeNextID
	if !sequential && (s.attributeCursorReady || offset != 0) {
		// OGRSQL is faster for random page jumps on indexed formats such as
		// GeoPackage. Keep the cursor for the next request only after a page
		// zero or an actually adjacent page has been read.
		s.attributeCursorReady = false
		s.featureCursorReady = false
		return openAttributePageDataset(ctx, s.dataset, s.source, layerName, offset, limit)
	}
	total, totalKnown := s.attributeTotals[layerName]
	if !totalKnown {
		count, countErr := layer.FeatureCount()
		if countErr == nil && count >= 0 {
			total = count
			s.attributeTotals[layerName] = count
		} else {
			total = -1
		}
	}
	reset := !sequential
	startID := s.attributeNextID
	if reset {
		startID = 1
	}
	schema := s.attributeSchemas[layerName]
	result, nextID, err := readAttributePageLayerCursor(ctx, layer, offset, limit, startID, reset, schema)
	if err != nil {
		s.attributeCursorReady = false
		return core.Layer{}, 0, err
	}
	if len(schema) == 0 && len(result.Fields) > 0 {
		s.attributeSchemas[layerName] = append([]core.Field(nil), result.Fields...)
	}
	s.featureCursorReady = false
	s.attributeCursorLayer = layerName
	s.attributeNextID = nextID
	s.attributeCursorReady = true
	if !totalKnown && total < 0 {
		total = offset + len(result.Features)
	}
	return result, total, nil
}

// OpenFeature reads one selected feature using the retained dataset.
func (s *AttributeSession) OpenFeature(ctx context.Context, layerName string, featureID uint64) (core.Feature, error) {
	if featureID == 0 {
		return core.Feature{}, fmt.Errorf("feature ID must be positive")
	}
	if err := ctx.Err(); err != nil {
		return core.Feature{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return core.Feature{}, err
	}
	if s.dataset == nil {
		return core.Feature{}, fmt.Errorf("attribute session for %q is closed", s.source)
	}
	s.attributeCursorReady = false
	layer, resolvedLayerName, err := s.layerForName(layerName)
	if err != nil {
		return core.Feature{}, err
	}
	layerName = resolvedLayerName
	reset := !s.featureCursorReady || s.featureCursorLayer != layerName || featureID < s.featureNextID
	startID := s.featureNextID
	if reset {
		startID = 1
	}
	result, nextID, err := openFeatureLayer(ctx, layer, featureID, startID, reset)
	if err != nil {
		s.featureCursorReady = false
		return core.Feature{}, err
	}
	s.featureCursorLayer = layerName
	s.featureNextID = nextID
	s.featureCursorReady = true
	return result, nil
}

func (s *AttributeSession) layerForName(requestedName string) (godal.Layer, string, error) {
	if s.layerReady && s.layerRequest == requestedName {
		return s.layer, s.layerName, nil
	}
	layer, err := selectLayer(s.dataset, s.source, requestedName)
	if err != nil {
		return godal.Layer{}, "", err
	}
	resolvedName := requestedName
	if resolvedName == "" {
		resolvedName = layer.Name()
	}
	s.layer = layer
	s.layerRequest = requestedName
	s.layerName = resolvedName
	s.layerReady = true
	return layer, resolvedName, nil
}

// Close releases the retained GDAL dataset. It is safe to call more than once.
func (s *AttributeSession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dataset == nil {
		return nil
	}
	dataset := s.dataset
	s.dataset = nil
	return dataset.Close()
}
