//go:build native

package gdal

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"gogis/drivers"
	"gogis/internal/core"

	"github.com/airbusgeo/godal"
)

const maxInitialFeatureCapacity = 65536

func initialFeatureCapacity(count int) int {
	return max(0, min(count, maxInitialFeatureCapacity))
}

// Reader opens vector layers through GDAL/OGR. It supports any installed GDAL
// vector driver, including SHP and GeoPackage.
// Reader reads a dataset. Encoding overrides the source's declared DBF
// encoding for Shapefile inputs only; an empty value follows GDAL's detection.
type Reader struct{ Encoding string }

func openDataset(source, encoding string) (*godal.Dataset, error) {
	if encoding == "" || !isShapefilePath(source) {
		return godal.Open(source)
	}
	if strings.ContainsAny(encoding, "\x00\r\n") {
		return nil, fmt.Errorf("invalid source encoding %q", encoding)
	}
	// ENCODING is an OGR Shapefile open option. It is scoped to this dataset
	// open and avoids mutating GDAL's process-wide SHAPE_ENCODING config.
	return godal.Open(source, godal.DriverOpenOption("ENCODING="+encoding))
}

func isShapefilePath(source string) bool {
	path := strings.ToLower(source)
	return strings.HasSuffix(path, ".shp") || strings.HasSuffix(path, ".shz") || strings.HasSuffix(path, ".shp.zip")
}

var _ drivers.LayerReader = Reader{}
var _ drivers.GeometryOnlyReader = Reader{}
var _ drivers.FeatureReader = Reader{}
var _ drivers.AttributePageReader = Reader{}
var _ drivers.LayerCollectionReader = Reader{}
var _ drivers.GeometryOnlyCollectionReader = Reader{}
var _ drivers.LayerWindowReader = Reader{}
var _ drivers.GeometryOnlyWindowReader = Reader{}

// Open reads a layer into the core snapshot model. Feature IDs are assigned
// in read order because the current core model intentionally does not expose
// a GDAL-specific FID type.
func (reader Reader) Open(ctx context.Context, source, layerName string) (core.Layer, error) {
	if err := ctx.Err(); err != nil {
		return core.Layer{}, err
	}
	registerDrivers()
	dataset, err := openDataset(source, reader.Encoding)
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

	return readLayer(ctx, layer)
}

// OpenGeometryOnly reads layer identity, CRS, feature IDs, and geometry while
// skipping attribute maps. Render pipelines can use this path when properties
// are fetched separately on demand.
func (reader Reader) OpenGeometryOnly(ctx context.Context, source, layerName string) (core.Layer, error) {
	if err := ctx.Err(); err != nil {
		return core.Layer{}, err
	}
	registerDrivers()
	dataset, err := openDataset(source, reader.Encoding)
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
	return readLayerOptions(ctx, layer, false)
}

// OpenFeature reads one feature by the sequential application ID assigned by
// this reader. It avoids materializing properties for preceding features;
// the underlying driver still scans until the requested ordinal is reached.
func (reader Reader) OpenFeature(ctx context.Context, source, layerName string, featureID uint64) (core.Feature, error) {
	if featureID == 0 {
		return core.Feature{}, fmt.Errorf("feature ID must be positive")
	}
	if err := ctx.Err(); err != nil {
		return core.Feature{}, err
	}
	registerDrivers()
	dataset, err := openDataset(source, reader.Encoding)
	if err != nil {
		return core.Feature{}, fmt.Errorf("open %q: %w", source, err)
	}
	defer dataset.Close()
	return openFeatureDataset(ctx, dataset, source, layerName, featureID)
}

func openFeatureDataset(ctx context.Context, dataset *godal.Dataset, source, layerName string, featureID uint64) (core.Feature, error) {
	layer, err := selectLayer(dataset, source, layerName)
	if err != nil {
		return core.Feature{}, err
	}
	result, _, err := openFeatureLayer(ctx, layer, featureID, 1, true)
	return result, err
}

func openFeatureLayer(ctx context.Context, layer godal.Layer, featureID, nextID uint64, reset bool) (core.Feature, uint64, error) {
	if reset {
		layer.ResetReading()
		nextID = 1
	}
	for {
		if err := ctx.Err(); err != nil {
			return core.Feature{}, nextID, err
		}
		feature := layer.NextFeature()
		if feature == nil {
			break
		}
		if nextID != featureID {
			feature.Close()
			nextID++
			continue
		}
		result, err := readFeature(feature, nextID, true)
		feature.Close()
		if err != nil {
			return core.Feature{}, nextID, fmt.Errorf("read feature %d: %w", nextID, err)
		}
		return result, nextID + 1, nil
	}
	return core.Feature{}, nextID, fmt.Errorf("feature %d not found in layer %q", featureID, layer.Name())
}

// OpenAttributePage reads only one property page. It scans preceding features
// to preserve the reader's sequential IDs but does not decode their fields or
// geometries.
func (reader Reader) OpenAttributePage(ctx context.Context, source, layerName string, offset, limit int) (core.Layer, int, error) {
	if offset < 0 || limit <= 0 {
		return core.Layer{}, 0, fmt.Errorf("invalid attribute page: offset=%d limit=%d", offset, limit)
	}
	if err := ctx.Err(); err != nil {
		return core.Layer{}, 0, err
	}
	registerDrivers()
	dataset, err := openDataset(source, reader.Encoding)
	if err != nil {
		return core.Layer{}, 0, fmt.Errorf("open %q: %w", source, err)
	}
	defer dataset.Close()
	return openAttributePageDataset(ctx, dataset, source, layerName, offset, limit)
}

func openAttributePageDataset(ctx context.Context, dataset *godal.Dataset, source, layerName string, offset, limit int) (core.Layer, int, error) {
	layer, err := selectLayer(dataset, source, layerName)
	if err != nil {
		return core.Layer{}, 0, err
	}
	total, countErr := layer.FeatureCount()
	if countErr != nil {
		total = -1
	}
	query := fmt.Sprintf("SELECT * FROM %s LIMIT %d OFFSET %d", quoteSQLIdentifier(layer.Name()), limit, offset)
	if resultSet, sqlErr := dataset.ExecuteSQL(query, godal.OGRSQLDialect()); sqlErr == nil && resultSet != nil {
		defer resultSet.Close()
		result, resultErr := readAttributePageLayer(ctx, resultSet.Layer, offset, limit)
		if resultErr != nil {
			return core.Layer{}, 0, resultErr
		}
		if total < 0 {
			total = offset + len(result.Features)
		}
		return result, total, nil
	}
	result := core.Layer{
		Name:     layer.Name(),
		Editable: true,
		Features: make([]core.Feature, 0, limit),
	}
	layer.ResetReading()
	var ordinal int
	for {
		if err := ctx.Err(); err != nil {
			return core.Layer{}, 0, err
		}
		feature := layer.NextFeature()
		if feature == nil {
			break
		}
		if ordinal < offset {
			feature.Close()
			ordinal++
			continue
		}
		if ordinal >= offset+limit {
			feature.Close()
			break
		}
		fields := feature.Fields()
		if len(result.Fields) == 0 {
			result.Fields = fieldSchema(fields)
		}
		properties := make(map[string]any, len(fields))
		for name, field := range fields {
			properties[name] = fieldValue(field)
		}
		result.Features = append(result.Features, core.Feature{ID: uint64(ordinal + 1), Properties: properties})
		feature.Close()
		ordinal++
	}
	if total < 0 {
		total = ordinal
	}
	return result, total, nil
}

func readAttributePageLayer(ctx context.Context, layer godal.Layer, offset, limit int) (core.Layer, error) {
	result := core.Layer{
		Name:     layer.Name(),
		Editable: true,
		Features: make([]core.Feature, 0, limit),
	}
	layer.ResetReading()
	for index := 0; index < limit; index++ {
		if err := ctx.Err(); err != nil {
			return core.Layer{}, err
		}
		feature := layer.NextFeature()
		if feature == nil {
			break
		}
		fields := feature.Fields()
		if len(result.Fields) == 0 {
			result.Fields = fieldSchema(fields)
		}
		properties := make(map[string]any, len(fields))
		for name, field := range fields {
			properties[name] = fieldValue(field)
		}
		result.Features = append(result.Features, core.Feature{ID: uint64(offset + index + 1), Properties: properties})
		feature.Close()
	}
	return result, nil
}

func readAttributePageLayerCursor(ctx context.Context, layer godal.Layer, offset, limit int, nextID uint64, reset bool, schema []core.Field) (core.Layer, uint64, error) {
	result := core.Layer{
		Name:     layer.Name(),
		Editable: true,
		Features: make([]core.Feature, 0, limit),
	}
	if reset {
		layer.ResetReading()
		nextID = 1
	}
	for nextID < uint64(offset)+1 {
		if err := ctx.Err(); err != nil {
			return core.Layer{}, nextID, err
		}
		feature := layer.NextFeature()
		if feature == nil {
			return result, nextID, nil
		}
		feature.Close()
		nextID++
	}
	for index := 0; index < limit; index++ {
		if err := ctx.Err(); err != nil {
			return core.Layer{}, nextID, err
		}
		feature := layer.NextFeature()
		if feature == nil {
			break
		}
		fields := feature.Fields()
		if len(result.Fields) == 0 {
			if len(schema) > 0 {
				result.Fields = append(result.Fields, schema...)
			} else {
				result.Fields = fieldSchema(fields)
			}

		}
		properties := make(map[string]any, len(fields))
		for name, field := range fields {
			properties[name] = fieldValue(field)
		}
		result.Features = append(result.Features, core.Feature{ID: nextID, Properties: properties})
		feature.Close()
		nextID++
	}
	return result, nextID, nil
}

func selectLayer(dataset *godal.Dataset, source, layerName string) (godal.Layer, error) {
	if layerName != "" {
		candidate := dataset.LayerByName(layerName)
		if candidate == nil {
			return godal.Layer{}, fmt.Errorf("layer %q not found in %q", layerName, source)
		}
		return *candidate, nil
	}
	layers := dataset.Layers()
	if len(layers) == 0 {
		return godal.Layer{}, fmt.Errorf("dataset %q contains no vector layers", source)
	}
	return layers[0], nil
}

// OpenWindow reads only features intersecting bounds, which is useful for
// viewport-driven loading of large vector layers. Bounds are minX, minY,
// maxX, maxY in the source layer's CRS.
func (reader Reader) OpenWindow(ctx context.Context, source, layerName string, bounds [4]float64) (core.Layer, error) {
	return reader.openWindow(ctx, source, layerName, bounds, true)
}

// OpenWindowGeometryOnly reads only features intersecting bounds and skips
// properties. It is intended for viewport renderers that load attributes on
// demand through OpenFeature or OpenAttributePage.
func (reader Reader) OpenWindowGeometryOnly(ctx context.Context, source, layerName string, bounds [4]float64) (core.Layer, error) {
	return reader.openWindow(ctx, source, layerName, bounds, false)
}

func (reader Reader) openWindow(ctx context.Context, source, layerName string, bounds [4]float64, includeProperties bool) (core.Layer, error) {
	if err := ctx.Err(); err != nil {
		return core.Layer{}, err
	}
	if bounds[0] > bounds[2] || bounds[1] > bounds[3] {
		return core.Layer{}, fmt.Errorf("invalid spatial window: [%v %v %v %v]", bounds[0], bounds[1], bounds[2], bounds[3])
	}
	registerDrivers()
	dataset, err := openDataset(source, reader.Encoding)
	if err != nil {
		return core.Layer{}, fmt.Errorf("open %q: %w", source, err)
	}
	defer dataset.Close()
	return openWindowDataset(ctx, dataset, source, layerName, bounds, includeProperties)
}

func openWindowDataset(ctx context.Context, dataset *godal.Dataset, source, layerName string, bounds [4]float64, includeProperties bool) (core.Layer, error) {
	layer, err := selectLayer(dataset, source, layerName)
	if err != nil {
		return core.Layer{}, err
	}
	return openWindowLayer(ctx, dataset, source, layer, layerName, bounds, includeProperties)
}

func openWindowLayer(ctx context.Context, dataset *godal.Dataset, source string, layer godal.Layer, layerName string, bounds [4]float64, includeProperties bool) (core.Layer, error) {
	if layerName == "" {
		layerName = layer.Name()
	}
	// GeoJSON is commonly backed by a streaming parser. For this driver,
	// OGRSQL result-set creation can cost more than scanning the source once
	// and asking GDAL for each feature envelope. Keep the SQL path for indexed
	// formats such as SHP and GeoPackage.
	if isJSONVectorSource(source) {
		return readWindowLayer(ctx, layer, layerName, bounds, includeProperties)
	}

	filterWKT := fmt.Sprintf("POLYGON ((%[1]g %[2]g, %[3]g %[2]g, %[3]g %[4]g, %[1]g %[4]g, %[1]g %[2]g))", bounds[0], bounds[1], bounds[2], bounds[3])
	filter, err := godal.NewGeometryFromWKT(filterWKT, nil)
	if err != nil {
		return core.Layer{}, fmt.Errorf("create spatial window: %w", err)
	}
	defer filter.Close()
	resultSet, err := dataset.ExecuteSQL("SELECT * FROM "+quoteSQLIdentifier(layerName), godal.SpatialFilter(filter))
	if err != nil {
		return core.Layer{}, fmt.Errorf("apply spatial window to layer %q: %w", layerName, err)
	}
	if resultSet == nil {
		return core.Layer{Name: layerName, Editable: true}, nil
	}
	defer resultSet.Close()
	result, err := readLayerOptions(ctx, resultSet.Layer, includeProperties)
	if err != nil {
		return core.Layer{}, err
	}
	result.Name = layerName
	return result, nil
}

func isJSONVectorSource(source string) bool {
	switch strings.ToLower(filepath.Ext(source)) {
	case ".geojson", ".json":
		return true
	default:
		return false
	}
}

func readWindowLayer(ctx context.Context, layer godal.Layer, layerName string, bounds [4]float64, includeProperties bool) (core.Layer, error) {
	result := core.Layer{Name: layerName, Editable: true}
	if spatialRef := layer.SpatialRef(); spatialRef != nil {
		defer spatialRef.Close()
		authorityName := spatialRef.AuthorityName("")
		authorityCode := spatialRef.AuthorityCode("")
		if authorityCode == "" {
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
			geometry := feature.Geometry()
			if geometry == nil {
				nextID++
				return
			}
			defer geometry.Close()
			envelope, err := geometry.Bounds()
			if err != nil {
				featureErr = err
				return
			}
			if envelope[2] < bounds[0] || envelope[0] > bounds[2] ||
				envelope[3] < bounds[1] || envelope[1] > bounds[3] {
				nextID++
				return
			}
			var fields map[string]godal.Field
			if includeProperties {
				fields = feature.Fields()
				if len(result.Fields) == 0 {
					result.Fields = fieldSchema(fields)
				}
			}
			loaded, readErr := readFeatureFieldsWithGeometry(feature, nextID, fields, geometry)
			if readErr != nil {
				featureErr = readErr
				return
			}
			result.Features = append(result.Features, loaded)
			nextID++
		}()
		if featureErr != nil {
			return core.Layer{}, fmt.Errorf("read feature %d: %w", nextID, featureErr)
		}
	}
	return result, nil
}

func quoteSQLIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// OpenAll reads every vector layer in a dataset as detached core snapshots.
// It is used by the desktop project loader so a multi-layer GeoPackage can be
// displayed without reopening the dataset per layer.
func (reader Reader) OpenAll(ctx context.Context, source string) ([]core.Layer, error) {
	return reader.openAll(ctx, source, true)
}

// OpenAllGeometryOnly reads all layers and geometry while skipping properties.
// It is intended for read-only render pipelines; callers that edit or save
// must use OpenAll so source attributes remain available.
func (reader Reader) OpenAllGeometryOnly(ctx context.Context, source string) ([]core.Layer, error) {
	return reader.openAll(ctx, source, false)
}

func (reader Reader) openAll(ctx context.Context, source string, includeProperties bool) ([]core.Layer, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	registerDrivers()
	dataset, err := openDataset(source, reader.Encoding)
	if err != nil {
		return nil, fmt.Errorf("open %q: %w", source, err)
	}
	defer dataset.Close()
	return openAllDataset(ctx, dataset, source, includeProperties)
}

func openAllDataset(ctx context.Context, dataset *godal.Dataset, source string, includeProperties bool) ([]core.Layer, error) {
	layers := dataset.Layers()
	if len(layers) == 0 {
		return nil, fmt.Errorf("dataset %q contains no vector layers", source)
	}
	result := make([]core.Layer, 0, len(layers))
	for _, layer := range layers {
		loaded, err := readLayerOptions(ctx, layer, includeProperties)
		if err != nil {
			return nil, err
		}
		result = append(result, loaded)
	}
	return result, nil
}

func readLayer(ctx context.Context, layer godal.Layer) (core.Layer, error) {
	return readLayerOptions(ctx, layer, true)
}

func readLayerOptions(ctx context.Context, layer godal.Layer, includeProperties bool) (core.Layer, error) {
	result := readLayerHeader(layer)
	// FeatureCount lets the common SHP/GeoPackage drivers reserve the final
	// feature slice up front. Driver counts are not always trustworthy, however,
	// and a very large reservation can exhaust memory before the first feature
	// is read. Bound the eager reservation; append grows the slice as needed.
	if count, countErr := layer.FeatureCount(); countErr == nil && count > 0 {
		result.Features = make([]core.Feature, 0, initialFeatureCapacity(count))
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
			var fields map[string]godal.Field
			if includeProperties && len(result.Fields) == 0 {
				fields = feature.Fields()
				result.Fields = fieldSchema(fields)
			} else if includeProperties {
				fields = feature.Fields()
			}
			loaded, err := readFeatureFields(feature, nextID, fields)
			if err != nil {
				featureErr = err
				return
			}
			result.Features = append(result.Features, loaded)
			nextID++
		}()
		if featureErr != nil {
			return core.Layer{}, fmt.Errorf("read feature %d geometry: %w", nextID, featureErr)
		}
	}
	return result, nil
}

func readLayerHeader(layer godal.Layer) core.Layer {
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
	return result
}

func readFeature(feature *godal.Feature, id uint64, includeProperties bool) (core.Feature, error) {
	var fields map[string]godal.Field
	if includeProperties {
		fields = feature.Fields()
	}
	return readFeatureFields(feature, id, fields)
}

func readFeatureFields(feature *godal.Feature, id uint64, fields map[string]godal.Field) (core.Feature, error) {
	geometry := feature.Geometry()
	if geometry == nil {
		return readFeatureFieldsWithGeometry(feature, id, fields, nil)
	}
	defer geometry.Close()
	return readFeatureFieldsWithGeometry(feature, id, fields, geometry)
}

func readFeatureFieldsWithGeometry(feature *godal.Feature, id uint64, fields map[string]godal.Field, geometry *godal.Geometry) (core.Feature, error) {
	var properties map[string]any
	if fields != nil {
		properties = make(map[string]any, len(fields))
		for name, field := range fields {
			properties[name] = fieldValue(field)
		}
	}
	if geometry == nil {
		return core.Feature{ID: id, Properties: properties}, nil
	}
	wkb, err := geometry.WKB()
	if err != nil {
		return core.Feature{}, err
	}
	return core.Feature{ID: id, Geometry: core.WKBGeometry{WKB: wkb}, Properties: properties}, nil
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
