//go:build native

package gdal

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"gogis/internal/core"

	"github.com/airbusgeo/godal"
)

// Writer persists a core layer through an OGR vector creation driver. The
// driver is selected from the destination extension so the same boundary can
// write GeoPackage and ESRI Shapefile datasets.
type Writer struct{}

var shapefileConfigMu sync.Mutex

// Write creates a new vector dataset and writes the layer schema, attributes,
// and WKT geometries. Existing destinations are intentionally rejected by the
// native driver rather than being silently replaced.
func (Writer) Write(ctx context.Context, destination string, layer core.Layer) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if layer.Name == "" {
		return fmt.Errorf("layer name is required")
	}
	if len(layer.Features) == 0 {
		return fmt.Errorf("layer %q has no features to infer geometry type", layer.Name)
	}

	registerDrivers()
	driver, err := driverForDestination(destination)
	if err != nil {
		return err
	}
	geometryType, err := ogrGeometryType(layer.Features[0])
	if err != nil {
		return err
	}
	for _, feature := range layer.Features[1:] {
		other, err := ogrGeometryType(feature)
		if err != nil {
			return err
		}
		if other != geometryType {
			return fmt.Errorf("feature %d geometry type %v conflicts with %v", feature.ID, other, geometryType)
		}
	}

	if driver == godal.Shapefile {
		shapefileConfigMu.Lock()
		defer shapefileConfigMu.Unlock()
		// GDAL reads SHAPE_ENCODING while the layer writes DBF values. godal's
		// per-call config scope ends after CreateVector, so keep the process
		// option active for layer creation and feature writes as well.
		previous, hadPrevious := os.LookupEnv("SHAPE_ENCODING")
		if err := os.Setenv("SHAPE_ENCODING", "UTF-8"); err != nil {
			return fmt.Errorf("set SHAPE_ENCODING: %w", err)
		}
		defer func() {
			if hadPrevious {
				_ = os.Setenv("SHAPE_ENCODING", previous)
			} else {
				_ = os.Unsetenv("SHAPE_ENCODING")
			}
		}()
	}
	dataset, err := godal.CreateVector(driver, destination)
	if err != nil {
		return fmt.Errorf("create %s dataset %q: %w", driver, destination, err)
	}
	closed := false
	defer func() {
		if !closed {
			_ = dataset.Close()
		}
	}()

	var spatialRef *godal.SpatialRef
	if layer.CRS.AuthorityCode != "" {
		spatialRef, err = godal.NewSpatialRef(layer.CRS.AuthorityCode)
		if err != nil {
			return fmt.Errorf("create spatial reference %q: %w", layer.CRS.AuthorityCode, err)
		}
		defer spatialRef.Close()
	}
	fieldDefinitions, err := fieldDefinitions(layer.Fields)
	if err != nil {
		return err
	}
	outputLayer, err := dataset.CreateLayer(layer.Name, spatialRef, geometryType, fieldDefinitions...)
	if err != nil {
		return fmt.Errorf("create layer %q: %w", layer.Name, err)
	}

	for _, feature := range layer.Features {
		if err := ctx.Err(); err != nil {
			return err
		}
		ogrGeometry, err := newOGRGeometry(feature.Geometry, spatialRef)
		if err != nil {
			return fmt.Errorf("feature %d geometry: %w", feature.ID, err)
		}
		ogrFeature, err := outputLayer.NewFeature(ogrGeometry)
		ogrGeometry.Close()
		if err != nil {
			return fmt.Errorf("create feature %d: %w", feature.ID, err)
		}
		if err := setFields(ogrFeature, feature.Properties, layer.Fields); err != nil {
			ogrFeature.Close()
			return fmt.Errorf("feature %d attributes: %w", feature.ID, err)
		}
		if err := outputLayer.UpdateFeature(ogrFeature); err != nil {
			ogrFeature.Close()
			return fmt.Errorf("write feature %d: %w", feature.ID, err)
		}
		ogrFeature.Close()
	}
	if err := dataset.Close(); err != nil {
		return fmt.Errorf("close dataset %q: %w", destination, err)
	}
	closed = true
	if driver == godal.Shapefile {
		// The DBF encoding is carried by a sidecar code-page file so readers do
		// not need a process-global SHAPE_ENCODING setting.
		if err := os.WriteFile(strings.TrimSuffix(destination, filepath.Ext(destination))+".cpg", []byte("UTF-8\n"), 0o600); err != nil {
			return fmt.Errorf("write shapefile code-page file: %w", err)
		}
	}
	return nil
}

func driverForDestination(destination string) (godal.DriverName, error) {
	switch strings.ToLower(filepath.Ext(destination)) {
	case ".gpkg":
		return godal.GeoPackage, nil
	case ".shp":
		return godal.Shapefile, nil
	default:
		return "", fmt.Errorf("unsupported vector output extension %q; use .gpkg or .shp", filepath.Ext(destination))
	}
}

func ogrGeometryType(feature core.Feature) (godal.GeometryType, error) {
	if feature.Geometry == nil {
		return godal.GTUnknown, fmt.Errorf("feature %d geometry is nil", feature.ID)
	}
	switch feature.Geometry.GeometryType() {
	case "POINT":
		return godal.GTPoint, nil
	case "LINESTRING":
		return godal.GTLineString, nil
	case "POLYGON":
		return godal.GTPolygon, nil
	default:
		return godal.GTUnknown, fmt.Errorf("feature %d has unsupported geometry type %q", feature.ID, feature.Geometry.GeometryType())
	}
}

func newOGRGeometry(geometry core.Geometry, spatialRef *godal.SpatialRef) (*godal.Geometry, error) {
	switch value := geometry.(type) {
	case core.WKBGeometry:
		if len(value.WKB) == 0 {
			return nil, fmt.Errorf("WKB geometry is empty")
		}
		return godal.NewGeometryFromWKB(value.WKB, spatialRef)
	default:
		wkt, err := core.ToWKT(geometry)
		if err != nil {
			return nil, err
		}
		return godal.NewGeometryFromWKT(wkt.WKT, spatialRef)
	}
}

func fieldDefinitions(fields []core.Field) ([]godal.CreateLayerOption, error) {
	options := make([]godal.CreateLayerOption, 0, len(fields))
	for _, field := range fields {
		if field.Name == "" {
			return nil, fmt.Errorf("field name is required")
		}
		fieldType, err := godalFieldType(field.Type)
		if err != nil {
			return nil, fmt.Errorf("field %q: %w", field.Name, err)
		}
		options = append(options, godal.NewFieldDefinition(field.Name, fieldType))
	}
	return options, nil
}

func godalFieldType(fieldType core.FieldType) (godal.FieldType, error) {
	switch fieldType {
	case core.FieldTypeText:
		return godal.FTString, nil
	case core.FieldTypeNumber:
		return godal.FTReal, nil
	case core.FieldTypeBool:
		return godal.FTInt, nil
	default:
		return godal.FTUnknown, fmt.Errorf("unsupported field type %q", fieldType)
	}
}

func setFields(feature *godal.Feature, properties map[string]any, fields []core.Field) error {
	ogrFields := feature.Fields()
	for _, field := range fields {
		value, exists := properties[field.Name]
		if !exists {
			continue
		}
		ogrField, exists := ogrFields[field.Name]
		if !exists {
			return fmt.Errorf("field %q was not created", field.Name)
		}
		switch field.Type {
		case core.FieldTypeText:
			text, ok := value.(string)
			if !ok {
				return fmt.Errorf("value for %q must be string", field.Name)
			}
			if err := feature.SetFieldValue(ogrField, text); err != nil {
				return err
			}
		case core.FieldTypeNumber:
			number, ok := numberValue(value)
			if !ok {
				return fmt.Errorf("value for %q must be numeric", field.Name)
			}
			if err := feature.SetFieldValue(ogrField, number); err != nil {
				return err
			}
		case core.FieldTypeBool:
			boolean, ok := value.(bool)
			if !ok {
				return fmt.Errorf("value for %q must be bool", field.Name)
			}
			integer := 0
			if boolean {
				integer = 1
			}
			if err := feature.SetFieldValue(ogrField, integer); err != nil {
				return err
			}
		}
	}
	return nil
}

func numberValue(value any) (float64, bool) {
	switch number := value.(type) {
	case float32:
		return float64(number), true
	case float64:
		return number, true
	case int:
		return float64(number), true
	case int32:
		return float64(number), true
	case int64:
		return float64(number), true
	case uint:
		return float64(number), true
	case uint32:
		return float64(number), true
	case uint64:
		return float64(number), true
	default:
		return 0, false
	}
}
