//go:build native

package gdal

import (
	"context"
	"fmt"
	"sync"
	"time"

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
	streamGeoJSON        bool
	streamIndexLimit     int
	streamOverview       []LayerOverview
	streamOverviewReady  bool
	streamIndex          []geoJSONFeatureIndex
	streamTailBlocks     []geoJSONTailBlock
	streamIndexReady     bool
	streamIndexStamp     geoJSONFileStamp
	layer                godal.Layer
	layerRequest         string
	layerName            string
	layerReady           bool
}

// lockContext waits for the serialized GDAL session lock without making a
// canceled UI/request context wait behind an unrelated long-running scan.
func (s *AttributeSession) lockContext(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.mu.TryLock() {
		if err := ctx.Err(); err != nil {
			s.mu.Unlock()
			return err
		}
		return nil
	}
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		if s.mu.TryLock() {
			if err := ctx.Err(); err != nil {
				s.mu.Unlock()
				return err
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// OpenAttributeSession opens a reusable read session for attribute requests.
func OpenAttributeSession(source string, encoding ...string) (*AttributeSession, error) {
	if isGeoJSONStreamSource(source) {
		return &AttributeSession{
			source:           source,
			streamGeoJSON:    true,
			streamIndexLimit: maxGeoJSONInMemoryIndexFeatures,
			attributeTotals:  make(map[string]int),
			attributeSchemas: make(map[string][]core.Field),
		}, nil
	}
	registerDrivers()
	selectedEncoding := ""
	if len(encoding) > 0 {
		selectedEncoding = encoding[0]
	}
	dataset, err := openDataset(source, selectedEncoding)
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

// SetGeoJSONStreamIndexFeatureLimit bounds the number of per-feature entries
// retained for a streamed GeoJSON source. It must be called before Inspect or
// any operation that builds the stream index. Non-GeoJSON sessions ignore the
// limit and report applied=false.
func (s *AttributeSession) SetGeoJSONStreamIndexFeatureLimit(limit int) (applied bool, err error) {
	if limit < 0 || limit > maxGeoJSONInMemoryIndexFeatures {
		return false, fmt.Errorf("GeoJSON stream index feature limit must be between 0 and %d", maxGeoJSONInMemoryIndexFeatures)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.streamGeoJSON {
		return false, nil
	}
	if s.streamIndexReady {
		if s.streamIndexLimit == limit {
			return true, nil
		}
		return true, fmt.Errorf("GeoJSON stream index is already initialized")
	}
	s.streamIndexLimit = limit
	return true, nil
}

// OpenAllGeometryOnly reads all layers through the retained dataset. Callers
// can use the same session for the initial render snapshot and later
// attribute requests, avoiding a second GDAL dataset open for the source.
func (s *AttributeSession) OpenAllGeometryOnly(ctx context.Context) ([]core.Layer, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := s.lockContext(ctx); err != nil {
		return nil, err
	}
	defer s.mu.Unlock()
	if s.streamGeoJSON {
		layer, err := readGeoJSONSourceSnapshot(ctx, s.source, "", false)
		if err != nil {
			return nil, err
		}
		return []core.Layer{layer}, nil
	}
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
	if err := s.lockContext(ctx); err != nil {
		return core.Layer{}, err
	}
	defer s.mu.Unlock()
	if s.streamGeoJSON {
		layer, err := readGeoJSONSourceSnapshot(ctx, s.source, layerName, false)
		if err != nil {
			return core.Layer{}, err
		}
		if layerName != "" && layerName != layer.Name {
			return core.Layer{}, fmt.Errorf("layer %q not found in %q", layerName, s.source)
		}
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
	return readMaterializedLayer(ctx, layer, false)
}

// OpenWindowGeometryOnly reads only geometries intersecting bounds through
// the retained dataset. Bounds are in the source layer's CRS. This shares the
// session lock with attribute requests because OGR layer cursors are mutable.
func (s *AttributeSession) OpenWindowGeometryOnly(ctx context.Context, layerName string, bounds [4]float64) (core.Layer, error) {
	return s.OpenWindow(ctx, layerName, bounds, false)
}

// OpenWindow reads features intersecting bounds through the retained dataset.
// Bounds are in the source layer's CRS. Calls share the attribute session lock
// because OGR layer cursors are mutable.
func (s *AttributeSession) OpenWindow(ctx context.Context, layerName string, bounds [4]float64, includeProperties bool) (core.Layer, error) {
	return s.OpenWindowWithLimits(ctx, layerName, bounds, includeProperties, maxMaterializedSnapshotFeatures, maxMaterializedSnapshotBytes)
}

// OpenWindowLimited reads at most maxFeatures intersecting features. A
// positive limit returns an error rather than silently truncating the window.
// Zero disables this feature-count budget; OpenWindow itself uses the standard
// snapshot feature and payload budgets.
func (s *AttributeSession) OpenWindowLimited(ctx context.Context, layerName string, bounds [4]float64, includeProperties bool, maxFeatures int) (core.Layer, error) {
	return s.OpenWindowWithLimits(ctx, layerName, bounds, includeProperties, maxFeatures, 0)
}

// OpenWindowWithLimits reads one spatial window subject to feature and
// estimated decoded-payload budgets. Exceeding either budget returns an error
// rather than truncating the result. A zero limit disables that budget.
func (s *AttributeSession) OpenWindowWithLimits(ctx context.Context, layerName string, bounds [4]float64, includeProperties bool, maxFeatures int, maxBytes int64) (core.Layer, error) {
	if err := validateSpatialWindow(bounds); err != nil {
		return core.Layer{}, err
	}
	if maxFeatures < 0 {
		return core.Layer{}, fmt.Errorf("window feature limit must not be negative")
	}
	if maxBytes < 0 {
		return core.Layer{}, fmt.Errorf("window byte limit must not be negative")
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
		if s.streamIndex == nil {
			layer, err := readGeoJSONSourceWindow(ctx, s.source, layerName, bounds, includeProperties, maxFeatures, maxBytes)
			if err != nil {
				return core.Layer{}, err
			}
			if err := validateGeoJSONSourceStamp(s.source, s.streamIndexStamp); err != nil {
				return core.Layer{}, err
			}
			return layer, nil
		}
		layer, err := readGeoJSONIndexedWindowWithTailBlocks(ctx, s.source, s.streamOverview[0], s.streamIndex,
			s.streamTailBlocks, bounds, includeProperties, maxFeatures, maxBytes)
		if err != nil {
			return core.Layer{}, err
		}
		if err := validateGeoJSONSourceStamp(s.source, s.streamIndexStamp); err != nil {
			return core.Layer{}, err
		}
		return layer, nil
	}
	if s.dataset == nil {
		return core.Layer{}, fmt.Errorf("attribute session for %q is closed", s.source)
	}
	s.featureCursorReady = false
	s.attributeCursorReady = false
	layer, resolvedLayerName, err := s.layerForName(layerName)
	if err != nil {
		return core.Layer{}, err
	}
	return openWindowLayerLimited(ctx, s.dataset, s.source, layer, resolvedLayerName, bounds, includeProperties, maxFeatures, maxBytes)
}

// OpenAttributePage reads one property page using the retained dataset.
func (s *AttributeSession) OpenAttributePage(ctx context.Context, layerName string, offset, limit int) (core.Layer, int, error) {
	if err := validateAttributePage(offset, limit); err != nil {
		return core.Layer{}, 0, err
	}
	if err := ctx.Err(); err != nil {
		return core.Layer{}, 0, err
	}
	if err := s.lockContext(ctx); err != nil {
		return core.Layer{}, 0, err
	}
	defer s.mu.Unlock()
	if s.streamGeoJSON {
		if !s.streamIndexReady {
			overviews, index, tailBlocks, stamp, err := inspectGeoJSONSourceWithIndexLimitAndTailBlocks(ctx, s.source, s.streamIndexLimit)
			if err != nil {
				return core.Layer{}, 0, err
			}
			s.streamOverview, s.streamIndex, s.streamTailBlocks, s.streamIndexStamp = overviews, index, tailBlocks, stamp
			s.streamOverviewReady, s.streamIndexReady = true, true
		}
		if err := validateGeoJSONSourceStamp(s.source, s.streamIndexStamp); err != nil {
			return core.Layer{}, 0, err
		}
		if s.streamIndex == nil {
			page, total, err := readGeoJSONSourceAttributePage(ctx, s.source, layerName, offset, limit)
			if err != nil {
				return core.Layer{}, 0, err
			}
			if err := validateGeoJSONSourceStamp(s.source, s.streamIndexStamp); err != nil {
				return core.Layer{}, 0, err
			}
			return page, total, nil
		}
		page, total, err := readGeoJSONIndexedAttributePage(ctx, s.source, layerName, offset, limit, s.streamIndex, s.streamOverview[0])
		if err != nil {
			return core.Layer{}, 0, err
		}
		if err := validateGeoJSONSourceStamp(s.source, s.streamIndexStamp); err != nil {
			return core.Layer{}, 0, err
		}
		return page, total, nil
	}
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
	if err := s.lockContext(ctx); err != nil {
		return core.Feature{}, err
	}
	defer s.mu.Unlock()
	if s.streamGeoJSON {
		if !s.streamIndexReady {
			overviews, index, tailBlocks, stamp, err := inspectGeoJSONSourceWithIndexLimitAndTailBlocks(ctx, s.source, s.streamIndexLimit)
			if err != nil {
				return core.Feature{}, err
			}
			s.streamOverview, s.streamIndex, s.streamTailBlocks, s.streamIndexStamp = overviews, index, tailBlocks, stamp
			s.streamOverviewReady, s.streamIndexReady = true, true
		}
		if err := validateGeoJSONSourceStamp(s.source, s.streamIndexStamp); err != nil {
			return core.Feature{}, err
		}
		if s.streamIndex == nil {
			feature, err := openGeoJSONSourceFeature(ctx, s.source, layerName, featureID)
			if err != nil {
				return core.Feature{}, err
			}
			if err := validateGeoJSONSourceStamp(s.source, s.streamIndexStamp); err != nil {
				return core.Feature{}, err
			}
			return feature, nil
		}
		if featureID > uint64(len(s.streamIndex)) {
			var feature core.Feature
			var err error
			if len(s.streamTailBlocks) > 0 && len(s.streamOverview) > 0 {
				feature, err = readGeoJSONTailFeatureWithBlocks(ctx, s.source, layerName, featureID,
					s.streamOverview[0], s.streamTailBlocks)
			} else {
				feature, err = openGeoJSONSourceFeature(ctx, s.source, layerName, featureID)
			}
			if err != nil {
				return core.Feature{}, err
			}
			if err := validateGeoJSONSourceStamp(s.source, s.streamIndexStamp); err != nil {
				return core.Feature{}, err
			}
			return feature, nil
		}
		feature, err := readGeoJSONIndexedFeature(ctx, s.source, layerName, featureID, s.streamIndex, s.streamOverview[0].Name)
		if err != nil {
			return core.Feature{}, err
		}
		if err := validateGeoJSONSourceStamp(s.source, s.streamIndexStamp); err != nil {
			return core.Feature{}, err
		}
		return feature, nil
	}
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
		s.streamGeoJSON = false
		s.streamOverview = nil
		s.streamIndex = nil
		s.streamTailBlocks = nil
		s.streamOverviewReady = false
		s.streamIndexReady = false
		return nil
	}
	dataset := s.dataset
	s.dataset = nil
	return dataset.Close()
}
