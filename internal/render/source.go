package render

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"gogis/internal/core"
)

// LayerSource is a normalized, UI-neutral geometry source for a vector layer.
// Coordinates are mapped into the unit square so Qt and other adapters can
// apply viewport transforms without knowing the source CRS units.
type LayerSource struct {
	Features            []HitFeature
	Labels              []LayerLabel
	Builder             ChunkBuilder
	ChunkSize           float64
	Extent              [4]float64
	PolygonFillVertices int
	PolygonFillCapacity int
}

// MaxChunkVertices bounds retained render payload per spatial chunk before
// publication to UI/native adapters.
const MaxChunkVertices = 2 * 1024 * 1024

// MaxBatchVertices bounds the flattened viewport payload before adapters copy
// it into native memory. Qt can expand each source vertex into as many as three
// scene-graph vertices. The 12 Mi source budget allows larger municipality-wide
// parcel networks while keeping a strict bound on native copies and GPU geometry.
const MaxBatchVertices = 12 * 1024 * 1024

// maxFullLayerChunkGridAxis bounds work when a builder prepares every
// normalized cell. Single-target viewport builders do not use this full-grid
// path and remain free to request finer chunk sizes.
const maxFullLayerChunkGridAxis = 256

// AppendVertexBatchWithinLimit appends a vertex batch without exceeding limit.
// On rejection it returns current unchanged.
func AppendVertexBatchWithinLimit(current, batch []Vertex, limit int) ([]Vertex, bool) {
	if limit < 0 || len(current) > limit || len(batch) > limit-len(current) {
		return current, false
	}
	needed := len(current) + len(batch)
	if needed > cap(current) {
		newCapacity := cap(current) * 2
		if newCapacity < needed {
			newCapacity = needed
		}
		if newCapacity > limit {
			newCapacity = limit
		}
		grown := make([]Vertex, len(current), newCapacity)
		copy(grown, current)
		current = grown
	}
	return append(current, batch...), true
}

type LayerLabel struct {
	Layer     string  `json:"layer"`
	FeatureID uint64  `json:"featureId"`
	Text      string  `json:"text"`
	X         float64 `json:"x"`
	Y         float64 `json:"y"`
	Rotation  float64 `json:"rotation"`
	HeightMM  float64 `json:"heightMm"`
	MinScale  float64 `json:"minScale"`
	MaxScale  float64 `json:"maxScale"`
}

// parsedFeaturePoints keeps the common single-part representation flat. A
// large import is usually made up of Point/LineString features, so retaining
// an outer [][]Point for every feature adds one allocation without helping
// rendering or hit testing.
type parsedFeaturePoints struct {
	// A nil parts slice is the compact single-part representation. Empty and
	// multipart geometries use a non-nil parts slice.
	points []Point
	parts  [][]Point
}

const maxInitialPointArenaCapacity = 1 << 20

func boundedInitialPointArenaCapacity(capacity int) int {
	return min(max(0, capacity), maxInitialPointArenaCapacity)
}

func initialPartArenaCapacity(featureCount int) int {
	if featureCount <= 0 {
		return 0
	}
	if featureCount > int(^uint(0)>>1)/2 {
		return maxInitialPointArenaCapacity
	}
	return min(featureCount*2, maxInitialPointArenaCapacity)
}

// NewLayerSource converts Point, LineString, and Polygon WKT features into a
// normalized render source. The builder returns one immutable batch for the
// source layer; chunk scheduling and viewport policy remain adapter concerns.
func NewLayerSource(layer core.Layer) (LayerSource, error) {
	parsed, lineFlags, minX, minY, maxX, maxY, err := parseLayerPoints(layer)
	if err != nil {
		return LayerSource{}, err
	}
	bounds := paddedDegenerateExtent([4]float64{minX, minY, maxX, maxY}, layer.CRS.AuthorityCode)
	minX, minY, maxX, maxY = bounds[0], bounds[1], bounds[2], bounds[3]
	return newLayerSource(layer, parsed, lineFlags, minX, minY, maxX, maxY, 0.25, nil, nil, false), nil
}

// NewLayerSources creates sources using one common extent. This is required
// for multi-layer maps: independently normalizing each layer would make
// layers with different source bounds overlap in the renderer.
func NewLayerSources(layers []core.Layer) (map[string]LayerSource, error) {
	sources, _, err := NewLayerSourcesWithFeatures(layers)
	return sources, err
}

// NewLayerSourcesWithFeatures returns the same normalized sources and one
// contiguous feature view for hit testing. The returned feature slice shares
// storage with each source's Features slice; callers must treat both as
// immutable after construction.
func NewLayerSourcesWithFeatures(layers []core.Layer) (map[string]LayerSource, []HitFeature, error) {
	return newLayerSourcesWithFeatures(layers, nil, 0.25, nil)
}

// LayerExtent calculates the padded source extent without retaining parsed
// coordinate arrays. It is useful when a render-only filter omits features
// but the map must keep the original layer framing.
func LayerExtent(layers []core.Layer) ([4]float64, error) {
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	crsCode := ""
	for _, layer := range layers {
		if crsCode == "" {
			crsCode = layer.CRS.AuthorityCode
		}
		for _, feature := range layer.Features {
			geometry, _, _, _, err := parseFeaturePointsInto(feature.Geometry, nil, nil)
			if err != nil {
				return [4]float64{}, fmt.Errorf("layer %q feature %d: %w", layer.Name, feature.ID, err)
			}
			if geometry.parts == nil {
				if err := updateExtent(geometry.points, &minX, &minY, &maxX, &maxY); err != nil {
					return [4]float64{}, err
				}
			} else {
				for _, part := range geometry.parts {
					if err := updateExtent(part, &minX, &minY, &maxX, &maxY); err != nil {
						return [4]float64{}, err
					}
				}
			}
		}
	}
	if math.IsInf(minX, 1) || math.IsInf(minY, 1) || math.IsInf(maxX, -1) || math.IsInf(maxY, -1) {
		return [4]float64{0, 0, 1, 1}, nil
	}
	return paddedDegenerateExtent([4]float64{minX, minY, maxX, maxY}, crsCode), nil
}

// NewLayerSourcesWithExtent normalizes a partial layer snapshot against the
// complete dataset extent. Preview and full sources can therefore use the
// same world coordinates even when the preview contains only a prefix.
func NewLayerSourcesWithExtent(layers []core.Layer, bounds [4]float64) (map[string]LayerSource, []HitFeature, error) {
	for _, value := range bounds {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return nil, nil, fmt.Errorf("invalid source extent %v", bounds)
		}
	}
	if bounds[0] > bounds[2] || bounds[1] > bounds[3] {
		return nil, nil, fmt.Errorf("invalid source extent %v", bounds)
	}
	return newLayerSourcesWithFeatures(layers, &bounds, 0.25, nil)
}

// NewLayerSourcesWithExtentAndChunkSize is NewLayerSourcesWithExtent with a
// caller-selected normalized tile size. The size is part of cache semantics;
// callers must keep it stable for the returned builder's lifetime.
func NewLayerSourcesWithExtentAndChunkSize(layers []core.Layer, bounds [4]float64, chunkSize float64) (map[string]LayerSource, []HitFeature, error) {
	if math.IsNaN(chunkSize) || math.IsInf(chunkSize, 0) || chunkSize <= 0 || chunkSize > 1 {
		return nil, nil, fmt.Errorf("invalid render chunk size %v", chunkSize)
	}
	for _, value := range bounds {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return nil, nil, fmt.Errorf("invalid source extent %v", bounds)
		}
	}
	if bounds[0] > bounds[2] || bounds[1] > bounds[3] {
		return nil, nil, fmt.Errorf("invalid source extent %v", bounds)
	}
	return newLayerSourcesWithFeatures(layers, &bounds, chunkSize, nil)
}

// NewLayerSourcesWithExtentAndChunkSizeForChunk builds render vertices only
// for target.X/target.Y. Hit-test geometry still covers every feature returned
// by the caller's spatial query.
func NewLayerSourcesWithExtentAndChunkSizeForChunk(layers []core.Layer, bounds [4]float64, chunkSize float64, target ChunkKey) (map[string]LayerSource, []HitFeature, error) {
	return newLayerSourcesWithExtentAndChunkSizeForChunk(layers, bounds, chunkSize, target, false)
}

// NewLayerSourcesWithExtentAndChunkSizeForChunkDeduplicatedOutlines collapses
// shared polygon edges in an outline-only overview batch without dropping any
// source feature. Exact-detail source generation remains unchanged.
func NewLayerSourcesWithExtentAndChunkSizeForChunkDeduplicatedOutlines(layers []core.Layer, bounds [4]float64, chunkSize float64, target ChunkKey) (map[string]LayerSource, []HitFeature, error) {
	return newLayerSourcesWithExtentAndChunkSizeForChunk(layers, bounds, chunkSize, target, true)
}

func newLayerSourcesWithExtentAndChunkSizeForChunk(layers []core.Layer, bounds [4]float64, chunkSize float64, target ChunkKey, deduplicatePolygonEdges bool) (map[string]LayerSource, []HitFeature, error) {
	if math.IsNaN(chunkSize) || math.IsInf(chunkSize, 0) || chunkSize <= 0 || chunkSize > 1 {
		return nil, nil, fmt.Errorf("invalid render chunk size %v", chunkSize)
	}
	for _, value := range bounds {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return nil, nil, fmt.Errorf("invalid source extent %v", bounds)
		}
	}
	if bounds[0] > bounds[2] || bounds[1] > bounds[3] {
		return nil, nil, fmt.Errorf("invalid source extent %v", bounds)
	}
	targetCell := [2]int{target.X, target.Y}
	return newLayerSourcesWithFeaturesOptions(layers, &bounds, chunkSize, &targetCell, deduplicatePolygonEdges)
}

func newLayerSourcesWithFeatures(layers []core.Layer, bounds *[4]float64, chunkSize float64, target *[2]int) (map[string]LayerSource, []HitFeature, error) {
	return newLayerSourcesWithFeaturesOptions(layers, bounds, chunkSize, target, false)
}

func newLayerSourcesWithFeaturesOptions(layers []core.Layer, bounds *[4]float64, chunkSize float64, target *[2]int, deduplicatePolygonEdges bool) (map[string]LayerSource, []HitFeature, error) {
	if target == nil && 1/chunkSize > maxFullLayerChunkGridAxis {
		return nil, nil, fmt.Errorf("full-layer render chunk size creates more than %d cells per axis", maxFullLayerChunkGridAxis)
	}
	parsed := make([][]parsedFeaturePoints, len(layers))
	lineFlags := make([][]bool, len(layers))
	seenLayers := make(map[string]struct{}, len(layers))
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	for index, layer := range layers {
		if _, exists := seenLayers[layer.Name]; exists {
			return nil, nil, fmt.Errorf("duplicate layer name %q", layer.Name)
		}
		seenLayers[layer.Name] = struct{}{}
		points, flags, layerMinX, layerMinY, layerMaxX, layerMaxY, err := parseLayerPoints(layer)
		if err != nil {
			return nil, nil, err
		}
		parsed[index] = points
		lineFlags[index] = flags
		minX, minY = math.Min(minX, layerMinX), math.Min(minY, layerMinY)
		maxX, maxY = math.Max(maxX, layerMaxX), math.Max(maxY, layerMaxY)
	}
	if math.IsInf(minX, 1) || math.IsInf(minY, 1) || math.IsInf(maxX, -1) || math.IsInf(maxY, -1) {
		minX, minY, maxX, maxY = 0, 0, 1, 1
	}
	if bounds != nil {
		minX, minY, maxX, maxY = bounds[0], bounds[1], bounds[2], bounds[3]
	}
	crsCode := ""
	for _, layer := range layers {
		if layer.CRS.AuthorityCode != "" {
			crsCode = layer.CRS.AuthorityCode
			break
		}
	}
	extent := paddedDegenerateExtent([4]float64{minX, minY, maxX, maxY}, crsCode)
	minX, minY, maxX, maxY = extent[0], extent[1], extent[2], extent[3]
	sources := make(map[string]LayerSource, len(layers))
	featureCount := 0
	for _, layer := range layers {
		featureCount += len(layer.Features)
	}
	features := make([]HitFeature, featureCount)
	featureOffset := 0
	for index, layer := range layers {
		featureEnd := featureOffset + len(layer.Features)
		source := newLayerSource(layer, parsed[index], lineFlags[index], minX, minY, maxX, maxY, chunkSize, features[featureOffset:featureEnd:featureEnd], target, deduplicatePolygonEdges)
		source.Extent = [4]float64{minX, minY, maxX, maxY}
		sources[layer.Name] = source
		featureOffset = featureEnd
	}
	return sources, features, nil
}

// paddedDegenerateExtent gives point-only and axis-aligned datasets a usable
// coordinate window. Without a non-zero span the UI cannot derive coordinates
// or scale, and normalized point coordinates collapse to an edge of the canvas.
func paddedDegenerateExtent(bounds [4]float64, authorityCode string) [4]float64 {
	spanX, spanY := bounds[2]-bounds[0], bounds[3]-bounds[1]
	padding := 0.5
	switch strings.ToUpper(strings.TrimSpace(authorityCode)) {
	case "EPSG:4326":
		padding = 0.005
	case "EPSG:3857", "EPSG:5179", "EPSG:5186":
		padding = 10
	}
	if spanX == 0 {
		xPadding := math.Max(padding, spanY*0.05)
		bounds[0] -= xPadding
		bounds[2] += xPadding
	}
	if spanY == 0 {
		yPadding := math.Max(padding, spanX*0.05)
		bounds[1] -= yPadding
		bounds[3] += yPadding
	}
	// A CRS transform can turn a point extent into a tiny non-zero span due to
	// floating-point rounding. Treat spans below coordinate precision as
	// degenerate too, otherwise normalization magnifies noise and inverse
	// viewport-window queries can miss the source feature.
	for _, axis := range []int{0, 1} {
		minIndex, maxIndex := axis, axis+2
		span := bounds[maxIndex] - bounds[minIndex]
		if span <= 0 {
			continue
		}
		center := bounds[minIndex] + span/2
		minimumSpan := math.Max(1, math.Abs(center)) * 1e-9
		if span < minimumSpan {
			extra := (minimumSpan - span) / 2
			bounds[minIndex] -= extra
			bounds[maxIndex] += extra
		}
	}
	return bounds
}

func parseLayerPoints(layer core.Layer) ([]parsedFeaturePoints, []bool, float64, float64, float64, float64, error) {
	if isUniformWKTLayer(layer, "MULTILINESTRING") {
		return parseUniformWKTMultiLineLayer(layer)
	}
	if isUniformWKTLayer(layer, "LINESTRING") {
		return parseUniformWKTLineLayer(layer)
	}
	if isUniformWKTLayer(layer, "MULTIPOINT") {
		return parseUniformWKTPartsLayer(layer, 0)
	}
	if isUniformWKTLayer(layer, "MULTIPOLYGON") {
		return parseUniformWKTPartsLayer(layer, 1)
	}
	if isSimpleWKTGeometryCollectionLayer(layer) {
		return parseUniformWKTGeometryCollectionLayer(layer)
	}
	parsed := make([]parsedFeaturePoints, len(layer.Features))
	lineFlags := make([]bool, len(layer.Features))
	// Common WKT line features contain two points. Keep their coordinate
	// storage in one layer-owned arena so parsing does not allocate once per
	// feature; each parsed slice still remains independently addressable.
	var pointArena []Point
	var partArena [][]Point
	if len(layer.Features) > 0 {
		if geometry, ok := layer.Features[0].Geometry.(core.WKBGeometry); ok {
			switch wkbBaseType(geometry.WKB) {
			case 4, 5, 6, 7:
				capacity := len(layer.Features)
				if capacity <= int(^uint(0)>>1)/2 {
					capacity *= 2
				}
				partArena = make([][]Point, 0, capacity)
			}
		}
	}
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	for index, feature := range layer.Features {
		if pointArena == nil {
			switch feature.Geometry.(type) {
			case core.WKTGeometry, core.WKBGeometry:
				pointArena = make([]Point, 0, pointArenaCapacity(layer.Features))
			}
		}
		geometry, line, nextArena, nextPartArena, err := parseFeaturePointsInto(feature.Geometry, pointArena, partArena)
		if err != nil {
			return nil, nil, 0, 0, 0, 0, fmt.Errorf("feature %d: %w", feature.ID, err)
		}
		pointArena = nextArena
		partArena = nextPartArena
		parsed[index] = geometry
		lineFlags[index] = line
		if geometry.parts == nil {
			if err := updateExtent(geometry.points, &minX, &minY, &maxX, &maxY); err != nil {
				return nil, nil, 0, 0, 0, 0, fmt.Errorf("feature %d: %w", feature.ID, err)
			}
		} else {
			for _, points := range geometry.parts {
				if err := updateExtent(points, &minX, &minY, &maxX, &maxY); err != nil {
					return nil, nil, 0, 0, 0, 0, fmt.Errorf("feature %d: %w", feature.ID, err)
				}
			}
		}
	}
	return parsed, lineFlags, minX, minY, maxX, maxY, nil
}

func parseUniformWKTLineLayer(layer core.Layer) ([]parsedFeaturePoints, []bool, float64, float64, float64, float64, error) {
	parsed := make([]parsedFeaturePoints, len(layer.Features))
	lineFlags := make([]bool, len(layer.Features))
	pointArena := make([]Point, 0, pointArenaCapacity(layer.Features))
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	for index, feature := range layer.Features {
		geometry := feature.Geometry.(core.WKTGeometry)
		// isUniformWKTLayer already validated the prefix and EMPTY suffix. The
		// coordinate scanner searches for '(' itself, so a second TrimSpace pass
		// would only rescan every feature in a large homogeneous layer.
		points, nextArena, err := wktPointsIntoStandard(geometry.WKT, pointArena)
		if err != nil {
			return nil, nil, 0, 0, 0, 0, fmt.Errorf("feature %d: %w", feature.ID, err)
		}
		pointArena = nextArena
		parsed[index] = parsedFeaturePoints{points: points}
		lineFlags[index] = true
		if err := updateExtent(points, &minX, &minY, &maxX, &maxY); err != nil {
			return nil, nil, 0, 0, 0, 0, fmt.Errorf("feature %d: %w", feature.ID, err)
		}
	}
	return parsed, lineFlags, minX, minY, maxX, maxY, nil
}

func isUniformWKTLayer(layer core.Layer, geometryType string) bool {
	if len(layer.Features) == 0 {
		return false
	}
	for _, feature := range layer.Features {
		geometry, ok := feature.Geometry.(core.WKTGeometry)
		if !ok {
			return false
		}
		wkt := strings.TrimSpace(geometry.WKT)
		if hasWKTEmptySuffix(wkt) || !hasWKTPrefix(wkt, geometryType) {
			return false
		}
	}
	return true
}

func isSimpleWKTGeometryCollectionLayer(layer core.Layer) bool {
	if !isUniformWKTLayer(layer, "GEOMETRYCOLLECTION") {
		return false
	}
	for _, feature := range layer.Features {
		geometry := feature.Geometry.(core.WKTGeometry)
		if !isSimpleWKTGeometryCollection(geometry.WKT) {
			return false
		}
	}
	return true
}

func isSimpleWKTGeometryCollection(wkt string) bool {
	wkt = strings.TrimSpace(wkt)
	open := strings.IndexByte(wkt, '(')
	if open < 0 || len(wkt) < open+2 || wkt[len(wkt)-1] != ')' {
		return false
	}
	content := wkt[open+1 : len(wkt)-1]
	depth, componentStart := 0, 0
	checkComponent := func(start, end int) bool {
		component := strings.TrimSpace(content[start:end])
		return isSupportedWKTGeometryCollectionComponent(component)
	}
	for index := 0; index < len(content); index++ {
		switch content[index] {
		case '(':
			depth++
		case ')':
			depth--
			if depth < 0 {
				return false
			}
		case ',':
			if depth == 0 {
				if !checkComponent(componentStart, index) {
					return false
				}
				componentStart = index + 1
			}
		}
	}
	return depth == 0 && checkComponent(componentStart, len(content))
}

func isSupportedWKTGeometryCollectionComponent(wkt string) bool {
	if hasWKTEmptySuffix(wkt) {
		return false
	}
	return hasWKTPrefix(wkt, "POINT") || hasWKTPrefix(wkt, "LINESTRING") ||
		hasWKTPrefix(wkt, "POLYGON") || hasWKTPrefix(wkt, "MULTIPOINT") ||
		hasWKTPrefix(wkt, "MULTILINESTRING") || hasWKTPrefix(wkt, "MULTIPOLYGON")
}

func parseUniformWKTMultiLineLayer(layer core.Layer) ([]parsedFeaturePoints, []bool, float64, float64, float64, float64, error) {
	parsed := make([]parsedFeaturePoints, len(layer.Features))
	lineFlags := make([]bool, len(layer.Features))
	pointArena := make([]Point, 0, pointArenaCapacity(layer.Features))
	partArena := make([][]Point, 0, initialPartArenaCapacity(len(layer.Features)))
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	for index, feature := range layer.Features {
		geometry := feature.Geometry.(core.WKTGeometry)
		parts, nextPointArena, nextPartArena, err := wktMultiLinePointsInto(
			geometry.WKT, pointArena, partArena)
		if err != nil {
			return nil, nil, 0, 0, 0, 0, fmt.Errorf("feature %d: %w", feature.ID, err)
		}
		pointArena, partArena = nextPointArena, nextPartArena
		parsed[index] = parsedFeaturePoints{parts: parts}
		for _, part := range parts {
			if err := updateExtent(part, &minX, &minY, &maxX, &maxY); err != nil {
				return nil, nil, 0, 0, 0, 0, fmt.Errorf("feature %d: %w", feature.ID, err)
			}
		}
	}
	return parsed, lineFlags, minX, minY, maxX, maxY, nil
}

func parseUniformWKTPartsLayer(layer core.Layer, parentDepth int) ([]parsedFeaturePoints, []bool, float64, float64, float64, float64, error) {
	parsed := make([]parsedFeaturePoints, len(layer.Features))
	lineFlags := make([]bool, len(layer.Features))
	pointArena := make([]Point, 0, pointArenaCapacity(layer.Features))
	partArena := make([][]Point, 0, initialPartArenaCapacity(len(layer.Features)))
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	for index, feature := range layer.Features {
		geometry := feature.Geometry.(core.WKTGeometry)
		parts, nextPointArena, nextPartArena, err := wktNestedPartsPointsInto(
			geometry.WKT, pointArena, partArena, parentDepth)
		if err != nil {
			return nil, nil, 0, 0, 0, 0, fmt.Errorf("feature %d: %w", feature.ID, err)
		}
		pointArena, partArena = nextPointArena, nextPartArena
		parsed[index] = parsedFeaturePoints{parts: parts}
		for _, part := range parts {
			if err := updateExtent(part, &minX, &minY, &maxX, &maxY); err != nil {
				return nil, nil, 0, 0, 0, 0, fmt.Errorf("feature %d: %w", feature.ID, err)
			}
		}
	}
	return parsed, lineFlags, minX, minY, maxX, maxY, nil
}

func parseUniformWKTGeometryCollectionLayer(layer core.Layer) ([]parsedFeaturePoints, []bool, float64, float64, float64, float64, error) {
	parsed := make([]parsedFeaturePoints, len(layer.Features))
	lineFlags := make([]bool, len(layer.Features))
	pointArena := make([]Point, 0, pointArenaCapacity(layer.Features))
	partArena := make([][]Point, 0, initialPartArenaCapacity(len(layer.Features)))
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	for index, feature := range layer.Features {
		geometry := feature.Geometry.(core.WKTGeometry)
		parts, nextPointArena, nextPartArena, err := wktSimpleGeometryCollectionPointsInto(
			geometry.WKT, pointArena, partArena)
		if err != nil {
			return nil, nil, 0, 0, 0, 0, fmt.Errorf("feature %d: %w", feature.ID, err)
		}
		pointArena, partArena = nextPointArena, nextPartArena
		parsed[index] = parsedFeaturePoints{parts: parts}
		for _, part := range parts {
			if err := updateExtent(part, &minX, &minY, &maxX, &maxY); err != nil {
				return nil, nil, 0, 0, 0, 0, fmt.Errorf("feature %d: %w", feature.ID, err)
			}
		}
	}
	return parsed, lineFlags, minX, minY, maxX, maxY, nil
}

func pointArenaCapacity(features []core.Feature) (capacity int) {
	defer func() { capacity = boundedInitialPointArenaCapacity(capacity) }()
	if len(features) == 0 {
		return 0
	}
	capacity = len(features)
	if capacity <= int(^uint(0)>>1)/2 {
		capacity *= 2
	}
	first := features[0].Geometry
	switch geometry := first.(type) {
	case core.WKTGeometry:
		wkt := strings.TrimSpace(geometry.WKT)
		if hasWKTPrefix(wkt, "POINT") {
			return len(features)
		}
		if hasWKTPrefix(wkt, "LINESTRING") || hasWKTPrefix(wkt, "MULTILINESTRING") ||
			hasWKTPrefix(wkt, "MULTIPOINT") || hasWKTPrefix(wkt, "MULTIPOLYGON") ||
			hasWKTPrefix(wkt, "GEOMETRYCOLLECTION") || hasWKTPrefix(wkt, "POLYGON") {
			if coordinateCount := wktCoordinateNumberCount(wkt); coordinateCount >= 2 &&
				coordinateCount%2 == 0 {
				points := coordinateCount / 2
				if points <= int(^uint(0)>>1)/len(features) {
					return points * len(features)
				}
			}
		}
	case core.WKBGeometry:
		switch wkbBaseType(geometry.WKB) {
		case 1:
			return len(features)
		case 5:
			if points, ok := standardWKBMultiLinePointCount(geometry.WKB); ok &&
				points <= int(^uint(0)>>1)/len(features) {
				return points * len(features)
			}
		case 4:
			if points, ok := standardWKBMultiPointPointCount(geometry.WKB); ok &&
				points <= int(^uint(0)>>1)/len(features) {
				return points * len(features)
			}
		case 6:
			if points, ok := standardWKBMultiPolygonPointCount(geometry.WKB); ok &&
				points <= int(^uint(0)>>1)/len(features) {
				return points * len(features)
			}
		case 7:
			if points, ok := standardWKBGeometryCollectionPointCount(geometry.WKB); ok &&
				points <= int(^uint(0)>>1)/len(features) {
				return points * len(features)
			}
		case 3:
			if points, ok := standardWKBPolygonPointCount(geometry.WKB); ok &&
				points <= int(^uint(0)>>1)/len(features) {
				return points * len(features)
			}
		}
	}
	return min(capacity, maxInitialPointArenaCapacity)
}

func standardWKBGeometryCollectionPointCount(data []byte) (int, bool) {
	if len(data) < 9 || (data[0] != 0 && data[0] != 1) {
		return 0, false
	}
	var order binary.ByteOrder = binary.BigEndian
	if data[0] == 1 {
		order = binary.LittleEndian
	}
	if order.Uint32(data[1:5]) != 7 {
		return 0, false
	}
	childCount := uint64(order.Uint32(data[5:9]))
	maxInt := uint64(int(^uint(0) >> 1))
	if childCount > maxInt {
		return 0, false
	}
	offset, total := 9, 0
	for child := uint64(0); child < childCount; child++ {
		if offset+5 > len(data) || (data[offset] != 0 && data[offset] != 1) {
			return 0, false
		}
		var childOrder binary.ByteOrder = binary.BigEndian
		if data[offset] == 1 {
			childOrder = binary.LittleEndian
		}
		childType := childOrder.Uint32(data[offset+1 : offset+5])
		offset += 5
		switch childType {
		case 1:
			if offset+16 > len(data) || total == int(maxInt) {
				return 0, false
			}
			total++
			offset += 16
		case 2:
			if offset+4 > len(data) {
				return 0, false
			}
			count := uint64(childOrder.Uint32(data[offset : offset+4]))
			offset += 4
			if count > uint64((len(data)-offset)/16) || count > maxInt || count > maxInt-uint64(total) {
				return 0, false
			}
			total += int(count)
			offset += int(count) * 16
		case 3:
			if offset+4 > len(data) {
				return 0, false
			}
			ringCount := uint64(childOrder.Uint32(data[offset : offset+4]))
			offset += 4
			if ringCount > maxInt {
				return 0, false
			}
			for ring := uint64(0); ring < ringCount; ring++ {
				if offset+4 > len(data) {
					return 0, false
				}
				count := uint64(childOrder.Uint32(data[offset : offset+4]))
				offset += 4
				if count > uint64((len(data)-offset)/16) || count > maxInt || count > maxInt-uint64(total) {
					return 0, false
				}
				total += int(count)
				offset += int(count) * 16
			}
		default:
			return 0, false
		}
	}
	if offset != len(data) {
		return 0, false
	}
	return total, true
}

func standardWKBMultiLinePointCount(data []byte) (int, bool) {
	if len(data) < 9 || (data[0] != 0 && data[0] != 1) {
		return 0, false
	}
	var order binary.ByteOrder = binary.BigEndian
	if data[0] == 1 {
		order = binary.LittleEndian
	}
	if order.Uint32(data[1:5]) != 5 {
		return 0, false
	}
	lineCount := uint64(order.Uint32(data[5:9]))
	maxInt := uint64(int(^uint(0) >> 1))
	if lineCount > maxInt {
		return 0, false
	}
	offset, total := 9, 0
	for line := uint64(0); line < lineCount; line++ {
		if offset+9 > len(data) || (data[offset] != 0 && data[offset] != 1) {
			return 0, false
		}
		var childOrder binary.ByteOrder = binary.BigEndian
		if data[offset] == 1 {
			childOrder = binary.LittleEndian
		}
		if childOrder.Uint32(data[offset+1:offset+5]) != 2 {
			return 0, false
		}
		count := uint64(childOrder.Uint32(data[offset+5 : offset+9]))
		if count > uint64((len(data)-offset-9)/16) || count > maxInt || count > maxInt-uint64(total) {
			return 0, false
		}
		total += int(count)
		offset += 9 + int(count)*16
	}
	if offset != len(data) {
		return 0, false
	}
	return total, true
}

func standardWKBMultiPointPointCount(data []byte) (int, bool) {
	if len(data) < 9 || (data[0] != 0 && data[0] != 1) {
		return 0, false
	}
	var order binary.ByteOrder = binary.BigEndian
	if data[0] == 1 {
		order = binary.LittleEndian
	}
	if order.Uint32(data[1:5]) != 4 {
		return 0, false
	}
	count := uint64(order.Uint32(data[5:9]))
	maxInt := uint64(int(^uint(0) >> 1))
	if count > maxInt || count > uint64((len(data)-9)/21) || 9+int(count)*21 != len(data) {
		return 0, false
	}
	offset := 9
	for index := uint64(0); index < count; index++ {
		if data[offset] != 0 && data[offset] != 1 {
			return 0, false
		}
		var childOrder binary.ByteOrder = binary.BigEndian
		if data[offset] == 1 {
			childOrder = binary.LittleEndian
		}
		if childOrder.Uint32(data[offset+1:offset+5]) != 1 {
			return 0, false
		}
		offset += 21
	}
	return int(count), true
}

func standardWKBMultiPolygonPointCount(data []byte) (int, bool) {
	if len(data) < 9 || (data[0] != 0 && data[0] != 1) {
		return 0, false
	}
	var order binary.ByteOrder = binary.BigEndian
	if data[0] == 1 {
		order = binary.LittleEndian
	}
	if order.Uint32(data[1:5]) != 6 {
		return 0, false
	}
	polygonCount := uint64(order.Uint32(data[5:9]))
	maxInt := uint64(int(^uint(0) >> 1))
	if polygonCount > maxInt {
		return 0, false
	}
	offset, total := 9, 0
	for polygon := uint64(0); polygon < polygonCount; polygon++ {
		if offset+9 > len(data) || (data[offset] != 0 && data[offset] != 1) {
			return 0, false
		}
		var childOrder binary.ByteOrder = binary.BigEndian
		if data[offset] == 1 {
			childOrder = binary.LittleEndian
		}
		if childOrder.Uint32(data[offset+1:offset+5]) != 3 {
			return 0, false
		}
		ringCount := uint64(childOrder.Uint32(data[offset+5 : offset+9]))
		if ringCount > maxInt {
			return 0, false
		}
		offset += 9
		for ring := uint64(0); ring < ringCount; ring++ {
			if offset+4 > len(data) {
				return 0, false
			}
			count := uint64(childOrder.Uint32(data[offset : offset+4]))
			offset += 4
			if count > uint64((len(data)-offset)/16) || count > maxInt || count > maxInt-uint64(total) {
				return 0, false
			}
			total += int(count)
			offset += int(count) * 16
		}
	}
	if offset != len(data) {
		return 0, false
	}
	return total, true
}

func wktCoordinateNumberCount(wkt string) int {
	open := strings.IndexByte(wkt, '(')
	if open < 0 {
		return 0
	}
	count := 0
	for index := open + 1; index < len(wkt); {
		if !isWKTNumberStart(wkt[index]) {
			index++
			continue
		}
		count++
		index++
		for index < len(wkt) {
			character := wkt[index]
			if character >= '0' && character <= '9' || character == '.' ||
				character == 'e' || character == 'E' || character == '+' || character == '-' {
				index++
				continue
			}
			break
		}
	}
	return count
}

func countSegmentVertices(start, end Point, chunkSize float64, counts map[[2]int]int) {
	startCellX := int(math.Floor(start.X / chunkSize))
	endCellX := int(math.Floor(end.X / chunkSize))
	startCellY := int(math.Floor(start.Y / chunkSize))
	endCellY := int(math.Floor(end.Y / chunkSize))
	if startCellX == endCellX && startCellY == endCellY {
		counts[[2]int{startCellX, startCellY}] += 2
		return
	}
	minCellX := int(math.Floor(math.Min(start.X, end.X) / chunkSize))
	maxCellX := int(math.Floor(math.Max(start.X, end.X) / chunkSize))
	minCellY := int(math.Floor(math.Min(start.Y, end.Y) / chunkSize))
	maxCellY := int(math.Floor(math.Max(start.Y, end.Y) / chunkSize))
	for cellY := minCellY; cellY <= maxCellY; cellY++ {
		for cellX := minCellX; cellX <= maxCellX; cellX++ {
			_, _, visible := clipSegmentToRect(start, end,
				float64(cellX)*chunkSize, float64(cellY)*chunkSize,
				float64(cellX+1)*chunkSize, float64(cellY+1)*chunkSize)
			if visible {
				counts[[2]int{cellX, cellY}] += 2
			}
		}
	}
}

func newLayerSource(layer core.Layer, parsed []parsedFeaturePoints, lineFlags []bool, minX, minY, maxX, maxY, chunkSize float64, features []HitFeature, target *[2]int, deduplicatePolygonEdges bool) LayerSource {
	if len(layer.Features) == 0 {
		return LayerSource{ChunkSize: chunkSize, Builder: emptyChunkBuilder()}
	}
	style := layer.Style
	if style == (core.LayerStyle{}) {
		style = core.DefaultLayerStyle()
	}
	spanX, spanY := maxX-minX, maxY-minY
	if spanX == 0 {
		spanX = 1
	}
	if spanY == 0 {
		spanY = 1
	}
	maxChunkCell := int(math.Ceil(1/chunkSize)) - 1
	if features == nil {
		features = make([]HitFeature, len(layer.Features))
	}
	chunkVertices := make(map[[2]int][]Vertex)
	chunkVertexLimitExceeded := make(map[[2]int]bool)
	var chunkHints map[[2]int]int
	coordinatesNormalized := false
	allPoints := len(parsed) == len(layer.Features)
	if allPoints {
		for index, geometry := range parsed {
			if geometry.parts != nil || len(geometry.points) != 1 || lineFlags[index] {
				allPoints = false
				break
			}
		}
	}
	allSimpleParts := !allPoints && len(parsed) == len(layer.Features)
	if allSimpleParts {
		for index, geometry := range parsed {
			isSimpleLine := len(lineFlags) == len(parsed) && lineFlags[index]
			if !isSimpleLine {
				isSimpleLine = isLineGeometry(layer.Features[index].Geometry)
			}
			if !isSimpleLine {
				allSimpleParts = false
				break
			}
			if geometry.parts == nil {
				if len(geometry.points) < 2 {
					allSimpleParts = false
					break
				}
				continue
			}
			for _, part := range geometry.parts {
				if len(part) < 2 {
					allSimpleParts = false
					break
				}
			}
			if !allSimpleParts {
				break
			}
		}
	}
	if allPoints || allSimpleParts {
		chunkHints = make(map[[2]int]int)
		normalizeAndHintPart := func(part []Point) {
			for pointIndex := range part {
				part[pointIndex].X = (part[pointIndex].X - minX) / spanX
				part[pointIndex].Y = (part[pointIndex].Y - minY) / spanY
			}
			if allPoints {
				point := part[0]
				cellX := int(math.Floor(point.X / chunkSize))
				cellY := int(math.Floor(point.Y / chunkSize))
				if cellX > maxChunkCell {
					cellX = maxChunkCell
				}
				if cellY > maxChunkCell {
					cellY = maxChunkCell
				}
				cell := [2]int{cellX, cellY}
				if target == nil || cell == *target {
					chunkHints[cell] += 2
				}
				return
			}
			for pointIndex := 1; pointIndex < len(part); pointIndex++ {
				start, end := part[pointIndex-1], part[pointIndex]
				if target == nil {
					countSegmentVertices(start, end, chunkSize, chunkHints)
					continue
				}
				minX, minY := float64(target[0])*chunkSize, float64(target[1])*chunkSize
				_, _, visible := clipSegmentToRect(start, end, minX, minY, minX+chunkSize, minY+chunkSize)
				if visible {
					chunkHints[*target] += 2
				}
			}
		}
		for index := range parsed {
			geometry := &parsed[index]
			if geometry.parts == nil {
				normalizeAndHintPart(geometry.points)
			} else {
				for _, part := range geometry.parts {
					normalizeAndHintPart(part)
				}
			}
		}
		coordinatesNormalized = true
	}
	appendChunkVertices := func(key [2]int, vertices ...Vertex) {
		if target != nil && key != *target {
			return
		}
		if chunkVertexLimitExceeded[key] {
			return
		}
		chunk := chunkVertices[key]
		if chunk == nil && chunkHints != nil {
			hint := chunkHints[key]
			if hint > MaxChunkVertices {
				chunkVertexLimitExceeded[key] = true
				return
			}
			chunk = make([]Vertex, 0, hint)
		}
		var ok bool
		chunk, ok = AppendVertexBatchWithinLimit(chunk, vertices, MaxChunkVertices)
		if !ok {
			delete(chunkVertices, key)
			chunkVertexLimitExceeded[key] = true
			return
		}
		chunkVertices[key] = chunk
	}
	seenPolygonEdges := make(map[[4]uint64]struct{})
	var deduplicateFeatureEdges bool
	appendOutlineSegment := func(key [2]int, start, end Point) {
		if deduplicatePolygonEdges && deduplicateFeatureEdges {
			if start.X == 0 {
				start.X = 0
			}
			if start.Y == 0 {
				start.Y = 0
			}
			if end.X == 0 {
				end.X = 0
			}
			if end.Y == 0 {
				end.Y = 0
			}
			if end.X < start.X || (end.X == start.X && end.Y < start.Y) {
				start, end = end, start
			}
			segment := [4]uint64{math.Float64bits(start.X), math.Float64bits(start.Y), math.Float64bits(end.X), math.Float64bits(end.Y)}
			if _, exists := seenPolygonEdges[segment]; exists {
				return
			}
			seenPolygonEdges[segment] = struct{}{}
		}
		appendChunkVertices(key, newLineVertex(start, style.LineWidthMM), newLineVertex(end, style.LineWidthMM))
	}
	// Multipart geometries retain their disjoint Parts view while the flattened
	// hit-test view shares one layer-owned arena instead of allocating once per
	// feature.
	var partsArena [][]Point
	var normalizedArena []Point
	var labels []LayerLabel
	totalMultipartPoints := 0
	for _, geometry := range parsed {
		if geometry.parts == nil {
			continue
		}
		for _, part := range geometry.parts {
			totalMultipartPoints += len(part)
		}
	}
	if totalMultipartPoints > 0 {
		normalizedArena = make([]Point, 0, totalMultipartPoints)
	}
	appendSegment := func(start, end Point) {
		if target != nil {
			cellMinX, cellMinY := float64(target[0])*chunkSize, float64(target[1])*chunkSize
			cellMaxX, cellMaxY := cellMinX+chunkSize, cellMinY+chunkSize
			clippedStart, clippedEnd, visible := clipSegmentToRect(start, end, cellMinX, cellMinY, cellMaxX, cellMaxY)
			if visible {
				appendOutlineSegment(*target, clippedStart, clippedEnd)
			}
			return
		}
		startCellX := int(math.Floor(start.X / chunkSize))
		endCellX := int(math.Floor(end.X / chunkSize))
		startCellY := int(math.Floor(start.Y / chunkSize))
		endCellY := int(math.Floor(end.Y / chunkSize))
		if startCellX == endCellX && startCellY == endCellY {
			key := [2]int{startCellX, startCellY}
			appendOutlineSegment(key, start, end)
			return
		}
		if start.Y == end.Y {
			minX, maxX := math.Min(start.X, end.X), math.Max(start.X, end.X)
			reverse := start.X > end.X
			minCellX, maxCellX := startCellX, endCellX
			if minCellX > maxCellX {
				minCellX, maxCellX = maxCellX, minCellX
			}
			for cellX := minCellX; cellX <= maxCellX; cellX++ {
				cellMinX, cellMaxX := float64(cellX)*chunkSize, float64(cellX+1)*chunkSize
				clippedMinX, clippedMaxX := math.Max(minX, cellMinX), math.Min(maxX, cellMaxX)
				if clippedMinX > clippedMaxX {
					continue
				}
				first, second := Point{X: clippedMinX, Y: start.Y}, Point{X: clippedMaxX, Y: start.Y}
				if reverse {
					first, second = second, first
				}
				appendOutlineSegment([2]int{cellX, startCellY}, first, second)
			}
			return
		}
		if start.X == end.X {
			minY, maxY := math.Min(start.Y, end.Y), math.Max(start.Y, end.Y)
			reverse := start.Y > end.Y
			minCellY, maxCellY := startCellY, endCellY
			if minCellY > maxCellY {
				minCellY, maxCellY = maxCellY, minCellY
			}
			for cellY := minCellY; cellY <= maxCellY; cellY++ {
				cellMinY, cellMaxY := float64(cellY)*chunkSize, float64(cellY+1)*chunkSize
				clippedMinY, clippedMaxY := math.Max(minY, cellMinY), math.Min(maxY, cellMaxY)
				if clippedMinY > clippedMaxY {
					continue
				}
				first, second := Point{X: start.X, Y: clippedMinY}, Point{X: start.X, Y: clippedMaxY}
				if reverse {
					first, second = second, first
				}
				appendOutlineSegment([2]int{startCellX, cellY}, first, second)
			}
			return
		}
		minCellX := int(math.Floor(math.Min(start.X, end.X) / chunkSize))
		maxCellX := int(math.Floor(math.Max(start.X, end.X) / chunkSize))
		minCellY := int(math.Floor(math.Min(start.Y, end.Y) / chunkSize))
		maxCellY := int(math.Floor(math.Max(start.Y, end.Y) / chunkSize))
		for cellY := minCellY; cellY <= maxCellY; cellY++ {
			for cellX := minCellX; cellX <= maxCellX; cellX++ {
				clippedStart, clippedEnd, visible := clipSegmentToRect(start, end,
					float64(cellX)*chunkSize, float64(cellY)*chunkSize,
					float64(cellX+1)*chunkSize, float64(cellY+1)*chunkSize)
				if !visible {
					continue
				}
				key := [2]int{cellX, cellY}
				appendOutlineSegment(key, clippedStart, clippedEnd)
			}
		}
	}
	for index, feature := range layer.Features {
		deduplicateFeatureEdges = deduplicatePolygonEdges && !lineFlags[index]
		// parseLayerPoints owns this storage, so normalize its coordinate
		// slices in place instead of allocating a second point slice per part.
		geometry := &parsed[index]
		processPart := func(part []Point) {
			if !coordinatesNormalized {
				for pointIndex := range part {
					point := &part[pointIndex]
					*point = Point{X: (point.X - minX) / spanX, Y: (point.Y - minY) / spanY}
				}
			}
			if len(part) == 1 {
				// A coincident vertex pair becomes a screen-sized square marker in
				// the Qt adapter; geometry and hit testing retain the real point.
				point := part[0]
				cellX := int(math.Floor(point.X / chunkSize))
				cellY := int(math.Floor(point.Y / chunkSize))
				if cellX > maxChunkCell {
					cellX = maxChunkCell
				}
				if cellY > maxChunkCell {
					cellY = maxChunkCell
				}
				marker := Vertex{X: float32(point.X), Y: float32(point.Y), SizeMM: float32(style.PointSizeMM), Kind: VertexPoint}
				appendChunkVertices([2]int{cellX, cellY}, marker, marker)
			} else {
				for pointIndex := 1; pointIndex < len(part); pointIndex++ {
					appendSegment(part[pointIndex-1], part[pointIndex])
				}
			}
		}
		var normalized []Point
		var hitParts [][]Point
		if geometry.parts == nil {
			processPart(geometry.points)
			normalized = geometry.points
			if lineFlags[index] {
				// Preserve the historical Parts view while keeping the temporary
				// parser representation flat. The one-element wrappers share a
				// layer-owned backing array instead of allocating per feature.
				if partsArena == nil {
					partsArena = make([][]Point, 0, len(layer.Features))
				}
				partsStart := len(partsArena)
				partsArena = append(partsArena, geometry.points)
				hitParts = partsArena[partsStart : partsStart+1]
			}
		} else if len(geometry.parts) == 1 {
			processPart(geometry.parts[0])
			// Most imported features have one part. Reuse the normalized part
			// for the compatibility flat view instead of copying every point.
			normalized = geometry.parts[0]
			// Keep the existing WKT Parts view for callers that inspect it.
			hitParts = geometry.parts
		} else {
			for _, part := range geometry.parts {
				processPart(part)
			}
			hitParts = geometry.parts
			if normalizedArena != nil {
				start := len(normalizedArena)
				for _, part := range geometry.parts {
					normalizedArena = append(normalizedArena, part...)
				}
				normalized = normalizedArena[start:]
			} else {
				totalPoints := 0
				for _, part := range geometry.parts {
					totalPoints += len(part)
				}
				normalized = make([]Point, 0, totalPoints)
				for _, part := range geometry.parts {
					normalized = append(normalized, part...)
				}
			}
		}
		features[index] = HitFeature{Layer: layer.Name, FeatureID: feature.ID, Vertices: normalized, Parts: hitParts}
		if layer.Labels.Enabled && feature.Label != nil && feature.Label.Text != "" {
			anchor := labelAnchor(normalized)
			lineGeometry := lineFlags[index] || isLineGeometry(feature.Geometry)
			if feature.Label.AnchorSet {
				anchor = Point{X: (feature.Label.X - minX) / spanX, Y: (feature.Label.Y - minY) / spanY}
			} else if lineGeometry {
				anchor, _, _ = labelLinePlacement(geometry)
			}
			rotation := 0.0
			switch layer.Labels.Placement {
			case "vertical":
				rotation = 90
			case "center-rotated":
				rotation = feature.Label.Rotation
			case "free-angle":
				rotation = feature.Label.Rotation
				if layer.Labels.RotationField == "" && !feature.Label.AnchorSet {
					if segmentAnchor, segmentAngle, found, err := LongestSegmentPlacement(feature.Geometry); err == nil && found {
						rotation = segmentAngle
						if lineGeometry {
							anchor = Point{X: (segmentAnchor.X - minX) / spanX, Y: (segmentAnchor.Y - minY) / spanY}
						}
					}
				}
			}
			labels = append(labels, LayerLabel{
				Layer: layer.Name, FeatureID: feature.ID, Text: feature.Label.Text,
				X: anchor.X, Y: anchor.Y, Rotation: rotation, HeightMM: layer.Labels.HeightMM,
				MinScale: layer.Labels.MinScale, MaxScale: layer.Labels.MaxScale,
			})
		}
	}
	geometryType := ""
	if layer.Features[0].Geometry != nil {
		geometryType = strings.ToUpper(layer.Features[0].Geometry.GeometryType())
	}
	color := style.PolygonColor
	if strings.Contains(geometryType, "POINT") {
		color = style.PointColor
	} else if strings.Contains(geometryType, "LINE") || strings.Contains(geometryType, "CURVE") {
		color = style.LineColor
	}
	packedColor := styleColor(color)
	for key, vertices := range chunkVertices {
		for index := range vertices {
			vertices[index].Color = packedColor
		}
		chunkVertices[key] = vertices
	}
	return LayerSource{
		Features:  features,
		Labels:    labels,
		ChunkSize: chunkSize,
		Extent:    [4]float64{minX, minY, maxX, maxY},
		Builder: func(ctx context.Context, key ChunkKey) (Chunk, error) {
			if err := ctx.Err(); err != nil {
				return Chunk{}, err
			}
			if chunkVertexLimitExceeded[[2]int{key.X, key.Y}] {
				return Chunk{}, fmt.Errorf("render chunk exceeds the %d-vertex safety limit", MaxChunkVertices)
			}
			vertices := chunkVertices[[2]int{key.X, key.Y}]
			// chunkVertices is immutable after source construction. BatchStore
			// takes the ownership boundary copy when it applies a result, so a
			// second copy here only adds allocation and memory bandwidth.
			return Chunk{Key: key, Vertices: vertices}, nil
		},
	}
}

func labelAnchor(points []Point) Point {
	if len(points) == 0 {
		return Point{}
	}
	var result Point
	for _, point := range points {
		result.X += point.X
		result.Y += point.Y
	}
	result.X /= float64(len(points))
	result.Y /= float64(len(points))
	return result
}

func labelLinePlacement(geometry *parsedFeaturePoints) (Point, float64, float64) {
	if geometry == nil {
		return Point{}, 0, 0
	}
	parts := geometry.parts
	if parts == nil {
		parts = [][]Point{geometry.points}
	}
	var bestAnchor Point
	var bestAngle, longest float64
	for _, part := range parts {
		var length float64
		for index := 1; index < len(part); index++ {
			dx, dy := part[index].X-part[index-1].X, part[index].Y-part[index-1].Y
			length += math.Hypot(dx, dy)
		}
		if length <= longest || length == 0 {
			continue
		}
		target := length * 0.5
		var traveled float64
		for index := 1; index < len(part); index++ {
			start, end := part[index-1], part[index]
			dx, dy := end.X-start.X, end.Y-start.Y
			segmentLength := math.Hypot(dx, dy)
			if segmentLength == 0 {
				continue
			}
			if traveled+segmentLength >= target {
				ratio := (target - traveled) / segmentLength
				bestAnchor = Point{X: start.X + dx*ratio, Y: start.Y + dy*ratio}
				bestAngle = math.Atan2(dy, dx) * 180 / math.Pi
				break
			}
			traveled += segmentLength
		}
		longest = length
	}
	return bestAnchor, bestAngle, longest
}

// ColorForGeometry selects the configured stroke color for the OGR/core
// geometry family represented by a layer's render source.
func ColorForGeometry(style core.LayerStyle, geometryType string) uint32 {
	if style == (core.LayerStyle{}) {
		style = core.DefaultLayerStyle()
	}
	geometryType = strings.ToUpper(geometryType)
	color := style.PolygonColor
	if strings.Contains(geometryType, "POINT") {
		color = style.PointColor
	} else if strings.Contains(geometryType, "LINE") || strings.Contains(geometryType, "CURVE") {
		color = style.LineColor
	}
	return styleColor(color)
}

// ColorForPolygonFill combines the configured polygon color alpha and opacity.
func ColorForPolygonFill(style core.LayerStyle) uint32 {
	if style == (core.LayerStyle{}) {
		style = core.DefaultLayerStyle()
	}
	color := styleColor(style.PolygonColor)
	alpha := float64(color&0xff) * style.FillOpacity
	return color&0xffffff00 | uint32(math.Round(alpha))
}

func newLineVertex(point Point, widthMM float64) Vertex {
	return Vertex{X: float32(point.X), Y: float32(point.Y), SizeMM: float32(widthMM), Kind: VertexLine}
}

func styleColor(value string) uint32 {
	if len(value) != 7 && len(value) != 9 || !strings.HasPrefix(value, "#") {
		return 0x2b6cb0ff
	}
	bytes, err := hex.DecodeString(value[1:])
	if err != nil {
		return 0x2b6cb0ff
	}
	if len(bytes) == 3 {
		bytes = append(bytes, 0xff)
	}
	return uint32(bytes[0])<<24 | uint32(bytes[1])<<16 | uint32(bytes[2])<<8 | uint32(bytes[3])
}

// clipSegmentToRect applies Liang-Barsky clipping and returns the visible
// segment portion inside an axis-aligned normalized grid cell.
func clipSegmentToRect(start, end Point, minX, minY, maxX, maxY float64) (Point, Point, bool) {
	deltaX, deltaY := end.X-start.X, end.Y-start.Y
	tMin, tMax := 0.0, 1.0
	if !clipBoundary(-deltaX, start.X-minX, &tMin, &tMax) ||
		!clipBoundary(deltaX, maxX-start.X, &tMin, &tMax) ||
		!clipBoundary(-deltaY, start.Y-minY, &tMin, &tMax) ||
		!clipBoundary(deltaY, maxY-start.Y, &tMin, &tMax) {
		return Point{}, Point{}, false
	}
	return Point{X: start.X + tMin*deltaX, Y: start.Y + tMin*deltaY},
		Point{X: start.X + tMax*deltaX, Y: start.Y + tMax*deltaY}, true
}

func clipBoundary(p, q float64, tMin, tMax *float64) bool {
	if p == 0 {
		return q >= 0
	}
	ratio := q / p
	if p < 0 {
		if ratio > *tMax {
			return false
		}
		if ratio > *tMin {
			*tMin = ratio
		}
		return true
	}
	if ratio < *tMin {
		return false
	}
	if ratio < *tMax {
		*tMax = ratio
	}
	return true
}

func emptyChunkBuilder() ChunkBuilder {
	return func(ctx context.Context, key ChunkKey) (Chunk, error) {
		if err := ctx.Err(); err != nil {
			return Chunk{}, err
		}
		return Chunk{Key: key}, nil
	}
}

func parseFeaturePointsInto(geometry core.Geometry, pointArena []Point, partArena [][]Point) (parsedFeaturePoints, bool, []Point, [][]Point, error) {
	if wkbGeometry, ok := geometry.(core.WKBGeometry); ok {
		switch wkbBaseType(wkbGeometry.WKB) {
		case 7:
			if parts, handled, nextPointArena, nextPartArena, err := parseWKBGeometryCollectionSimpleXY(wkbGeometry.WKB, pointArena, partArena); handled {
				if err != nil {
					return parsedFeaturePoints{}, false, pointArena, partArena, err
				}
				return parsedFeaturePoints{parts: parts}, false, nextPointArena, nextPartArena, nil
			}
		case 5:
			if parts, handled, nextPointArena, nextPartArena := parseWKBMultiLineStandardXY(wkbGeometry.WKB, pointArena, partArena); handled {
				return parsedFeaturePoints{parts: parts}, true, nextPointArena, nextPartArena, nil
			}
		case 4:
			if parts, handled, nextPointArena, nextPartArena := parseWKBMultiPointStandardXY(wkbGeometry.WKB, pointArena, partArena); handled {
				return parsedFeaturePoints{parts: parts}, false, nextPointArena, nextPartArena, nil
			}
		case 6:
			if parts, handled, nextPointArena, nextPartArena := parseWKBMultiPolygonStandardXY(wkbGeometry.WKB, pointArena, partArena); handled {
				return parsedFeaturePoints{parts: parts}, false, nextPointArena, nextPartArena, nil
			}
		case 3:
			if parsed, handled := parseWKBStandardPolygonXY(wkbGeometry.WKB, pointArena); handled {
				if parsed.parts == nil {
					return parsedFeaturePoints{points: parsed.points}, false, parsed.arena, partArena, nil
				}
				return parsedFeaturePoints{parts: parsed.parts}, false, parsed.arena, partArena, nil
			}
		case 1, 2:
			if points, simple, nextArena, err := parseWKBSimple(wkbGeometry.WKB, pointArena); err != nil {
				return parsedFeaturePoints{}, false, pointArena, partArena, err
			} else if simple {
				return parsedFeaturePoints{points: points}, false, nextArena, partArena, nil
			}
		}
	}
	if wktGeometry, ok := geometry.(core.WKTGeometry); ok {
		return parseWKTFeaturePointsInto(wktGeometry.WKT, pointArena, partArena)
	}
	parts, err := wktParts(geometry)
	if err != nil {
		return parsedFeaturePoints{}, false, pointArena, partArena, err
	}
	return parsedFeaturePoints{parts: parts}, false, pointArena, partArena, nil
}

func parseWKBMultiPolygonStandardXY(data []byte, pointArena []Point, partArena [][]Point) ([][]Point, bool, []Point, [][]Point) {
	if len(data) < 9 || (data[0] != 0 && data[0] != 1) {
		return nil, false, pointArena, partArena
	}
	var order binary.ByteOrder = binary.BigEndian
	if data[0] == 1 {
		order = binary.LittleEndian
	}
	if order.Uint32(data[1:5]) != 6 {
		return nil, false, pointArena, partArena
	}
	polygonCount := uint64(order.Uint32(data[5:9]))
	maxInt := uint64(int(^uint(0) >> 1))
	if polygonCount > maxInt {
		return nil, false, pointArena, partArena
	}
	partStart := len(partArena)
	offset := 9
	totalPoints := 0
	for polygon := uint64(0); polygon < polygonCount; polygon++ {
		if offset+9 > len(data) || (data[offset] != 0 && data[offset] != 1) {
			return nil, false, pointArena, partArena
		}
		var childOrder binary.ByteOrder = binary.BigEndian
		if data[offset] == 1 {
			childOrder = binary.LittleEndian
		}
		if childOrder.Uint32(data[offset+1:offset+5]) != 3 {
			return nil, false, pointArena, partArena
		}
		ringCount := uint64(childOrder.Uint32(data[offset+5 : offset+9]))
		if ringCount > maxInt {
			return nil, false, pointArena, partArena
		}
		offset += 9
		for ring := uint64(0); ring < ringCount; ring++ {
			if offset+4 > len(data) {
				return nil, false, pointArena, partArena
			}
			pointCount := uint64(childOrder.Uint32(data[offset : offset+4]))
			offset += 4
			if pointCount > uint64((len(data)-offset)/16) || pointCount > maxInt || pointCount > maxInt-uint64(totalPoints) {
				return nil, false, pointArena, partArena
			}
			totalPoints += int(pointCount)
			offset += int(pointCount) * 16
		}
	}
	if offset != len(data) {
		return nil, false, pointArena, partArena
	}
	if cap(pointArena)-len(pointArena) < totalPoints {
		newCapacity := cap(pointArena) * 2
		if newCapacity < len(pointArena)+totalPoints {
			newCapacity = len(pointArena) + totalPoints
		}
		grown := make([]Point, len(pointArena), newCapacity)
		copy(grown, pointArena)
		pointArena = grown
	}
	offset = 9
	for polygon := uint64(0); polygon < polygonCount; polygon++ {
		var childOrder binary.ByteOrder = binary.BigEndian
		if data[offset] == 1 {
			childOrder = binary.LittleEndian
		}
		ringCount := uint64(childOrder.Uint32(data[offset+5 : offset+9]))
		offset += 9
		for ring := uint64(0); ring < ringCount; ring++ {
			pointCount := int(childOrder.Uint32(data[offset : offset+4]))
			offset += 4
			start := len(pointArena)
			pointArena = pointArena[:start+pointCount]
			points := pointArena[start : start+pointCount]
			for index := range points {
				coordinateOffset := offset + index*16
				points[index] = Point{
					X: math.Float64frombits(childOrder.Uint64(data[coordinateOffset : coordinateOffset+8])),
					Y: math.Float64frombits(childOrder.Uint64(data[coordinateOffset+8 : coordinateOffset+16])),
				}
			}
			partArena = append(partArena, points)
			offset += pointCount * 16
		}
	}
	return partArena[partStart:], true, pointArena, partArena
}

func parseWKBMultiPointStandardXY(data []byte, pointArena []Point, partArena [][]Point) ([][]Point, bool, []Point, [][]Point) {
	if len(data) < 9 || (data[0] != 0 && data[0] != 1) {
		return nil, false, pointArena, partArena
	}
	var order binary.ByteOrder = binary.BigEndian
	if data[0] == 1 {
		order = binary.LittleEndian
	}
	if order.Uint32(data[1:5]) != 4 {
		return nil, false, pointArena, partArena
	}
	pointCount := uint64(order.Uint32(data[5:9]))
	maxInt := uint64(int(^uint(0) >> 1))
	if pointCount > maxInt || pointCount > uint64((len(data)-9)/21) || 9+int(pointCount)*21 != len(data) {
		return nil, false, pointArena, partArena
	}
	partStart := len(partArena)
	if cap(pointArena)-len(pointArena) < int(pointCount) {
		newCapacity := cap(pointArena) * 2
		if newCapacity < len(pointArena)+int(pointCount) {
			newCapacity = len(pointArena) + int(pointCount)
		}
		grown := make([]Point, len(pointArena), newCapacity)
		copy(grown, pointArena)
		pointArena = grown
	}
	offset := 9
	for index := uint64(0); index < pointCount; index++ {
		if data[offset] != 0 && data[offset] != 1 {
			return nil, false, pointArena, partArena
		}
		var childOrder binary.ByteOrder = binary.BigEndian
		if data[offset] == 1 {
			childOrder = binary.LittleEndian
		}
		if childOrder.Uint32(data[offset+1:offset+5]) != 1 {
			return nil, false, pointArena, partArena
		}
		start := len(pointArena)
		pointArena = append(pointArena, Point{
			X: math.Float64frombits(childOrder.Uint64(data[offset+5 : offset+13])),
			Y: math.Float64frombits(childOrder.Uint64(data[offset+13 : offset+21])),
		})
		partArena = append(partArena, pointArena[start:start+1])
		offset += 21
	}
	return partArena[partStart:], true, pointArena, partArena
}

func parseWKBMultiLineStandardXY(data []byte, pointArena []Point, partArena [][]Point) ([][]Point, bool, []Point, [][]Point) {
	if len(data) < 9 || (data[0] != 0 && data[0] != 1) {
		return nil, false, pointArena, partArena
	}
	var order binary.ByteOrder = binary.BigEndian
	if data[0] == 1 {
		order = binary.LittleEndian
	}
	if order.Uint32(data[1:5]) != 5 {
		return nil, false, pointArena, partArena
	}
	lineCount := uint64(order.Uint32(data[5:9]))
	maxInt := uint64(int(^uint(0) >> 1))
	if lineCount > maxInt {
		return nil, false, pointArena, partArena
	}
	partStart := len(partArena)
	offset := 9
	totalPoints := 0
	for line := uint64(0); line < lineCount; line++ {
		if offset+9 > len(data) || (data[offset] != 0 && data[offset] != 1) {
			return nil, false, pointArena, partArena
		}
		var childOrder binary.ByteOrder = binary.BigEndian
		if data[offset] == 1 {
			childOrder = binary.LittleEndian
		}
		if childOrder.Uint32(data[offset+1:offset+5]) != 2 {
			return nil, false, pointArena, partArena
		}
		pointCount := uint64(childOrder.Uint32(data[offset+5 : offset+9]))
		if pointCount > uint64((len(data)-offset-9)/16) || pointCount > maxInt || pointCount > maxInt-uint64(totalPoints) {
			return nil, false, pointArena, partArena
		}
		totalPoints += int(pointCount)
		offset += 9 + int(pointCount)*16
	}
	if offset != len(data) {
		return nil, false, pointArena, partArena
	}
	if cap(pointArena)-len(pointArena) < totalPoints {
		newCapacity := cap(pointArena) * 2
		if newCapacity < len(pointArena)+totalPoints {
			newCapacity = len(pointArena) + totalPoints
		}
		grown := make([]Point, len(pointArena), newCapacity)
		copy(grown, pointArena)
		pointArena = grown
	}
	offset = 9
	for line := uint64(0); line < lineCount; line++ {
		var childOrder binary.ByteOrder = binary.BigEndian
		if data[offset] == 1 {
			childOrder = binary.LittleEndian
		}
		pointCount := int(childOrder.Uint32(data[offset+5 : offset+9]))
		offset += 9
		start := len(pointArena)
		pointArena = pointArena[:start+pointCount]
		points := pointArena[start : start+pointCount]
		for index := range points {
			coordinateOffset := offset + index*16
			points[index] = Point{
				X: math.Float64frombits(childOrder.Uint64(data[coordinateOffset : coordinateOffset+8])),
				Y: math.Float64frombits(childOrder.Uint64(data[coordinateOffset+8 : coordinateOffset+16])),
			}
		}
		partArena = append(partArena, points)
		offset += pointCount * 16
	}
	return partArena[partStart:], true, pointArena, partArena
}

func parseWKBGeometryCollectionSimpleXY(data []byte, pointArena []Point, partArena [][]Point) ([][]Point, bool, []Point, [][]Point, error) {
	if len(data) < 9 || (data[0] != 0 && data[0] != 1) {
		return nil, false, pointArena, partArena, nil
	}
	var order binary.ByteOrder = binary.BigEndian
	if data[0] == 1 {
		order = binary.LittleEndian
	}
	if order.Uint32(data[1:5]) != 7 {
		return nil, false, pointArena, partArena, nil
	}
	childCount := uint64(order.Uint32(data[5:9]))
	if childCount > uint64(int(^uint(0)>>1)) {
		return nil, false, pointArena, partArena, nil
	}
	partStart := len(partArena)
	offset := 9
	for child := uint64(0); child < childCount; child++ {
		if offset+5 > len(data) || (data[offset] != 0 && data[offset] != 1) {
			return nil, false, pointArena, partArena, nil
		}
		var childOrder binary.ByteOrder = binary.BigEndian
		if data[offset] == 1 {
			childOrder = binary.LittleEndian
		}
		childType := childOrder.Uint32(data[offset+1 : offset+5])
		offset += 5
		switch childType {
		case 1:
			if offset+16 > len(data) {
				return nil, false, pointArena, partArena, nil
			}
			point := Point{
				X: math.Float64frombits(childOrder.Uint64(data[offset : offset+8])),
				Y: math.Float64frombits(childOrder.Uint64(data[offset+8 : offset+16])),
			}
			if math.IsNaN(point.X) || math.IsNaN(point.Y) {
				return nil, false, pointArena, partArena, nil
			}
			pointArena = append(pointArena, point)
			partArena = append(partArena, pointArena[len(pointArena)-1:])
			offset += 16
		case 2:
			if offset+4 > len(data) {
				return nil, false, pointArena, partArena, nil
			}
			pointCount := uint64(childOrder.Uint32(data[offset : offset+4]))
			offset += 4
			if pointCount > uint64((len(data)-offset)/16) || pointCount > uint64(int(^uint(0)>>1)) {
				return nil, false, pointArena, partArena, nil
			}
			start := len(pointArena)
			needed := start + int(pointCount)
			if needed > cap(pointArena) {
				newCapacity := cap(pointArena) * 2
				if newCapacity < needed {
					newCapacity = needed
				}
				grown := make([]Point, start, newCapacity)
				copy(grown, pointArena)
				pointArena = grown
			}
			pointArena = pointArena[:needed]
			points := pointArena[start:]
			for index := range points {
				coordinateOffset := offset + index*16
				points[index] = Point{
					X: math.Float64frombits(childOrder.Uint64(data[coordinateOffset : coordinateOffset+8])),
					Y: math.Float64frombits(childOrder.Uint64(data[coordinateOffset+8 : coordinateOffset+16])),
				}
				if math.IsNaN(points[index].X) || math.IsNaN(points[index].Y) {
					return nil, false, pointArena, partArena, nil
				}
			}
			partArena = append(partArena, points)
			offset += int(pointCount) * 16
		case 3:
			if offset+4 > len(data) {
				return nil, false, pointArena, partArena, nil
			}
			ringCount := uint64(childOrder.Uint32(data[offset : offset+4]))
			if ringCount > uint64(int(^uint(0)>>1)) {
				return nil, false, pointArena, partArena, nil
			}
			scanOffset := offset + 4
			totalPoints := 0
			maxInt := uint64(int(^uint(0) >> 1))
			for ring := uint64(0); ring < ringCount; ring++ {
				if scanOffset+4 > len(data) {
					return nil, false, pointArena, partArena, nil
				}
				pointCount := uint64(childOrder.Uint32(data[scanOffset : scanOffset+4]))
				scanOffset += 4
				if pointCount > uint64((len(data)-scanOffset)/16) || pointCount > maxInt || pointCount > maxInt-uint64(totalPoints) {
					return nil, false, pointArena, partArena, nil
				}
				totalPoints += int(pointCount)
				scanOffset += int(pointCount) * 16
			}
			if cap(pointArena)-len(pointArena) < totalPoints {
				newCapacity := cap(pointArena) * 2
				if newCapacity < len(pointArena)+totalPoints {
					newCapacity = len(pointArena) + totalPoints
				}
				grown := make([]Point, len(pointArena), newCapacity)
				copy(grown, pointArena)
				pointArena = grown
			}
			childEnd := offset + 4
			for ring := uint64(0); ring < ringCount; ring++ {
				pointCount := int(childOrder.Uint32(data[childEnd : childEnd+4]))
				childEnd += 4
				start := len(pointArena)
				pointArena = pointArena[:start+pointCount]
				points := pointArena[start : start+pointCount]
				for index := range points {
					coordinateOffset := childEnd + index*16
					points[index] = Point{
						X: math.Float64frombits(childOrder.Uint64(data[coordinateOffset : coordinateOffset+8])),
						Y: math.Float64frombits(childOrder.Uint64(data[coordinateOffset+8 : coordinateOffset+16])),
					}
				}
				partArena = append(partArena, points)
				childEnd += pointCount * 16
			}
			if childEnd > len(data) {
				return nil, false, pointArena, partArena, nil
			}
			offset = childEnd
		default:
			return nil, false, pointArena, partArena, nil
		}
	}
	if offset != len(data) {
		return nil, false, pointArena, partArena, nil
	}
	return partArena[partStart:], true, pointArena, partArena, nil
}

func parseWKTFeaturePointsInto(wkt string, pointArena []Point, partArena [][]Point) (parsedFeaturePoints, bool, []Point, [][]Point, error) {
	wkt = strings.TrimSpace(wkt)
	empty := hasWKTEmptySuffix(wkt)
	if !empty && len(wkt) > 0 {
		switch wkt[0] {
		case 'P', 'p':
			if hasWKTPrefix(wkt, "POINT") {
				points, nextArena, err := wktPointsIntoStandard(wkt, pointArena)
				if err != nil {
					return parsedFeaturePoints{}, false, pointArena, partArena, err
				}
				return parsedFeaturePoints{points: points}, false, nextArena, partArena, nil
			}
			if hasWKTPrefix(wkt, "POLYGON") {
				if points, nextArena, handled, err := wktSingleRingPointsInto(wkt, pointArena); handled {
					if err != nil {
						return parsedFeaturePoints{}, false, pointArena, partArena, err
					}
					return parsedFeaturePoints{points: points}, false, nextArena, partArena, nil
				}
			}
		case 'L', 'l':
			if hasWKTPrefix(wkt, "MULTILINESTRING") {
				parts, nextArena, nextPartArena, err := wktMultiLinePointsInto(wkt, pointArena, partArena)
				if err != nil {
					return parsedFeaturePoints{}, false, pointArena, partArena, err
				}
				return parsedFeaturePoints{parts: parts}, false, nextArena, nextPartArena, nil
			}
			if hasWKTPrefix(wkt, "LINESTRING") {
				points, nextArena, err := wktPointsIntoStandard(wkt, pointArena)
				if err != nil {
					return parsedFeaturePoints{}, false, pointArena, partArena, err
				}
				return parsedFeaturePoints{points: points}, true, nextArena, partArena, nil
			}
		}
	}
	parts, err := wktParts(core.WKTGeometry{WKT: wkt})
	if err != nil {
		return parsedFeaturePoints{}, false, pointArena, partArena, err
	}
	return parsedFeaturePoints{parts: parts}, false, pointArena, partArena, nil
}

func wktMultiLinePointsInto(wkt string, pointArena []Point, partArena [][]Point) ([][]Point, []Point, [][]Point, error) {
	open := strings.IndexByte(wkt, '(')
	if open < 0 || len(wkt) < open+2 || wkt[len(wkt)-1] != ')' {
		return nil, pointArena, partArena, errors.New("geometry has no coordinate group")
	}
	content := wkt[open+1 : len(wkt)-1]
	partStart := len(partArena)
	depth, groupStart := 0, -1
	for index := 0; index < len(content); index++ {
		switch content[index] {
		case '(':
			if depth == 0 {
				groupStart = index
			}
			depth++
		case ')':
			depth--
			if depth < 0 {
				return nil, pointArena, partArena, errors.New("geometry has unbalanced parentheses")
			}
			if depth == 0 && groupStart >= 0 {
				points, nextArena, err := wktCoordinatePointsInto(content[groupStart:index+1], pointArena, true)
				if err != nil {
					return nil, pointArena, partArena, err
				}
				partArena = append(partArena, points)
				pointArena = nextArena
				groupStart = -1
			}
		}
	}
	if depth != 0 || groupStart >= 0 {
		return nil, pointArena, partArena, errors.New("geometry has unbalanced parentheses")
	}
	return partArena[partStart:], pointArena, partArena, nil
}

func wktNestedPartsPointsInto(wkt string, pointArena []Point, partArena [][]Point, parentDepth int) ([][]Point, []Point, [][]Point, error) {
	open := strings.IndexByte(wkt, '(')
	if open < 0 || len(wkt) < open+2 || wkt[len(wkt)-1] != ')' {
		return nil, pointArena, partArena, errors.New("geometry has no coordinate group")
	}
	content := wkt[open+1 : len(wkt)-1]
	partStart := len(partArena)
	depth, groupStart := 0, -1
	for index := 0; index < len(content); index++ {
		switch content[index] {
		case '(':
			if depth == parentDepth {
				groupStart = index
			}
			depth++
		case ')':
			depth--
			if depth < 0 {
				return nil, pointArena, partArena, errors.New("geometry has unbalanced parentheses")
			}
			if depth == parentDepth && groupStart >= 0 {
				points, nextArena, err := wktCoordinatePointsInto(content[groupStart:index+1], pointArena, true)
				if err != nil {
					return nil, pointArena, partArena, err
				}
				partArena = append(partArena, points)
				pointArena = nextArena
				groupStart = -1
			}
		}
	}
	if depth != 0 || groupStart >= 0 {
		return nil, pointArena, partArena, errors.New("geometry has unbalanced parentheses")
	}
	if len(partArena) == partStart && parentDepth == 0 {
		points, nextArena, err := wktCoordinatePointsInto(wkt, pointArena, false)
		if err != nil {
			return nil, pointArena, partArena, err
		}
		pointArena = nextArena
		for index := range points {
			partArena = append(partArena, points[index:index+1])
		}
	}
	return partArena[partStart:], pointArena, partArena, nil
}

func wktSimpleGeometryCollectionPointsInto(wkt string, pointArena []Point, partArena [][]Point) ([][]Point, []Point, [][]Point, error) {
	open := strings.IndexByte(wkt, '(')
	if open < 0 || len(wkt) < open+2 || wkt[len(wkt)-1] != ')' {
		return nil, pointArena, partArena, errors.New("geometry has no component group")
	}
	content := wkt[open+1 : len(wkt)-1]
	partStart, componentStart, depth := len(partArena), 0, 0
	appendComponent := func(start, end int) error {
		component := strings.TrimSpace(content[start:end])
		if hasWKTPrefix(component, "POINT") || hasWKTPrefix(component, "LINESTRING") {
			points, nextArena, err := wktCoordinatePointsInto(component, pointArena, false)
			if err != nil {
				return err
			}
			partArena = append(partArena, points)
			pointArena = nextArena
			return nil
		}
		parentDepth := 0
		if hasWKTPrefix(component, "MULTIPOLYGON") {
			parentDepth = 1
		}
		parts, nextPointArena, nextPartArena, err := wktNestedPartsPointsInto(component, pointArena, partArena, parentDepth)
		if err != nil {
			return err
		}
		_ = parts
		pointArena, partArena = nextPointArena, nextPartArena
		return nil
	}
	for index := 0; index < len(content); index++ {
		switch content[index] {
		case '(':
			depth++
		case ')':
			depth--
			if depth < 0 {
				return nil, pointArena, partArena, errors.New("geometry has unbalanced parentheses")
			}
		case ',':
			if depth == 0 {
				if err := appendComponent(componentStart, index); err != nil {
					return nil, pointArena, partArena, err
				}
				componentStart = index + 1
			}
		}
	}
	if depth != 0 {
		return nil, pointArena, partArena, errors.New("geometry has unbalanced parentheses")
	}
	if err := appendComponent(componentStart, len(content)); err != nil {
		return nil, pointArena, partArena, err
	}
	return partArena[partStart:], pointArena, partArena, nil
}

func wktSingleRingPointsInto(wkt string, pointArena []Point) ([]Point, []Point, bool, error) {
	start := strings.IndexByte(wkt, '(')
	if start < 0 || len(wkt) < start+4 || wkt[len(wkt)-1] != ')' {
		return nil, pointArena, false, nil
	}
	content := strings.TrimSpace(wkt[start+1 : len(wkt)-1])
	if len(content) < 2 || content[0] != '(' || content[len(content)-1] != ')' {
		return nil, pointArena, false, nil
	}
	ring := content[1 : len(content)-1]
	if strings.IndexByte(ring, '(') >= 0 || strings.IndexByte(ring, ')') >= 0 {
		return nil, pointArena, false, nil
	}
	points, nextArena, err := wktPointsInto(core.WKTGeometry{WKT: ring}, pointArena)
	return points, nextArena, true, err
}

func updateExtent(points []Point, minX, minY, maxX, maxY *float64) error {
	for _, point := range points {
		if math.IsNaN(point.X) || math.IsInf(point.X, 0) || math.IsNaN(point.Y) || math.IsInf(point.Y, 0) {
			return fmt.Errorf("geometry coordinate is not finite: (%v, %v)", point.X, point.Y)
		}
		// Direct comparisons avoid four math.Min/Max calls for every point in a
		// large import while preserving the extent result for finite coordinates.
		if point.X < *minX {
			*minX = point.X
		}
		if point.Y < *minY {
			*minY = point.Y
		}
		if point.X > *maxX {
			*maxX = point.X
		}
		if point.Y > *maxY {
			*maxY = point.Y
		}
	}
	return nil
}

func wktParts(geometry core.Geometry) ([][]Point, error) {
	if wkbGeometry, ok := geometry.(core.WKBGeometry); ok {
		return parseWKBParts(wkbGeometry.WKB)
	}
	wktGeometry, ok := geometry.(core.WKTGeometry)
	if !ok {
		return nil, errors.New("geometry is not core.WKTGeometry")
	}
	wkt := strings.TrimSpace(wktGeometry.WKT)
	if hasWKTEmptySuffix(wkt) {
		return [][]Point{}, nil
	}
	if hasWKTPrefix(wkt, "MULTIPOINT") {
		groups, err := wktGroups(wkt)
		if err != nil {
			return nil, err
		}
		if len(groups) == 0 {
			points, err := wktPoints(geometry)
			if err != nil {
				return nil, err
			}
			parts := make([][]Point, len(points))
			for index, point := range points {
				parts[index] = []Point{point}
			}
			return parts, nil
		}
		parts := make([][]Point, 0, len(groups))
		for _, group := range groups {
			points, err := wktPoints(core.WKTGeometry{WKT: group})
			if err != nil {
				return nil, err
			}
			parts = append(parts, points)
		}
		return parts, nil
	}
	if hasWKTPrefix(wkt, "MULTILINESTRING") {
		groups, err := wktGroups(wkt)
		if err != nil {
			return nil, err
		}
		parts := make([][]Point, 0, len(groups))
		for _, group := range groups {
			points, err := wktPoints(core.WKTGeometry{WKT: group})
			if err != nil {
				return nil, err
			}
			parts = append(parts, points)
		}
		return parts, nil
	}
	if hasWKTPrefix(wkt, "MULTIPOLYGON") {
		polygons, err := wktGroups(wkt)
		if err != nil {
			return nil, err
		}
		parts := make([][]Point, 0)
		for _, polygon := range polygons {
			rings, err := wktGroups(polygon)
			if err != nil {
				return nil, err
			}
			for _, ring := range rings {
				points, err := wktPoints(core.WKTGeometry{WKT: ring})
				if err != nil {
					return nil, err
				}
				parts = append(parts, points)
			}
		}
		return parts, nil
	}
	if hasWKTPrefix(wkt, "POLYGON") {
		rings, err := wktGroups(wkt)
		if err != nil {
			return nil, err
		}
		parts := make([][]Point, 0, len(rings))
		for _, ring := range rings {
			points, err := wktPoints(core.WKTGeometry{WKT: ring})
			if err != nil {
				return nil, err
			}
			parts = append(parts, points)
		}
		return parts, nil
	}
	if hasWKTPrefix(wkt, "GEOMETRYCOLLECTION") {
		components, err := wktComponents(wkt)
		if err != nil {
			return nil, err
		}
		parts := make([][]Point, 0)
		for _, component := range components {
			componentParts, err := wktParts(core.WKTGeometry{WKT: component})
			if err != nil {
				return nil, err
			}
			parts = append(parts, componentParts...)
		}
		return parts, nil
	}
	points, err := wktPoints(geometry)
	if err != nil {
		return nil, err
	}
	return [][]Point{points}, nil
}

func isLineGeometry(geometry core.Geometry) bool {
	switch value := geometry.(type) {
	case core.WKTGeometry:
		wkt := strings.TrimSpace(value.WKT)
		return hasWKTPrefix(wkt, "LINESTRING") || hasWKTPrefix(wkt, "MULTILINESTRING")
	case core.WKBGeometry:
		baseType := wkbBaseType(value.WKB)
		return baseType == 2 || baseType == 5
	default:
		return false
	}
}

func hasWKTPrefix(wkt, prefix string) bool {
	if len(wkt) < len(prefix) {
		return false
	}
	for index := range prefix {
		left, right := wkt[index], prefix[index]
		if left >= 'a' && left <= 'z' {
			left -= 'a' - 'A'
		}
		if left != right {
			return false
		}
	}
	return true
}

func hasWKTEmptySuffix(wkt string) bool {
	const suffix = " EMPTY"
	if len(wkt) < len(suffix) {
		return false
	}
	return hasWKTPrefix(wkt[len(wkt)-len(suffix):], suffix)
}

func wkbBaseType(data []byte) uint32 {
	if len(data) < 5 {
		return 0
	}
	var order binary.ByteOrder
	switch data[0] {
	case 0:
		order = binary.BigEndian
	case 1:
		order = binary.LittleEndian
	default:
		return 0
	}
	typeCode := order.Uint32(data[1:5]) & 0x0fffffff
	if typeCode >= 1000 {
		typeCode %= 1000
	}
	return typeCode
}

func standardWKBPolygonPointCount(data []byte) (int, bool) {
	if len(data) < 9 || (data[0] != 0 && data[0] != 1) {
		return 0, false
	}
	var order binary.ByteOrder = binary.BigEndian
	if data[0] == 1 {
		order = binary.LittleEndian
	}
	if order.Uint32(data[1:5]) != 3 {
		return 0, false
	}
	ringCount := uint64(order.Uint32(data[5:9]))
	maxInt := uint64(int(^uint(0) >> 1))
	if ringCount > uint64(len(data))/4 || ringCount > maxInt {
		return 0, false
	}
	offset := 9
	total := 0
	for ring := uint64(0); ring < ringCount; ring++ {
		if offset+4 > len(data) {
			return 0, false
		}
		count := uint64(order.Uint32(data[offset : offset+4]))
		offset += 4
		if count > uint64((len(data)-offset)/16) || count > maxInt ||
			count > maxInt-uint64(total) {
			return 0, false
		}
		total += int(count)
		offset += int(count) * 16
	}
	if offset != len(data) {
		return 0, false
	}
	return total, true
}

// parseWKBSimple handles the two common flat WKB types without allocating the
// outer [][]Point wrapper. Complex types return simple=false and are parsed by
// parseWKBParts, which retains their component structure. Simple coordinates
// are appended to pointArena when supplied so a layer can share one backing
// array across features.
func parseWKBSimple(data []byte, pointArena []Point) ([]Point, bool, []Point, error) {
	if points, handled := parseWKBStandardXY(data, pointArena); handled {
		return points.points, true, points.arena, points.err
	}
	startOffset := len(pointArena)
	cursor := &wkbCursor{data: data}
	if cursor.offset >= len(cursor.data) {
		return nil, false, pointArena, errors.New("WKB is truncated")
	}
	order := cursor.data[cursor.offset]
	cursor.offset++
	switch order {
	case 0:
		cursor.order = binary.BigEndian
	case 1:
		cursor.order = binary.LittleEndian
	default:
		return nil, false, pointArena, errors.New("WKB has invalid byte order")
	}
	typeCode, err := cursor.uint32()
	if err != nil {
		return nil, false, pointArena, err
	}
	hasZ := typeCode&0x80000000 != 0 || typeCode >= 1000 && typeCode < 2000
	hasM := typeCode&0x40000000 != 0 || typeCode >= 2000 && typeCode < 3000
	if typeCode&0x20000000 != 0 {
		if _, err := cursor.uint32(); err != nil {
			return nil, false, pointArena, err
		}
	}
	base := typeCode & 0x0fffffff
	if base >= 1000 {
		base %= 1000
	}
	if base != 1 && base != 2 {
		return nil, false, pointArena, nil
	}
	readPoint := func() (Point, error) {
		x, err := cursor.float64()
		if err != nil {
			return Point{}, err
		}
		y, err := cursor.float64()
		if err != nil {
			return Point{}, err
		}
		for extra := 0; extra < wkbBoolInt(hasZ)+wkbBoolInt(hasM); extra++ {
			if _, err := cursor.float64(); err != nil {
				return Point{}, err
			}
		}
		return Point{X: x, Y: y}, nil
	}
	if base == 1 {
		point, err := readPoint()
		if err != nil {
			return nil, true, pointArena, err
		}
		if cursor.offset != len(data) {
			return nil, true, pointArena, errors.New("WKB has trailing bytes")
		}
		if math.IsNaN(point.X) || math.IsNaN(point.Y) {
			return nil, true, pointArena, nil
		}
		pointArena = append(pointArena, point)
		return pointArena[startOffset:], true, pointArena, nil
	}
	count, err := cursor.uint32()
	if err != nil {
		return nil, true, pointArena, err
	}
	for index := uint32(0); index < count; index++ {
		point, readErr := readPoint()
		err = readErr
		if err != nil {
			return nil, true, pointArena, err
		}
		pointArena = append(pointArena, point)
	}
	if cursor.offset != len(data) {
		return nil, true, pointArena, errors.New("WKB has trailing bytes")
	}
	return pointArena[startOffset:], true, pointArena, nil
}

// parseWKBStandardPolygonXY handles the common little/big-endian 2D POLYGON
// layout while keeping all ring coordinates in the layer-owned arena. It
// returns handled=false for extended, malformed, or trailing-byte input so the
// generic WKB parser remains the validation fallback.
type parsedWKBStandardPolygon struct {
	points []Point
	parts  [][]Point
	arena  []Point
}

func parseWKBStandardPolygonXY(data []byte, pointArena []Point) (parsedWKBStandardPolygon, bool) {
	if len(data) < 9 || (data[0] != 0 && data[0] != 1) {
		return parsedWKBStandardPolygon{}, false
	}
	var order binary.ByteOrder = binary.BigEndian
	if data[0] == 1 {
		order = binary.LittleEndian
	}
	if order.Uint32(data[1:5]) != 3 {
		return parsedWKBStandardPolygon{}, false
	}
	ringCount := uint64(order.Uint32(data[5:9]))
	maxInt := uint64(int(^uint(0) >> 1))
	if ringCount > uint64(len(data))/4 || ringCount > maxInt {
		return parsedWKBStandardPolygon{}, false
	}
	var parts [][]Point
	if ringCount == 0 {
		parts = make([][]Point, 0)
	} else if ringCount > 1 {
		parts = make([][]Point, int(ringCount))
	}
	totalPoints := 0
	scanOffset := 9
	for ring := uint64(0); ring < ringCount; ring++ {
		if scanOffset+4 > len(data) {
			return parsedWKBStandardPolygon{}, false
		}
		pointCount := uint64(order.Uint32(data[scanOffset : scanOffset+4]))
		scanOffset += 4
		if pointCount > uint64((len(data)-scanOffset)/16) || pointCount > maxInt {
			return parsedWKBStandardPolygon{}, false
		}
		count := int(pointCount)
		if totalPoints > int(maxInt)-count {
			return parsedWKBStandardPolygon{}, false
		}
		totalPoints += count
		scanOffset += count * 16
	}
	if scanOffset != len(data) {
		return parsedWKBStandardPolygon{}, false
	}
	if cap(pointArena)-len(pointArena) < totalPoints {
		newCapacity := cap(pointArena) * 2
		if newCapacity < len(pointArena)+totalPoints {
			newCapacity = len(pointArena) + totalPoints
		}
		grown := make([]Point, len(pointArena), newCapacity)
		copy(grown, pointArena)
		pointArena = grown
	}
	offset := 9
	for ring := uint64(0); ring < ringCount; ring++ {
		count := int(order.Uint32(data[offset : offset+4]))
		offset += 4
		start := len(pointArena)
		pointArena = pointArena[:start+count]
		points := pointArena[start : start+count]
		for index := range points {
			coordinateOffset := offset + index*16
			points[index] = Point{
				X: math.Float64frombits(order.Uint64(data[coordinateOffset : coordinateOffset+8])),
				Y: math.Float64frombits(order.Uint64(data[coordinateOffset+8 : coordinateOffset+16])),
			}
		}
		if parts != nil {
			parts[ring] = points
		} else {
			return parsedWKBStandardPolygon{points: points, arena: pointArena}, true
		}
		offset += count * 16
	}
	return parsedWKBStandardPolygon{parts: parts, arena: pointArena}, true
}

type parsedWKBStandardXY struct {
	points []Point
	arena  []Point
	err    error
}

// parseWKBStandardXY handles the common little/big-endian 2D POINT and
// LINESTRING layouts without the generic cursor's interface dispatch. It
// declines malformed or extended layouts so the generic parser preserves all
// existing validation and Z/M/EWKB behavior.
func parseWKBStandardXY(data []byte, pointArena []Point) (parsedWKBStandardXY, bool) {
	if len(data) < 5 || (data[0] != 0 && data[0] != 1) {
		return parsedWKBStandardXY{}, false
	}
	var order binary.ByteOrder = binary.BigEndian
	if data[0] == 1 {
		order = binary.LittleEndian
	}
	typeCode := order.Uint32(data[1:5])
	if typeCode != 1 && typeCode != 2 {
		return parsedWKBStandardXY{}, false
	}
	startOffset := len(pointArena)
	if typeCode == 1 {
		if len(data) != 21 {
			return parsedWKBStandardXY{}, false
		}
		point := Point{
			X: math.Float64frombits(order.Uint64(data[5:13])),
			Y: math.Float64frombits(order.Uint64(data[13:21])),
		}
		if math.IsNaN(point.X) || math.IsNaN(point.Y) {
			return parsedWKBStandardXY{arena: pointArena}, true
		}
		pointArena = append(pointArena, point)
		return parsedWKBStandardXY{points: pointArena[startOffset:], arena: pointArena}, true
	}
	if len(data) < 9 {
		return parsedWKBStandardXY{}, false
	}
	count := uint64(order.Uint32(data[5:9]))
	expected := uint64(9) + count*16
	if expected != uint64(len(data)) || count > uint64(int(^uint(0)>>1)) {
		return parsedWKBStandardXY{}, false
	}
	pointCount := int(count)
	if cap(pointArena)-len(pointArena) < pointCount {
		grown := make([]Point, len(pointArena), len(pointArena)+pointCount)
		copy(grown, pointArena)
		pointArena = grown
	}
	start := len(pointArena)
	pointArena = pointArena[:start+pointCount]
	points := pointArena[startOffset:]
	for index := range points {
		offset := 9 + index*16
		points[index] = Point{
			X: math.Float64frombits(order.Uint64(data[offset : offset+8])),
			Y: math.Float64frombits(order.Uint64(data[offset+8 : offset+16])),
		}
	}
	return parsedWKBStandardXY{points: points, arena: pointArena}, true
}

type wkbCursor struct {
	data   []byte
	offset int
	order  binary.ByteOrder
}

const maxWKBGeometryNestingDepth = 64

func parseWKBParts(data []byte) ([][]Point, error) {
	cursor := &wkbCursor{data: data}
	parts, err := cursor.geometryParts(0)
	if err != nil {
		return nil, err
	}
	if cursor.offset != len(data) {
		return nil, errors.New("WKB has trailing bytes")
	}
	return parts, nil
}

func (c *wkbCursor) geometryParts(depth int) ([][]Point, error) {
	if depth > maxWKBGeometryNestingDepth {
		return nil, fmt.Errorf("WKB geometry nesting exceeds the limit of %d", maxWKBGeometryNestingDepth)
	}
	if c.offset >= len(c.data) {
		return nil, errors.New("WKB is truncated")
	}
	order := c.data[c.offset]
	c.offset++
	switch order {
	case 0:
		c.order = binary.BigEndian
	case 1:
		c.order = binary.LittleEndian
	default:
		return nil, errors.New("WKB has invalid byte order")
	}
	typeCode, err := c.uint32()
	if err != nil {
		return nil, err
	}
	hasZ := typeCode&0x80000000 != 0 || typeCode >= 1000 && typeCode < 2000
	hasM := typeCode&0x40000000 != 0 || typeCode >= 2000 && typeCode < 3000
	if typeCode&0x20000000 != 0 {
		if _, err := c.uint32(); err != nil {
			return nil, err
		}
	}
	base := typeCode & 0x0fffffff
	if base >= 1000 {
		base %= 1000
	}
	readPoint := func() (Point, error) {
		x, err := c.float64()
		if err != nil {
			return Point{}, err
		}
		y, err := c.float64()
		if err != nil {
			return Point{}, err
		}
		for extra := 0; extra < wkbBoolInt(hasZ)+wkbBoolInt(hasM); extra++ {
			if _, err := c.float64(); err != nil {
				return Point{}, err
			}
		}
		return Point{X: x, Y: y}, nil
	}
	readPoints := func() ([]Point, error) {
		count, err := c.uint32()
		if err != nil {
			return nil, err
		}
		stride := 16 + 8*(wkbBoolInt(hasZ)+wkbBoolInt(hasM))
		if uint64(count) > uint64(len(c.data)-c.offset)/uint64(stride) {
			return nil, errors.New("WKB coordinate count exceeds the remaining data")
		}
		points := make([]Point, int(count))
		for index := range points {
			points[index], err = readPoint()
			if err != nil {
				return nil, err
			}
		}
		return points, nil
	}
	switch base {
	case 1, 2:
		if base == 1 {
			point, err := readPoint()
			if err != nil {
				return nil, err
			}
			if math.IsNaN(point.X) || math.IsNaN(point.Y) {
				return nil, nil
			}
			return [][]Point{{point}}, nil
		}
		points, err := readPoints()
		if err != nil {
			return nil, err
		}
		return [][]Point{points}, nil
	case 3:
		ringCount, err := c.uint32()
		if err != nil {
			return nil, err
		}
		if uint64(ringCount) > uint64(len(c.data)-c.offset)/4 {
			return nil, errors.New("WKB ring count exceeds the remaining data")
		}
		parts := make([][]Point, int(ringCount))
		for index := range parts {
			parts[index], err = readPoints()
			if err != nil {
				return nil, err
			}
		}
		return parts, nil
	case 4, 5, 6, 7:
		count, err := c.uint32()
		if err != nil {
			return nil, err
		}
		if uint64(count) > uint64(len(c.data)-c.offset)/5 {
			return nil, errors.New("WKB child geometry count exceeds the remaining data")
		}
		parts := make([][]Point, 0)
		for index := uint32(0); index < count; index++ {
			child, err := c.geometryParts(depth + 1)
			if err != nil {
				return nil, err
			}
			parts = append(parts, child...)
		}
		return parts, nil
	default:
		return nil, fmt.Errorf("unsupported WKB geometry type %d", base)
	}
}

func (c *wkbCursor) uint32() (uint32, error) {
	if c.offset+4 > len(c.data) {
		return 0, errors.New("WKB is truncated")
	}
	value := c.order.Uint32(c.data[c.offset : c.offset+4])
	c.offset += 4
	return value, nil
}

func (c *wkbCursor) float64() (float64, error) {
	if c.offset+8 > len(c.data) {
		return 0, errors.New("WKB is truncated")
	}
	value := math.Float64frombits(c.order.Uint64(c.data[c.offset : c.offset+8]))
	c.offset += 8
	return value, nil
}

func wkbBoolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func wktGroups(wkt string) ([]string, error) {
	start := strings.IndexByte(wkt, '(')
	if start < 0 {
		return nil, errors.New("geometry has no coordinate group")
	}
	content := wkt[start:]
	if len(content) < 2 || content[len(content)-1] != ')' {
		return nil, errors.New("geometry has unbalanced parentheses")
	}
	content = content[1 : len(content)-1]
	capacity := len(content) / 8
	if capacity < 2 {
		capacity = 2
	}
	groups := make([]string, 0, capacity)
	depth, groupStart := 0, -1
	for index := 0; index < len(content); index++ {
		character := content[index]
		switch character {
		case '(':
			if depth == 0 {
				groupStart = index
			}
			depth++
		case ')':
			depth--
			if depth < 0 {
				return nil, errors.New("geometry has unbalanced parentheses")
			}
			if depth == 0 && groupStart >= 0 {
				groups = append(groups, content[groupStart:index+1])
				groupStart = -1
			}
		}
	}
	if depth != 0 {
		return nil, errors.New("geometry has unbalanced parentheses")
	}
	return groups, nil
}

func wktComponents(wkt string) ([]string, error) {
	start := strings.IndexByte(wkt, '(')
	if start < 0 {
		return nil, errors.New("geometry has no component group")
	}
	content := wkt[start:]
	if len(content) < 2 || content[len(content)-1] != ')' {
		return nil, errors.New("geometry has unbalanced parentheses")
	}
	content = content[1 : len(content)-1]
	capacity := len(content) / 8
	if capacity < 2 {
		capacity = 2
	}
	components := make([]string, 0, capacity)
	depth, componentStart := 0, 0
	for index := 0; index < len(content); index++ {
		character := content[index]
		switch character {
		case '(':
			depth++
		case ')':
			depth--
			if depth < 0 {
				return nil, errors.New("geometry has unbalanced parentheses")
			}
		case ',':
			if depth == 0 {
				component := strings.TrimSpace(content[componentStart:index])
				if component != "" {
					components = append(components, component)
				}
				componentStart = index + 1
			}
		}
	}
	if depth != 0 {
		return nil, errors.New("geometry has unbalanced parentheses")
	}
	if component := strings.TrimSpace(content[componentStart:]); component != "" {
		components = append(components, component)
	}
	return components, nil
}

func wktPoints(geometry core.Geometry) ([]Point, error) {
	wktGeometry, ok := geometry.(core.WKTGeometry)
	if !ok {
		return nil, errors.New("geometry is not core.WKTGeometry")
	}
	points, _, err := wktPointsInto(wktGeometry, nil)
	return points, err
}

func wktPointsInto(wktGeometry core.WKTGeometry, points []Point) ([]Point, []Point, error) {
	return wktCoordinatePointsInto(wktGeometry.WKT, points, false)
}

func wktPointsIntoStandard(wkt string, points []Point) ([]Point, []Point, error) {
	return wktCoordinatePointsInto(wkt, points, true)
}

func wktCoordinatePointsInto(wkt string, points []Point, standardType bool) ([]Point, []Point, error) {
	// WKT numeric tokens are ASCII. Scan bytes directly so each coordinate does
	// not require a rune decode and temporary strings.Builder allocation.
	startOffset := len(points)
	if points == nil {
		points = make([]Point, 0, 2)
	}
	var pendingX float64
	havePendingX := false
	if open := strings.IndexByte(wkt, '('); open >= 0 {
		if standardType {
			wkt = wkt[open+1:]
		} else {
			prefixHasNumber := false
			for index := 0; index < open; index++ {
				if isWKTNumberStart(wkt[index]) {
					prefixHasNumber = true
					break
				}
			}
			if !prefixHasNumber {
				wkt = wkt[open+1:]
			}
		}
	}
	for index := 0; index < len(wkt); {
		if !isWKTNumberStart(wkt[index]) {
			index++
			continue
		}
		start := index
		index++
		for index < len(wkt) {
			character := wkt[index]
			switch {
			case character >= '0' && character <= '9', character == '.':
				index++
			case character == 'e' || character == 'E':
				index++
				if index < len(wkt) && (wkt[index] == '+' || wkt[index] == '-') {
					index++
				}
			default:
				goto tokenDone
			}
		}
	tokenDone:
		value, err := parseWKTNumber(wkt[start:index])
		if err != nil {
			return nil, points, err
		}
		if havePendingX {
			points = append(points, Point{X: pendingX, Y: value})
			havePendingX = false
		} else {
			pendingX = value
			havePendingX = true
		}
	}
	if len(points) == startOffset || havePendingX {
		return nil, points, errors.New("WKT has an invalid coordinate list")
	}
	return points[startOffset:], points, nil
}

func parseWKTNumber(token string) (float64, error) {
	index := 0
	negative := false
	if index < len(token) && (token[index] == '+' || token[index] == '-') {
		negative = token[index] == '-'
		index++
	}
	value := 0.0
	digits := 0
	for index < len(token) && token[index] >= '0' && token[index] <= '9' {
		value = value*10 + float64(token[index]-'0')
		index++
		digits++
	}
	if index < len(token) && token[index] == '.' {
		index++
		factor := 0.1
		for index < len(token) && token[index] >= '0' && token[index] <= '9' {
			value += float64(token[index]-'0') * factor
			factor *= 0.1
			index++
			digits++
		}
	}
	if digits == 0 || index != len(token) || math.IsInf(value, 0) {
		return strconv.ParseFloat(token, 64)
	}
	if negative {
		return -value, nil
	}
	return value, nil
}

func isWKTNumberStart(character byte) bool {
	return (character >= '0' && character <= '9') || character == '+' || character == '-' || character == '.'
}
