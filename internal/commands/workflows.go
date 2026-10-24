package commands

// Workflows contains format-neutral operations used by the milestone-D UI and
// CLI. CSV parsing deliberately lives here (rather than in a widget) so the
// same validation and row diagnostics are reproducible without a GUI.
import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"gogis/internal/core"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/transform"
)

type CSVOptions struct {
	Delimiter      rune
	HasHeader      bool
	Encoding       string
	XField, YField string
	CRS            core.CRS
	LayerName      string
	FieldTypes     map[string]core.FieldType
	// MaxRows bounds interactive imports; zero leaves the CLI unrestricted.
	MaxRows int
}
type CSVRowError struct {
	Row    int
	Reason string
}
type CSVImportResult struct {
	Layer    core.Layer
	Accepted int
	Rejected int
	Errors   []CSVRowError
}

// ImportCSVPointFiles combines compatible coordinate tables in input order.
// Every file is parsed with the same explicit mapping and CRS; no partial
// result is returned when a file has an invalid schema or the limit is hit.
func ImportCSVPointFiles(ctx context.Context, inputs []io.Reader, options CSVOptions) (CSVImportResult, error) {
	return importCSVPointFiles(ctx, len(inputs), func(index int, fileOptions CSVOptions) (CSVImportResult, error) {
		if inputs[index] == nil {
			return CSVImportResult{}, fmt.Errorf("CSV input %d is nil", index+1)
		}
		return ImportCSVPoints(ctx, inputs[index], fileOptions)
	}, options)
}

// ImportCSVPointSources opens and closes inputs one at a time. Desktop and CLI
// callers use this to avoid exhausting file descriptors for large selections.
func ImportCSVPointSources(ctx context.Context, count int, open func(int) (io.ReadCloser, error), options CSVOptions) (CSVImportResult, error) {
	if open == nil {
		return CSVImportResult{}, errors.New("CSV opener is required")
	}
	return importCSVPointFiles(ctx, count, func(index int, fileOptions CSVOptions) (CSVImportResult, error) {
		input, err := open(index)
		if err != nil {
			return CSVImportResult{}, err
		}
		if input == nil {
			return CSVImportResult{}, errors.New("CSV opener returned no input")
		}
		part, readErr := ImportCSVPoints(ctx, input, fileOptions)
		closeErr := input.Close()
		if readErr != nil {
			return CSVImportResult{}, readErr
		}
		if closeErr != nil {
			return CSVImportResult{}, fmt.Errorf("close CSV input: %w", closeErr)
		}
		return part, nil
	}, options)
}

func importCSVPointFiles(ctx context.Context, count int, readOne func(int, CSVOptions) (CSVImportResult, error), options CSVOptions) (CSVImportResult, error) {
	if count <= 0 {
		return CSVImportResult{}, errors.New("at least one CSV input is required")
	}
	var combined CSVImportResult
	var maxFeatureID uint64
	for index := 0; index < count; index++ {
		if err := ctx.Err(); err != nil {
			return CSVImportResult{}, err
		}
		fileOptions := options
		if options.MaxRows > 0 {
			remaining := options.MaxRows - combined.Accepted - combined.Rejected
			if remaining <= 0 {
				remaining = 1 // Permit an additional header-only file.
			}
			fileOptions.MaxRows = remaining
		}
		part, err := readOne(index, fileOptions)
		if err != nil {
			return CSVImportResult{}, fmt.Errorf("CSV input %d: %w", index+1, err)
		}
		if options.MaxRows > 0 && combined.Accepted+combined.Rejected+part.Accepted+part.Rejected > options.MaxRows {
			return CSVImportResult{}, fmt.Errorf("CSV inputs exceed the %d-row interactive limit", options.MaxRows)
		}
		if index == 0 {
			combined.Layer = part.Layer
			for _, feature := range combined.Layer.Features {
				if feature.ID > maxFeatureID {
					maxFeatureID = feature.ID
				}
			}
		} else {
			if len(part.Layer.Fields) != len(combined.Layer.Fields) {
				return CSVImportResult{}, fmt.Errorf("CSV input %d has a different field schema", index+1)
			}
			for fieldIndex, field := range part.Layer.Fields {
				if field != combined.Layer.Fields[fieldIndex] {
					return CSVImportResult{}, fmt.Errorf("CSV input %d field %d differs from the first input", index+1, fieldIndex+1)
				}
			}
			offset := maxFeatureID
			for _, feature := range part.Layer.Features {
				if feature.ID > ^uint64(0)-offset {
					return CSVImportResult{}, fmt.Errorf("CSV feature ID overflow")
				}
				feature.ID += offset
				if feature.ID > maxFeatureID {
					maxFeatureID = feature.ID
				}
				combined.Layer.Features = append(combined.Layer.Features, feature)
			}
		}
		combined.Accepted += part.Accepted
		combined.Rejected += part.Rejected
		for _, diagnostic := range part.Errors {
			if len(combined.Errors) >= maxCSVRowDiagnostics {
				break
			}
			diagnostic.Reason = fmt.Sprintf("input %d: %s", index+1, diagnostic.Reason)
			combined.Errors = append(combined.Errors, diagnostic)
		}
	}
	return combined, nil
}

type CSVPreview struct {
	Headers   []string
	Rows      [][]string
	Errors    []CSVRowError
	Delimiter rune
	Encoding  string
	Truncated bool
}

const maxCSVRowDiagnostics = 100

// PreviewCSV reads only a bounded sample for a GUI preview. It never infers a
// CRS and reports malformed rows with their source line number.
func PreviewCSV(ctx context.Context, r io.Reader, delimiter rune, encoding string, maxRows int) (CSVPreview, error) {
	if maxRows <= 0 {
		maxRows = 50
	}
	if delimiter == 0 {
		delimiter = ','
	}
	decoded, err := decodeCSV(r, encoding)
	if err != nil {
		return CSVPreview{}, err
	}
	// Preview never needs the remainder of a large file or an enormous field.
	reader := csv.NewReader(io.LimitReader(decoded, 1<<20))
	reader.Comma = delimiter
	reader.FieldsPerRecord = -1
	reader.ReuseRecord = false
	header, err := reader.Read()
	if err != nil {
		return CSVPreview{}, err
	}
	if err := validateCSVText(header); err != nil {
		return CSVPreview{}, fmt.Errorf("CSV header: %w", err)
	}
	for i := range header {
		if i == 0 {
			header[i] = strings.TrimPrefix(header[i], "\ufeff")
		}
		header[i] = strings.TrimSpace(header[i])
	}
	preview := CSVPreview{Headers: header, Delimiter: delimiter, Encoding: encoding}
	// A malformed file must not make a five-row preview scan indefinitely.
	maxScanned := maxRows * 4
	if maxScanned < maxRows {
		maxScanned = maxRows
	}
	for row := 2; len(preview.Rows) < maxRows && row-2 < maxScanned; row++ {
		if err := ctx.Err(); err != nil {
			return CSVPreview{}, err
		}
		record, readErr := reader.Read()
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			if !isCSVParseError(readErr) {
				return CSVPreview{}, fmt.Errorf("read CSV preview: %w", readErr)
			}
			preview.Errors = append(preview.Errors, CSVRowError{csvSourceLine(reader, readErr, row), readErr.Error()})
			continue
		}
		if len(record) != len(header) {
			preview.Errors = append(preview.Errors, CSVRowError{csvSourceLine(reader, nil, row), fmt.Sprintf("expected %d fields, got %d", len(header), len(record))})
			continue
		}
		if err := validateCSVText(record); err != nil {
			preview.Errors = append(preview.Errors, CSVRowError{csvSourceLine(reader, nil, row), err.Error()})
			continue
		}
		preview.Rows = append(preview.Rows, append([]string(nil), record...))
	}
	preview.Truncated = len(preview.Rows) == maxRows || len(preview.Errors)+len(preview.Rows) >= maxScanned
	return preview, nil
}

// ReadCSVTable reads a non-spatial 업무대장 using the same delimiter and
// encoding rules as point import. It returns row diagnostics instead of
// silently dropping malformed records.
func ReadCSVTable(ctx context.Context, r io.Reader, delimiter rune, encoding string) ([]map[string]any, []CSVRowError, error) {
	return ReadCSVTableLimited(ctx, r, delimiter, encoding, 0)
}

// ReadCSVTableLimited caps interactive table materialization; zero is unlimited.
func ReadCSVTableLimited(ctx context.Context, r io.Reader, delimiter rune, encoding string, maxRows int) ([]map[string]any, []CSVRowError, error) {
	if delimiter == 0 {
		delimiter = ','
	}
	decoded, err := decodeCSV(r, encoding)
	if err != nil {
		return nil, nil, err
	}
	reader := csv.NewReader(decoded)
	reader.Comma = delimiter
	reader.FieldsPerRecord = -1
	reader.ReuseRecord = false
	header, err := reader.Read()
	if err != nil {
		return nil, nil, err
	}
	if err := validateCSVText(header); err != nil {
		return nil, nil, fmt.Errorf("CSV header: %w", err)
	}
	seen := map[string]bool{}
	for i := range header {
		if i == 0 {
			header[i] = strings.TrimPrefix(header[i], "\ufeff")
		}
		header[i] = strings.TrimSpace(header[i])
		if header[i] == "" || seen[header[i]] {
			return nil, nil, fmt.Errorf("invalid or duplicate header at column %d", i+1)
		}
		seen[header[i]] = true
	}
	rows := []map[string]any{}
	diagnostics := []CSVRowError{}
	for row := 2; ; row++ {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		record, readErr := reader.Read()
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil && !isCSVParseError(readErr) {
			return nil, nil, fmt.Errorf("read CSV table: %w", readErr)
		}
		if maxRows > 0 && row-1 > maxRows {
			return nil, nil, fmt.Errorf("CSV table exceeds the %d-row interactive limit; split the file or use the CLI", maxRows)
		}
		if readErr != nil {
			if len(diagnostics) < maxCSVRowDiagnostics {
				diagnostics = append(diagnostics, CSVRowError{csvSourceLine(reader, readErr, row), readErr.Error()})
			}
			continue
		}
		if len(record) != len(header) {
			if len(diagnostics) < maxCSVRowDiagnostics {
				diagnostics = append(diagnostics, CSVRowError{csvSourceLine(reader, nil, row), fmt.Sprintf("expected %d fields, got %d", len(header), len(record))})
			}
			continue
		}
		if err := validateCSVText(record); err != nil {
			if len(diagnostics) < maxCSVRowDiagnostics {
				diagnostics = append(diagnostics, CSVRowError{csvSourceLine(reader, nil, row), err.Error()})
			}
			continue
		}
		values := make(map[string]any, len(header))
		for i, value := range record {
			values[header[i]] = value
		}
		rows = append(rows, values)
	}
	return rows, diagnostics, nil
}

func decodeCSV(r io.Reader, encoding string) (io.Reader, error) {
	switch strings.ToUpper(strings.TrimSpace(encoding)) {
	case "", "UTF-8", "UTF8":
		return r, nil
	case "CP949", "EUC-KR":
		return transform.NewReader(r, korean.EUCKR.NewDecoder()), nil
	default:
		return nil, fmt.Errorf("unsupported CSV encoding %q", encoding)
	}
}

// ImportCSVPoints imports a delimited coordinate table, preserving all source
// fields as text so identifiers such as 0012 cannot lose leading zeroes.
func ImportCSVPoints(ctx context.Context, r io.Reader, options CSVOptions) (CSVImportResult, error) {
	if options.Delimiter == 0 {
		options.Delimiter = ','
	}
	if options.XField == "" || options.YField == "" {
		return CSVImportResult{}, errors.New("X/Y fields are required")
	}
	if options.LayerName == "" {
		options.LayerName = "csv_points"
	}
	decoded, err := decodeCSV(r, options.Encoding)
	if err != nil {
		return CSVImportResult{}, err
	}
	reader := csv.NewReader(decoded)
	reader.Comma = options.Delimiter
	reader.FieldsPerRecord = -1
	// Each row is copied into properties before the next Read call.
	reader.ReuseRecord = true
	first, err := reader.Read()
	if err != nil {
		return CSVImportResult{}, fmt.Errorf("read CSV header: %w", err)
	}
	if err := validateCSVText(first); err != nil {
		return CSVImportResult{}, fmt.Errorf("CSV header: %w", err)
	}
	if !options.HasHeader {
		return CSVImportResult{}, errors.New("a header row is required for explicit X/Y mapping")
	}
	fields := make([]core.Field, len(first))
	names := map[string]bool{}
	indices := map[string]int{}
	for i, name := range first {
		if i == 0 {
			name = strings.TrimPrefix(name, "\ufeff")
		}
		name = strings.TrimSpace(name)
		if name == "" || names[name] {
			return CSVImportResult{}, fmt.Errorf("invalid or duplicate header at column %d", i+1)
		}
		names[name] = true
		indices[name] = i
		fieldType := core.FieldTypeText
		if options.FieldTypes != nil {
			if requested, ok := options.FieldTypes[name]; ok {
				fieldType = requested
			}
		}
		fields[i] = core.Field{Name: name, Type: fieldType}
	}
	xi, xok := indices[options.XField]
	yi, yok := indices[options.YField]
	if !xok || !yok {
		return CSVImportResult{}, errors.New("X/Y field not found in header")
	}
	result := CSVImportResult{Layer: core.Layer{Name: options.LayerName, CRS: options.CRS, GeometryType: "POINT", Editable: true, GeometryLocked: false, Fields: fields}}
	for row := 2; ; row++ {
		if err := ctx.Err(); err != nil {
			return CSVImportResult{}, err
		}
		record, readErr := reader.Read()
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil && !isCSVParseError(readErr) {
			return CSVImportResult{}, fmt.Errorf("read CSV points: %w", readErr)
		}
		if options.MaxRows > 0 && row-1 > options.MaxRows {
			return CSVImportResult{}, fmt.Errorf("CSV exceeds the %d-row interactive limit; split the file or use the CLI", options.MaxRows)
		}
		if readErr != nil {
			result.Rejected++
			if len(result.Errors) < maxCSVRowDiagnostics {
				result.Errors = append(result.Errors, CSVRowError{csvSourceLine(reader, readErr, row), readErr.Error()})
			}
			continue
		}
		if len(record) != len(fields) {
			result.Rejected++
			if len(result.Errors) < maxCSVRowDiagnostics {
				result.Errors = append(result.Errors, CSVRowError{csvSourceLine(reader, nil, row), fmt.Sprintf("expected %d fields, got %d", len(fields), len(record))})
			}
			continue
		}
		if err := validateCSVText(record); err != nil {
			result.Rejected++
			if len(result.Errors) < maxCSVRowDiagnostics {
				result.Errors = append(result.Errors, CSVRowError{csvSourceLine(reader, nil, row), err.Error()})
			}
			continue
		}
		x, ex := strconv.ParseFloat(strings.TrimSpace(record[xi]), 64)
		y, ey := strconv.ParseFloat(strings.TrimSpace(record[yi]), 64)
		if ex != nil || ey != nil || math.IsNaN(x) || math.IsNaN(y) || math.IsInf(x, 0) || math.IsInf(y, 0) {
			result.Rejected++
			if len(result.Errors) < maxCSVRowDiagnostics {
				result.Errors = append(result.Errors, CSVRowError{csvSourceLine(reader, nil, row), "X/Y must be finite numbers"})
			}
			continue
		}
		props := make(map[string]any, len(record))
		var fieldError error
		for i, value := range record {
			props[fields[i].Name], fieldError = parseCSVField(value, fields[i].Type)
			if fieldError != nil {
				fieldError = fmt.Errorf("field %q: %w", fields[i].Name, fieldError)
				break
			}
		}
		if fieldError != nil {
			result.Rejected++
			if len(result.Errors) < maxCSVRowDiagnostics {
				result.Errors = append(result.Errors, CSVRowError{csvSourceLine(reader, nil, row), fieldError.Error()})
			}
			continue
		}
		result.Layer.Features = append(result.Layer.Features, core.Feature{ID: uint64(row - 1), Geometry: csvPointWKT(x, y), Properties: props})
		result.Accepted++
	}
	return result, nil
}

func csvPointWKT(x, y float64) core.WKTGeometry {
	var buffer [96]byte
	text := append(buffer[:0], "POINT ("...)
	text = strconv.AppendFloat(text, x, 'g', -1, 64)
	text = append(text, ' ')
	text = strconv.AppendFloat(text, y, 'g', -1, 64)
	text = append(text, ')')
	return core.WKTGeometry{WKT: string(text)}
}

func csvSourceLine(reader *csv.Reader, readErr error, fallback int) int {
	var parseErr *csv.ParseError
	if errors.As(readErr, &parseErr) && parseErr.StartLine > 0 {
		return parseErr.StartLine
	}
	if readErr == nil {
		if line, _ := reader.FieldPos(0); line > 0 {
			return line
		}
	}
	return fallback
}

func isCSVParseError(err error) bool {
	var parseErr *csv.ParseError
	return errors.As(err, &parseErr)
}

func validateCSVText(record []string) error {
	for i, value := range record {
		if !utf8.ValidString(value) || containsReplacementRune(value) {
			return fmt.Errorf("column %d contains undecodable text; check the selected encoding", i+1)
		}
	}
	return nil
}

// IndexByte uses the standard library's architecture-specific byte search on
// longer fields, while avoiding a second UTF-8 rune decode after ValidString.
func containsReplacementRune(value string) bool {
	for {
		index := strings.IndexByte(value, 0xef)
		if index < 0 {
			return false
		}
		if len(value)-index >= 3 && value[index+1] == 0xbf && value[index+2] == 0xbd {
			return true
		}
		value = value[index+1:]
	}
}

func parseCSVField(value string, fieldType core.FieldType) (any, error) {
	if fieldType != core.FieldTypeText && strings.TrimSpace(value) == "" {
		return nil, nil
	}
	switch fieldType {
	case core.FieldTypeNumber:
		number, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err != nil || math.IsNaN(number) || math.IsInf(number, 0) {
			return nil, fmt.Errorf("invalid finite number %q", value)
		}
		return number, nil
	case core.FieldTypeBool:
		parsed, err := strconv.ParseBool(strings.TrimSpace(value))
		if err != nil {
			return nil, fmt.Errorf("invalid boolean %q", value)
		}
		return parsed, nil
	}
	return value, nil
}

// PolygonFromOrderedPoints creates one closed polygon and rejects degenerate
// or self-intersecting input instead of guessing an order.
func PolygonFromOrderedPoints(points []core.Feature, name string) (core.Layer, error) {
	if len(points) < 3 {
		return core.Layer{}, errors.New("at least three points are required")
	}
	coords := make([]string, 0, len(points)+1)
	for _, feature := range points {
		if feature.Geometry == nil || feature.Geometry.GeometryType() != "POINT" {
			return core.Layer{}, errors.New("all selected features must be points")
		}
		g, err := core.ToWKT(feature.Geometry)
		if err != nil {
			return core.Layer{}, err
		}
		body := strings.TrimSpace(g.WKT)
		open := strings.Index(body, "(")
		close := strings.LastIndex(body, ")")
		if open < 0 || close <= open {
			return core.Layer{}, errors.New("invalid point geometry")
		}
		body = strings.TrimSpace(body[open+1 : close])
		parts := strings.Fields(body)
		if len(parts) != 2 {
			return core.Layer{}, errors.New("invalid point geometry")
		}
		coords = append(coords, parts[0]+" "+parts[1])
	}
	if coords[0] != coords[len(coords)-1] {
		coords = append(coords, coords[0])
	}
	parsed := make([]metricPoint, 0, len(coords))
	for _, value := range coords {
		parts := strings.Fields(value)
		x, ex := strconv.ParseFloat(parts[0], 64)
		y, ey := strconv.ParseFloat(parts[1], 64)
		if ex != nil || ey != nil || math.IsNaN(x) || math.IsNaN(y) || math.IsInf(x, 0) || math.IsInf(y, 0) {
			return core.Layer{}, errors.New("invalid point coordinate")
		}
		parsed = append(parsed, metricPoint{x, y})
	}
	areaTwice := 0.0
	for i := 1; i < len(parsed); i++ {
		if parsed[i] == parsed[i-1] {
			return core.Layer{}, errors.New("polygon contains duplicate consecutive points")
		}
		areaTwice += parsed[i-1].x*parsed[i].y - parsed[i].x*parsed[i-1].y
	}
	if math.Abs(areaTwice) <= 1e-12 {
		return core.Layer{}, errors.New("polygon has zero area")
	}
	edges := len(parsed) - 1
	for i := 0; i < edges; i++ {
		for j := i + 1; j < edges; j++ {
			if j == i+1 || i == 0 && j == edges-1 {
				continue // adjacent edges share one intended endpoint
			}
			if segmentsIntersect(parsed[i], parsed[i+1], parsed[j], parsed[j+1]) {
				return core.Layer{}, errors.New("polygon self-intersects")
			}
		}
	}
	wkt := "POLYGON ((" + strings.Join(coords, ", ") + "))"
	return core.Layer{Name: name, GeometryType: "POLYGON", Editable: true, GeometryLocked: true, Features: []core.Feature{{ID: 1, Geometry: core.WKTGeometry{WKT: wkt}, Properties: map[string]any{}}}}, nil
}

func segmentsIntersect(a, b, c, d metricPoint) bool {
	orient := func(p, q, r metricPoint) float64 { return (q.x-p.x)*(r.y-p.y) - (q.y-p.y)*(r.x-p.x) }
	onSegment := func(p, q, r metricPoint) bool {
		return math.Min(p.x, q.x) <= r.x && r.x <= math.Max(p.x, q.x) && math.Min(p.y, q.y) <= r.y && r.y <= math.Max(p.y, q.y)
	}
	ab1, ab2 := orient(a, b, c), orient(a, b, d)
	cd1, cd2 := orient(c, d, a), orient(c, d, b)
	if (ab1 > 0 && ab2 < 0 || ab1 < 0 && ab2 > 0) && (cd1 > 0 && cd2 < 0 || cd1 < 0 && cd2 > 0) {
		return true
	}
	return ab1 == 0 && onSegment(a, b, c) || ab2 == 0 && onSegment(a, b, d) || cd1 == 0 && onSegment(c, d, a) || cd2 == 0 && onSegment(c, d, b)
}

type JoinReport struct{ Matched, UnmatchedFeatures, UnmatchedRows, DuplicateRows int }

func JoinLayerTable(layer core.Layer, table []map[string]any, layerKey, tableKey string) (core.Layer, JoinReport, error) {
	if layerKey == "" || tableKey == "" {
		return core.Layer{}, JoinReport{}, errors.New("join keys are required")
	}
	layerFields := make(map[string]bool, len(layer.Fields))
	for _, field := range layer.Fields {
		layerFields[field.Name] = true
	}
	if !layerFields[layerKey] {
		return core.Layer{}, JoinReport{}, fmt.Errorf("layer key %q does not exist", layerKey)
	}
	if len(table) == 0 {
		return core.Layer{}, JoinReport{}, errors.New("join table has no rows")
	}
	if _, ok := table[0][tableKey]; !ok {
		return core.Layer{}, JoinReport{}, fmt.Errorf("table key %q does not exist", tableKey)
	}
	index := map[string]map[string]any{}
	var report JoinReport
	for _, row := range table {
		key, exists := row[tableKey]
		if !exists || key == nil || strings.TrimSpace(fmt.Sprint(key)) == "" {
			report.UnmatchedRows++
			continue
		}
		textKey := fmt.Sprint(key)
		if _, exists := index[textKey]; exists {
			report.DuplicateRows++
			continue
		}
		index[textKey] = row
	}
	if report.DuplicateRows > 0 {
		return core.Layer{}, report, fmt.Errorf("join table has %d duplicate key row(s); resolve them before joining", report.DuplicateRows)
	}
	tableFields := map[string]bool{}
	for _, row := range table {
		for field := range row {
			if field != tableKey {
				if layerFields[field] {
					return core.Layer{}, JoinReport{}, fmt.Errorf("join field %q conflicts with the layer schema", field)
				}
				tableFields[field] = true
			}
		}
	}
	result := layer.Clone()
	fieldNames := make([]string, 0, len(tableFields))
	for field := range tableFields {
		fieldNames = append(fieldNames, field)
	}
	sort.Strings(fieldNames)
	for _, field := range fieldNames {
		result.Fields = append(result.Fields, core.Field{Name: field, Type: core.FieldTypeText})
	}
	usedRows := map[string]bool{}
	for i := range result.Features {
		value, exists := result.Features[i].Properties[layerKey]
		if !exists || value == nil || strings.TrimSpace(fmt.Sprint(value)) == "" {
			report.UnmatchedFeatures++
			continue
		}
		key := fmt.Sprint(value)
		row, ok := index[key]
		if !ok {
			report.UnmatchedFeatures++
			continue
		}
		if result.Features[i].Properties == nil {
			result.Features[i].Properties = make(map[string]any, len(row))
		}
		for field, value := range row {
			if field != tableKey {
				result.Features[i].Properties[field] = value
			}
		}
		report.Matched++
		usedRows[key] = true
	}
	report.UnmatchedRows += len(index) - len(usedRows)
	return result, report, nil
}

type ValidationIssue struct {
	FeatureID    uint64
	Rule, Reason string
}

// ValidateLayer performs deterministic, non-mutating checks suitable for an
// error report. It intentionally does not infer CRS or repair geometries.
func ValidateLayer(layer core.Layer, requiredFields []string, uniqueField string) []ValidationIssue {
	issues := []ValidationIssue{}
	seen := map[string]uint64{}
	for _, feature := range layer.Features {
		for _, field := range requiredFields {
			value, exists := feature.Properties[field]
			if !exists || value == nil || strings.TrimSpace(fmt.Sprint(value)) == "" {
				issues = append(issues, ValidationIssue{feature.ID, "required", "required field is empty: " + field})
			}
		}
		if feature.Geometry == nil {
			issues = append(issues, ValidationIssue{feature.ID, "geometry", "geometry is missing"})
		} else if wkt, err := core.ToWKT(feature.Geometry); err != nil || strings.Contains(strings.ToUpper(wkt.WKT), "EMPTY") {
			issues = append(issues, ValidationIssue{feature.ID, "geometry", "geometry is invalid or empty"})
		}
		if uniqueField != "" {
			key := strings.TrimSpace(fmt.Sprint(feature.Properties[uniqueField]))
			if key == "" {
				issues = append(issues, ValidationIssue{feature.ID, "unique", "unique key is empty"})
			} else if previous, exists := seen[key]; exists {
				issues = append(issues, ValidationIssue{feature.ID, "unique", fmt.Sprintf("duplicate key %q; first feature %d", key, previous)})
			} else {
				seen[key] = feature.ID
			}
		}
	}
	return issues
}

// CalculatePlanarMetrics returns length or area in the units of the layer CRS.
// Callers must supply an appropriate projected CRS; geographic degrees are not
// silently converted to metres.
func CalculatePlanarMetrics(layer core.Layer) (core.Layer, error) {
	crs := strings.ToUpper(strings.TrimSpace(layer.CRS.AuthorityCode))
	if crs == "" {
		return core.Layer{}, errors.New("planar measurement requires a known projected CRS")
	}
	if crs == "OGC:CRS84" || strings.HasPrefix(crs, "EPSG:") {
		if crs == "OGC:CRS84" {
			return core.Layer{}, errors.New("planar measurement requires a projected CRS; geographic coordinates use angular units")
		}
		code, err := strconv.Atoi(strings.TrimPrefix(crs, "EPSG:"))
		if err == nil && code >= 4000 && code < 5000 {
			return core.Layer{}, errors.New("planar measurement requires a projected CRS; geographic coordinates use angular units")
		}
	}
	result := layer.Clone()
	fields := map[string]bool{}
	for _, field := range result.Fields {
		fields[field.Name] = true
	}
	if !fields["length"] {
		result.Fields = append(result.Fields, core.Field{Name: "length", Type: core.FieldTypeNumber})
	}
	if !fields["area"] {
		result.Fields = append(result.Fields, core.Field{Name: "area", Type: core.FieldTypeNumber})
	}
	for i := range result.Features {
		if result.Features[i].Geometry == nil {
			return core.Layer{}, fmt.Errorf("feature %d has no geometry", result.Features[i].ID)
		}
		geometryType := result.Features[i].Geometry.GeometryType()
		if geometryType != "POINT" && geometryType != "LINESTRING" && geometryType != "POLYGON" {
			return core.Layer{}, fmt.Errorf("feature %d has unsupported geometry %s for planar measurement", result.Features[i].ID, geometryType)
		}
		wkt, err := core.ToWKT(result.Features[i].Geometry)
		if err != nil {
			return core.Layer{}, err
		}
		points, err := metricPoints(wkt.WKT)
		if err != nil {
			return core.Layer{}, err
		}
		length := 0.0
		area := 0.0
		for j := 1; j < len(points); j++ {
			dx := points[j].x - points[j-1].x
			dy := points[j].y - points[j-1].y
			length += math.Hypot(dx, dy)
			if geometryType == "POLYGON" {
				area += points[j-1].x*points[j].y - points[j].x*points[j-1].y
			}
		}
		if result.Features[i].Properties == nil {
			result.Features[i].Properties = map[string]any{}
		}
		result.Features[i].Properties["length"] = length
		result.Features[i].Properties["area"] = math.Abs(area) / 2
	}
	return result, nil
}

type metricPoint struct{ x, y float64 }

func metricPoints(wkt string) ([]metricPoint, error) {
	if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(wkt)), "POLYGON") && strings.Contains(strings.ReplaceAll(wkt, " ", ""), "),(") {
		return nil, errors.New("polygon holes require a measurement method that accounts for inner rings")
	}
	start := strings.Index(wkt, "(")
	end := strings.LastIndex(wkt, ")")
	if start < 0 || end <= start {
		return nil, errors.New("unsupported geometry for metric calculation")
	}
	text := strings.ReplaceAll(wkt[start:end], "(", "")
	text = strings.ReplaceAll(text, ")", "")
	chunks := strings.Split(text, ",")
	points := make([]metricPoint, 0, len(chunks))
	for _, chunk := range chunks {
		values := strings.Fields(strings.TrimSpace(chunk))
		if len(values) < 2 {
			continue
		}
		x, ex := strconv.ParseFloat(values[0], 64)
		y, ey := strconv.ParseFloat(values[1], 64)
		if ex != nil || ey != nil {
			return nil, errors.New("invalid coordinate")
		}
		points = append(points, metricPoint{x, y})
	}
	return points, nil
}

func GenerateLabelField(layer core.Layer, fieldName, template string) (core.Layer, error) {
	if fieldName == "" {
		return core.Layer{}, errors.New("label field is required")
	}
	result := layer.Clone()
	found := false
	for _, field := range result.Fields {
		if field.Name == fieldName {
			found = true
		}
	}
	if !found {
		result.Fields = append(result.Fields, core.Field{Name: fieldName, Type: core.FieldTypeText})
	}
	for i := range result.Features {
		if result.Features[i].Properties == nil {
			result.Features[i].Properties = map[string]any{}
		}
		text := template
		if text == "" {
			text = "{id}"
		}
		text = strings.ReplaceAll(text, "{id}", strconv.FormatUint(result.Features[i].ID, 10))
		for key, value := range result.Features[i].Properties {
			text = strings.ReplaceAll(text, "{"+key+"}", fmt.Sprint(value))
		}
		result.Features[i].Properties[fieldName] = text
		result.Features[i].Label = &core.Label{Text: text}
	}
	result.Labels.Enabled = true
	result.Labels.Expression = fieldName
	return result, nil
}

// RemapFields makes a deliberate output schema mapping (old=new). It refuses
// collisions instead of silently truncating DBF names.
func RemapFields(layer core.Layer, mappings map[string]string) (core.Layer, error) {
	if len(mappings) == 0 {
		return layer.Clone(), nil
	}
	available := map[string]bool{}
	for _, field := range layer.Fields {
		available[field.Name] = true
	}
	for source := range mappings {
		if !available[source] {
			return core.Layer{}, fmt.Errorf("field mapping source %q does not exist", source)
		}
	}
	used := map[string]bool{}
	targets := make(map[string]string, len(layer.Fields))
	for _, field := range layer.Fields {
		target := field.Name
		if mapped, ok := mappings[target]; ok {
			target = strings.TrimSpace(mapped)
		}
		if target == "" || used[strings.ToUpper(target)] {
			return core.Layer{}, fmt.Errorf("field mapping collision at %q", target)
		}
		used[strings.ToUpper(target)] = true
		targets[field.Name] = target
	}
	result := layer.Clone()
	for i := range result.Fields {
		result.Fields[i].Name = targets[layer.Fields[i].Name]
	}
	for i, feature := range layer.Features {
		properties := make(map[string]any, len(feature.Properties))
		for name, value := range feature.Properties {
			if target, ok := targets[name]; ok {
				properties[target] = value
			} else {
				properties[name] = value
			}
		}
		result.Features[i].Properties = properties
	}
	return result, nil
}

type AggregateRow struct {
	Group string
	Count int
	Sum   float64
}

func AggregateByField(layer core.Layer, groupField, sumField string) ([]AggregateRow, error) {
	if groupField == "" {
		return nil, errors.New("group field is required")
	}
	groups := map[string]*AggregateRow{}
	for _, feature := range layer.Features {
		key := fmt.Sprint(feature.Properties[groupField])
		row := groups[key]
		if row == nil {
			row = &AggregateRow{Group: key}
			groups[key] = row
		}
		row.Count++
		if sumField != "" {
			value, ok := numericValue(feature.Properties[sumField])
			if !ok {
				return nil, fmt.Errorf("feature %d field %q is not numeric", feature.ID, sumField)
			}
			row.Sum += value
		}
	}
	result := make([]AggregateRow, 0, len(groups))
	for _, row := range groups {
		result = append(result, *row)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Group < result[j].Group })
	return result, nil
}

func numericValue(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case uint64:
		return float64(typed), true
	case string:
		number, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		return number, err == nil
	default:
		return 0, false
	}
}
