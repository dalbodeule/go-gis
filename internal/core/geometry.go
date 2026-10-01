package core

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// WKTGeometry is the interchange representation used at native driver
// boundaries. Keeping WKT here avoids exposing GDAL or GEOS C handles to the
// application core and makes snapshots safe to clone.
type WKTGeometry struct {
	WKT string
}

// GeometryType returns the WKT type token, for example POINT or POLYGON.
func (g WKTGeometry) GeometryType() string {
	typeName := g.WKT
	if index := strings.IndexAny(typeName, " (\t\r\n"); index >= 0 {
		typeName = typeName[:index]
	}
	if canonical := canonicalWKTType(typeName); canonical != "" {
		return canonical
	}
	return strings.ToUpper(typeName)
}

func canonicalWKTType(typeName string) string {
	switch len(typeName) {
	case 5:
		if asciiEqualFold(typeName, "POINT") {
			return "POINT"
		}
	case 7:
		if asciiEqualFold(typeName, "POLYGON") {
			return "POLYGON"
		}
	case 10:
		if asciiEqualFold(typeName, "LINESTRING") {
			return "LINESTRING"
		}
		if asciiEqualFold(typeName, "MULTIPOINT") {
			return "MULTIPOINT"
		}
	case 13:
		if asciiEqualFold(typeName, "MULTIPOLYGON") {
			return "MULTIPOLYGON"
		}
	case 15:
		if asciiEqualFold(typeName, "MULTILINESTRING") {
			return "MULTILINESTRING"
		}
	case 18:
		if asciiEqualFold(typeName, "GEOMETRYCOLLECTION") {
			return "GEOMETRYCOLLECTION"
		}
	}
	return ""
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

// Clone returns a detached WKT geometry.
func (g WKTGeometry) Clone() Geometry {
	return g
}

// WKBPoint is a lightweight XY coordinate decoded from a WKB geometry.
type WKBPoint struct {
	X float64
	Y float64
}

// WKBGeometry keeps the compact binary geometry returned by GDAL. It avoids
// forcing every imported feature through an intermediate WKT string.
type WKBGeometry struct {
	WKB []byte
}

// Clone returns an independent WKB geometry.
func (g WKBGeometry) Clone() Geometry {
	return WKBGeometry{WKB: append([]byte(nil), g.WKB...)}
}

// GeometryType returns the OGC base geometry name encoded in the WKB header.
func (g WKBGeometry) GeometryType() string {
	if len(g.WKB) < 5 {
		return ""
	}
	var order binary.ByteOrder
	switch g.WKB[0] {
	case 0:
		order = binary.BigEndian
	case 1:
		order = binary.LittleEndian
	default:
		return ""
	}
	return wkbTypeName(order.Uint32(g.WKB[1:5]))
}

// PointCount returns the number of coordinate tuples in a WKB geometry without
// materializing point slices. It validates the full byte layout and supports
// OGC dimensional type codes and EWKB Z/M/SRID flags.
func (g WKBGeometry) PointCount() (int, error) {
	counter := wkbPointCounter{data: g.WKB}
	count, err := counter.geometry(0)
	if err != nil {
		return 0, err
	}
	if counter.offset != len(counter.data) {
		return 0, errors.New("WKB contains trailing data")
	}
	return count, nil
}

type wkbPointCounter struct {
	data     []byte
	offset   int
	elements int
}

func (c *wkbPointCounter) consumeElements(count uint32) error {
	if uint64(count) > uint64(maxWKBDecodeElements-c.elements) {
		return errors.New("WKB geometry exceeds the 1000000-element decode safety limit")
	}
	c.elements += int(count)
	return nil
}

func (c *wkbPointCounter) uint32(order binary.ByteOrder) (uint32, error) {
	if c.offset < 0 || c.offset > len(c.data)-4 {
		return 0, errors.New("WKB is truncated")
	}
	value := order.Uint32(c.data[c.offset : c.offset+4])
	c.offset += 4
	return value, nil
}

func (c *wkbPointCounter) skipCoordinateTuples(count uint32, dimensions int) error {
	if dimensions < 2 || dimensions > 4 {
		return errors.New("WKB has invalid coordinate dimensions")
	}
	bytesPerPoint := dimensions * 8
	if uint64(count) > uint64(len(c.data)-c.offset)/uint64(bytesPerPoint) {
		return errors.New("WKB coordinate count exceeds available bytes")
	}
	if err := c.consumeElements(count); err != nil {
		return err
	}
	c.offset += int(count) * bytesPerPoint
	return nil
}

func (c *wkbPointCounter) geometry(depth int) (int, error) {
	if depth >= 64 {
		return 0, errors.New("WKB geometry collection nesting exceeds 64 levels")
	}
	if c.offset < 0 || c.offset > len(c.data)-5 {
		return 0, errors.New("WKB geometry header is truncated")
	}
	var order binary.ByteOrder
	switch c.data[c.offset] {
	case 0:
		order = binary.BigEndian
	case 1:
		order = binary.LittleEndian
	default:
		return 0, errors.New("WKB has invalid byte order")
	}
	c.offset++
	typeCode, err := c.uint32(order)
	if err != nil {
		return 0, err
	}
	dimensions := 2
	hasSRID := typeCode&0x20000000 != 0
	base := typeCode & 0x0fffffff
	if typeCode&0x80000000 != 0 {
		dimensions++
	}
	if typeCode&0x40000000 != 0 {
		dimensions++
	}
	if typeCode&0xe0000000 == 0 {
		switch {
		case base >= 3000 && base < 4000:
			base -= 3000
			dimensions = 4
		case base >= 2000 && base < 3000:
			base -= 2000
			dimensions = 3
		case base >= 1000 && base < 2000:
			base -= 1000
			dimensions = 3
		}
	}
	if hasSRID {
		if _, err := c.uint32(order); err != nil {
			return 0, err
		}
	}
	if base < 1 || base > 7 {
		return 0, fmt.Errorf("unsupported WKB geometry type %d", base)
	}
	switch base {
	case 1:
		if err := c.skipCoordinateTuples(1, dimensions); err != nil {
			return 0, err
		}
		return 1, nil
	case 2:
		count, err := c.uint32(order)
		if err != nil {
			return 0, err
		}
		if err := c.skipCoordinateTuples(count, dimensions); err != nil {
			return 0, err
		}
		return int(count), nil
	case 3:
		ringCount, err := c.uint32(order)
		if err != nil {
			return 0, err
		}
		if uint64(ringCount) > uint64(len(c.data)-c.offset)/4 {
			return 0, errors.New("WKB ring count exceeds available bytes")
		}
		if err := c.consumeElements(ringCount); err != nil {
			return 0, err
		}
		total := 0
		for ring := uint32(0); ring < ringCount; ring++ {
			count, err := c.uint32(order)
			if err != nil {
				return 0, err
			}
			if uint64(count) > uint64(int(^uint(0)>>1)-total) {
				return 0, errors.New("WKB coordinate count overflows int")
			}
			if err := c.skipCoordinateTuples(count, dimensions); err != nil {
				return 0, err
			}
			total += int(count)
		}
		return total, nil
	default:
		childCount, err := c.uint32(order)
		if err != nil {
			return 0, err
		}
		if uint64(childCount) > uint64(len(c.data)-c.offset)/5 {
			return 0, errors.New("WKB child count exceeds available bytes")
		}
		if err := c.consumeElements(childCount); err != nil {
			return 0, err
		}
		total := 0
		for child := uint32(0); child < childCount; child++ {
			count, err := c.geometry(depth + 1)
			if err != nil {
				return 0, err
			}
			if count > int(^uint(0)>>1)-total {
				return 0, errors.New("WKB coordinate count overflows int")
			}
			total += count
		}
		return total, nil
	}
}

// Parts decodes geometry components without creating a WKT representation.
// Polygon rings remain separate parts so render hit testing does not connect
// disjoint components or holes.
func (g WKBGeometry) Parts() ([][]WKBPoint, error) {
	geometry, err := decodeWKB(g.WKB)
	if err != nil {
		return nil, err
	}
	return geometry.parts(), nil
}

// WKT converts a compact geometry to the legacy interchange representation.
// It is intentionally called only at boundaries that still require WKT.
func (g WKBGeometry) WKT() (string, error) {
	geometry, err := decodeWKB(g.WKB)
	if err != nil {
		return "", err
	}
	return geometry.wkt(), nil
}

// ToWKT normalizes either geometry representation at a legacy driver boundary.
func ToWKT(geometry Geometry) (WKTGeometry, error) {
	switch value := geometry.(type) {
	case WKTGeometry:
		return value, nil
	case WKBGeometry:
		wkt, err := value.WKT()
		if err != nil {
			return WKTGeometry{}, err
		}
		return WKTGeometry{WKT: wkt}, nil
	default:
		return WKTGeometry{}, fmt.Errorf("unsupported geometry type %T", geometry)
	}
}

type decodedWKB struct {
	typeCode uint32
	points   []WKBPoint
	rings    [][]WKBPoint
	children []decodedWKB
}

const maxWKBDecodeElements = 1_000_000

func (g decodedWKB) typeName() string {
	return wkbTypeName(g.typeCode)
}

func wkbTypeName(typeCode uint32) string {
	base := typeCode & 0x0fffffff
	if base >= 1000 {
		base %= 1000
	}
	switch base {
	case 1:
		return "POINT"
	case 2:
		return "LINESTRING"
	case 3:
		return "POLYGON"
	case 4:
		return "MULTIPOINT"
	case 5:
		return "MULTILINESTRING"
	case 6:
		return "MULTIPOLYGON"
	case 7:
		return "GEOMETRYCOLLECTION"
	default:
		return ""
	}
}

func (g decodedWKB) parts() [][]WKBPoint {
	switch g.typeName() {
	case "POINT", "LINESTRING":
		return [][]WKBPoint{g.points}
	case "POLYGON":
		return g.rings
	default:
		parts := make([][]WKBPoint, 0)
		for _, child := range g.children {
			parts = append(parts, child.parts()...)
		}
		return parts
	}
}

func (g decodedWKB) wkt() string {
	name := g.typeName()
	if name == "" {
		return "GEOMETRYCOLLECTION EMPTY"
	}
	if len(g.points) == 0 && len(g.rings) == 0 && len(g.children) == 0 {
		return name + " EMPTY"
	}
	pointText := func(points []WKBPoint) string {
		values := make([]string, len(points))
		for index, point := range points {
			values[index] = strconv.FormatFloat(point.X, 'g', -1, 64) + " " + strconv.FormatFloat(point.Y, 'g', -1, 64)
		}
		return strings.Join(values, ", ")
	}
	switch name {
	case "POINT", "LINESTRING":
		return name + " (" + pointText(g.points) + ")"
	case "POLYGON":
		rings := make([]string, len(g.rings))
		for index, ring := range g.rings {
			rings[index] = "(" + pointText(ring) + ")"
		}
		return name + " (" + strings.Join(rings, ", ") + ")"
	case "GEOMETRYCOLLECTION":
		children := make([]string, len(g.children))
		for index, child := range g.children {
			children[index] = child.wkt()
		}
		return name + " (" + strings.Join(children, ", ") + ")"
	default:
		children := make([]string, len(g.children))
		for index, child := range g.children {
			children[index] = strings.TrimPrefix(child.wkt(), child.typeName()+" ")
		}
		return name + " (" + strings.Join(children, ", ") + ")"
	}
}

func decodeWKB(data []byte) (decodedWKB, error) {
	reader := &wkbReader{data: data}
	geometry, err := reader.geometry(0)
	if err != nil {
		return decodedWKB{}, err
	}
	if reader.offset != len(data) {
		return decodedWKB{}, errors.New("WKB has trailing bytes")
	}
	return geometry, nil
}

// MapWKBXY copies a WKB geometry and maps every XY coordinate in the copy.
// Z/M ordinates and EWKB SRID metadata are preserved byte-for-byte. It is
// intended for native coordinate transforms that should avoid a WKB->WKT
// conversion and subsequent text parsing.
func MapWKBXY(data []byte, mapXY func(x, y float64) (float64, float64, error)) ([]byte, error) {
	if mapXY == nil {
		return nil, errors.New("WKB coordinate mapper is nil")
	}
	result := append([]byte(nil), data...)
	mapper := &wkbMapper{data: result}
	if err := mapper.geometry(mapXY, 0); err != nil {
		return nil, err
	}
	if mapper.offset != len(result) {
		return nil, errors.New("WKB has trailing bytes")
	}
	return result, nil
}

// MoveWKBVertex returns a copy of a WKB geometry with the zero-based XY
// vertex at vertexIndex moved to x,y. Empty (NaN) points are not counted.
// Additional ordinates and EWKB metadata are preserved by MapWKBXY.
func MoveWKBVertex(data []byte, vertexIndex int, x, y float64) ([]byte, error) {
	if vertexIndex < 0 {
		return nil, errors.New("vertex index must not be negative")
	}
	if math.IsNaN(x) || math.IsNaN(y) || math.IsInf(x, 0) || math.IsInf(y, 0) {
		return nil, errors.New("vertex coordinates must be finite")
	}
	result := append([]byte(nil), data...)
	move := &wkbVertexMove{index: vertexIndex, x: x, y: y}
	mapper := &wkbMapper{data: result, vertexMove: move}
	if err := mapper.geometry(nil, 0); err != nil {
		return nil, err
	}
	if mapper.offset != len(result) {
		return nil, errors.New("WKB has trailing bytes")
	}
	if !move.found {
		return nil, fmt.Errorf("vertex index %d is out of range", vertexIndex)
	}
	return result, nil
}

type wkbVertexCoordinate struct {
	xOffset int
	yOffset int
	x       float64
	y       float64
}

type wkbVertexMove struct {
	index  int
	x      float64
	y      float64
	found  bool
	coords []wkbVertexCoordinate
}

type wkbMapper struct {
	data       []byte
	offset     int
	order      binary.ByteOrder
	vertexMove *wkbVertexMove
}

func (m *wkbMapper) geometry(mapXY func(float64, float64) (float64, float64, error), depth int) error {
	if depth >= 64 {
		return errors.New("WKB geometry collection nesting exceeds 64 levels")
	}
	if m.offset >= len(m.data) {
		return errors.New("WKB is truncated")
	}
	byteOrder := m.data[m.offset]
	m.offset++
	switch byteOrder {
	case 0:
		m.order = binary.BigEndian
	case 1:
		m.order = binary.LittleEndian
	default:
		return errors.New("WKB has invalid byte order")
	}
	typeCode, err := m.uint32()
	if err != nil {
		return err
	}
	hasZ := typeCode&0x80000000 != 0 || typeCode >= 1000 && typeCode < 2000
	hasM := typeCode&0x40000000 != 0 || typeCode >= 2000 && typeCode < 3000
	if typeCode&0x20000000 != 0 {
		if _, err := m.uint32(); err != nil {
			return err
		}
	}
	base := typeCode & 0x0fffffff
	if base >= 1000 {
		base %= 1000
	}
	readPoint := func() error {
		xOffset, x, err := m.float64()
		if err != nil {
			return err
		}
		yOffset, y, err := m.float64()
		if err != nil {
			return err
		}
		for extra := 0; extra < boolInt(hasZ)+boolInt(hasM); extra++ {
			if _, _, err := m.float64(); err != nil {
				return err
			}
		}
		if math.IsNaN(x) || math.IsNaN(y) {
			return nil
		}
		mappedX, mappedY := x, y
		if m.vertexMove != nil {
			pointIndex := len(m.vertexMove.coords)
			m.vertexMove.coords = append(m.vertexMove.coords, wkbVertexCoordinate{xOffset: xOffset, yOffset: yOffset, x: x, y: y})
			if pointIndex == m.vertexMove.index {
				mappedX, mappedY = m.vertexMove.x, m.vertexMove.y
				m.vertexMove.found = true
			}
		} else {
			mappedX, mappedY, err = mapXY(x, y)
			if err != nil {
				return err
			}
		}
		m.order.PutUint64(m.data[xOffset:xOffset+8], math.Float64bits(mappedX))
		m.order.PutUint64(m.data[yOffset:yOffset+8], math.Float64bits(mappedY))
		return nil
	}
	readPoints := func() error {
		count, err := m.uint32()
		if err != nil {
			return err
		}
		for index := uint32(0); index < count; index++ {
			if err := readPoint(); err != nil {
				return err
			}
		}
		return nil
	}
	switch base {
	case 1:
		return readPoint()
	case 2:
		return readPoints()
	case 3:
		ringCount, err := m.uint32()
		if err != nil {
			return err
		}
		for index := uint32(0); index < ringCount; index++ {
			ringStart := 0
			if m.vertexMove != nil {
				ringStart = len(m.vertexMove.coords)
			}
			if err := readPoints(); err != nil {
				return err
			}
			if m.vertexMove != nil {
				ringEnd := len(m.vertexMove.coords)
				if ringEnd-ringStart > 1 {
					first, last := m.vertexMove.coords[ringStart], m.vertexMove.coords[ringEnd-1]
					if first.x == last.x && first.y == last.y &&
						(m.vertexMove.index == ringStart || m.vertexMove.index == ringEnd-1) {
						for _, point := range []wkbVertexCoordinate{first, last} {
							m.order.PutUint64(m.data[point.xOffset:point.xOffset+8], math.Float64bits(m.vertexMove.x))
							m.order.PutUint64(m.data[point.yOffset:point.yOffset+8], math.Float64bits(m.vertexMove.y))
						}
					}
				}
			}
		}
		return nil
	case 4, 5, 6, 7:
		count, err := m.uint32()
		if err != nil {
			return err
		}
		for index := uint32(0); index < count; index++ {
			if err := m.geometry(mapXY, depth+1); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("unsupported WKB geometry type %d", base)
	}
}

func (m *wkbMapper) uint32() (uint32, error) {
	if m.offset+4 > len(m.data) {
		return 0, errors.New("WKB is truncated")
	}
	value := m.order.Uint32(m.data[m.offset : m.offset+4])
	m.offset += 4
	return value, nil
}

func (m *wkbMapper) float64() (int, float64, error) {
	start := m.offset
	if m.offset+8 > len(m.data) {
		return 0, 0, errors.New("WKB is truncated")
	}
	value := math.Float64frombits(m.order.Uint64(m.data[m.offset : m.offset+8]))
	m.offset += 8
	return start, value, nil
}

type wkbReader struct {
	data     []byte
	offset   int
	order    binary.ByteOrder
	elements int
}

func (r *wkbReader) consumeElements(count uint32) error {
	if uint64(count) > uint64(maxWKBDecodeElements-r.elements) {
		return errors.New("WKB geometry exceeds the 1000000-element decode safety limit")
	}
	r.elements += int(count)
	return nil
}

func (r *wkbReader) geometry(depth int) (decodedWKB, error) {
	if depth >= 64 {
		return decodedWKB{}, errors.New("WKB geometry collection nesting exceeds 64 levels")
	}
	if r.offset >= len(r.data) {
		return decodedWKB{}, errors.New("WKB is truncated")
	}
	byteOrder := r.data[r.offset]
	r.offset++
	switch byteOrder {
	case 0:
		r.order = binary.BigEndian
	case 1:
		r.order = binary.LittleEndian
	default:
		return decodedWKB{}, errors.New("WKB has invalid byte order")
	}
	typeCode, err := r.uint32()
	if err != nil {
		return decodedWKB{}, err
	}
	hasZ := typeCode&0x80000000 != 0 || typeCode >= 1000 && typeCode < 2000
	hasM := typeCode&0x40000000 != 0 || typeCode >= 2000 && typeCode < 3000
	if typeCode&0x20000000 != 0 {
		if _, err := r.uint32(); err != nil {
			return decodedWKB{}, err
		}
	}
	base := typeCode & 0x0fffffff
	if base >= 1000 {
		base %= 1000
	}
	geometry := decodedWKB{typeCode: base}
	readPoint := func() (WKBPoint, error) {
		x, err := r.float64()
		if err != nil {
			return WKBPoint{}, err
		}
		y, err := r.float64()
		if err != nil {
			return WKBPoint{}, err
		}
		for extra := 0; extra < boolInt(hasZ)+boolInt(hasM); extra++ {
			if _, err := r.float64(); err != nil {
				return WKBPoint{}, err
			}
		}
		return WKBPoint{X: x, Y: y}, nil
	}
	pointBytes := (2 + boolInt(hasZ) + boolInt(hasM)) * 8
	switch base {
	case 1:
		if err := r.consumeElements(1); err != nil {
			return decodedWKB{}, err
		}
		point, err := readPoint()
		if err != nil {
			return decodedWKB{}, err
		}
		if math.IsNaN(point.X) || math.IsNaN(point.Y) {
			return geometry, nil
		}
		geometry.points = []WKBPoint{point}
	case 2:
		points, err := r.points(readPoint, pointBytes)
		if err != nil {
			return decodedWKB{}, err
		}
		geometry.points = points
	case 3:
		ringCount, err := r.uint32()
		if err != nil {
			return decodedWKB{}, err
		}
		if uint64(ringCount) > uint64(len(r.data)-r.offset)/4 {
			return decodedWKB{}, errors.New("WKB ring count exceeds available bytes")
		}
		if err := r.consumeElements(ringCount); err != nil {
			return decodedWKB{}, err
		}
		geometry.rings = make([][]WKBPoint, int(ringCount))
		for index := range geometry.rings {
			points, err := r.points(readPoint, pointBytes)
			if err != nil {
				return decodedWKB{}, err
			}
			geometry.rings[index] = points
		}
	case 4, 5, 6, 7:
		count, err := r.uint32()
		if err != nil {
			return decodedWKB{}, err
		}
		if uint64(count) > uint64(len(r.data)-r.offset)/5 {
			return decodedWKB{}, errors.New("WKB child count exceeds available bytes")
		}
		if err := r.consumeElements(count); err != nil {
			return decodedWKB{}, err
		}
		geometry.children = make([]decodedWKB, int(count))
		for index := range geometry.children {
			child, err := r.geometry(depth + 1)
			if err != nil {
				return decodedWKB{}, err
			}
			geometry.children[index] = child
		}
	default:
		return decodedWKB{}, fmt.Errorf("unsupported WKB geometry type %d", base)
	}
	return geometry, nil
}

func (r *wkbReader) points(readPoint func() (WKBPoint, error), pointBytes int) ([]WKBPoint, error) {
	count, err := r.uint32()
	if err != nil {
		return nil, err
	}
	if pointBytes < 16 || uint64(count) > uint64(len(r.data)-r.offset)/uint64(pointBytes) {
		return nil, errors.New("WKB coordinate count exceeds available bytes")
	}
	if err := r.consumeElements(count); err != nil {
		return nil, err
	}
	points := make([]WKBPoint, int(count))
	for index := range points {
		points[index], err = readPoint()
		if err != nil {
			return nil, err
		}
	}
	return points, nil
}

func (r *wkbReader) uint32() (uint32, error) {
	if r.offset+4 > len(r.data) {
		return 0, errors.New("WKB is truncated")
	}
	value := r.order.Uint32(r.data[r.offset : r.offset+4])
	r.offset += 4
	return value, nil
}

func (r *wkbReader) float64() (float64, error) {
	if r.offset+8 > len(r.data) {
		return 0, errors.New("WKB is truncated")
	}
	value := math.Float64frombits(r.order.Uint64(r.data[r.offset : r.offset+8]))
	r.offset += 8
	return value, nil
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
