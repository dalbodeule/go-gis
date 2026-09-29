package render

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"

	"gogis/internal/core"
)

// LayerSource is a normalized, UI-neutral geometry source for a vector layer.
// Coordinates are mapped into the unit square so Qt and other adapters can
// apply viewport transforms without knowing the source CRS units.
type LayerSource struct {
	Features  []HitFeature
	Builder   ChunkBuilder
	ChunkSize float64
}

// NewLayerSource converts Point, LineString, and Polygon WKT features into a
// normalized render source. The builder returns one immutable batch for the
// source layer; chunk scheduling and viewport policy remain adapter concerns.
func NewLayerSource(layer core.Layer) (LayerSource, error) {
	parsed, minX, minY, maxX, maxY, err := parseLayerPoints(layer)
	if err != nil {
		return LayerSource{}, err
	}
	return newLayerSource(layer, parsed, minX, minY, maxX, maxY), nil
}

// NewLayerSources creates sources using one common extent. This is required
// for multi-layer maps: independently normalizing each layer would make
// layers with different source bounds overlap in the renderer.
func NewLayerSources(layers []core.Layer) (map[string]LayerSource, error) {
	parsed := make([][][][]Point, len(layers))
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	for index, layer := range layers {
		if _, exists := findLayerSource(layers[:index], layer.Name); exists {
			return nil, fmt.Errorf("duplicate layer name %q", layer.Name)
		}
		points, layerMinX, layerMinY, layerMaxX, layerMaxY, err := parseLayerPoints(layer)
		if err != nil {
			return nil, err
		}
		parsed[index] = points
		minX, minY = math.Min(minX, layerMinX), math.Min(minY, layerMinY)
		maxX, maxY = math.Max(maxX, layerMaxX), math.Max(maxY, layerMaxY)
	}
	if math.IsInf(minX, 1) || math.IsInf(minY, 1) || math.IsInf(maxX, -1) || math.IsInf(maxY, -1) {
		minX, minY, maxX, maxY = 0, 0, 1, 1
	}
	sources := make(map[string]LayerSource, len(layers))
	for index, layer := range layers {
		sources[layer.Name] = newLayerSource(layer, parsed[index], minX, minY, maxX, maxY)
	}
	return sources, nil
}

func findLayerSource(layers []core.Layer, name string) (core.Layer, bool) {
	for _, layer := range layers {
		if layer.Name == name {
			return layer, true
		}
	}
	return core.Layer{}, false
}

func parseLayerPoints(layer core.Layer) ([][][]Point, float64, float64, float64, float64, error) {
	parsed := make([][][]Point, len(layer.Features))
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	for index, feature := range layer.Features {
		parts, err := wktParts(feature.Geometry)
		if err != nil {
			return nil, 0, 0, 0, 0, fmt.Errorf("feature %d: %w", feature.ID, err)
		}
		parsed[index] = parts
		for _, points := range parts {
			for _, point := range points {
				minX, minY = math.Min(minX, point.X), math.Min(minY, point.Y)
				maxX, maxY = math.Max(maxX, point.X), math.Max(maxY, point.Y)
			}
		}
	}
	return parsed, minX, minY, maxX, maxY, nil
}

func newLayerSource(layer core.Layer, parsed [][][]Point, minX, minY, maxX, maxY float64) LayerSource {
	if len(layer.Features) == 0 {
		return LayerSource{ChunkSize: 0.25, Builder: emptyChunkBuilder()}
	}
	spanX, spanY := maxX-minX, maxY-minY
	if spanX == 0 {
		spanX = 1
	}
	if spanY == 0 {
		spanY = 1
	}
	features := make([]HitFeature, len(layer.Features))
	const chunkSize = 0.25
	chunkVertices := make(map[[2]int][]Vertex)
	appendSegment := func(start, end Point) {
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
				chunkVertices[key] = append(chunkVertices[key],
					Vertex{X: float32(clippedStart.X), Y: float32(clippedStart.Y)},
					Vertex{X: float32(clippedEnd.X), Y: float32(clippedEnd.Y)})
			}
		}
	}
	for index, feature := range layer.Features {
		normalizedParts := make([][]Point, len(parsed[index]))
		var normalized []Point
		for partIndex, part := range parsed[index] {
			normalizedParts[partIndex] = make([]Point, len(part))
			for pointIndex, point := range part {
				normalized := Point{X: (point.X - minX) / spanX, Y: (point.Y - minY) / spanY}
				normalizedParts[partIndex][pointIndex] = normalized
			}
			normalized = append(normalized, normalizedParts[partIndex]...)
			if len(normalizedParts[partIndex]) == 1 {
				// Draw a small cross for point features and keep the actual point
				// for hit testing.
				point := normalizedParts[partIndex][0]
				const radius = 0.004
				appendSegment(Point{X: point.X - radius, Y: point.Y}, Point{X: point.X + radius, Y: point.Y})
				appendSegment(Point{X: point.X, Y: point.Y - radius}, Point{X: point.X, Y: point.Y + radius})
			} else {
				for pointIndex := 1; pointIndex < len(normalizedParts[partIndex]); pointIndex++ {
					appendSegment(normalizedParts[partIndex][pointIndex-1], normalizedParts[partIndex][pointIndex])
				}
			}
		}
		features[index] = HitFeature{Layer: layer.Name, FeatureID: feature.ID, Vertices: normalized, Parts: normalizedParts}
	}
	return LayerSource{
		Features:  features,
		ChunkSize: chunkSize,
		Builder: func(ctx context.Context, key ChunkKey) (Chunk, error) {
			if err := ctx.Err(); err != nil {
				return Chunk{}, err
			}
			vertices := chunkVertices[[2]int{key.X, key.Y}]
			return Chunk{Key: key, Vertices: append([]Vertex(nil), vertices...)}, nil
		},
	}
}

// clipSegmentToRect applies Liang-Barsky clipping and returns the visible
// segment portion inside an axis-aligned normalized grid cell.
func clipSegmentToRect(start, end Point, minX, minY, maxX, maxY float64) (Point, Point, bool) {
	deltaX, deltaY := end.X-start.X, end.Y-start.Y
	tMin, tMax := 0.0, 1.0
	for _, boundary := range [][2]float64{
		{-deltaX, start.X - minX},
		{deltaX, maxX - start.X},
		{-deltaY, start.Y - minY},
		{deltaY, maxY - start.Y},
	} {
		p, q := boundary[0], boundary[1]
		if p == 0 {
			if q < 0 {
				return Point{}, Point{}, false
			}
			continue
		}
		ratio := q / p
		if p < 0 {
			if ratio > tMax {
				return Point{}, Point{}, false
			}
			if ratio > tMin {
				tMin = ratio
			}
		} else {
			if ratio < tMin {
				return Point{}, Point{}, false
			}
			if ratio < tMax {
				tMax = ratio
			}
		}
	}
	return Point{X: start.X + tMin*deltaX, Y: start.Y + tMin*deltaY},
		Point{X: start.X + tMax*deltaX, Y: start.Y + tMax*deltaY}, true
}

func emptyChunkBuilder() ChunkBuilder {
	return func(ctx context.Context, key ChunkKey) (Chunk, error) {
		if err := ctx.Err(); err != nil {
			return Chunk{}, err
		}
		return Chunk{Key: key}, nil
	}
}

func wktParts(geometry core.Geometry) ([][]Point, error) {
	wktGeometry, ok := geometry.(core.WKTGeometry)
	if !ok {
		return nil, errors.New("geometry is not core.WKTGeometry")
	}
	wkt := strings.TrimSpace(wktGeometry.WKT)
	upper := strings.ToUpper(wkt)
	if strings.HasSuffix(upper, " EMPTY") {
		return nil, nil
	}
	if strings.HasPrefix(upper, "MULTIPOINT") {
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
	if strings.HasPrefix(upper, "MULTILINESTRING") {
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
	if strings.HasPrefix(upper, "MULTIPOLYGON") {
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
	if strings.HasPrefix(upper, "POLYGON") {
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
	if strings.HasPrefix(upper, "GEOMETRYCOLLECTION") {
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
	groups := make([]string, 0)
	depth, groupStart := 0, -1
	for index, character := range content {
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
	components := make([]string, 0)
	depth, componentStart := 0, 0
	for index, character := range content {
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
	values := make([]float64, 0)
	token := strings.Builder{}
	flush := func() error {
		if token.Len() == 0 {
			return nil
		}
		value, err := strconv.ParseFloat(token.String(), 64)
		if err != nil {
			return err
		}
		values = append(values, value)
		token.Reset()
		return nil
	}
	for _, character := range wktGeometry.WKT {
		if unicode.IsDigit(character) || character == '-' || character == '+' || character == '.' {
			token.WriteRune(character)
			continue
		}
		if (character == 'e' || character == 'E') && token.Len() > 0 {
			token.WriteRune(character)
			continue
		}
		if err := flush(); err != nil {
			return nil, err
		}
	}
	if err := flush(); err != nil {
		return nil, err
	}
	if len(values) == 0 || len(values)%2 != 0 {
		return nil, errors.New("WKT has an invalid coordinate list")
	}
	points := make([]Point, len(values)/2)
	for index := range points {
		points[index] = Point{X: values[index*2], Y: values[index*2+1]}
	}
	return points, nil
}
