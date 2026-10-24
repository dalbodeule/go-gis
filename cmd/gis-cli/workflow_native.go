//go:build native

package main

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gogis/drivers/gdal"
	geosdriver "gogis/drivers/geos"
	"gogis/internal/commands"
	"gogis/internal/core"
)

func workflowDelimiter(value string) rune {
	if value == "\\t" || strings.EqualFold(value, "tab") {
		return '\t'
	}
	runes := []rune(value)
	if len(runes) == 1 {
		return runes[0]
	}
	return ','
}

func parseFieldMappings(value string) (map[string]string, error) {
	result := map[string]string{}
	if strings.TrimSpace(value) == "" {
		return result, nil
	}
	for _, item := range strings.Split(value, ",") {
		parts := strings.SplitN(item, "=", 2)
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
			return nil, fmt.Errorf("invalid field mapping %q; use old=new", item)
		}
		result[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
	}
	return result, nil
}

func parseCSVFieldTypes(numberFields, boolFields string) map[string]core.FieldType {
	result := map[string]core.FieldType{}
	for _, name := range strings.Split(numberFields, ",") {
		if strings.TrimSpace(name) != "" {
			result[strings.TrimSpace(name)] = core.FieldTypeNumber
		}
	}
	for _, name := range strings.Split(boolFields, ",") {
		if strings.TrimSpace(name) != "" {
			result[strings.TrimSpace(name)] = core.FieldTypeBool
		}
	}
	return result
}

func runWorkflow(ctx context.Context, command string, options workflowOptions) error {
	if options.outputLayer == "" {
		options.outputLayer = "workflow_result"
	}
	switch command {
	case "create-layer":
		geometryType := strings.ToUpper(strings.TrimSpace(options.geometryType))
		if geometryType != "POINT" && geometryType != "LINESTRING" && geometryType != "POLYGON" {
			return fmt.Errorf("unsupported empty layer geometry type %q", options.geometryType)
		}
		layer := core.Layer{Name: options.outputLayer, GeometryType: geometryType, CRS: core.CRS{AuthorityCode: options.crs}, Editable: true, GeometryLocked: false}
		return writeWorkflowLayer(ctx, options.output, layer)
	case "import-csv":
		paths := options.inputs
		if len(paths) == 0 {
			paths = []string{options.input}
		}
		files := make([]*os.File, 0, len(paths))
		defer func() {
			for _, file := range files {
				_ = file.Close()
			}
		}()
		inputs := make([]io.Reader, 0, len(paths))
		for _, path := range paths {
			file, err := os.Open(path)
			if err != nil {
				return err
			}
			files = append(files, file)
			inputs = append(inputs, file)
		}
		result, err := commands.ImportCSVPointFiles(ctx, inputs, commands.CSVOptions{Delimiter: workflowDelimiter(options.delimiter), HasHeader: true, Encoding: options.encoding, XField: options.xField, YField: options.yField, CRS: core.CRS{AuthorityCode: options.crs}, LayerName: options.outputLayer, FieldTypes: parseCSVFieldTypes(options.numberFields, options.boolFields)})
		if err != nil {
			return err
		}
		if result.Rejected > 0 && !options.allowInvalid {
			return fmt.Errorf("CSV import rejected %d rows (first row %d: %s)", result.Rejected, result.Errors[0].Row, result.Errors[0].Reason)
		}
		if result.Rejected > 0 {
			fmt.Printf("accepted=%d rejected=%d\n", result.Accepted, result.Rejected)
		}
		mappings, err := parseFieldMappings(options.fieldMap)
		if err != nil {
			return err
		}
		if options.buildBoundary {
			tagged, boundaries, err := commands.BuildBoundaryHulls(ctx, result.Layer, options.xField, options.yField, options.boundaryGroup, result.Layer.Name+"_boundaries")
			if err != nil {
				return err
			}
			tagged, err = commands.RemapFields(tagged, mappings)
			if err != nil {
				return err
			}
			return (gdal.Writer{}).WriteLayers(ctx, options.output, []core.Layer{tagged, boundaries})
		}
		result.Layer, err = commands.RemapFields(result.Layer, mappings)
		if err != nil {
			return err
		}
		return writeWorkflowLayer(ctx, options.output, result.Layer)
	case "split-boundary":
		source, err := (gdal.Reader{}).Open(ctx, options.input, options.layer)
		if err != nil {
			return err
		}
		boundaries, err := (gdal.Reader{}).Open(ctx, options.boundaryFile, options.boundaryLayer)
		if err != nil {
			return err
		}
		prefix := options.outputLayer
		if prefix == "" || prefix == "workflow_result" {
			prefix = source.Name + "_split"
		}
		layers, err := commands.SplitPolygonLayerByBoundaries(ctx, geosdriver.NewOperator(), source, boundaries, prefix, 0)
		if err != nil {
			return err
		}
		if len(layers) == 0 {
			return fmt.Errorf("no source polygons intersect the boundaries")
		}
		return (gdal.Writer{}).WriteLayers(ctx, options.output, layers)
	case "join":
		layer, err := (gdal.Reader{}).Open(ctx, options.input, options.layer)
		if err != nil {
			return err
		}
		tableFile, err := os.Open(options.table)
		if err != nil {
			return err
		}
		defer tableFile.Close()
		rows, diagnostics, err := commands.ReadCSVTable(ctx, tableFile, workflowDelimiter(options.delimiter), options.encoding)
		if err != nil {
			return err
		}
		if len(diagnostics) > 0 {
			return fmt.Errorf("table contains %d malformed rows (first row %d: %s)", len(diagnostics), diagnostics[0].Row, diagnostics[0].Reason)
		}
		result, report, err := commands.JoinLayerTable(layer, rows, options.layerKey, options.tableKey)
		if err != nil {
			return err
		}
		result.Name = options.outputLayer
		mappings, err := parseFieldMappings(options.fieldMap)
		if err != nil {
			return err
		}
		result, err = commands.RemapFields(result, mappings)
		if err != nil {
			return err
		}
		fmt.Printf("matched=%d unmatched_features=%d unmatched_rows=%d duplicate_rows=%d\n", report.Matched, report.UnmatchedFeatures, report.UnmatchedRows, report.DuplicateRows)
		return writeWorkflowLayer(ctx, options.output, result)
	case "polygon":
		layer, err := (gdal.Reader{}).Open(ctx, options.input, options.layer)
		if err != nil {
			return err
		}
		ids, err := parseFeatureIDs(options.featureIDs)
		if err != nil {
			return err
		}
		byID := map[uint64]core.Feature{}
		for _, feature := range layer.Features {
			byID[feature.ID] = feature
		}
		points := make([]core.Feature, 0, len(ids))
		for _, id := range ids {
			feature, ok := byID[id]
			if !ok {
				return fmt.Errorf("feature %d not found", id)
			}
			points = append(points, feature)
		}
		result, err := commands.PolygonFromOrderedPoints(points, options.outputLayer)
		if err != nil {
			return err
		}
		result.CRS = layer.CRS
		mappings, err := parseFieldMappings(options.fieldMap)
		if err != nil {
			return err
		}
		result, err = commands.RemapFields(result, mappings)
		if err != nil {
			return err
		}
		return writeWorkflowLayer(ctx, options.output, result)
	case "label-field":
		layer, err := (gdal.Reader{}).Open(ctx, options.input, options.layer)
		if err != nil {
			return err
		}
		result, err := commands.GenerateLabelField(layer, options.field, options.template)
		if err != nil {
			return err
		}
		result.Name = options.outputLayer
		mappings, err := parseFieldMappings(options.fieldMap)
		if err != nil {
			return err
		}
		result, err = commands.RemapFields(result, mappings)
		if err != nil {
			return err
		}
		return writeWorkflowLayer(ctx, options.output, result)
	case "aggregate":
		layer, err := (gdal.Reader{}).Open(ctx, options.input, options.layer)
		if err != nil {
			return err
		}
		rows, err := commands.AggregateByField(layer, options.groupField, options.sumField)
		if err != nil {
			return err
		}
		file, err := os.Create(options.output)
		if err != nil {
			return err
		}
		defer file.Close()
		writer := csv.NewWriter(file)
		if err := writer.Write([]string{"group", "count", "sum"}); err != nil {
			return err
		}
		for _, row := range rows {
			if err := writer.Write([]string{row.Group, strconv.Itoa(row.Count), strconv.FormatFloat(row.Sum, 'g', -1, 64)}); err != nil {
				return err
			}
		}
		writer.Flush()
		return writer.Error()
	case "measure":
		layer, err := (gdal.Reader{}).Open(ctx, options.input, options.layer)
		if err != nil {
			return err
		}
		result, err := commands.CalculatePlanarMetrics(layer)
		if err != nil {
			return err
		}
		result.Name = options.outputLayer
		return writeWorkflowLayer(ctx, options.output, result)
	case "validate":
		layer, err := (gdal.Reader{}).Open(ctx, options.input, options.layer)
		if err != nil {
			return err
		}
		required := []string{}
		if options.required != "" {
			for _, field := range strings.Split(options.required, ",") {
				required = append(required, strings.TrimSpace(field))
			}
		}
		issues := commands.ValidateLayer(layer, required, options.unique)
		return writeValidationReport(options.output, issues)
	default:
		return fmt.Errorf("unsupported workflow %q", command)
	}
}

func parseFeatureIDs(value string) ([]uint64, error) {
	parts := strings.Split(value, ",")
	ids := make([]uint64, 0, len(parts))
	for _, part := range parts {
		id, err := strconv.ParseUint(strings.TrimSpace(part), 10, 64)
		if err != nil || id == 0 {
			return nil, fmt.Errorf("invalid feature ID %q", part)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func writeWorkflowLayer(ctx context.Context, destination string, layer core.Layer) error {
	ext := strings.ToLower(filepath.Ext(destination))
	if ext == ".csv" {
		return writeLayerCSV(destination, layer)
	}
	if ext != ".gpkg" && ext != ".shp" {
		return fmt.Errorf("workflow output must be .gpkg or .shp")
	}
	return (gdal.Writer{}).Write(ctx, destination, layer)
}

func writeLayerCSV(destination string, layer core.Layer) error {
	file, err := os.Create(destination)
	if err != nil {
		return err
	}
	defer file.Close()
	writer := csv.NewWriter(file)
	headers := make([]string, 0, len(layer.Fields)+1)
	for _, field := range layer.Fields {
		headers = append(headers, field.Name)
	}
	headers = append(headers, "WKT")
	if err := writer.Write(headers); err != nil {
		return err
	}
	for _, feature := range layer.Features {
		row := make([]string, 0, len(headers))
		for _, field := range layer.Fields {
			row = append(row, fmt.Sprint(feature.Properties[field.Name]))
		}
		wkt := ""
		if feature.Geometry != nil {
			if value, convertErr := core.ToWKT(feature.Geometry); convertErr == nil {
				wkt = value.WKT
			}
		}
		row = append(row, wkt)
		if err := writer.Write(row); err != nil {
			return err
		}
	}
	writer.Flush()
	return writer.Error()
}

func writeValidationReport(destination string, issues []commands.ValidationIssue) error {
	file, err := os.Create(destination)
	if err != nil {
		return err
	}
	defer file.Close()
	writer := csv.NewWriter(file)
	if err := writer.Write([]string{"feature_id", "rule", "reason"}); err != nil {
		return err
	}
	for _, issue := range issues {
		if err := writer.Write([]string{fmt.Sprint(issue.FeatureID), issue.Rule, issue.Reason}); err != nil {
			return err
		}
	}
	writer.Flush()
	return writer.Error()
}
