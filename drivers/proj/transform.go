//go:build native

package proj

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"gogis/internal/core"

	projlib "github.com/twpayne/go-proj/v11"
)

var numberPattern = regexp.MustCompile(`[-+]?(?:\d+(?:\.\d*)?|\.\d+)(?:[eE][-+]?\d+)?`)

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

	result := layer.Clone()
	result.CRS = target
	for i := range result.Features {
		if err := ctx.Err(); err != nil {
			return core.Layer{}, err
		}
		geometry, ok := result.Features[i].Geometry.(core.WKTGeometry)
		if !ok {
			return core.Layer{}, fmt.Errorf("feature %d geometry is not core.WKTGeometry", result.Features[i].ID)
		}
		wkt, err := transformXY(geometry.WKT, pj, source, target)
		if err != nil {
			return core.Layer{}, fmt.Errorf("feature %d: %w", result.Features[i].ID, err)
		}
		result.Features[i].Geometry = core.WKTGeometry{WKT: wkt}
	}
	return result, nil
}

// transformXY keeps the core WKT contract in XY order while using PROJ's
// native axis order at the C boundary. EPSG:4326 is latitude/longitude in
// PROJ's native order; the projected Korean CRSs used by the MVP are already
// consumed and returned as easting/northing.
func transformXY(wkt string, pj *projlib.PJ, source, target core.CRS) (string, error) {
	numbers := numberPattern.FindAllStringIndex(wkt, -1)
	if len(numbers)%2 != 0 {
		return "", fmt.Errorf("WKT must contain XY coordinate pairs")
	}
	var builder strings.Builder
	last := 0
	for i := 0; i < len(numbers); i += 2 {
		span := numbers[i]
		builder.WriteString(wkt[last:span[0]])
		value, err := strconv.ParseFloat(wkt[span[0]:span[1]], 64)
		if err != nil {
			return "", err
		}
		if i%2 == 0 {
			x := value
			if i+1 >= len(numbers) {
				return "", fmt.Errorf("missing y coordinate")
			}
			ySpan := numbers[i+1]
			y, err := strconv.ParseFloat(wkt[ySpan[0]:ySpan[1]], 64)
			if err != nil {
				return "", err
			}
			coord := projlib.NewCoord(x, y, 0, 0)
			if isLatitudeLongitudeCRS(source) {
				coord[0], coord[1] = coord[1], coord[0]
			}
			transformed, err := pj.Forward(coord)
			if err != nil {
				return "", err
			}
			outputX, outputY := transformed.X(), transformed.Y()
			if isLatitudeLongitudeCRS(target) {
				outputX, outputY = outputY, outputX
			}
			builder.WriteString(strconv.FormatFloat(outputX, 'g', -1, 64))
			builder.WriteString(wkt[span[1]:ySpan[0]])
			builder.WriteString(strconv.FormatFloat(outputY, 'g', -1, 64))
			last = ySpan[1]
		}
	}
	builder.WriteString(wkt[last:])
	return builder.String(), nil
}

func isLatitudeLongitudeCRS(crs core.CRS) bool {
	return strings.EqualFold(strings.TrimSpace(crs.AuthorityCode), "EPSG:4326")
}
