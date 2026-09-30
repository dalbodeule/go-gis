package dxf

import (
	"bufio"
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"

	"gogis/internal/core"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/transform"
)

// Profile controls the deliberately explicit DXF header choices.
type Profile struct {
	// ACADVersion is an AutoCAD version such as AC1015.
	ACADVersion string
	// CodePage is written to $DWGCODEPAGE and must be validated against the CAD target.
	CodePage   string
	TextHeight float64
}

// ARESUTF8 is an experimental profile. ARES compatibility and Korean font
// rendering still require verification with the target application.
var ARESUTF8 = Profile{ACADVersion: "AC1015", CodePage: "UTF-8", TextHeight: 1}

// ARESCP949 is the legacy Korean code-page profile. ARES/CAD compatibility
// still needs validation in the target application, but the byte encoding and
// declared DXF header are now deterministic.
var ARESCP949 = Profile{ACADVersion: "AC1015", CodePage: "ANSI_949", TextHeight: 1}

// Exporter writes a small, inspectable ASCII DXF subset without hiding header
// or encoding decisions behind a third-party library.
type Exporter struct {
	Profile Profile
}

// Export writes Point, LineString, Polygon and a property-driven TEXT label.
func (e Exporter) Export(ctx context.Context, destination string, layer core.Layer, profile string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	configuration := e.Profile
	if configuration.ACADVersion == "" {
		configuration = ARESUTF8
	}
	switch profile {
	case "", "ares-utf8":
	case "ares-cp949":
		configuration = ARESCP949
	default:
		return fmt.Errorf("unsupported DXF profile %q", profile)
	}
	encode, err := newTextEncoder(configuration.CodePage)
	if err != nil {
		return err
	}
	file, err := os.Create(destination)
	if err != nil {
		return err
	}
	defer file.Close()
	writer := bufio.NewWriter(file)
	utf8Output := strings.EqualFold(configuration.CodePage, "UTF-8") || strings.EqualFold(configuration.CodePage, "UTF8")
	var encodeBuffer []byte
	write := func(code int, value string) error {
		if !utf8Output {
			encodeBuffer, err = encode(value, encodeBuffer[:0])
			if err != nil {
				return err
			}
		}
		if err := writeCodeLine(writer, code); err != nil {
			return err
		}
		if utf8Output {
			if _, err := writer.WriteString(value); err != nil {
				return err
			}
		} else {
			if _, err := writer.Write(encodeBuffer); err != nil {
				return err
			}
		}
		return writer.WriteByte('\n')
	}
	var floatBuffer []byte
	writeFloat := func(code int, value float64) error {
		floatBuffer = strconv.AppendFloat(floatBuffer[:0], value, 'g', -1, 64)
		if err := writeCodeLine(writer, code); err != nil {
			return err
		}
		if _, err := writer.Write(floatBuffer); err != nil {
			return err
		}
		return writer.WriteByte('\n')
	}
	if err := writeHeader(write, configuration); err != nil {
		return err
	}
	for _, feature := range layer.Features {
		if err := ctx.Err(); err != nil {
			return err
		}
		if wkb, ok := feature.Geometry.(core.WKBGeometry); ok {
			handled, err := writeWKBGeometryWithFloat(write, writeFloat, wkb, layer.Name)
			if err != nil {
				return fmt.Errorf("feature %d geometry: %w", feature.ID, err)
			}
			if !handled {
				geometry, err := core.ToWKT(feature.Geometry)
				if err != nil {
					return fmt.Errorf("feature %d geometry: %w", feature.ID, err)
				}
				if err := writeWKTWithFloat(write, writeFloat, geometry.WKT, layer.Name); err != nil {
					return fmt.Errorf("feature %d: %w", feature.ID, err)
				}
			}
		} else {
			geometry, err := core.ToWKT(feature.Geometry)
			if err != nil {
				return fmt.Errorf("feature %d geometry: %w", feature.ID, err)
			}
			if err := writeWKTWithFloat(write, writeFloat, geometry.WKT, layer.Name); err != nil {
				return fmt.Errorf("feature %d: %w", feature.ID, err)
			}
		}
		if feature.Label != nil && feature.Label.Text != "" {
			label := *feature.Label
			if label.Height <= 0 {
				label.Height = configuration.TextHeight
			}
			if err := writeTextWithFloat(write, writeFloat, label); err != nil {
				return err
			}
		} else if label, ok := feature.Properties["label"].(string); ok && label != "" {
			if err := writeTextWithFloat(write, writeFloat, core.Label{Text: label, Height: configuration.TextHeight}); err != nil {
				return err
			}
		}
	}
	if err := write(0, "ENDSEC"); err != nil {
		return err
	}
	if err := write(0, "EOF"); err != nil {
		return err
	}
	return writer.Flush()
}

func writeWKBGeometry(write func(int, string) error, geometry core.WKBGeometry, layer string) (bool, error) {
	return writeWKBGeometryWithFloat(write, nil, geometry, layer)
}

func writeWKBGeometryWithFloat(write func(int, string) error, writeFloat floatValueWriter, geometry core.WKBGeometry, layer string) (bool, error) {
	if handled, err := writeSimpleWKB2DWithFloat(write, writeFloat, geometry.WKB, layer); handled {
		return true, err
	}
	typeName := geometry.GeometryType()
	switch typeName {
	case "POINT", "LINESTRING", "POLYGON", "MULTIPOINT", "MULTILINESTRING", "MULTIPOLYGON":
	default:
		return false, nil
	}
	parts, err := geometry.Parts()
	if err != nil {
		return true, err
	}
	if len(parts) == 0 || allWKBPartsEmpty(parts) {
		return true, nil
	}
	switch typeName {
	case "POINT", "MULTIPOINT":
		for _, part := range parts {
			if len(part) != 1 {
				return true, fmt.Errorf("invalid WKB point part")
			}
			if err := entityWithFloat(write, writeFloat, "POINT", layer, part[0].X, part[0].Y, 0, 0); err != nil {
				return true, err
			}
		}
	case "LINESTRING", "MULTILINESTRING":
		for _, part := range parts {
			if len(part) < 2 {
				return true, fmt.Errorf("invalid WKB line part")
			}
			if err := writePolylinePointsWithFloat(write, writeFloat, layer, part, false); err != nil {
				return true, err
			}
		}
	case "POLYGON", "MULTIPOLYGON":
		for _, part := range parts {
			if len(part) < 3 {
				return true, fmt.Errorf("invalid WKB polygon ring")
			}
			if err := writePolylinePointsWithFloat(write, writeFloat, layer, part, true); err != nil {
				return true, err
			}
		}
	}
	return true, nil
}

// writeSimpleWKB2DWithFloat handles standard 2D POINT, LINESTRING, and
// POLYGON layouts without materializing core.WKBGeometry.Parts. Extended
// dimensions and collections fall back to the general decoder.
func writeSimpleWKB2DWithFloat(write func(int, string) error, writeFloat floatValueWriter, data []byte, layer string) (bool, error) {
	if len(data) < 5 || (data[0] != 0 && data[0] != 1) {
		return false, nil
	}
	var order binary.ByteOrder = binary.BigEndian
	if data[0] == 1 {
		order = binary.LittleEndian
	}
	typeCode := order.Uint32(data[1:5])
	if typeCode != 1 && typeCode != 2 && typeCode != 3 {
		return false, nil
	}
	if typeCode == 1 {
		if len(data) != 21 {
			return true, fmt.Errorf("invalid WKB point length %d", len(data))
		}
		x := math.Float64frombits(order.Uint64(data[5:13]))
		y := math.Float64frombits(order.Uint64(data[13:21]))
		if math.IsNaN(x) || math.IsNaN(y) {
			return true, nil
		}
		return true, entityWithFloat(write, writeFloat, "POINT", layer, x, y, 0, 0)
	}
	if len(data) < 9 {
		return true, fmt.Errorf("truncated WKB geometry")
	}
	count := order.Uint32(data[5:9])
	if typeCode == 3 {
		if count == 0 {
			if len(data) != 9 {
				return true, fmt.Errorf("invalid WKB polygon length %d", len(data))
			}
			return true, nil
		}
		offset := 9
		for ring := uint32(0); ring < count; ring++ {
			if offset+4 > len(data) {
				return true, fmt.Errorf("truncated WKB polygon ring")
			}
			pointCount := order.Uint32(data[offset : offset+4])
			if pointCount < 3 || uint64(pointCount) > uint64((len(data)-offset-4)/16) {
				return true, fmt.Errorf("invalid WKB polygon point count %d", pointCount)
			}
			offset += 4 + int(pointCount)*16
		}
		if offset != len(data) {
			return true, fmt.Errorf("invalid WKB polygon length %d", len(data))
		}
		offset = 9
		for ring := uint32(0); ring < count; ring++ {
			pointCount := order.Uint32(data[offset : offset+4])
			offset += 4
			if err := write(0, "LWPOLYLINE"); err != nil {
				return true, err
			}
			if err := write(8, layer); err != nil {
				return true, err
			}
			if err := write(90, strconv.FormatUint(uint64(pointCount), 10)); err != nil {
				return true, err
			}
			if err := write(70, "1"); err != nil {
				return true, err
			}
			for index := uint32(0); index < pointCount; index++ {
				x := math.Float64frombits(order.Uint64(data[offset : offset+8]))
				y := math.Float64frombits(order.Uint64(data[offset+8 : offset+16]))
				if err := writeNumber(write, writeFloat, 10, x); err != nil {
					return true, err
				}
				if err := writeNumber(write, writeFloat, 20, y); err != nil {
					return true, err
				}
				offset += 16
			}
		}
		return true, nil
	}
	if count == 0 {
		return false, nil
	}
	if count < 2 || uint64(count) > uint64((len(data)-9)/16) || 9+int(count)*16 != len(data) {
		return true, fmt.Errorf("invalid WKB linestring point count %d", count)
	}
	if err := write(0, "LWPOLYLINE"); err != nil {
		return true, err
	}
	if err := write(8, layer); err != nil {
		return true, err
	}
	if err := write(90, strconv.FormatUint(uint64(count), 10)); err != nil {
		return true, err
	}
	for index := uint32(0); index < count; index++ {
		offset := 9 + int(index)*16
		x := math.Float64frombits(order.Uint64(data[offset : offset+8]))
		y := math.Float64frombits(order.Uint64(data[offset+8 : offset+16]))
		if err := writeNumber(write, writeFloat, 10, x); err != nil {
			return true, err
		}
		if err := writeNumber(write, writeFloat, 20, y); err != nil {
			return true, err
		}
	}
	return true, nil
}

func allWKBPartsEmpty(parts [][]core.WKBPoint) bool {
	for _, part := range parts {
		if len(part) > 0 {
			return false
		}
	}
	return true
}

func writeCodeLine(writer *bufio.Writer, code int) error {
	var buffer [20]byte
	index := len(buffer)
	if code == 0 {
		if err := writer.WriteByte('0'); err != nil {
			return err
		}
		return writer.WriteByte('\n')
	}
	negative := code < 0
	if negative {
		code = -code
	}
	for code > 0 {
		index--
		buffer[index] = byte('0' + code%10)
		code /= 10
	}
	if negative {
		index--
		buffer[index] = '-'
	}
	for _, digit := range buffer[index:] {
		if err := writer.WriteByte(digit); err != nil {
			return err
		}
	}
	return writer.WriteByte('\n')
}

func encodeText(value, codePage string) ([]byte, error) {
	encode, err := newTextEncoder(codePage)
	if err != nil {
		return nil, err
	}
	return encode(value, nil)
}

type textEncoder func(string, []byte) ([]byte, error)

func newTextEncoder(codePage string) (textEncoder, error) {
	switch strings.ToUpper(strings.TrimSpace(codePage)) {
	case "UTF-8", "UTF8":
		return func(value string, dst []byte) ([]byte, error) {
			return append(dst, value...), nil
		}, nil
	case "CP949", "ANSI_949", "EUC-KR":
		encoder := korean.EUCKR.NewEncoder()
		var sourceBuffer []byte
		return func(value string, dst []byte) ([]byte, error) {
			encoder.Reset()
			if cap(sourceBuffer) < len(value) {
				sourceBuffer = make([]byte, len(value))
			} else {
				sourceBuffer = sourceBuffer[:len(value)]
			}
			copy(sourceBuffer, value)
			if cap(dst) < len(value)*2 {
				dst = make([]byte, 0, len(value)*2)
			}
			dst = dst[:0]
			source := sourceBuffer
			for len(source) > 0 {
				if len(dst) == cap(dst) {
					grown := make([]byte, len(dst), cap(dst)*2+1)
					copy(grown, dst)
					dst = grown
				}
				written, consumed, err := encoder.Transform(dst[len(dst):cap(dst)], source, true)
				dst = dst[:len(dst)+written]
				source = source[consumed:]
				if err == transform.ErrShortDst {
					continue
				}
				if err != nil {
					return nil, fmt.Errorf("encode DXF text as CP949: %w", err)
				}
			}
			if _, _, err := encoder.Transform(dst[len(dst):cap(dst)], nil, true); err != nil && err != transform.ErrShortDst {
				return nil, fmt.Errorf("encode DXF text as CP949: %w", err)
			}
			return dst, nil
		}, nil
	default:
		return nil, fmt.Errorf("unsupported DXF code page %q", codePage)
	}
}

func writeHeader(write func(int, string) error, profile Profile) error {
	if err := write(0, "SECTION"); err != nil {
		return err
	}
	if err := write(2, "HEADER"); err != nil {
		return err
	}
	for _, item := range [][2]string{{"$ACADVER", profile.ACADVersion}, {"$DWGCODEPAGE", profile.CodePage}} {
		if err := write(9, item[0]); err != nil {
			return err
		}
		if err := write(1, item[1]); err != nil {
			return err
		}
	}
	if err := write(0, "ENDSEC"); err != nil {
		return err
	}
	if err := write(0, "SECTION"); err != nil {
		return err
	}
	return write(2, "ENTITIES")
}

func writeWKT(write func(int, string) error, wkt, layer string) error {
	return writeWKTWithFloat(write, nil, wkt, layer)
}

type floatValueWriter func(int, float64) error

func writeWKTWithFloat(write func(int, string) error, writeFloat floatValueWriter, wkt, layer string) error {
	trimmed := strings.TrimSpace(wkt)
	if hasWKTEmptySuffix(trimmed) || asciiEqualFold(trimmed, "EMPTY") {
		return nil
	}
	if hasWKTPrefix(trimmed, "POINT") {
		values, err := numbers(wkt)
		if err != nil || len(values) < 2 {
			return fmt.Errorf("invalid POINT WKT")
		}
		return entityWithFloat(write, writeFloat, "POINT", layer, values[0], values[1], 0, 0)
	}
	if hasWKTPrefix(trimmed, "MULTIPOINT") {
		groups, err := geometryGroups(wkt)
		if err != nil {
			return fmt.Errorf("invalid MULTIPOINT WKT")
		}
		if len(groups) == 0 {
			values, numberErr := numbers(wkt)
			if numberErr != nil || len(values) == 0 || len(values)%2 != 0 {
				return fmt.Errorf("invalid MULTIPOINT WKT")
			}
			for index := 0; index < len(values); index += 2 {
				if err := entityWithFloat(write, writeFloat, "POINT", layer, values[index], values[index+1], 0, 0); err != nil {
					return err
				}
			}
			return nil
		}
		for _, group := range groups {
			values, err := numbers(group)
			if err != nil || len(values) < 2 {
				return fmt.Errorf("invalid MULTIPOINT component")
			}
			if err := entityWithFloat(write, writeFloat, "POINT", layer, values[0], values[1], 0, 0); err != nil {
				return err
			}
		}
		return nil
	}
	if hasWKTPrefix(trimmed, "LINESTRING") {
		values, err := numbers(wkt)
		if err != nil || len(values) < 4 || len(values)%2 != 0 {
			return fmt.Errorf("invalid LINESTRING WKT")
		}
		return writePolylineWithFloat(write, writeFloat, layer, values, false)
	}
	if hasWKTPrefix(trimmed, "POLYGON") {
		values, err := numbers(wkt)
		if err != nil || len(values) < 6 || len(values)%2 != 0 {
			return fmt.Errorf("invalid POLYGON WKT")
		}
		return writePolylineWithFloat(write, writeFloat, layer, values, true)
	}
	if hasWKTPrefix(trimmed, "MULTILINESTRING") {
		groups, err := geometryGroups(wkt)
		if err != nil || len(groups) == 0 {
			return fmt.Errorf("invalid MULTILINESTRING WKT")
		}
		for _, group := range groups {
			values, err := numbers(group)
			if err != nil || len(values) < 4 || len(values)%2 != 0 {
				return fmt.Errorf("invalid MULTILINESTRING component")
			}
			if err := writePolylineWithFloat(write, writeFloat, layer, values, false); err != nil {
				return err
			}
		}
		return nil
	}
	if hasWKTPrefix(trimmed, "MULTIPOLYGON") {
		polygons, err := geometryGroups(wkt)
		if err != nil || len(polygons) == 0 {
			return fmt.Errorf("invalid MULTIPOLYGON WKT")
		}
		for _, polygon := range polygons {
			rings, err := geometryGroups(polygon)
			if err != nil || len(rings) == 0 {
				return fmt.Errorf("invalid MULTIPOLYGON component")
			}
			for _, ring := range rings {
				values, err := numbers(ring)
				if err != nil || len(values) < 6 || len(values)%2 != 0 {
					return fmt.Errorf("invalid MULTIPOLYGON ring")
				}
				if err := writePolylineWithFloat(write, writeFloat, layer, values, true); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if hasWKTPrefix(trimmed, "GEOMETRYCOLLECTION") {
		components, err := geometryComponents(wkt)
		if err != nil || len(components) == 0 {
			return fmt.Errorf("invalid GEOMETRYCOLLECTION WKT")
		}
		for _, component := range components {
			if err := writeWKTWithFloat(write, writeFloat, component, layer); err != nil {
				return err
			}
		}
		return nil
	}
	return fmt.Errorf("unsupported geometry type")
}

func hasWKTPrefix(wkt, prefix string) bool {
	if len(wkt) < len(prefix) {
		return false
	}
	return asciiEqualFold(wkt[:len(prefix)], prefix)
}

func hasWKTEmptySuffix(wkt string) bool {
	const suffix = " EMPTY"
	return len(wkt) >= len(suffix) && asciiEqualFold(wkt[len(wkt)-len(suffix):], suffix)
}

func asciiEqualFold(left, right string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		value := left[index]
		if value >= 'a' && value <= 'z' {
			value -= 'a' - 'A'
		}
		if value != right[index] {
			return false
		}
	}
	return true
}

func writePolyline(write func(int, string) error, layer string, values []float64, closed bool) error {
	return writePolylineWithFloat(write, nil, layer, values, closed)
}

func writePolylineWithFloat(write func(int, string) error, writeFloat floatValueWriter, layer string, values []float64, closed bool) error {
	if err := write(0, "LWPOLYLINE"); err != nil {
		return err
	}
	if err := write(8, layer); err != nil {
		return err
	}
	if err := write(90, strconv.Itoa(len(values)/2)); err != nil {
		return err
	}
	if closed {
		if err := write(70, "1"); err != nil {
			return err
		}
	}
	for i := 0; i < len(values); i += 2 {
		if err := writeNumber(write, writeFloat, 10, values[i]); err != nil {
			return err
		}
		if err := writeNumber(write, writeFloat, 20, values[i+1]); err != nil {
			return err
		}
	}
	return nil
}

func writeNumber(write func(int, string) error, writeFloat floatValueWriter, code int, value float64) error {
	if writeFloat != nil {
		return writeFloat(code, value)
	}
	return write(code, format(value))
}

func writePolylinePoints(write func(int, string) error, layer string, points []core.WKBPoint, closed bool) error {
	return writePolylinePointsWithFloat(write, nil, layer, points, closed)
}

func writePolylinePointsWithFloat(write func(int, string) error, writeFloat floatValueWriter, layer string, points []core.WKBPoint, closed bool) error {
	if err := write(0, "LWPOLYLINE"); err != nil {
		return err
	}
	if err := write(8, layer); err != nil {
		return err
	}
	if err := write(90, strconv.Itoa(len(points))); err != nil {
		return err
	}
	if closed {
		if err := write(70, "1"); err != nil {
			return err
		}
	}
	for _, point := range points {
		if err := writeNumber(write, writeFloat, 10, point.X); err != nil {
			return err
		}
		if err := writeNumber(write, writeFloat, 20, point.Y); err != nil {
			return err
		}
	}
	return nil
}

// geometryGroups returns the immediate parenthesized components inside a
// WKT geometry's outer group. It preserves nesting so MULTIPOLYGON rings can
// be handled without turning separate parts into one connected line.
func geometryGroups(wkt string) ([]string, error) {
	start := strings.IndexByte(wkt, '(')
	if start < 0 {
		return nil, fmt.Errorf("geometry has no coordinate group")
	}
	content := wkt[start:]
	if len(content) < 2 || content[0] != '(' || content[len(content)-1] != ')' {
		return nil, fmt.Errorf("geometry has unbalanced parentheses")
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
				return nil, fmt.Errorf("geometry has unbalanced parentheses")
			}
			if depth == 0 && groupStart >= 0 {
				groups = append(groups, content[groupStart:index+1])
				groupStart = -1
			}
		}
	}
	if depth != 0 {
		return nil, fmt.Errorf("geometry has unbalanced parentheses")
	}
	return groups, nil
}

func geometryComponents(wkt string) ([]string, error) {
	start := strings.IndexByte(wkt, '(')
	if start < 0 {
		return nil, fmt.Errorf("geometry has no component group")
	}
	content := wkt[start:]
	if len(content) < 2 || content[0] != '(' || content[len(content)-1] != ')' {
		return nil, fmt.Errorf("geometry has unbalanced parentheses")
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
				return nil, fmt.Errorf("geometry has unbalanced parentheses")
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
		return nil, fmt.Errorf("geometry has unbalanced parentheses")
	}
	if component := strings.TrimSpace(content[componentStart:]); component != "" {
		components = append(components, component)
	}
	return components, nil
}

func writeText(write func(int, string) error, label core.Label) error {
	return writeTextWithFloat(write, nil, label)
}

func writeTextWithFloat(write func(int, string) error, writeFloat floatValueWriter, label core.Label) error {
	if err := write(0, "TEXT"); err != nil {
		return err
	}
	if err := write(8, "LABEL"); err != nil {
		return err
	}
	if err := writeNumber(write, writeFloat, 10, label.X); err != nil {
		return err
	}
	if err := writeNumber(write, writeFloat, 20, label.Y); err != nil {
		return err
	}
	if err := writeNumber(write, writeFloat, 40, label.Height); err != nil {
		return err
	}
	if label.Rotation != 0 {
		if err := writeNumber(write, writeFloat, 50, label.Rotation); err != nil {
			return err
		}
	}
	if label.Style != "" {
		if err := write(7, label.Style); err != nil {
			return err
		}
	}
	return write(1, label.Text)
}

func entity(write func(int, string) error, kind, layer string, x, y, x2, y2 float64) error {
	return entityWithFloat(write, nil, kind, layer, x, y, x2, y2)
}

func entityWithFloat(write func(int, string) error, writeFloat floatValueWriter, kind, layer string, x, y, x2, y2 float64) error {
	if err := write(0, kind); err != nil {
		return err
	}
	if err := write(8, layer); err != nil {
		return err
	}
	if err := writeNumber(write, writeFloat, 10, x); err != nil {
		return err
	}
	if err := writeNumber(write, writeFloat, 20, y); err != nil {
		return err
	}
	if kind == "LINE" {
		if err := writeNumber(write, writeFloat, 11, x2); err != nil {
			return err
		}
		return writeNumber(write, writeFloat, 21, y2)
	}
	return nil
}

func numbers(wkt string) ([]float64, error) {
	capacity := len(wkt) / 8
	if capacity < 8 {
		capacity = 8
	}
	result := make([]float64, 0, capacity)
	for index := 0; index < len(wkt); {
		if !isNumberStart(wkt[index]) {
			index++
			continue
		}
		start := index
		index++
		for index < len(wkt) {
			character := wkt[index]
			if character >= '0' && character <= '9' || character == '.' || character == 'e' || character == 'E' || character == '+' || character == '-' {
				index++
				continue
			}
			break
		}
		value, err := strconv.ParseFloat(wkt[start:index], 64)
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, nil
}

func isNumberStart(character byte) bool {
	return character >= '0' && character <= '9' || character == '+' || character == '-' || character == '.'
}

func format(value float64) string { return strconv.FormatFloat(value, 'g', -1, 64) }
