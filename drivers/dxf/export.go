package dxf

import (
	"bufio"
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"gogis/internal/core"
	"golang.org/x/text/encoding/japanese"
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
// UTF-8 is supported by the DXF format starting with AutoCAD 2007 (AC1021).
var ARESUTF8 = Profile{ACADVersion: "AC1021", CodePage: "UTF-8", TextHeight: 1}

// ARESCP949 is the legacy Korean code-page profile. ARES/CAD compatibility
// still needs validation in the target application, but the byte encoding and
// declared DXF header are now deterministic.
var ARESCP949 = Profile{ACADVersion: "AC1015", CodePage: "ANSI_949", TextHeight: 1}

// ARESSHIFTJIS is the common legacy code page for Japanese CAD deployments.
var ARESSHIFTJIS = Profile{ACADVersion: "AC1015", CodePage: "ANSI_932", TextHeight: 1}

// Exporter writes a small, inspectable ASCII DXF subset without hiding header
// or encoding decisions behind a third-party library.
type Exporter struct {
	Profile Profile
	// Triangulate emits filled polygons as DXF SOLID triangles when supplied.
	// The caller owns the triangulator's native resource lifetime.
	Triangulate func(context.Context, core.Geometry) ([][3][2]float64, error)
}

// LayerSpec declares a CAD layer before its features are loaded. Loaders can
// release each source layer after it has been written to the DXF stream.
type LayerSpec struct {
	Name                string
	Styles              []string
	GeometryType        string
	PointLabelPlacement string
	PointLabelOffsetMM  float64
	LabelHeightMM       float64
	Bounds              [4]float64
	HasBounds           bool
	SkipGeometry        bool
	SkipLabels          bool
	FillPolygons        bool
}

// Export writes Point, LineString, Polygon and a property-driven TEXT label.
func (e Exporter) Export(ctx context.Context, destination string, layer core.Layer, profile string) error {
	if layer.Name == "" {
		layer.Name = "0"
	}
	styles := make([]string, 0)
	for _, feature := range layer.Features {
		if feature.Label != nil && feature.Label.Text != "" && feature.Label.Style != "" {
			styles = append(styles, feature.Label.Style)
		}
	}
	geometryType, bounds, hasBounds := layerGeometryBounds(layer)
	return e.export(ctx, destination, []LayerSpec{{Name: layer.Name, Styles: styles, GeometryType: geometryType,
		PointLabelPlacement: layer.Labels.PointPlacement, PointLabelOffsetMM: layer.Labels.PointOffsetMM, LabelHeightMM: layer.Labels.HeightMM,
		Bounds: bounds, HasBounds: hasBounds, FillPolygons: layer.Style.FillOpacity > 0}},
		func(int) (core.Layer, error) { return layer, nil }, profile, true)
}

func layerGeometryBounds(layer core.Layer) (string, [4]float64, bool) {
	var geometryType string
	var bounds [4]float64
	hasBounds := false
	for _, feature := range layer.Features {
		if feature.Geometry == nil {
			continue
		}
		if geometryType == "" {
			geometryType = feature.Geometry.GeometryType()
		}
		var points [][]core.WKBPoint
		switch geometry := feature.Geometry.(type) {
		case core.WKBGeometry:
			var err error
			points, err = geometry.Parts()
			if err != nil {
				continue
			}
		default:
			wkt, err := core.ToWKT(feature.Geometry)
			if err != nil {
				continue
			}
			values, err := numbers(wkt.WKT)
			if err != nil || len(values) < 2 || len(values)%2 != 0 {
				continue
			}
			part := make([]core.WKBPoint, 0, len(values)/2)
			for index := 0; index+1 < len(values); index += 2 {
				part = append(part, core.WKBPoint{X: values[index], Y: values[index+1]})
			}
			points = [][]core.WKBPoint{part}
		}
		for _, part := range points {
			for _, point := range part {
				if math.IsNaN(point.X) || math.IsInf(point.X, 0) || math.IsNaN(point.Y) || math.IsInf(point.Y, 0) {
					continue
				}
				if !hasBounds {
					bounds = [4]float64{point.X, point.Y, point.X, point.Y}
					hasBounds = true
					continue
				}
				bounds[0] = math.Min(bounds[0], point.X)
				bounds[1] = math.Min(bounds[1], point.Y)
				bounds[2] = math.Max(bounds[2], point.X)
				bounds[3] = math.Max(bounds[3], point.Y)
			}
		}
	}
	return geometryType, bounds, hasBounds
}

// ExportProject writes all declared layers into one DXF, loading one layer at
// a time. Unlike the legacy single-layer Export, TEXT stays on its source CAD
// layer instead of a shared LABEL layer.
func (e Exporter) ExportProject(ctx context.Context, destination string, layers []LayerSpec, load func(int) (core.Layer, error), profile string) error {
	return e.export(ctx, destination, layers, load, profile, false)
}

func (e Exporter) export(ctx context.Context, destination string, specs []LayerSpec, load func(int) (core.Layer, error), profile string, separateLabels bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(specs) == 0 || load == nil {
		return fmt.Errorf("DXF export requires at least one layer and a loader")
	}
	seenLayers := make(map[string]bool, len(specs))
	for _, spec := range specs {
		if spec.Name == "" || strings.TrimSpace(spec.Name) != spec.Name || utf8.RuneCountInString(spec.Name) > 255 ||
			strings.ContainsAny(spec.Name, "<>/\\\":;?*|=,'") || strings.ContainsFunc(spec.Name, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
			return fmt.Errorf("invalid DXF layer name %q", spec.Name)
		}
		key := strings.ToUpper(spec.Name)
		if seenLayers[key] {
			return fmt.Errorf("duplicate DXF layer name %q; rename one project layer before export", spec.Name)
		}
		seenLayers[key] = true
	}
	configuration := e.Profile
	if configuration.ACADVersion == "" {
		configuration = ARESUTF8
	}
	switch profile {
	case "", "ares-utf8":
	case "ares-cp949":
		configuration = ARESCP949
	case "ares-shift-jis":
		configuration = ARESSHIFTJIS
	default:
		return fmt.Errorf("unsupported DXF profile %q", profile)
	}
	encode, err := newTextEncoder(configuration.CodePage)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(destination), ".gogis-dxf-*.tmp")
	if err != nil {
		return err
	}
	defer func() {
		_ = file.Close()
		_ = os.Remove(file.Name())
	}()
	writer := bufio.NewWriter(file)
	utf8Output := strings.EqualFold(configuration.CodePage, "UTF-8") || strings.EqualFold(configuration.CodePage, "UTF8")
	var encodeBuffer []byte
	writeRaw := func(code int, value string) error {
		if strings.ContainsAny(value, "\r\n\x00") {
			return fmt.Errorf("DXF group %d contains a line break or NUL", code)
		}
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
	nextHandle := uint64(0x30)
	allocHandle := func() string {
		handle := strings.ToUpper(strconv.FormatUint(nextHandle, 16))
		nextHandle++
		return handle
	}
	entitySubclass := ""
	write := func(code int, value string) error {
		if code == 0 {
			switch value {
			case "POINT":
				entitySubclass = "AcDbPoint"
			case "LINE":
				entitySubclass = "AcDbLine"
			case "LWPOLYLINE":
				entitySubclass = "AcDbPolyline"
			case "HATCH":
				entitySubclass = "AcDbHatch"
			case "SOLID":
				entitySubclass = "AcDbTrace"
			case "TEXT":
				entitySubclass = "AcDbText"
			default:
				entitySubclass = ""
			}
			if entitySubclass != "" {
				if err := writeRaw(0, value); err != nil {
					return err
				}
				return writePairs(writeRaw, dxfPair{5, allocHandle()}, dxfPair{330, "1F"}, dxfPair{100, "AcDbEntity"})
			}
		}
		if err := writeRaw(code, value); err != nil {
			return err
		}
		if code == 8 && entitySubclass != "" {
			subclass := entitySubclass
			entitySubclass = ""
			return writeRaw(100, subclass)
		}
		return nil
	}
	var floatBuffer []byte
	writeFloat := func(code int, value float64) error {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return fmt.Errorf("DXF group %d has non-finite coordinate or size", code)
		}
		floatBuffer = strconv.AppendFloat(floatBuffer[:0], value, 'g', -1, 64)
		if err := writeCodeLine(writer, code); err != nil {
			return err
		}
		if _, err := writer.Write(floatBuffer); err != nil {
			return err
		}
		return writer.WriteByte('\n')
	}
	if err := writeHeader(write, configuration, specs, separateLabels, allocHandle); err != nil {
		return err
	}
	for index, spec := range specs {
		layer, loadErr := load(index)
		if loadErr != nil {
			return fmt.Errorf("load DXF layer %q: %w", spec.Name, loadErr)
		}
		for _, feature := range layer.Features {
			if err := ctx.Err(); err != nil {
				return err
			}
			if !spec.SkipGeometry {
				if spec.FillPolygons {
					if e.Triangulate != nil {
						triangles, err := e.Triangulate(ctx, feature.Geometry)
						if err != nil {
							return fmt.Errorf("feature %d fill triangulation: %w", feature.ID, err)
						}
						if err := writeSolidTrianglesWithFloat(write, writeFloat, spec.Name, triangles); err != nil {
							return fmt.Errorf("feature %d triangle fill: %w", feature.ID, err)
						}
					} else if err := writeGeometrySolidFillWithFloat(write, writeFloat, feature.Geometry, spec.Name); err != nil {
						return fmt.Errorf("feature %d fill: %w", feature.ID, err)
					}
				}
				if wkb, ok := feature.Geometry.(core.WKBGeometry); ok {
					handled, err := writeWKBGeometryWithFloat(write, writeFloat, wkb, spec.Name)
					if err != nil {
						return fmt.Errorf("feature %d geometry: %w", feature.ID, err)
					}
					if !handled {
						geometry, err := core.ToWKT(feature.Geometry)
						if err != nil {
							return fmt.Errorf("feature %d geometry: %w", feature.ID, err)
						}
						if err := writeWKTWithFloat(write, writeFloat, geometry.WKT, spec.Name); err != nil {
							return fmt.Errorf("feature %d: %w", feature.ID, err)
						}
					}
				} else {
					geometry, err := core.ToWKT(feature.Geometry)
					if err != nil {
						return fmt.Errorf("feature %d geometry: %w", feature.ID, err)
					}
					if err := writeWKTWithFloat(write, writeFloat, geometry.WKT, spec.Name); err != nil {
						return fmt.Errorf("feature %d: %w", feature.ID, err)
					}
				}
			}
			if spec.SkipLabels {
				continue
			}
			if feature.Label != nil && feature.Label.Text != "" {
				label := *feature.Label
				if label.Height <= 0 {
					label.Height = configuration.TextHeight
				}
				labelLayer := spec.Name
				if separateLabels {
					labelLayer = "LABEL"
				}
				alignment, aligned := pointTextAlignment(label, feature.Geometry, spec)
				if err := writeTextOnLayerAlignedWithFloat(write, writeFloat, label, labelLayer, alignment, aligned); err != nil {
					return err
				}
			} else if label, ok := feature.Properties["label"].(string); ok && label != "" {
				labelLayer := spec.Name
				if separateLabels {
					labelLayer = "LABEL"
				}
				if err := writeTextOnLayerWithFloat(write, writeFloat, core.Label{Text: label, Height: configuration.TextHeight}, labelLayer); err != nil {
					return err
				}
			}
		}
	}
	if err := write(0, "ENDSEC"); err != nil {
		return err
	}
	if err := write(0, "EOF"); err != nil {
		return err
	}
	if err := writer.Flush(); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return os.Rename(file.Name(), destination)
}

func writeWKBGeometry(write func(int, string) error, geometry core.WKBGeometry, layer string) (bool, error) {
	return writeWKBGeometryWithFloat(write, nil, geometry, layer)
}

// SOLID triangles avoid CAD HATCH rendering for dense polygon layers while
// the following LWPOLYLINE entities retain every original boundary and hole.
func writeSolidTrianglesWithFloat(write func(int, string) error, writeFloat floatValueWriter, layer string, triangles [][3][2]float64) error {
	for _, triangle := range triangles {
		if err := writePairs(write, dxfPair{0, "SOLID"}, dxfPair{8, layer}); err != nil {
			return err
		}
		for index, point := range [4][2]float64{triangle[0], triangle[1], triangle[2], triangle[2]} {
			if err := writeNumber(write, writeFloat, 10+index, point[0]); err != nil {
				return err
			}
			if err := writeNumber(write, writeFloat, 20+index, point[1]); err != nil {
				return err
			}
			if err := write(30+index, "0"); err != nil {
				return err
			}
		}
	}
	return nil
}

// A solid HATCH keeps the source polygon's fill, including interior rings.
// It is emitted before the outline so the boundary remains visible in CAD.
func writeGeometrySolidFillWithFloat(write func(int, string) error, writeFloat floatValueWriter, geometry core.Geometry, layer string) error {
	if geometry == nil || !strings.Contains(strings.ToUpper(geometry.GeometryType()), "POLYGON") {
		return nil
	}
	var wkt string
	switch value := geometry.(type) {
	case core.WKBGeometry:
		var err error
		decoded, err := value.WKT()
		if err != nil {
			return err
		}
		wkt = decoded
	case core.WKTGeometry:
		wkt = value.WKT
	default:
		return fmt.Errorf("unsupported polygon geometry %T", geometry)
	}
	polygons, err := polygonRingsFromWKT(wkt)
	if err != nil {
		return err
	}
	for _, polygon := range polygons {
		if err := writeSolidHatchWithFloat(write, writeFloat, layer, polygon); err != nil {
			return err
		}
	}
	return nil
}

func polygonRingsFromWKT(wkt string) ([][][]core.WKBPoint, error) {
	if hasWKTEmptySuffix(strings.TrimSpace(wkt)) {
		return nil, nil
	}
	var polygonGroups [][]string
	if hasWKTPrefix(strings.TrimSpace(wkt), "MULTIPOLYGON") {
		polygons, err := geometryGroups(wkt)
		if err != nil {
			return nil, err
		}
		for _, polygon := range polygons {
			rings, err := geometryGroups(polygon)
			if err != nil {
				return nil, err
			}
			polygonGroups = append(polygonGroups, rings)
		}
	} else {
		var err error
		rings, err := geometryGroups(wkt)
		if err != nil {
			return nil, err
		}
		polygonGroups = append(polygonGroups, rings)
	}
	polygons := make([][][]core.WKBPoint, 0, len(polygonGroups))
	for _, ringGroups := range polygonGroups {
		rings := make([][]core.WKBPoint, 0, len(ringGroups))
		for _, group := range ringGroups {
			values, err := numbers(group)
			if err != nil || len(values) < 6 || len(values)%2 != 0 {
				return nil, fmt.Errorf("invalid polygon ring")
			}
			points := make([]core.WKBPoint, 0, len(values)/2)
			for index := 0; index < len(values); index += 2 {
				points = append(points, core.WKBPoint{X: values[index], Y: values[index+1]})
			}
			rings = append(rings, points)
		}
		polygons = append(polygons, rings)
	}
	return polygons, nil
}

type hatchBoundaryPath struct {
	points   []core.WKBPoint
	external bool
}

func writeSolidHatchWithFloat(write func(int, string) error, writeFloat floatValueWriter, layer string, rings [][]core.WKBPoint) error {
	paths := make([]hatchBoundaryPath, 0, len(rings))
	for ringIndex, ring := range rings {
		if len(ring) > 1 && ring[0] == ring[len(ring)-1] {
			ring = ring[:len(ring)-1]
		}
		if len(ring) < 3 {
			continue
		}
		area := 0.0
		origin := ring[0]
		for index := 1; index+1 < len(ring); index++ {
			a := ring[index]
			b := ring[index+1]
			area += (a.X-origin.X)*(b.Y-origin.Y) - (b.X-origin.X)*(a.Y-origin.Y)
		}
		if area != 0 {
			paths = append(paths, hatchBoundaryPath{points: ring, external: ringIndex == 0})
		}
	}
	if len(paths) == 0 {
		return nil
	}
	if err := writePairs(write, dxfPair{0, "HATCH"}, dxfPair{8, layer},
		dxfPair{10, "0"}, dxfPair{20, "0"}, dxfPair{30, "0"},
		dxfPair{2, "SOLID"}, dxfPair{70, "1"}, dxfPair{71, "0"},
		dxfPair{91, strconv.Itoa(len(paths))}); err != nil {
		return err
	}
	// Elevation is 0 in the 2D project CRS; OCS defaults to +Z.
	return writeSolidHatchPaths(write, writeFloat, paths)
}

func writeSolidHatchPaths(write func(int, string) error, writeFloat floatValueWriter, paths []hatchBoundaryPath) error {
	for _, path := range paths {
		flags := "2" // Polyline boundary.
		if path.external {
			flags = "3" // External + polyline boundary.
		}
		if err := writePairs(write, dxfPair{92, flags}, dxfPair{72, "0"}, dxfPair{73, "1"},
			dxfPair{93, strconv.Itoa(len(path.points))}); err != nil {
			return err
		}
		for _, point := range path.points {
			if err := writeNumber(write, writeFloat, 10, point.X); err != nil {
				return err
			}
			if err := writeNumber(write, writeFloat, 20, point.Y); err != nil {
				return err
			}
		}
		if err := write(97, "0"); err != nil {
			return err
		}
	}
	return writePairs(write, dxfPair{75, "0"}, dxfPair{76, "1"}, dxfPair{98, "0"})
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
	case "CP949", "ANSI_949", "EUC-KR", "SHIFT-JIS", "SHIFT_JIS", "CP932", "ANSI_932":
		encoderName := "CP949"
		var encoder transform.Transformer = korean.EUCKR.NewEncoder()
		if normalized := strings.ToUpper(strings.TrimSpace(codePage)); normalized == "SHIFT-JIS" || normalized == "SHIFT_JIS" || normalized == "CP932" || normalized == "ANSI_932" {
			encoderName = "Shift-JIS"
			encoder = japanese.ShiftJIS.NewEncoder()
		}
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
					return nil, fmt.Errorf("encode DXF text as %s: %w", encoderName, err)
				}
			}
			if _, _, err := encoder.Transform(dst[len(dst):cap(dst)], nil, true); err != nil && err != transform.ErrShortDst {
				return nil, fmt.Errorf("encode DXF text as %s: %w", encoderName, err)
			}
			return dst, nil
		}, nil
	default:
		return nil, fmt.Errorf("unsupported DXF code page %q", codePage)
	}
}

func writeHeader(write func(int, string) error, profile Profile, specs []LayerSpec, separateLabels bool, allocHandle func() string) error {
	if err := write(0, "SECTION"); err != nil {
		return err
	}
	if err := write(2, "HEADER"); err != nil {
		return err
	}
	if err := writePairs(write, dxfPair{9, "$ACADVER"}, dxfPair{1, profile.ACADVersion},
		dxfPair{9, "$DWGCODEPAGE"}, dxfPair{3, profile.CodePage},
		dxfPair{9, "$PDMODE"}, dxfPair{70, "35"},
		// Negative PDSIZE is a percentage of the viewport; 3% keeps cadastral
		// control points recognizable at typical CAD zoom levels.
		dxfPair{9, "$PDSIZE"}, dxfPair{40, "-3"},
		dxfPair{9, "$EXTNAMES"}, dxfPair{290, "1"}); err != nil {
		return err
	}
	centerX, centerY, viewSize, hasView := initialView(specs)
	if hasView {
		if err := writePairs(write, dxfPair{9, "$VIEWCTR"}); err != nil {
			return err
		}
		if err := writeNumber(write, nil, 10, centerX); err != nil {
			return err
		}
		if err := writeNumber(write, nil, 20, centerY); err != nil {
			return err
		}
		if err := writePairs(write, dxfPair{9, "$VIEWSIZE"}); err != nil {
			return err
		}
		if err := writeNumber(write, nil, 40, viewSize); err != nil {
			return err
		}
	}
	if err := write(0, "ENDSEC"); err != nil {
		return err
	}
	// AC1015 entities carry database handles and an owning model-space block
	// record. Declare those references (and used layer/style names) explicitly
	// so strict CAD readers need not repair the drawing on open.
	if err := writePairs(write, dxfPair{0, "SECTION"}, dxfPair{2, "TABLES"}); err != nil {
		return err
	}
	if hasView {
		if err := writeActiveViewport(write, centerX, centerY, viewSize); err != nil {
			return err
		}
	}
	if err := writePairs(write,
		dxfPair{0, "TABLE"}, dxfPair{2, "LTYPE"}, dxfPair{5, "5"}, dxfPair{330, "0"}, dxfPair{100, "AcDbSymbolTable"}, dxfPair{70, "1"},
		dxfPair{0, "LTYPE"}, dxfPair{5, "14"}, dxfPair{330, "5"}, dxfPair{100, "AcDbSymbolTableRecord"}, dxfPair{100, "AcDbLinetypeTableRecord"},
		dxfPair{2, "Continuous"}, dxfPair{70, "0"}, dxfPair{3, "Solid line"}, dxfPair{72, "65"}, dxfPair{73, "0"}, dxfPair{40, "0"},
		dxfPair{0, "ENDTAB"}); err != nil {
		return err
	}
	layers := []string{"0"}
	for _, spec := range specs {
		if spec.Name != "0" {
			layers = append(layers, spec.Name)
		}
	}
	if separateLabels && !strings.EqualFold(specs[0].Name, "LABEL") {
		layers = append(layers, "LABEL")
	}
	if err := writePairs(write, dxfPair{0, "TABLE"}, dxfPair{2, "LAYER"}, dxfPair{5, "2"}, dxfPair{330, "0"},
		dxfPair{100, "AcDbSymbolTable"}, dxfPair{70, strconv.Itoa(len(layers))}); err != nil {
		return err
	}
	for _, name := range layers {
		handle := "10"
		if name != "0" {
			handle = allocHandle()
		}
		if err := writePairs(write,
			dxfPair{0, "LAYER"}, dxfPair{5, handle}, dxfPair{330, "2"}, dxfPair{100, "AcDbSymbolTableRecord"},
			dxfPair{100, "AcDbLayerTableRecord"}, dxfPair{2, name}, dxfPair{70, "0"}, dxfPair{62, "7"},
			dxfPair{6, "Continuous"}); err != nil {
			return err
		}
	}
	if err := write(0, "ENDTAB"); err != nil {
		return err
	}
	styles := []string{"Standard"}
	seenStyles := map[string]bool{"STANDARD": true}
	for _, spec := range specs {
		for _, name := range spec.Styles {
			if name == "" {
				continue
			}
			key := strings.ToUpper(name)
			if !seenStyles[key] {
				styles = append(styles, name)
				seenStyles[key] = true
			}
		}
	}
	if err := writePairs(write, dxfPair{0, "TABLE"}, dxfPair{2, "STYLE"}, dxfPair{5, "6"}, dxfPair{330, "0"},
		dxfPair{100, "AcDbSymbolTable"}, dxfPair{70, strconv.Itoa(len(styles))}); err != nil {
		return err
	}
	for _, name := range styles {
		handle := "16"
		if name != "Standard" {
			handle = allocHandle()
		}
		font := "malgun.ttf"
		if strings.EqualFold(profile.CodePage, "ANSI_932") {
			font = "meiryo.ttc"
		}
		if err := writePairs(write,
			dxfPair{0, "STYLE"}, dxfPair{5, handle}, dxfPair{330, "6"}, dxfPair{100, "AcDbSymbolTableRecord"},
			dxfPair{100, "AcDbTextStyleTableRecord"}, dxfPair{2, name}, dxfPair{70, "0"},
			dxfPair{40, "0"}, dxfPair{41, "1"}, dxfPair{50, "0"}, dxfPair{71, "0"}, dxfPair{42, "1"},
			dxfPair{3, font}, dxfPair{4, ""}); err != nil {
			return err
		}
	}
	if err := write(0, "ENDTAB"); err != nil {
		return err
	}
	if err := writePairs(write,
		dxfPair{0, "TABLE"}, dxfPair{2, "BLOCK_RECORD"}, dxfPair{5, "7"}, dxfPair{330, "0"},
		dxfPair{100, "AcDbSymbolTable"}, dxfPair{70, "1"},
		dxfPair{0, "BLOCK_RECORD"}, dxfPair{5, "1F"}, dxfPair{330, "7"}, dxfPair{100, "AcDbSymbolTableRecord"},
		dxfPair{100, "AcDbBlockTableRecord"}, dxfPair{2, "*Model_Space"}, dxfPair{0, "ENDTAB"},
		dxfPair{0, "ENDSEC"}, dxfPair{0, "SECTION"}, dxfPair{2, "BLOCKS"},
		dxfPair{0, "BLOCK"}, dxfPair{5, "20"}, dxfPair{330, "1F"}, dxfPair{100, "AcDbEntity"}, dxfPair{8, "0"},
		dxfPair{100, "AcDbBlockBegin"}, dxfPair{2, "*Model_Space"}, dxfPair{70, "0"},
		dxfPair{10, "0"}, dxfPair{20, "0"}, dxfPair{30, "0"}, dxfPair{3, "*Model_Space"}, dxfPair{1, ""},
		dxfPair{0, "ENDBLK"}, dxfPair{5, "21"}, dxfPair{330, "1F"}, dxfPair{100, "AcDbEntity"},
		dxfPair{8, "0"}, dxfPair{100, "AcDbBlockEnd"}, dxfPair{0, "ENDSEC"}); err != nil {
		return err
	}
	if err := write(0, "SECTION"); err != nil {
		return err
	}
	return write(2, "ENTITIES")
}

// An *ACTIVE VPORT entry controls the opening view in modern CAD readers.
// The legacy $VIEWCTR/$VIEWSIZE header values above remain for older readers.
func writeActiveViewport(write func(int, string) error, centerX, centerY, viewHeight float64) error {
	if err := writePairs(write,
		dxfPair{0, "TABLE"}, dxfPair{2, "VPORT"}, dxfPair{5, "8"}, dxfPair{330, "0"},
		dxfPair{100, "AcDbSymbolTable"}, dxfPair{70, "1"},
		dxfPair{0, "VPORT"}, dxfPair{5, "1A"}, dxfPair{330, "8"},
		dxfPair{100, "AcDbSymbolTableRecord"}, dxfPair{100, "AcDbViewportTableRecord"},
		dxfPair{2, "*ACTIVE"}, dxfPair{70, "0"},
		dxfPair{10, "0"}, dxfPair{20, "0"}, dxfPair{11, "1"}, dxfPair{21, "1"}); err != nil {
		return err
	}
	if err := writeNumber(write, nil, 12, centerX); err != nil {
		return err
	}
	if err := writeNumber(write, nil, 22, centerY); err != nil {
		return err
	}
	if err := writePairs(write,
		dxfPair{16, "0"}, dxfPair{26, "0"}, dxfPair{36, "1"},
		dxfPair{17, "0"}, dxfPair{27, "0"}, dxfPair{37, "0"},
		dxfPair{40, "50"}, dxfPair{41, "1.6"}); err != nil {
		return err
	}
	if err := writeNumber(write, nil, 45, viewHeight); err != nil {
		return err
	}
	return write(0, "ENDTAB")
}

// initialView derives a conservative opening view from the selected project
// layers. Point-only layers are excluded when any non-point layer is available
// so isolated survey points do not zoom the drawing out to their outliers.
func initialView(specs []LayerSpec) (centerX, centerY, viewSize float64, ok bool) {
	var all, preferred [4]float64
	var hasAll, hasPreferred bool
	for _, spec := range specs {
		if !spec.HasBounds || !validBounds(spec.Bounds) {
			continue
		}
		all, hasAll = mergeBounds(all, hasAll, spec.Bounds)
		if !strings.Contains(strings.ToUpper(spec.GeometryType), "POINT") {
			preferred, hasPreferred = mergeBounds(preferred, hasPreferred, spec.Bounds)
		}
	}
	bounds, hasBounds := preferred, hasPreferred
	if !hasBounds {
		bounds, hasBounds = all, hasAll
	}
	if !hasBounds {
		return 0, 0, 0, false
	}
	centerX = bounds[0] + (bounds[2]-bounds[0])/2
	centerY = bounds[1] + (bounds[3]-bounds[1])/2
	viewSize = math.Max(bounds[2]-bounds[0], bounds[3]-bounds[1]) * 1.15
	if viewSize <= 0 || math.IsNaN(viewSize) || math.IsInf(viewSize, 0) {
		viewSize = 1
	}
	return centerX, centerY, viewSize, true
}

func validBounds(bounds [4]float64) bool {
	for _, value := range bounds {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return false
		}
	}
	return bounds[0] <= bounds[2] && bounds[1] <= bounds[3]
}

func mergeBounds(current [4]float64, hasCurrent bool, next [4]float64) ([4]float64, bool) {
	if !hasCurrent {
		return next, true
	}
	return [4]float64{
		math.Min(current[0], next[0]), math.Min(current[1], next[1]),
		math.Max(current[2], next[2]), math.Max(current[3], next[3]),
	}, true
}

type dxfPair struct {
	code  int
	value string
}

func writePairs(write func(int, string) error, pairs ...dxfPair) error {
	for _, pair := range pairs {
		if err := write(pair.code, pair.value); err != nil {
			return err
		}
	}
	return nil
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
		rings, err := polygonRingsFromWKT(wkt)
		if err != nil {
			return err
		}
		for _, polygon := range rings {
			for _, ring := range polygon {
				if err := writePolylinePointsWithFloat(write, writeFloat, layer, ring, true); err != nil {
					return err
				}
			}
		}
		return nil
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
	return writeTextOnLayerWithFloat(write, writeFloat, label, "LABEL")
}

func writeTextOnLayerWithFloat(write func(int, string) error, writeFloat floatValueWriter, label core.Label, layer string) error {
	return writeTextOnLayerAlignedWithFloat(write, writeFloat, label, layer, textAlignment{}, false)
}

type textAlignment struct {
	x, y                 float64
	horizontal, vertical int
}

func pointTextAlignment(label core.Label, geometry core.Geometry, spec LayerSpec) (textAlignment, bool) {
	if geometry == nil || !strings.Contains(strings.ToUpper(geometry.GeometryType()), "POINT") || !label.AnchorSet {
		return textAlignment{}, false
	}
	placement := spec.PointLabelPlacement
	if placement == "" {
		placement = "NE"
	}
	offset := spec.PointLabelOffsetMM
	if offset <= 0 && spec.PointLabelPlacement == "" {
		offset = 1.5 // backward-compatible default for projects saved before point placement existed
	}
	baseHeight := spec.LabelHeightMM
	if baseHeight <= 0 {
		baseHeight = label.Height
	}
	if baseHeight <= 0 {
		baseHeight = 1
	}
	distance := offset * label.Height / baseHeight
	if distance <= 0 {
		return textAlignment{}, false
	}
	unit := 1 / math.Sqrt2
	result := textAlignment{x: label.X, y: label.Y}
	switch placement {
	case "N":
		result.y += distance
		result.horizontal, result.vertical = 1, 1 // centered, bottom-aligned
	case "NE":
		result.x += distance * unit
		result.y += distance * unit
		result.horizontal, result.vertical = 0, 1 // left, bottom
	case "E":
		result.x += distance
		result.horizontal, result.vertical = 0, 2 // left, middle
	case "SE":
		result.x += distance * unit
		result.y -= distance * unit
		result.horizontal, result.vertical = 0, 3 // left, top
	case "S":
		result.y -= distance
		result.horizontal, result.vertical = 1, 3 // centered, top-aligned
	case "SW":
		result.x -= distance * unit
		result.y -= distance * unit
		result.horizontal, result.vertical = 2, 3 // right, top
	case "W":
		result.x -= distance
		result.horizontal, result.vertical = 2, 2 // right, middle
	case "NW":
		result.x -= distance * unit
		result.y += distance * unit
		result.horizontal, result.vertical = 2, 1 // right, bottom
	default:
		return textAlignment{}, false
	}
	return result, true
}

func writeTextOnLayerAlignedWithFloat(write func(int, string) error, writeFloat floatValueWriter, label core.Label, layer string, alignment textAlignment, aligned bool) error {
	if err := write(0, "TEXT"); err != nil {
		return err
	}
	if err := write(8, layer); err != nil {
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
	if err := write(1, label.Text); err != nil {
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
	if aligned {
		if err := write(72, strconv.Itoa(alignment.horizontal)); err != nil {
			return err
		}
		if err := writeNumber(write, writeFloat, 11, alignment.x); err != nil {
			return err
		}
		if err := writeNumber(write, writeFloat, 21, alignment.y); err != nil {
			return err
		}
		if err := writeNumber(write, writeFloat, 31, 0); err != nil {
			return err
		}
	}
	// TEXT has two AcDbText subclass sections in the R2000+ DXF schema.
	// The second one owns vertical justification (73), whose default is 0.
	if err := write(100, "AcDbText"); err != nil {
		return err
	}
	if aligned {
		return write(73, strconv.Itoa(alignment.vertical))
	}
	return nil
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
