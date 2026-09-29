//go:build native

package gdal

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"gogis/internal/core"

	"github.com/airbusgeo/godal"
)

// Reader opens vector layers through GDAL/OGR. It supports any installed GDAL
// vector driver, including SHP and GeoPackage.
type Reader struct{}

// Open reads a layer into the core snapshot model. Feature IDs are assigned
// in read order because the current core model intentionally does not expose
// a GDAL-specific FID type.
func (Reader) Open(ctx context.Context, source, layerName string) (core.Layer, error) {
	if err := ctx.Err(); err != nil {
		return core.Layer{}, err
	}
	godal.RegisterAll()
	dataset, err := godal.Open(source)
	if err != nil {
		return core.Layer{}, fmt.Errorf("open %q: %w", source, err)
	}
	defer dataset.Close()

	var layer godal.Layer
	if layerName != "" {
		candidate := dataset.LayerByName(layerName)
		if candidate == nil {
			return core.Layer{}, fmt.Errorf("layer %q not found in %q", layerName, source)
		}
		layer = *candidate
	} else {
		layers := dataset.Layers()
		if len(layers) == 0 {
			return core.Layer{}, fmt.Errorf("dataset %q contains no vector layers", source)
		}
		layer = layers[0]
	}

	result := core.Layer{Name: layer.Name(), Editable: true}
	if spatialRef := layer.SpatialRef(); spatialRef != nil {
		defer spatialRef.Close()
		authorityName := spatialRef.AuthorityName("")
		authorityCode := spatialRef.AuthorityCode("")
		if authorityCode == "" {
			// Some drivers omit the authority node until GDAL identifies it.
			_ = spatialRef.AutoIdentifyEPSG()
			authorityName = spatialRef.AuthorityName("")
			authorityCode = spatialRef.AuthorityCode("")
		}
		if authorityCode != "" {
			if authorityName == "" {
				authorityName = "EPSG"
			}
			result.CRS.AuthorityCode = authorityName + ":" + authorityCode
		}
	}

	layer.ResetReading()
	var nextID uint64 = 1
	for {
		if err := ctx.Err(); err != nil {
			return core.Layer{}, err
		}
		feature := layer.NextFeature()
		if feature == nil {
			break
		}
		var featureErr error
		func() {
			defer feature.Close()
			fields := feature.Fields()
			if len(result.Fields) == 0 {
				result.Fields = fieldSchema(fields)
			}
			properties := make(map[string]any, len(fields))
			for name, field := range fields {
				properties[name] = fieldValue(field)
			}
			geometry := feature.Geometry()
			if geometry == nil {
				return
			}
			defer geometry.Close()
			wkt, wktErr := geometry.WKT()
			if wktErr != nil {
				featureErr = wktErr
				return
			}
			result.Features = append(result.Features, core.Feature{ID: nextID, Geometry: core.WKTGeometry{WKT: wkt}, Properties: properties})
			nextID++
		}()
		if featureErr != nil {
			return core.Layer{}, fmt.Errorf("read feature %d geometry: %w", nextID, featureErr)
		}
	}
	return result, nil
}

func fieldSchema(fields map[string]godal.Field) []core.Field {
	names := make([]string, 0, len(fields))
	for name := range fields {
		names = append(names, name)
	}
	sort.Strings(names)
	result := make([]core.Field, 0, len(names))
	for _, name := range names {
		fieldType := core.FieldTypeText
		switch fields[name].Type() {
		case godal.FTInt, godal.FTInt64:
			fieldType = core.FieldTypeNumber
		case godal.FTReal:
			fieldType = core.FieldTypeNumber
		}
		result = append(result, core.Field{Name: name, Type: fieldType})
	}
	return result
}

func fieldValue(field godal.Field) any {
	switch field.Type() {
	case godal.FTInt, godal.FTInt64:
		return field.Int()
	case godal.FTReal:
		return field.Float()
	case godal.FTString:
		return strings.TrimSpace(field.String())
	default:
		return field.String()
	}
}
