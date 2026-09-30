//go:build native

package proj

import (
	"context"
	"encoding/binary"
	"fmt"
	"maps"
	"math"
	"strconv"
	"strings"

	"gogis/internal/core"

	projlib "github.com/twpayne/go-proj/v11"
)

// Transformer transforms XY WKT coordinates through PROJ. Z/M coordinates
// are intentionally rejected until their axis and unit semantics are fixed.
type Transformer struct{}

// Transform applies one CRS transformation to every WKT feature in a layer.
func (Transformer) Transform(ctx context.Context, source, target core.CRS, layer core.Layer) (core.Layer, error) {
	if err := ctx.Err(); err != nil {
		return core.Layer{}, err
	}
	if source.AuthorityCode == "" || target.AuthorityCode == "" {
		return core.Layer{}, fmt.Errorf("source and target CRS are required")
	}
	pj, err := projlib.NewCRSToCRS(source.AuthorityCode, target.AuthorityCode, nil)
	if err != nil {
		return core.Layer{}, fmt.Errorf("create PROJ transformation: %w", err)
	}
	return transformLayerWithPJ(ctx, source, target, layer, pj)
}

// TransformLayers transforms layers that share one source CRS and target CRS
// through one retained PROJ pipeline. It avoids repeating pipeline creation
// when a dataset contains many layers with the same CRS.
func (Transformer) TransformLayers(ctx context.Context, source, target core.CRS, layers []core.Layer) ([]core.Layer, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if source.AuthorityCode == "" || target.AuthorityCode == "" {
		return nil, fmt.Errorf("source and target CRS are required")
	}
	pj, err := projlib.NewCRSToCRS(source.AuthorityCode, target.AuthorityCode, nil)
	if err != nil {
		return nil, fmt.Errorf("create PROJ transformation: %w", err)
	}
	result := make([]core.Layer, len(layers))
	for index, layer := range layers {
		converted, transformErr := transformLayerWithPJ(ctx, source, target, layer, pj)
		if transformErr != nil {
			return nil, fmt.Errorf("transform layer %q from %s to %s: %w", layer.Name, source.AuthorityCode, target.AuthorityCode, transformErr)
		}
		result[index] = converted
	}
	return result, nil
}

func transformLayerWithPJ(ctx context.Context, source, target core.CRS, layer core.Layer, pj *projlib.PJ) (core.Layer, error) {

	result := cloneLayerForTransform(layer)
	result.CRS = target
	sourceLatLon := isLatitudeLongitudeCRS(source)
	targetLatLon := isLatitudeLongitudeCRS(target)
	if handled, err := transformWKBPointLayer(result.Features, pj, sourceLatLon, targetLatLon); handled {
		if err != nil {
			return core.Layer{}, err
		}
		return result, nil
	}
	if handled, err := transformWKBLineStringLayer(result.Features, pj, sourceLatLon, targetLatLon); handled {
		if err != nil {
			return core.Layer{}, err
		}
		return result, nil
	}
	if handled, err := transformWKBSimpleMultiPointLayer(result.Features, pj, sourceLatLon, targetLatLon); handled {
		if err != nil {
			return core.Layer{}, err
		}
		return result, nil
	}
	if handled, err := transformWKBSimpleMultiLineLayer(result.Features, pj, sourceLatLon, targetLatLon); handled {
		if err != nil {
			return core.Layer{}, err
		}
		return result, nil
	}
	if handled, err := transformWKBSimpleMultiPolygonLayer(result.Features, pj, sourceLatLon, targetLatLon); handled {
		if err != nil {
			return core.Layer{}, err
		}
		return result, nil
	}
	if handled, err := transformWKBSimplePolygonLayer(result.Features, pj, sourceLatLon, targetLatLon); handled {
		if err != nil {
			return core.Layer{}, err
		}
		return result, nil
	}
	if handled, err := transformWKTPointLayer(result.Features, pj, sourceLatLon, targetLatLon); handled {
		if err != nil {
			return core.Layer{}, err
		}
		return result, nil
	}
	if handled, err := transformWKTLineStringLayer(result.Features, pj, sourceLatLon, targetLatLon); handled {
		if err != nil {
			return core.Layer{}, err
		}
		return result, nil
	}
	if handled, err := transformWKTSimplePolygonLayer(result.Features, pj, sourceLatLon, targetLatLon); handled {
		if err != nil {
			return core.Layer{}, err
		}
		return result, nil
	}
	for i := range result.Features {
		if err := ctx.Err(); err != nil {
			return core.Layer{}, err
		}
		if wkbGeometry, ok := result.Features[i].Geometry.(core.WKBGeometry); ok {
			mapped, err := core.MapWKBXY(wkbGeometry.WKB, func(x, y float64) (float64, float64, error) {
				return transformCoordinate(x, y, pj, sourceLatLon, targetLatLon)
			})
			if err != nil {
				return core.Layer{}, fmt.Errorf("feature %d geometry: %w", result.Features[i].ID, err)
			}
			result.Features[i].Geometry = core.WKBGeometry{WKB: mapped}
			continue
		}
		geometry, err := core.ToWKT(result.Features[i].Geometry)
		if err != nil {
			return core.Layer{}, fmt.Errorf("feature %d geometry: %w", result.Features[i].ID, err)
		}
		wkt, err := transformXY(geometry.WKT, pj, sourceLatLon, targetLatLon)
		if err != nil {
			return core.Layer{}, fmt.Errorf("feature %d: %w", result.Features[i].ID, err)
		}
		result.Features[i].Geometry = core.WKTGeometry{WKT: wkt}
	}
	return result, nil
}

type wkbPointLayout struct {
	featureIndex int
	xOffset      int
	littleEndian bool
}

// transformWKBPointLayer batches the common WKB POINT case into one PROJ
// native call. A feature-by-feature call is still used for lines, polygons,
// collections, and mixed WKT/WKB layers because those need the generic mapper.
func transformWKBPointLayer(features []core.Feature, pj *projlib.PJ, sourceLatLon, targetLatLon bool) (bool, error) {
	if len(features) == 0 {
		return false, nil
	}
	layouts := make([]wkbPointLayout, len(features))
	coordinates := make([]float64, 0, len(features)*2)
	totalWKBBytes := 0
	for index := range features {
		geometry, ok := features[index].Geometry.(core.WKBGeometry)
		if !ok {
			return false, nil
		}
		layout, ok := parseWKBPointLayout(geometry.WKB)
		if !ok {
			return false, nil
		}
		layout.featureIndex = index
		layouts[index] = layout
		if len(geometry.WKB) > int(^uint(0)>>1)-totalWKBBytes {
			return false, nil
		}
		totalWKBBytes += len(geometry.WKB)
	}
	wkbArena := make([]byte, 0, totalWKBBytes)
	for index := range features {
		geometry := features[index].Geometry.(core.WKBGeometry)
		start := len(wkbArena)
		wkbArena = append(wkbArena, geometry.WKB...)
		data := wkbArena[start:len(wkbArena)]
		features[index].Geometry = core.WKBGeometry{WKB: data}
		layout := layouts[index]
		x := readWKBUint64(data, layout.xOffset, layout.littleEndian)
		y := readWKBUint64(data, layout.xOffset+8, layout.littleEndian)
		if math.IsNaN(math.Float64frombits(x)) || math.IsNaN(math.Float64frombits(y)) {
			continue
		}
		if sourceLatLon {
			coordinates = append(coordinates, math.Float64frombits(y), math.Float64frombits(x))
		} else {
			coordinates = append(coordinates, math.Float64frombits(x), math.Float64frombits(y))
		}
	}
	if err := pj.ForwardFlatCoords(coordinates, 2, -1, -1); err != nil {
		return true, fmt.Errorf("transform WKB POINT layer: %w", err)
	}
	coordinateIndex := 0
	for _, layout := range layouts {
		data := features[layout.featureIndex].Geometry.(core.WKBGeometry).WKB
		xBits := readWKBUint64(data, layout.xOffset, layout.littleEndian)
		yBits := readWKBUint64(data, layout.xOffset+8, layout.littleEndian)
		if math.IsNaN(math.Float64frombits(xBits)) || math.IsNaN(math.Float64frombits(yBits)) {
			continue
		}
		x, y := coordinates[coordinateIndex], coordinates[coordinateIndex+1]
		coordinateIndex += 2
		if targetLatLon {
			x, y = y, x
		}
		writeWKBUint64(data, layout.xOffset, layout.littleEndian, math.Float64bits(x))
		writeWKBUint64(data, layout.xOffset+8, layout.littleEndian, math.Float64bits(y))
	}
	return true, nil
}

type wkbLineLayout struct {
	featureIndex int
	pointCount   int
	littleEndian bool
}

func transformWKBLineStringLayer(features []core.Feature, pj *projlib.PJ, sourceLatLon, targetLatLon bool) (bool, error) {
	if len(features) == 0 {
		return false, nil
	}
	layouts := make([]wkbLineLayout, len(features))
	totalPoints := 0
	totalWKBBytes := 0
	for index := range features {
		geometry, ok := features[index].Geometry.(core.WKBGeometry)
		if !ok {
			return false, nil
		}
		layout, ok := parseWKBLineLayout(geometry.WKB)
		if !ok {
			return false, nil
		}
		layout.featureIndex = index
		layouts[index] = layout
		if layout.pointCount > int(^uint(0)>>1)/2-totalPoints ||
			len(geometry.WKB) > int(^uint(0)>>1)-totalWKBBytes {
			return false, nil
		}
		totalPoints += layout.pointCount
		totalWKBBytes += len(geometry.WKB)
	}
	coordinates := make([]float64, 0, totalPoints*2)
	validPoints := make([]bool, totalPoints)
	wkbArena := make([]byte, 0, totalWKBBytes)
	globalPointIndex := 0
	for index, layout := range layouts {
		geometry := features[index].Geometry.(core.WKBGeometry)
		start := len(wkbArena)
		wkbArena = append(wkbArena, geometry.WKB...)
		data := wkbArena[start:len(wkbArena)]
		features[index].Geometry = core.WKBGeometry{WKB: data}
		for pointIndex := 0; pointIndex < layout.pointCount; pointIndex++ {
			offset := 9 + pointIndex*16
			x := math.Float64frombits(readWKBUint64(data, offset, layout.littleEndian))
			y := math.Float64frombits(readWKBUint64(data, offset+8, layout.littleEndian))
			if math.IsNaN(x) || math.IsNaN(y) {
				globalPointIndex++
				continue
			}
			validPoints[globalPointIndex] = true
			globalPointIndex++
			if sourceLatLon {
				coordinates = append(coordinates, y, x)
			} else {
				coordinates = append(coordinates, x, y)
			}
		}
	}
	if err := pj.ForwardFlatCoords(coordinates, 2, -1, -1); err != nil {
		return true, fmt.Errorf("transform WKB LINESTRING layer: %w", err)
	}
	coordinateIndex := 0
	globalPointIndex = 0
	for _, layout := range layouts {
		data := features[layout.featureIndex].Geometry.(core.WKBGeometry).WKB
		for pointIndex := 0; pointIndex < layout.pointCount; pointIndex++ {
			if !validPoints[globalPointIndex] {
				globalPointIndex++
				continue
			}
			x, y := coordinates[coordinateIndex], coordinates[coordinateIndex+1]
			coordinateIndex += 2
			globalPointIndex++
			if targetLatLon {
				x, y = y, x
			}
			offset := 9 + pointIndex*16
			writeWKBUint64(data, offset, layout.littleEndian, math.Float64bits(x))
			writeWKBUint64(data, offset+8, layout.littleEndian, math.Float64bits(y))
		}
	}
	return true, nil
}

func parseWKBLineLayout(data []byte) (wkbLineLayout, bool) {
	if len(data) < 9 {
		return wkbLineLayout{}, false
	}
	var order binary.ByteOrder
	switch data[0] {
	case 0:
		order = binary.BigEndian
	case 1:
		order = binary.LittleEndian
	default:
		return wkbLineLayout{}, false
	}
	if order.Uint32(data[1:5]) != 2 {
		return wkbLineLayout{}, false
	}
	pointCount := uint64(order.Uint32(data[5:9]))
	maxInt := uint64(int(^uint(0) >> 1))
	if pointCount > maxInt || pointCount > uint64((len(data)-9)/16) ||
		9+int(pointCount)*16 != len(data) {
		return wkbLineLayout{}, false
	}
	return wkbLineLayout{pointCount: int(pointCount), littleEndian: data[0] == 1}, true
}

type wkbMultiPointLayout struct {
	featureIndex int
	pointCount   int
	littleEndian bool
}

func transformWKBSimpleMultiPointLayer(features []core.Feature, pj *projlib.PJ, sourceLatLon, targetLatLon bool) (bool, error) {
	if len(features) == 0 {
		return false, nil
	}
	layouts := make([]wkbMultiPointLayout, len(features))
	totalPoints := 0
	totalWKBBytes := 0
	for index := range features {
		geometry, ok := features[index].Geometry.(core.WKBGeometry)
		if !ok {
			return false, nil
		}
		layout, ok := parseWKBSimpleMultiPointLayout(geometry.WKB)
		if !ok {
			return false, nil
		}
		layout.featureIndex = index
		layouts[index] = layout
		if layout.pointCount > int(^uint(0)>>1)/2-totalPoints ||
			len(geometry.WKB) > int(^uint(0)>>1)-totalWKBBytes {
			return false, nil
		}
		totalPoints += layout.pointCount
		totalWKBBytes += len(geometry.WKB)
	}
	coordinates := make([]float64, 0, totalPoints*2)
	validPoints := make([]bool, totalPoints)
	wkbArena := make([]byte, 0, totalWKBBytes)
	globalPointIndex := 0
	for index, layout := range layouts {
		geometry := features[index].Geometry.(core.WKBGeometry)
		start := len(wkbArena)
		wkbArena = append(wkbArena, geometry.WKB...)
		data := wkbArena[start:len(wkbArena)]
		features[index].Geometry = core.WKBGeometry{WKB: data}
		var order binary.ByteOrder = binary.LittleEndian
		if !layout.littleEndian {
			order = binary.BigEndian
		}
		pointCount := int(order.Uint32(data[5:9]))
		for pointIndex := 0; pointIndex < pointCount; pointIndex++ {
			offset := 9 + pointIndex*21 + 5
			x := math.Float64frombits(readWKBUint64(data, offset, layout.littleEndian))
			y := math.Float64frombits(readWKBUint64(data, offset+8, layout.littleEndian))
			if math.IsNaN(x) || math.IsNaN(y) {
				globalPointIndex++
				continue
			}
			validPoints[globalPointIndex] = true
			globalPointIndex++
			if sourceLatLon {
				coordinates = append(coordinates, y, x)
			} else {
				coordinates = append(coordinates, x, y)
			}
		}
	}
	if err := pj.ForwardFlatCoords(coordinates, 2, -1, -1); err != nil {
		return true, fmt.Errorf("transform WKB MULTIPOINT layer: %w", err)
	}
	coordinateIndex := 0
	globalPointIndex = 0
	for _, layout := range layouts {
		data := features[layout.featureIndex].Geometry.(core.WKBGeometry).WKB
		var order binary.ByteOrder = binary.LittleEndian
		if !layout.littleEndian {
			order = binary.BigEndian
		}
		pointCount := int(order.Uint32(data[5:9]))
		for pointIndex := 0; pointIndex < pointCount; pointIndex++ {
			if !validPoints[globalPointIndex] {
				globalPointIndex++
				continue
			}
			x, y := coordinates[coordinateIndex], coordinates[coordinateIndex+1]
			coordinateIndex += 2
			globalPointIndex++
			if targetLatLon {
				x, y = y, x
			}
			offset := 9 + pointIndex*21 + 5
			writeWKBUint64(data, offset, layout.littleEndian, math.Float64bits(x))
			writeWKBUint64(data, offset+8, layout.littleEndian, math.Float64bits(y))
		}
	}
	return true, nil
}

func parseWKBSimpleMultiPointLayout(data []byte) (wkbMultiPointLayout, bool) {
	if len(data) < 9 {
		return wkbMultiPointLayout{}, false
	}
	var order binary.ByteOrder
	switch data[0] {
	case 0:
		order = binary.BigEndian
	case 1:
		order = binary.LittleEndian
	default:
		return wkbMultiPointLayout{}, false
	}
	if order.Uint32(data[1:5]) != 4 {
		return wkbMultiPointLayout{}, false
	}
	pointCount := uint64(order.Uint32(data[5:9]))
	maxInt := uint64(int(^uint(0) >> 1))
	if pointCount > maxInt || pointCount > uint64((len(data)-9)/21) ||
		9+int(pointCount)*21 != len(data) {
		return wkbMultiPointLayout{}, false
	}
	for index := uint64(0); index < pointCount; index++ {
		offset := 9 + int(index)*21
		if data[offset] != data[0] || order.Uint32(data[offset+1:offset+5]) != 1 {
			return wkbMultiPointLayout{}, false
		}
	}
	return wkbMultiPointLayout{pointCount: int(pointCount), littleEndian: data[0] == 1}, true
}

type wkbMultiLineLayout struct {
	featureIndex int
	pointCount   int
	littleEndian bool
}

func transformWKBSimpleMultiLineLayer(features []core.Feature, pj *projlib.PJ, sourceLatLon, targetLatLon bool) (bool, error) {
	if len(features) == 0 {
		return false, nil
	}
	layouts := make([]wkbMultiLineLayout, len(features))
	totalPoints := 0
	totalWKBBytes := 0
	for index := range features {
		geometry, ok := features[index].Geometry.(core.WKBGeometry)
		if !ok {
			return false, nil
		}
		layout, ok := parseWKBSimpleMultiLineLayout(geometry.WKB)
		if !ok {
			return false, nil
		}
		layout.featureIndex = index
		layouts[index] = layout
		if layout.pointCount > int(^uint(0)>>1)/2-totalPoints ||
			len(geometry.WKB) > int(^uint(0)>>1)-totalWKBBytes {
			return false, nil
		}
		totalPoints += layout.pointCount
		totalWKBBytes += len(geometry.WKB)
	}
	coordinates := make([]float64, 0, totalPoints*2)
	validPoints := make([]bool, totalPoints)
	wkbArena := make([]byte, 0, totalWKBBytes)
	globalPointIndex := 0
	for index, layout := range layouts {
		geometry := features[index].Geometry.(core.WKBGeometry)
		start := len(wkbArena)
		wkbArena = append(wkbArena, geometry.WKB...)
		data := wkbArena[start:len(wkbArena)]
		features[index].Geometry = core.WKBGeometry{WKB: data}
		var order binary.ByteOrder = binary.LittleEndian
		if !layout.littleEndian {
			order = binary.BigEndian
		}
		lineCount := int(order.Uint32(data[5:9]))
		offset := 9
		for lineIndex := 0; lineIndex < lineCount; lineIndex++ {
			pointCount := int(order.Uint32(data[offset+5 : offset+9]))
			offset += 9
			for pointIndex := 0; pointIndex < pointCount; pointIndex++ {
				x := math.Float64frombits(readWKBUint64(data, offset+pointIndex*16, layout.littleEndian))
				y := math.Float64frombits(readWKBUint64(data, offset+pointIndex*16+8, layout.littleEndian))
				if math.IsNaN(x) || math.IsNaN(y) {
					globalPointIndex++
					continue
				}
				validPoints[globalPointIndex] = true
				globalPointIndex++
				if sourceLatLon {
					coordinates = append(coordinates, y, x)
				} else {
					coordinates = append(coordinates, x, y)
				}
			}
			offset += pointCount * 16
		}
	}
	if err := pj.ForwardFlatCoords(coordinates, 2, -1, -1); err != nil {
		return true, fmt.Errorf("transform WKB MULTILINESTRING layer: %w", err)
	}
	coordinateIndex := 0
	globalPointIndex = 0
	for _, layout := range layouts {
		data := features[layout.featureIndex].Geometry.(core.WKBGeometry).WKB
		var order binary.ByteOrder = binary.LittleEndian
		if !layout.littleEndian {
			order = binary.BigEndian
		}
		lineCount := int(order.Uint32(data[5:9]))
		offset := 9
		for lineIndex := 0; lineIndex < lineCount; lineIndex++ {
			pointCount := int(order.Uint32(data[offset+5 : offset+9]))
			offset += 9
			for pointIndex := 0; pointIndex < pointCount; pointIndex++ {
				if !validPoints[globalPointIndex] {
					globalPointIndex++
					continue
				}
				x, y := coordinates[coordinateIndex], coordinates[coordinateIndex+1]
				coordinateIndex += 2
				globalPointIndex++
				if targetLatLon {
					x, y = y, x
				}
				pointOffset := offset + pointIndex*16
				writeWKBUint64(data, pointOffset, layout.littleEndian, math.Float64bits(x))
				writeWKBUint64(data, pointOffset+8, layout.littleEndian, math.Float64bits(y))
			}
			offset += pointCount * 16
		}
	}
	return true, nil
}

func parseWKBSimpleMultiLineLayout(data []byte) (wkbMultiLineLayout, bool) {
	if len(data) < 9 {
		return wkbMultiLineLayout{}, false
	}
	var order binary.ByteOrder
	switch data[0] {
	case 0:
		order = binary.BigEndian
	case 1:
		order = binary.LittleEndian
	default:
		return wkbMultiLineLayout{}, false
	}
	if order.Uint32(data[1:5]) != 5 {
		return wkbMultiLineLayout{}, false
	}
	lineCount := uint64(order.Uint32(data[5:9]))
	maxInt := uint64(int(^uint(0) >> 1))
	if lineCount > maxInt {
		return wkbMultiLineLayout{}, false
	}
	offset, totalPoints := 9, 0
	for lineIndex := uint64(0); lineIndex < lineCount; lineIndex++ {
		if offset+9 > len(data) || data[offset] != data[0] || order.Uint32(data[offset+1:offset+5]) != 2 {
			return wkbMultiLineLayout{}, false
		}
		pointCount := uint64(order.Uint32(data[offset+5 : offset+9]))
		if pointCount > maxInt || pointCount > uint64((len(data)-offset-9)/16) ||
			pointCount > maxInt-uint64(totalPoints) {
			return wkbMultiLineLayout{}, false
		}
		offset += 9 + int(pointCount)*16
		totalPoints += int(pointCount)
	}
	if offset != len(data) {
		return wkbMultiLineLayout{}, false
	}
	return wkbMultiLineLayout{pointCount: totalPoints, littleEndian: data[0] == 1}, true
}

type wkbMultiPolygonLayout struct {
	featureIndex int
	pointCount   int
	littleEndian bool
}

func transformWKBSimpleMultiPolygonLayer(features []core.Feature, pj *projlib.PJ, sourceLatLon, targetLatLon bool) (bool, error) {
	if len(features) == 0 {
		return false, nil
	}
	layouts := make([]wkbMultiPolygonLayout, len(features))
	totalPoints := 0
	totalWKBBytes := 0
	for index := range features {
		geometry, ok := features[index].Geometry.(core.WKBGeometry)
		if !ok {
			return false, nil
		}
		layout, ok := parseWKBSimpleMultiPolygonLayout(geometry.WKB)
		if !ok {
			return false, nil
		}
		layout.featureIndex = index
		layouts[index] = layout
		if layout.pointCount > int(^uint(0)>>1)/2-totalPoints ||
			len(geometry.WKB) > int(^uint(0)>>1)-totalWKBBytes {
			return false, nil
		}
		totalPoints += layout.pointCount
		totalWKBBytes += len(geometry.WKB)
	}
	coordinates := make([]float64, 0, totalPoints*2)
	validPoints := make([]bool, totalPoints)
	wkbArena := make([]byte, 0, totalWKBBytes)
	globalPointIndex := 0
	for index, layout := range layouts {
		geometry := features[index].Geometry.(core.WKBGeometry)
		start := len(wkbArena)
		wkbArena = append(wkbArena, geometry.WKB...)
		data := wkbArena[start:len(wkbArena)]
		features[index].Geometry = core.WKBGeometry{WKB: data}
		var order binary.ByteOrder = binary.LittleEndian
		if !layout.littleEndian {
			order = binary.BigEndian
		}
		polygonCount := int(order.Uint32(data[5:9]))
		offset := 9
		for polygonIndex := 0; polygonIndex < polygonCount; polygonIndex++ {
			pointCount := int(order.Uint32(data[offset+9 : offset+13]))
			offset += 13
			for pointIndex := 0; pointIndex < pointCount; pointIndex++ {
				x := math.Float64frombits(readWKBUint64(data, offset+pointIndex*16, layout.littleEndian))
				y := math.Float64frombits(readWKBUint64(data, offset+pointIndex*16+8, layout.littleEndian))
				if math.IsNaN(x) || math.IsNaN(y) {
					globalPointIndex++
					continue
				}
				validPoints[globalPointIndex] = true
				globalPointIndex++
				if sourceLatLon {
					coordinates = append(coordinates, y, x)
				} else {
					coordinates = append(coordinates, x, y)
				}
			}
			offset += pointCount * 16
		}
	}
	if err := pj.ForwardFlatCoords(coordinates, 2, -1, -1); err != nil {
		return true, fmt.Errorf("transform WKB MULTIPOLYGON layer: %w", err)
	}
	coordinateIndex := 0
	globalPointIndex = 0
	for _, layout := range layouts {
		data := features[layout.featureIndex].Geometry.(core.WKBGeometry).WKB
		var order binary.ByteOrder = binary.LittleEndian
		if !layout.littleEndian {
			order = binary.BigEndian
		}
		polygonCount := int(order.Uint32(data[5:9]))
		offset := 9
		for polygonIndex := 0; polygonIndex < polygonCount; polygonIndex++ {
			pointCount := int(order.Uint32(data[offset+9 : offset+13]))
			offset += 13
			for pointIndex := 0; pointIndex < pointCount; pointIndex++ {
				if !validPoints[globalPointIndex] {
					globalPointIndex++
					continue
				}
				x, y := coordinates[coordinateIndex], coordinates[coordinateIndex+1]
				coordinateIndex += 2
				globalPointIndex++
				if targetLatLon {
					x, y = y, x
				}
				pointOffset := offset + pointIndex*16
				writeWKBUint64(data, pointOffset, layout.littleEndian, math.Float64bits(x))
				writeWKBUint64(data, pointOffset+8, layout.littleEndian, math.Float64bits(y))
			}
			offset += pointCount * 16
		}
	}
	return true, nil
}

func parseWKBSimpleMultiPolygonLayout(data []byte) (wkbMultiPolygonLayout, bool) {
	if len(data) < 9 {
		return wkbMultiPolygonLayout{}, false
	}
	var order binary.ByteOrder
	switch data[0] {
	case 0:
		order = binary.BigEndian
	case 1:
		order = binary.LittleEndian
	default:
		return wkbMultiPolygonLayout{}, false
	}
	if order.Uint32(data[1:5]) != 6 {
		return wkbMultiPolygonLayout{}, false
	}
	polygonCount := uint64(order.Uint32(data[5:9]))
	maxInt := uint64(int(^uint(0) >> 1))
	if polygonCount > maxInt {
		return wkbMultiPolygonLayout{}, false
	}
	offset, totalPoints := 9, 0
	for polygonIndex := uint64(0); polygonIndex < polygonCount; polygonIndex++ {
		if offset+13 > len(data) || data[offset] != data[0] || order.Uint32(data[offset+1:offset+5]) != 3 || order.Uint32(data[offset+5:offset+9]) != 1 {
			return wkbMultiPolygonLayout{}, false
		}
		pointCount := uint64(order.Uint32(data[offset+9 : offset+13]))
		if pointCount > maxInt || pointCount > uint64((len(data)-offset-13)/16) ||
			pointCount > maxInt-uint64(totalPoints) {
			return wkbMultiPolygonLayout{}, false
		}
		offset += 13 + int(pointCount)*16
		totalPoints += int(pointCount)
	}
	if offset != len(data) {
		return wkbMultiPolygonLayout{}, false
	}
	return wkbMultiPolygonLayout{pointCount: totalPoints, littleEndian: data[0] == 1}, true
}

type wkbPolygonLayout struct {
	featureIndex int
	pointCount   int
	littleEndian bool
}

func transformWKBSimplePolygonLayer(features []core.Feature, pj *projlib.PJ, sourceLatLon, targetLatLon bool) (bool, error) {
	if len(features) == 0 {
		return false, nil
	}
	layouts := make([]wkbPolygonLayout, len(features))
	totalPoints := 0
	totalWKBBytes := 0
	for index := range features {
		geometry, ok := features[index].Geometry.(core.WKBGeometry)
		if !ok {
			return false, nil
		}
		layout, ok := parseWKBSimplePolygonLayout(geometry.WKB)
		if !ok {
			return false, nil
		}
		layout.featureIndex = index
		layouts[index] = layout
		if layout.pointCount > int(^uint(0)>>1)/2-totalPoints ||
			len(geometry.WKB) > int(^uint(0)>>1)-totalWKBBytes {
			return false, nil
		}
		totalPoints += layout.pointCount
		totalWKBBytes += len(geometry.WKB)
	}
	coordinates := make([]float64, 0, totalPoints*2)
	validPoints := make([]bool, totalPoints)
	wkbArena := make([]byte, 0, totalWKBBytes)
	globalPointIndex := 0
	for index, layout := range layouts {
		geometry := features[index].Geometry.(core.WKBGeometry)
		start := len(wkbArena)
		wkbArena = append(wkbArena, geometry.WKB...)
		data := wkbArena[start:len(wkbArena)]
		features[index].Geometry = core.WKBGeometry{WKB: data}
		for pointIndex := 0; pointIndex < layout.pointCount; pointIndex++ {
			offset := 13 + pointIndex*16
			x := math.Float64frombits(readWKBUint64(data, offset, layout.littleEndian))
			y := math.Float64frombits(readWKBUint64(data, offset+8, layout.littleEndian))
			if math.IsNaN(x) || math.IsNaN(y) {
				globalPointIndex++
				continue
			}
			validPoints[globalPointIndex] = true
			globalPointIndex++
			if sourceLatLon {
				coordinates = append(coordinates, y, x)
			} else {
				coordinates = append(coordinates, x, y)
			}
		}
	}
	if err := pj.ForwardFlatCoords(coordinates, 2, -1, -1); err != nil {
		return true, fmt.Errorf("transform WKB POLYGON layer: %w", err)
	}
	coordinateIndex := 0
	globalPointIndex = 0
	for _, layout := range layouts {
		data := features[layout.featureIndex].Geometry.(core.WKBGeometry).WKB
		for pointIndex := 0; pointIndex < layout.pointCount; pointIndex++ {
			if !validPoints[globalPointIndex] {
				globalPointIndex++
				continue
			}
			x, y := coordinates[coordinateIndex], coordinates[coordinateIndex+1]
			coordinateIndex += 2
			globalPointIndex++
			if targetLatLon {
				x, y = y, x
			}
			offset := 13 + pointIndex*16
			writeWKBUint64(data, offset, layout.littleEndian, math.Float64bits(x))
			writeWKBUint64(data, offset+8, layout.littleEndian, math.Float64bits(y))
		}
	}
	return true, nil
}

func parseWKBSimplePolygonLayout(data []byte) (wkbPolygonLayout, bool) {
	if len(data) < 13 {
		return wkbPolygonLayout{}, false
	}
	var order binary.ByteOrder
	switch data[0] {
	case 0:
		order = binary.BigEndian
	case 1:
		order = binary.LittleEndian
	default:
		return wkbPolygonLayout{}, false
	}
	if order.Uint32(data[1:5]) != 3 || order.Uint32(data[5:9]) != 1 {
		return wkbPolygonLayout{}, false
	}
	pointCount := uint64(order.Uint32(data[9:13]))
	maxInt := uint64(int(^uint(0) >> 1))
	if pointCount > maxInt || pointCount > uint64((len(data)-13)/16) ||
		13+int(pointCount)*16 != len(data) {
		return wkbPolygonLayout{}, false
	}
	return wkbPolygonLayout{pointCount: int(pointCount), littleEndian: data[0] == 1}, true
}

// transformWKTPointLayer batches the common 2D WKT POINT case. The generic
// WKT path remains responsible for lines, polygons, dimensions, and unusual
// formatting where preserving the original delimiters is important.
func transformWKTPointLayer(features []core.Feature, pj *projlib.PJ, sourceLatLon, targetLatLon bool) (bool, error) {
	if len(features) == 0 {
		return false, nil
	}
	coordinates := make([]float64, 0, len(features)*2)
	for index := range features {
		geometry, ok := features[index].Geometry.(core.WKTGeometry)
		if !ok {
			return false, nil
		}
		x, y, ok := parseWKTPointXY(geometry.WKT)
		if !ok {
			return false, nil
		}
		if sourceLatLon {
			coordinates = append(coordinates, y, x)
		} else {
			coordinates = append(coordinates, x, y)
		}
	}
	if err := pj.ForwardFlatCoords(coordinates, 2, -1, -1); err != nil {
		return true, fmt.Errorf("transform WKT POINT layer: %w", err)
	}
	for index := range features {
		x, y := coordinates[index*2], coordinates[index*2+1]
		if targetLatLon {
			x, y = y, x
		}
		var encodedBuffer [64]byte
		encoded := encodedBuffer[:0]
		encoded = append(encoded, "POINT ("...)
		encoded = strconv.AppendFloat(encoded, x, 'g', -1, 64)
		encoded = append(encoded, ' ')
		encoded = strconv.AppendFloat(encoded, y, 'g', -1, 64)
		encoded = append(encoded, ')')
		features[index].Geometry = core.WKTGeometry{WKT: string(encoded)}
	}
	return true, nil
}

func parseWKTPointXY(wkt string) (float64, float64, bool) {
	wkt = strings.TrimSpace(wkt)
	open := strings.IndexByte(wkt, '(')
	if open <= 0 || len(wkt) < open+3 || wkt[len(wkt)-1] != ')' ||
		!strings.EqualFold(strings.TrimSpace(wkt[:open]), "POINT") {
		return 0, 0, false
	}
	body := strings.TrimSpace(wkt[open+1 : len(wkt)-1])
	if body == "" || strings.IndexAny(body, "()") >= 0 {
		return 0, 0, false
	}
	xStart := nextWKTNumberStart(body, 0)
	if xStart < 0 || strings.TrimSpace(body[:xStart]) != "" {
		return 0, 0, false
	}
	xEnd := scanWKTNumber(body, xStart)
	x, err := strconv.ParseFloat(body[xStart:xEnd], 64)
	if err != nil {
		return 0, 0, false
	}
	yStart := nextWKTNumberStart(body, xEnd)
	if yStart < 0 || strings.TrimSpace(body[xEnd:yStart]) != "" {
		return 0, 0, false
	}
	yEnd := scanWKTNumber(body, yStart)
	y, err := strconv.ParseFloat(body[yStart:yEnd], 64)
	if err != nil || strings.TrimSpace(body[yEnd:]) != "" {
		return 0, 0, false
	}
	return x, y, true
}

func transformWKTLineStringLayer(features []core.Feature, pj *projlib.PJ, sourceLatLon, targetLatLon bool) (bool, error) {
	if len(features) == 0 {
		return false, nil
	}
	coordinates := make([]float64, 0, len(features)*4)
	starts := make([]int, len(features))
	counts := make([]int, len(features))
	for index := range features {
		geometry, ok := features[index].Geometry.(core.WKTGeometry)
		if !ok {
			return false, nil
		}
		start := len(coordinates)
		var appendedOK bool
		coordinates, appendedOK = appendWKTLineStringXY(coordinates, geometry.WKT)
		if !appendedOK {
			return false, nil
		}
		starts[index] = start
		counts[index] = (len(coordinates) - start) / 2
		if sourceLatLon {
			for point := start; point < len(coordinates); point += 2 {
				coordinates[point], coordinates[point+1] = coordinates[point+1], coordinates[point]
			}
		}
	}
	if err := pj.ForwardFlatCoords(coordinates, 2, -1, -1); err != nil {
		return true, fmt.Errorf("transform WKT LINESTRING layer: %w", err)
	}
	for index := range features {
		original := features[index].Geometry.(core.WKTGeometry).WKT
		estimatedSize := len(original) + 32*counts[index]
		var stackBuffer [128]byte
		var encoded []byte
		if estimatedSize <= len(stackBuffer) {
			encoded = stackBuffer[:0]
		} else {
			encoded = make([]byte, 0, estimatedSize)
		}
		encoded = append(encoded, "LINESTRING ("...)
		start := starts[index]
		for point := 0; point < counts[index]; point++ {
			if point > 0 {
				encoded = append(encoded, ',', ' ')
			}
			x, y := coordinates[start+point*2], coordinates[start+point*2+1]
			if targetLatLon {
				x, y = y, x
			}
			encoded = strconv.AppendFloat(encoded, x, 'g', -1, 64)
			encoded = append(encoded, ' ')
			encoded = strconv.AppendFloat(encoded, y, 'g', -1, 64)
		}
		encoded = append(encoded, ')')
		features[index].Geometry = core.WKTGeometry{WKT: string(encoded)}
	}
	return true, nil
}

func appendWKTLineStringXY(points []float64, wkt string) ([]float64, bool) {
	wkt = strings.TrimSpace(wkt)
	open := strings.IndexByte(wkt, '(')
	if open <= 0 || len(wkt) < open+5 || wkt[len(wkt)-1] != ')' ||
		!strings.EqualFold(strings.TrimSpace(wkt[:open]), "LINESTRING") {
		return points, false
	}
	body := strings.TrimSpace(wkt[open+1 : len(wkt)-1])
	if body == "" || strings.IndexAny(body, "()") >= 0 {
		return points, false
	}
	return appendWKTCoordinatePairs(points, body, 2)
}

func appendWKTCoordinatePairs(points []float64, body string, minimumPoints int) ([]float64, bool) {
	if body == "" || strings.IndexAny(body, "()") >= 0 {
		return points, false
	}
	start := len(points)
	for offset := 0; ; {
		for offset < len(body) && isWKTWhitespace(body[offset]) {
			offset++
		}
		if offset >= len(body) || !isWKTNumberStart(body[offset]) {
			return points, false
		}
		xEnd := scanWKTNumber(body, offset)
		x, err := strconv.ParseFloat(body[offset:xEnd], 64)
		if err != nil {
			return points, false
		}
		offset = xEnd
		for offset < len(body) && isWKTWhitespace(body[offset]) {
			offset++
		}
		if offset >= len(body) || !isWKTNumberStart(body[offset]) {
			return points, false
		}
		yEnd := scanWKTNumber(body, offset)
		y, err := strconv.ParseFloat(body[offset:yEnd], 64)
		if err != nil {
			return points, false
		}
		points = append(points, x, y)
		offset = yEnd
		for offset < len(body) && isWKTWhitespace(body[offset]) {
			offset++
		}
		if offset == len(body) {
			break
		}
		if body[offset] != ',' {
			return points, false
		}
		offset++
	}
	if len(points)-start < 4 {
		return points, false
	}
	if (len(points)-start)/2 < minimumPoints {
		return points, false
	}
	return points, true
}

func transformWKTSimplePolygonLayer(features []core.Feature, pj *projlib.PJ, sourceLatLon, targetLatLon bool) (bool, error) {
	if len(features) == 0 {
		return false, nil
	}
	coordinates := make([]float64, 0, len(features)*10)
	starts := make([]int, len(features))
	counts := make([]int, len(features))
	for index := range features {
		geometry, ok := features[index].Geometry.(core.WKTGeometry)
		if !ok {
			return false, nil
		}
		wkt := strings.TrimSpace(geometry.WKT)
		open := strings.IndexByte(wkt, '(')
		if open <= 0 || len(wkt) < open+7 || wkt[len(wkt)-1] != ')' ||
			!strings.EqualFold(strings.TrimSpace(wkt[:open]), "POLYGON") {
			return false, nil
		}
		body := strings.TrimSpace(wkt[open+1 : len(wkt)-1])
		if len(body) < 4 || body[0] != '(' || body[len(body)-1] != ')' {
			return false, nil
		}
		start := len(coordinates)
		var appendedOK bool
		coordinates, appendedOK = appendWKTCoordinatePairs(coordinates, strings.TrimSpace(body[1:len(body)-1]), 4)
		if !appendedOK {
			return false, nil
		}
		starts[index] = start
		counts[index] = (len(coordinates) - start) / 2
		if sourceLatLon {
			for point := start; point < len(coordinates); point += 2 {
				coordinates[point], coordinates[point+1] = coordinates[point+1], coordinates[point]
			}
		}
	}
	if err := pj.ForwardFlatCoords(coordinates, 2, -1, -1); err != nil {
		return true, fmt.Errorf("transform WKT POLYGON layer: %w", err)
	}
	for index := range features {
		original := features[index].Geometry.(core.WKTGeometry).WKT
		estimatedSize := len(original) + 32*counts[index]
		var stackBuffer [256]byte
		var encoded []byte
		if estimatedSize <= len(stackBuffer) {
			encoded = stackBuffer[:0]
		} else {
			encoded = make([]byte, 0, estimatedSize)
		}
		encoded = append(encoded, "POLYGON (("...)
		start := starts[index]
		for point := 0; point < counts[index]; point++ {
			if point > 0 {
				encoded = append(encoded, ',', ' ')
			}
			x, y := coordinates[start+point*2], coordinates[start+point*2+1]
			if targetLatLon {
				x, y = y, x
			}
			encoded = strconv.AppendFloat(encoded, x, 'g', -1, 64)
			encoded = append(encoded, ' ')
			encoded = strconv.AppendFloat(encoded, y, 'g', -1, 64)
		}
		encoded = append(encoded, ')', ')')
		features[index].Geometry = core.WKTGeometry{WKT: string(encoded)}
	}
	return true, nil
}

func isWKTNumberStart(character byte) bool {
	return (character >= '0' && character <= '9') || character == '+' || character == '-' || character == '.'
}

func isWKTWhitespace(character byte) bool {
	return character == ' ' || character == '\t' || character == '\r' || character == '\n'
}

func parseWKBPointLayout(data []byte) (wkbPointLayout, bool) {
	if len(data) < 5 {
		return wkbPointLayout{}, false
	}
	layout := wkbPointLayout{}
	switch data[0] {
	case 0:
		layout.littleEndian = false
	case 1:
		layout.littleEndian = true
	default:
		return wkbPointLayout{}, false
	}
	var typeCode uint32
	if layout.littleEndian {
		typeCode = binary.LittleEndian.Uint32(data[1:5])
	} else {
		typeCode = binary.BigEndian.Uint32(data[1:5])
	}
	hasZ := typeCode&0x80000000 != 0 || typeCode >= 1000 && typeCode < 2000
	hasM := typeCode&0x40000000 != 0 || typeCode >= 2000 && typeCode < 3000
	if typeCode&0x20000000 != 0 {
		if len(data) < 9 {
			return wkbPointLayout{}, false
		}
		// EWKB SRID metadata is four bytes immediately after the type code.
		layout.xOffset = 9
	} else {
		layout.xOffset = 5
	}
	base := typeCode & 0x0fffffff
	if base >= 1000 {
		base %= 1000
	}
	if base != 1 {
		return wkbPointLayout{}, false
	}
	if len(data) < layout.xOffset+16+8*(boolInt(hasZ)+boolInt(hasM)) {
		return wkbPointLayout{}, false
	}
	return layout, true
}

func readWKBUint64(data []byte, offset int, littleEndian bool) uint64 {
	if littleEndian {
		return binary.LittleEndian.Uint64(data[offset : offset+8])
	}
	return binary.BigEndian.Uint64(data[offset : offset+8])
}

func writeWKBUint64(data []byte, offset int, littleEndian bool, value uint64) {
	if littleEndian {
		binary.LittleEndian.PutUint64(data[offset:offset+8], value)
		return
	}
	binary.BigEndian.PutUint64(data[offset:offset+8], value)
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

// cloneLayerForTransform detaches mutable feature metadata without cloning
// geometry bytes up front. Every supported geometry is replaced with a newly
// transformed value below, so cloning WKB before MapWKBXY would duplicate the
// same binary buffer and add avoidable memory bandwidth.
func cloneLayerForTransform(layer core.Layer) core.Layer {
	result := layer
	result.Fields = append([]core.Field(nil), layer.Fields...)
	result.Features = make([]core.Feature, len(layer.Features))
	for index, feature := range layer.Features {
		clone := feature
		clone.Properties = maps.Clone(feature.Properties)
		if feature.Label != nil {
			label := *feature.Label
			clone.Label = &label
		}
		result.Features[index] = clone
	}
	return result
}

func transformCoordinate(x, y float64, pj *projlib.PJ, sourceLatLon, targetLatLon bool) (float64, float64, error) {
	coord := projlib.NewCoord(x, y, 0, 0)
	if sourceLatLon {
		coord[0], coord[1] = coord[1], coord[0]
	}
	transformed, err := pj.Forward(coord)
	if err != nil {
		return 0, 0, err
	}
	outputX, outputY := transformed.X(), transformed.Y()
	if targetLatLon {
		outputX, outputY = outputY, outputX
	}
	return outputX, outputY, nil
}

// transformXY keeps the core WKT contract in XY order while using PROJ's
// native axis order at the C boundary. EPSG:4326 is latitude/longitude in
// PROJ's native order; the projected Korean CRSs used by the MVP are already
// consumed and returned as easting/northing.
func transformXY(wkt string, pj *projlib.PJ, sourceLatLon, targetLatLon bool) (string, error) {
	if !containsWKTNumber(wkt) {
		return wkt, nil
	}
	var builder strings.Builder
	builder.Grow(len(wkt))
	last := 0
	for {
		xStart := nextWKTNumberStart(wkt, last)
		if xStart < 0 {
			break
		}
		xEnd := scanWKTNumber(wkt, xStart)
		x, err := strconv.ParseFloat(wkt[xStart:xEnd], 64)
		if err != nil {
			return "", err
		}
		yStart := nextWKTNumberStart(wkt, xEnd)
		if yStart < 0 {
			return "", fmt.Errorf("WKT must contain XY coordinate pairs")
		}
		yEnd := scanWKTNumber(wkt, yStart)
		y, err := strconv.ParseFloat(wkt[yStart:yEnd], 64)
		if err != nil {
			return "", err
		}
		outputX, outputY, err := transformCoordinate(x, y, pj, sourceLatLon, targetLatLon)
		if err != nil {
			return "", err
		}
		builder.WriteString(wkt[last:xStart])
		builder.WriteString(strconv.FormatFloat(outputX, 'g', -1, 64))
		builder.WriteString(wkt[xEnd:yStart])
		builder.WriteString(strconv.FormatFloat(outputY, 'g', -1, 64))
		last = yEnd
	}
	builder.WriteString(wkt[last:])
	return builder.String(), nil
}

func containsWKTNumber(wkt string) bool {
	return nextWKTNumberStart(wkt, 0) >= 0
}

func nextWKTNumberStart(wkt string, offset int) int {
	for index := offset; index < len(wkt); index++ {
		character := wkt[index]
		if character >= '0' && character <= '9' || character == '+' || character == '-' || character == '.' {
			return index
		}
	}
	return -1
}

func scanWKTNumber(wkt string, start int) int {
	index := start
	for index < len(wkt) {
		character := wkt[index]
		if character >= '0' && character <= '9' || character == '+' || character == '-' || character == '.' || character == 'e' || character == 'E' {
			index++
			continue
		}
		break
	}
	return index
}

func isLatitudeLongitudeCRS(crs core.CRS) bool {
	return strings.EqualFold(strings.TrimSpace(crs.AuthorityCode), "EPSG:4326")
}
