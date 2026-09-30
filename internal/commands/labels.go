package commands

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"gogis/internal/core"
)

var ErrLabelFieldMissing = errors.New("label field is required")

// GenerateLabels creates detached labels from one feature property. The
// representative position is the point itself, the midpoint vertex for a
// line, or the area centroid for a simple polygon ring.
func GenerateLabels(ctx context.Context, layer core.Layer, field string, height float64, style string) (core.Layer, error) {
	return generateLabels(ctx, layer, field, height, style, true)
}

func generateLabels(ctx context.Context, layer core.Layer, field string, height float64, style string, clone bool) (core.Layer, error) {
	if field == "" {
		return core.Layer{}, ErrLabelFieldMissing
	}
	result := layer
	if clone {
		result = layer.Clone()
	} else {
		// Label assignment changes the feature struct. Copy only the feature
		// headers so geometry and property maps stay read-only/shared until the
		// final AddLayer ownership boundary.
		result.Features = append([]core.Feature(nil), layer.Features...)
	}
	var labelArena []core.Label
	for index := range result.Features {
		if err := ctx.Err(); err != nil {
			return core.Layer{}, err
		}
		feature := &result.Features[index]
		value, ok := feature.Properties[field]
		if !ok || value == nil {
			feature.Label = nil
			continue
		}
		text := labelText(value)
		if text == "" {
			feature.Label = nil
			continue
		}
		point, err := representativePoint(feature.Geometry)
		if err != nil {
			return core.Layer{}, fmt.Errorf("feature %d: %w", feature.ID, err)
		}
		if labelArena == nil {
			labelArena = make([]core.Label, len(result.Features))
		}
		labelArena[index] = core.Label{Text: text, X: point.X, Y: point.Y, Height: height, Style: style}
		feature.Label = &labelArena[index]
	}
	return result, nil
}

func labelText(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case fmt.Stringer:
		return strings.TrimSpace(typed.String())
	default:
		return strings.TrimSpace(fmt.Sprint(value))
	}
}

// LabelProjectLayer generates labels and adds the result atomically.
func (s *ProjectService) LabelProjectLayer(ctx context.Context, sourceName, field, resultName string, height float64, style string) error {
	if resultName == "" {
		return errors.New("result layer name is required")
	}
	var layer core.Layer
	found := false
	for _, candidate := range s.project.Layers {
		if candidate.Name == sourceName {
			layer = candidate
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("%w: %s", ErrLayerMissing, sourceName)
	}
	result, err := generateLabels(ctx, layer, field, height, style, false)
	if err != nil {
		return err
	}
	result.Name = resultName
	if err := s.BeginEdit(); err != nil {
		return err
	}
	if err := s.AddLayer(result); err != nil {
		_ = s.Rollback()
		return err
	}
	if err := s.Commit(); err != nil {
		_ = s.Rollback()
		return err
	}
	return nil
}

type labelPoint struct{ X, Y float64 }

func representativePoint(geometry core.Geometry) (labelPoint, error) {
	if wkbGeometry, ok := geometry.(core.WKBGeometry); ok {
		return representativeWKBPoint(wkbGeometry)
	}
	if wktGeometry, ok := geometry.(core.WKTGeometry); ok {
		if point, handled, err := representativeWKTPoint(wktGeometry.WKT); handled {
			return point, err
		}
	}
	wktGeometry, err := core.ToWKT(geometry)
	if err != nil {
		return labelPoint{}, errors.New("geometry is not convertible to WKT")
	}
	typeName := strings.TrimSpace(wktGeometry.GeometryType())
	var scratch [16]labelPoint
	values, err := coordinatePairsInto(wktGeometry.WKT, scratch[:0])
	if err != nil {
		return labelPoint{}, err
	}
	if len(values) == 0 {
		return labelPoint{}, errors.New("geometry has no coordinates")
	}
	switch typeName {
	case "POINT":
		return values[0], nil
	case "LINESTRING":
		return lineMidpoint(values), nil
	case "POLYGON":
		return polygonCentroid(values), nil
	default:
		return labelPoint{}, fmt.Errorf("unsupported label geometry type %q", typeName)
	}
}

func representativeWKTPoint(wkt string) (labelPoint, bool, error) {
	wkt = strings.TrimSpace(wkt)
	open := strings.IndexByte(wkt, '(')
	if open <= 0 || len(wkt) < open+3 || wkt[len(wkt)-1] != ')' ||
		!strings.EqualFold(strings.TrimSpace(wkt[:open]), "POINT") {
		return labelPoint{}, false, nil
	}
	body := strings.TrimSpace(wkt[open+1 : len(wkt)-1])
	if body == "" || strings.IndexAny(body, "()") >= 0 {
		return labelPoint{}, true, errors.New("geometry has no coordinates")
	}
	offset := 0
	for offset < len(body) && isCoordinateWhitespace(body[offset]) {
		offset++
	}
	if offset >= len(body) || !isCoordinateStartByte(body[offset]) {
		return labelPoint{}, true, errors.New("geometry has no coordinates")
	}
	xEnd := offset
	for xEnd < len(body) && isCoordinateByte(body[xEnd]) {
		xEnd++
	}
	x, err := strconv.ParseFloat(body[offset:xEnd], 64)
	if err != nil {
		return labelPoint{}, true, err
	}
	offset = xEnd
	for offset < len(body) && isCoordinateWhitespace(body[offset]) {
		offset++
	}
	if offset >= len(body) || !isCoordinateStartByte(body[offset]) {
		return labelPoint{}, true, errors.New("coordinate list has an odd number of values")
	}
	yEnd := offset
	for yEnd < len(body) && isCoordinateByte(body[yEnd]) {
		yEnd++
	}
	y, err := strconv.ParseFloat(body[offset:yEnd], 64)
	if err != nil {
		return labelPoint{}, true, err
	}
	for yEnd < len(body) && isCoordinateWhitespace(body[yEnd]) {
		yEnd++
	}
	if yEnd != len(body) {
		return labelPoint{}, true, errors.New("geometry has more than one coordinate pair")
	}
	return labelPoint{X: x, Y: y}, true, nil
}

func representativeWKBPoint(geometry core.WKBGeometry) (labelPoint, error) {
	if point, handled, err := representativeStandardWKB(geometry.WKB); handled {
		return point, err
	}
	parts, err := geometry.Parts()
	if err != nil {
		return labelPoint{}, err
	}
	if len(parts) == 0 {
		return labelPoint{}, errors.New("geometry has no coordinates")
	}
	switch geometry.GeometryType() {
	case "POINT":
		if len(parts[0]) == 0 {
			return labelPoint{}, errors.New("geometry has no coordinates")
		}
		return labelPoint{X: parts[0][0].X, Y: parts[0][0].Y}, nil
	case "LINESTRING":
		return lineMidpoint(wkbPointsToLabelPoints(parts[0])), nil
	case "POLYGON":
		total := 0
		for _, part := range parts {
			total += len(part)
		}
		points := make([]labelPoint, 0, total)
		for _, part := range parts {
			points = append(points, wkbPointsToLabelPoints(part)...)
		}
		return polygonCentroid(points), nil
	default:
		return labelPoint{}, fmt.Errorf("unsupported label geometry type %q", geometry.GeometryType())
	}
}

// representativeStandardWKB handles the common 2D POINT/LINESTRING layout
// without allocating decoded parts. Extended and multipart WKB falls back to
// core.Parts so its existing validation and centroid semantics remain intact.
func representativeStandardWKB(data []byte) (labelPoint, bool, error) {
	if len(data) < 5 || (data[0] != 0 && data[0] != 1) {
		return labelPoint{}, false, nil
	}
	var order binary.ByteOrder = binary.BigEndian
	if data[0] == 1 {
		order = binary.LittleEndian
	}
	typeCode := order.Uint32(data[1:5])
	if typeCode == 1 {
		if len(data) != 21 {
			return labelPoint{}, false, nil
		}
		x := math.Float64frombits(order.Uint64(data[5:13]))
		y := math.Float64frombits(order.Uint64(data[13:21]))
		if math.IsNaN(x) || math.IsNaN(y) {
			return labelPoint{}, true, errors.New("geometry has no coordinates")
		}
		return labelPoint{X: x, Y: y}, true, nil
	}
	if typeCode == 3 {
		if len(data) < 9 {
			return labelPoint{}, false, nil
		}
		ringCount := uint64(order.Uint32(data[5:9]))
		offset := 9
		pointCount := uint64(0)
		areaTwice := 0.0
		cx := 0.0
		cy := 0.0
		var first, previous labelPoint
		for ring := uint64(0); ring < ringCount; ring++ {
			if offset+4 > len(data) {
				return labelPoint{}, false, nil
			}
			ringPoints := uint64(order.Uint32(data[offset : offset+4]))
			offset += 4
			for point := uint64(0); point < ringPoints; point++ {
				if offset+16 > len(data) {
					return labelPoint{}, false, nil
				}
				current := labelPoint{
					X: math.Float64frombits(order.Uint64(data[offset : offset+8])),
					Y: math.Float64frombits(order.Uint64(data[offset+8 : offset+16])),
				}
				if math.IsNaN(current.X) || math.IsNaN(current.Y) {
					return labelPoint{}, false, nil
				}
				if pointCount == 0 {
					first = current
				} else {
					cross := previous.X*current.Y - current.X*previous.Y
					areaTwice += cross
					cx += (previous.X + current.X) * cross
					cy += (previous.Y + current.Y) * cross
				}
				previous = current
				pointCount++
				offset += 16
			}
		}
		if offset != len(data) || pointCount < 3 {
			return labelPoint{}, false, nil
		}
		cross := previous.X*first.Y - first.X*previous.Y
		areaTwice += cross
		cx += (previous.X + first.X) * cross
		cy += (previous.Y + first.Y) * cross
		if areaTwice == 0 {
			return labelPoint{}, false, nil
		}
		return labelPoint{X: cx / (3 * areaTwice), Y: cy / (3 * areaTwice)}, true, nil
	}
	if typeCode != 2 || len(data) < 9 {
		return labelPoint{}, false, nil
	}
	count := uint64(order.Uint32(data[5:9]))
	if count == 0 || uint64(9)+count*16 != uint64(len(data)) {
		return labelPoint{}, false, nil
	}
	readPoint := func(index uint64) labelPoint {
		offset := 9 + int(index)*16
		return labelPoint{
			X: math.Float64frombits(order.Uint64(data[offset : offset+8])),
			Y: math.Float64frombits(order.Uint64(data[offset+8 : offset+16])),
		}
	}
	first := readPoint(0)
	if count == 1 {
		return first, true, nil
	}
	total := 0.0
	previous := first
	for index := uint64(1); index < count; index++ {
		current := readPoint(index)
		total += distance(previous, current)
		previous = current
	}
	if total == 0 {
		return first, true, nil
	}
	target := total / 2
	traversed := 0.0
	previous = first
	for index := uint64(1); index < count; index++ {
		current := readPoint(index)
		segment := distance(previous, current)
		if traversed+segment >= target {
			ratio := (target - traversed) / segment
			return labelPoint{
				X: previous.X + ratio*(current.X-previous.X),
				Y: previous.Y + ratio*(current.Y-previous.Y),
			}, true, nil
		}
		traversed += segment
		previous = current
	}
	return previous, true, nil
}

func wkbPointsToLabelPoints(points []core.WKBPoint) []labelPoint {
	result := make([]labelPoint, len(points))
	for index, point := range points {
		result[index] = labelPoint{X: point.X, Y: point.Y}
	}
	return result
}

func lineMidpoint(points []labelPoint) labelPoint {
	if len(points) == 1 {
		return points[0]
	}
	total := 0.0
	for index := 1; index < len(points); index++ {
		total += distance(points[index-1], points[index])
	}
	if total == 0 {
		return points[0]
	}
	target := total / 2
	traversed := 0.0
	for index := 1; index < len(points); index++ {
		segment := distance(points[index-1], points[index])
		if traversed+segment >= target {
			ratio := (target - traversed) / segment
			return labelPoint{
				X: points[index-1].X + ratio*(points[index].X-points[index-1].X),
				Y: points[index-1].Y + ratio*(points[index].Y-points[index-1].Y),
			}
		}
		traversed += segment
	}
	return points[len(points)-1]
}

func distance(left, right labelPoint) float64 {
	deltaX := right.X - left.X
	deltaY := right.Y - left.Y
	return math.Hypot(deltaX, deltaY)
}

func coordinatePairs(wkt string) ([]labelPoint, error) {
	return coordinatePairsInto(wkt, nil)
}

func coordinatePairsInto(wkt string, pairs []labelPoint) ([]labelPoint, error) {
	var pendingX float64
	haveX := false
	for index := 0; index < len(wkt); {
		if !isCoordinateStartByte(wkt[index]) {
			index++
			continue
		}
		start := index
		for index < len(wkt) && isCoordinateByte(wkt[index]) {
			index++
		}
		value, err := strconv.ParseFloat(wkt[start:index], 64)
		if err != nil {
			return nil, err
		}
		if !haveX {
			pendingX = value
			haveX = true
			continue
		}
		pairs = append(pairs, labelPoint{X: pendingX, Y: value})
		haveX = false
	}
	if haveX {
		return nil, errors.New("coordinate list has an odd number of values")
	}
	return pairs, nil
}

func isCoordinateByte(character byte) bool {
	return character >= '0' && character <= '9' ||
		character == '-' || character == '+' || character == '.' ||
		character == 'e' || character == 'E'
}

func isCoordinateStartByte(character byte) bool {
	return character >= '0' && character <= '9' || character == '-' || character == '+' || character == '.'
}

func isCoordinateWhitespace(character byte) bool {
	return character == ' ' || character == '\t' || character == '\r' || character == '\n'
}

func polygonCentroid(points []labelPoint) labelPoint {
	if len(points) < 3 {
		return points[0]
	}
	areaTwice := 0.0
	cx := 0.0
	cy := 0.0
	for index, current := range points {
		next := points[(index+1)%len(points)]
		cross := current.X*next.Y - next.X*current.Y
		areaTwice += cross
		cx += (current.X + next.X) * cross
		cy += (current.Y + next.Y) * cross
	}
	if areaTwice == 0 {
		return points[len(points)/2]
	}
	return labelPoint{X: cx / (3 * areaTwice), Y: cy / (3 * areaTwice)}
}
