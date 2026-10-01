//go:build native

package gdal

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gogis/internal/core"

	"github.com/airbusgeo/godal"
)

// isGeoJSONCollection limits the streaming path to the standard FeatureCollection
// form. Other JSON vector variants retain GDAL's format detection behavior.
func isGeoJSONCollection(path string) bool {
	if !strings.EqualFold(filepath.Ext(path), ".geojson") && !strings.EqualFold(filepath.Ext(path), ".json") {
		return false
	}
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()
	reader := bufio.NewReaderSize(file, 32<<10)
	decoder := json.NewDecoder(reader)
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return false
	}
	memberNeedsComma := false
	for {
		name, nextReader, closed, _, err := decodeBoundedJSONMember(decoder, reader, maxGeoJSONMetadataBytes, memberNeedsComma)
		if err != nil {
			return true // route malformed/oversized object metadata through the bounded validator
		}
		if closed {
			return false
		}
		reader = nextReader
		decoder = json.NewDecoder(reader)
		if name == "features" {
			return true
		}
		value, reader, _, err := decodeBoundedJSONRaw(decoder, reader, maxGeoJSONMetadataBytes)
		if err != nil {
			return true
		}
		decoder = json.NewDecoder(reader)
		memberNeedsComma = true
		if name == "type" {
			var kind string
			if json.Unmarshal(value, &kind) == nil && kind == "FeatureCollection" {
				return true
			}
			return false
		}
	}
}

type streamedGeoJSONFeature struct {
	Type          string          `json:"type"`
	Geometry      json.RawMessage `json:"geometry"`
	Properties    json.RawMessage `json:"properties"`
	sourceOffset  int64
	sourceLength  uint32
	featureBounds [4]float64
	hasBounds     bool
}

type geoJSONFeatureIndex struct {
	offset int64
	bounds [4]float64
	length uint32
	valid  bool
}

type geoJSONTailBlock struct {
	startOffset  int64
	endOffset    int64
	firstOrdinal int
	featureCount int
	bounds       [4]float64
	hasBounds    bool
}

const geoJSONTailBlockFeatureCount = 4096
const maxGeoJSONTailIndexBlocks = 32_768

type geoJSONTailIndexBuilder struct {
	blocks  []geoJSONTailBlock
	current geoJSONTailBlock
}

func (builder *geoJSONTailIndexBuilder) add(ordinal int, feature *streamedGeoJSONFeature) {
	if builder.current.featureCount == 0 {
		builder.current.startOffset = feature.sourceOffset
		builder.current.firstOrdinal = ordinal
	}
	builder.current.endOffset = feature.sourceOffset + int64(feature.sourceLength)
	builder.current.featureCount++
	if feature.hasBounds {
		builder.current.bounds, builder.current.hasBounds = mergeGeoJSONBounds(
			builder.current.bounds, builder.current.hasBounds, feature.featureBounds)
	}
	if builder.current.featureCount >= geoJSONTailBlockFeatureCount {
		builder.flush()
	}
}

func (builder *geoJSONTailIndexBuilder) flush() {
	if builder.current.featureCount == 0 {
		return
	}
	builder.blocks = append(builder.blocks, builder.current)
	builder.current = geoJSONTailBlock{}
	if len(builder.blocks) <= maxGeoJSONTailIndexBlocks {
		return
	}
	merged := make([]geoJSONTailBlock, 0, (len(builder.blocks)+1)/2)
	for index := 0; index < len(builder.blocks); index += 2 {
		block := builder.blocks[index]
		if index+1 < len(builder.blocks) {
			next := builder.blocks[index+1]
			block.endOffset = next.endOffset
			block.featureCount += next.featureCount
			block.bounds, block.hasBounds = mergeGeoJSONBounds(block.bounds, block.hasBounds, next.bounds)
		}
		merged = append(merged, block)
	}
	builder.blocks = merged
}

const maxGeoJSONInMemoryIndexFeatures = 1_000_000
const maxGeoJSONGeometryCoordinates = 1_000_000
const maxGeoJSONGeometryCollectionDepth = 64
const maxGeoJSONGeometryCollectionMembers = 100_000
const maxGeoJSONGeometrySkipDepth = maxGeoJSONGeometryCollectionDepth*2 + 16
const maxGeoJSONPositionOrdinates = 16
const maxGeoJSONPropertiesBytes = 8 << 20
const maxGeoJSONPropertyNodes = 100_000
const maxGeoJSONPropertyDepth = 128
const geoJSONPropertyPreflightThreshold = 64 << 10
const maxGeoJSONMetadataBytes = 1 << 20

var errGeoJSONSequenceScanStopped = errors.New("stop GeoJSONSeq scan")

func decodeBoundedJSONMember(decoder *json.Decoder, source io.Reader, maxBytes int, needsComma bool) (string, *bufio.Reader, bool, int64, error) {
	reader := bufio.NewReader(io.MultiReader(decoder.Buffered(), source))
	var consumed int64
	readByte := func() (byte, error) {
		value, err := reader.ReadByte()
		if err == nil {
			consumed++
		}
		return value, err
	}
	readNonSpace := func() (byte, error) {
		for {
			value, err := readByte()
			if err != nil || (value != ' ' && value != '\t' && value != '\r' && value != '\n') {
				return value, err
			}
		}
	}
	separator, err := readNonSpace()
	if err != nil {
		return "", nil, false, consumed, err
	}
	if separator == '}' {
		return "", reader, true, consumed, nil
	}
	if needsComma {
		if separator != ',' {
			return "", nil, false, consumed, fmt.Errorf("expected comma between GeoJSON members")
		}
		separator, err = readNonSpace()
		if err != nil {
			return "", nil, false, consumed, err
		}
	} else if separator == ',' {
		return "", nil, false, consumed, fmt.Errorf("unexpected comma before first GeoJSON member")
	}
	if separator != '"' {
		return "", nil, false, consumed, fmt.Errorf("expected GeoJSON member key, got %q", separator)
	}
	keyBytes := []byte{'"'}
	inEscape := false
	for {
		value, err := readByte()
		if err != nil {
			return "", nil, false, consumed, fmt.Errorf("read GeoJSON member key: %w", err)
		}
		keyBytes = append(keyBytes, value)
		if len(keyBytes) > maxBytes {
			return "", nil, false, consumed, fmt.Errorf("JSON member key exceeds the %d byte limit", maxBytes)
		}
		if inEscape {
			inEscape = false
		} else if value == '\\' {
			inEscape = true
		} else if value == '"' {
			break
		}
	}
	var key string
	if err := json.Unmarshal(keyBytes, &key); err != nil {
		return "", nil, false, consumed, fmt.Errorf("decode GeoJSON member key: %w", err)
	}
	for {
		value, err := readByte()
		if err != nil {
			return "", nil, false, consumed, err
		}
		if value == ' ' || value == '\t' || value == '\r' || value == '\n' {
			continue
		}
		if value != ':' {
			return "", nil, false, consumed, fmt.Errorf("expected colon after GeoJSON member key")
		}
		return key, reader, false, consumed, nil
	}
}

func decodeBoundedJSONRaw(decoder *json.Decoder, source io.Reader, maxBytes int) (json.RawMessage, *bufio.Reader, int64, error) {
	limited := &io.LimitedReader{R: io.MultiReader(decoder.Buffered(), source), N: int64(maxBytes) + 1}
	bounded := json.NewDecoder(limited)
	var value json.RawMessage
	if err := bounded.Decode(&value); err != nil {
		if limited.N == 0 {
			return nil, nil, 0, fmt.Errorf("JSON member value exceeds the %d byte limit", maxBytes)
		}
		return nil, nil, 0, err
	}
	if len(value) > maxBytes {
		return nil, nil, 0, fmt.Errorf("JSON member value exceeds the %d byte limit", maxBytes)
	}
	continuation := bufio.NewReader(io.MultiReader(bounded.Buffered(), limited.R))
	return value, continuation, bounded.InputOffset(), nil
}

type geoJSONFileStamp struct {
	size      int64
	modTimeNS int64
	file      os.FileInfo
}

func geoJSONSourceStamp(path string) (geoJSONFileStamp, error) {
	info, err := os.Stat(path)
	if err != nil {
		return geoJSONFileStamp{}, err
	}
	return geoJSONFileStamp{size: info.Size(), modTimeNS: info.ModTime().UnixNano(), file: info}, nil
}

func sameGeoJSONFileStamp(first, second geoJSONFileStamp) bool {
	if first.size != second.size || first.modTimeNS != second.modTimeNS {
		return false
	}
	if first.file == nil || second.file == nil {
		return first.file == nil && second.file == nil
	}
	return os.SameFile(first.file, second.file)
}

func validateGeoJSONSourceStamp(path string, expected geoJSONFileStamp) error {
	actual, err := geoJSONSourceStamp(path)
	if err != nil {
		return fmt.Errorf("stat indexed GeoJSON %q: %w", path, err)
	}
	if !sameGeoJSONFileStamp(actual, expected) {
		return fmt.Errorf("GeoJSON source %q changed after indexing; reload the layer", path)
	}
	return nil
}

type geoJSONCollectionInfo struct {
	Name  string
	CRS   string
	Count int
}

func geoJSONLayerName(path, name string) string {
	if name != "" {
		return name
	}
	return strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
}

func isGeoJSONSequence(path string) bool {
	extension := strings.ToLower(filepath.Ext(path))
	return extension == ".geojsonl" || extension == ".geojsons"
}

func isGeoJSONStreamSource(path string) bool {
	return isGeoJSONCollection(path) || isGeoJSONSequence(path)
}

// readJSONFeatureObject copies one top-level feature object into a bounded
// buffer. Unlike json.Decoder.Decode(&RawMessage), it rejects an oversized
// object before allocating memory proportional to its full size.
func readJSONFeatureObject(reader *bufio.Reader, featureBuffer *[]byte, streamOffset *int64, maxBytes int) ([]byte, bool, int64, int64, error) {
	buffer := (*featureBuffer)[:0]
	for {
		value, err := reader.ReadByte()
		if err != nil {
			return nil, false, 0, 0, err
		}
		*streamOffset = *streamOffset + 1
		switch value {
		case ' ', '\t', '\r', '\n', ',':
			continue
		case ']':
			*featureBuffer = buffer
			return nil, true, 0, 0, nil
		case '{':
			startOffset := *streamOffset - 1
			buffer = append(buffer, value)
			depth := 1
			inString, escaped := false, false
			for depth > 0 {
				value, err = reader.ReadByte()
				if err != nil {
					return nil, false, 0, 0, fmt.Errorf("unterminated GeoJSON feature: %w", err)
				}
				*streamOffset = *streamOffset + 1
				if len(buffer) >= maxBytes {
					return nil, false, 0, 0, fmt.Errorf("GeoJSON feature exceeds the %d MiB feature limit", maxBytes>>20)
				}
				buffer = append(buffer, value)
				if inString {
					if escaped {
						escaped = false
					} else if value == '\\' {
						escaped = true
					} else if value == '"' {
						inString = false
					}
					continue
				}
				switch value {
				case '"':
					inString = true
				case '{', '[':
					depth++
				case '}', ']':
					depth--
				}
			}
			*featureBuffer = buffer
			return buffer, false, startOffset, *streamOffset, nil
		default:
			return nil, false, 0, 0, fmt.Errorf("expected GeoJSON feature object or end of array, got %q", value)
		}
	}
}

func resumeGeoJSONCollectionDecoder(reader *bufio.Reader) (*json.Decoder, *bufio.Reader, bool, int64, error) {
	var consumed int64
	for {
		value, err := reader.ReadByte()
		if err != nil {
			return nil, reader, false, consumed, fmt.Errorf("finish GeoJSON features array: %w", err)
		}
		consumed++
		if value == ' ' || value == '\t' || value == '\r' || value == '\n' {
			continue
		}
		switch value {
		case ',':
			decoder := json.NewDecoder(io.MultiReader(strings.NewReader("{"), reader))
			if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
				return nil, reader, false, consumed, fmt.Errorf("resume GeoJSON FeatureCollection: %v", err)
			}
			return decoder, reader, false, consumed, nil
		case '}':
			return nil, reader, true, consumed, nil
		default:
			return nil, reader, false, consumed, fmt.Errorf("unexpected byte %q after GeoJSON features array", value)
		}
	}
}

func scanGeoJSONCollection(ctx context.Context, path string, visit func(int, *streamedGeoJSONFeature) error) (geoJSONCollectionInfo, [4]float64, bool, error) {
	return scanGeoJSONCollectionWithFeatureLimit(ctx, path, maxGeoJSONFeatureMiB<<20, visit)
}

func readGeoJSONSequenceRecord(reader *bufio.Reader, buffer *[]byte, maxBytes int) ([]byte, int, bool, error) {
	line := (*buffer)[:0]
	var bytesRead int
	for {
		fragment, err := reader.ReadSlice('\n')
		bytesRead += len(fragment)
		if len(fragment) > maxBytes+2-len(line) {
			return nil, bytesRead, false, fmt.Errorf("GeoJSONSeq record exceeds the %d MiB feature limit", maxBytes>>20)
		}
		line = append(line, fragment...)
		if err == bufio.ErrBufferFull {
			continue
		}
		if err == io.EOF {
			if len(line) == 0 {
				*buffer = line
				return nil, bytesRead, true, nil
			}
			*buffer = line
			return line, bytesRead, false, nil
		}
		if err != nil {
			return nil, bytesRead, false, fmt.Errorf("read GeoJSONSeq record: %w", err)
		}
		*buffer = line
		return line, bytesRead, false, nil
	}
}

// scanGeoJSONSequence reads newline-delimited or RFC 8142 record-separator
// GeoJSON one bounded record at a time. It never asks GDAL to materialize the
// sequence as an in-memory layer.
func scanGeoJSONSequence(ctx context.Context, path string, visit func(int, *streamedGeoJSONFeature) error) (geoJSONCollectionInfo, [4]float64, bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return geoJSONCollectionInfo{}, [4]float64{}, false, fmt.Errorf("open GeoJSONSeq %q: %w", path, err)
	}
	defer file.Close()
	reader := bufio.NewReaderSize(file, 64<<10)
	info := geoJSONCollectionInfo{}
	info.Name = geoJSONLayerName(path, "")
	info.CRS = "EPSG:4326"
	var bounds [4]float64
	hasBounds := false
	var recordBuffer []byte
	var streamOffset int64
	var feature streamedGeoJSONFeature
	for {
		if err := ctx.Err(); err != nil {
			return info, bounds, hasBounds, err
		}
		lineStart := streamOffset
		raw, bytesRead, eof, err := readGeoJSONSequenceRecord(reader, &recordBuffer, maxGeoJSONFeatureMiB<<20)
		streamOffset += int64(bytesRead)
		if err != nil {
			return info, bounds, hasBounds, fmt.Errorf("read GeoJSONSeq record %d: %w", info.Count+1, err)
		}
		if eof {
			break
		}
		start, end := 0, len(raw)
		for start < end && (raw[start] == ' ' || raw[start] == '\t' || raw[start] == '\r' || raw[start] == '\n') {
			start++
		}
		if start < end && raw[start] == 0x1e {
			start++
		}
		for start < end && (raw[start] == ' ' || raw[start] == '\t' || raw[start] == '\r' || raw[start] == '\n') {
			start++
		}
		for end > start && (raw[end-1] == ' ' || raw[end-1] == '\t' || raw[end-1] == '\r' || raw[end-1] == '\n') {
			end--
		}
		if start == end {
			continue
		}
		object := raw[start:end]
		if len(object) > maxGeoJSONFeatureMiB<<20 {
			return info, bounds, hasBounds, fmt.Errorf("GeoJSONSeq record %d exceeds the %d MiB feature limit", info.Count+1, maxGeoJSONFeatureMiB)
		}
		if object[0] != '{' {
			return info, bounds, hasBounds, fmt.Errorf("GeoJSONSeq record %d must be a Feature object", info.Count+1)
		}
		feature = streamedGeoJSONFeature{}
		if err := json.Unmarshal(object, &feature); err != nil {
			return info, bounds, hasBounds, fmt.Errorf("decode GeoJSONSeq record %d: %w", info.Count+1, err)
		}
		if feature.Type != "Feature" {
			return info, bounds, hasBounds, fmt.Errorf("GeoJSONSeq record %d has type %q, want Feature", info.Count+1, feature.Type)
		}
		if info.Count == int(^uint(0)>>1) {
			return info, bounds, hasBounds, fmt.Errorf("GeoJSONSeq %q has too many features for this platform", path)
		}
		info.Count++
		feature.sourceOffset = lineStart + int64(start)
		feature.sourceLength = uint32(len(object))
		feature.featureBounds = [4]float64{}
		feature.hasBounds = false
		if len(feature.Geometry) > 0 && !bytes.Equal(bytes.TrimSpace(feature.Geometry), []byte("null")) {
			featureBounds, hasCoordinates, err := geoJSONGeometryBounds(feature.Geometry)
			if err != nil {
				return info, bounds, hasBounds, fmt.Errorf("read GeoJSONSeq record %d bounds: %w", info.Count, err)
			}
			if hasCoordinates {
				bounds, hasBounds = mergeGeoJSONBounds(bounds, hasBounds, featureBounds)
				feature.featureBounds = featureBounds
				feature.hasBounds = true
			}
		}
		if visit != nil {
			if err := visit(info.Count, &feature); err != nil {
				return info, bounds, hasBounds, err
			}
		}
	}
	return info, bounds, hasBounds, nil
}

func readGeoJSONSequenceWindow(ctx context.Context, path, layerName string, bounds [4]float64, includeProperties bool, maxFeatures int, maxBytes int64, stopAfter int) (core.Layer, error) {
	if err := validateSpatialWindow(bounds); err != nil {
		return core.Layer{}, err
	}
	result := core.Layer{Editable: true}
	var payloadBytes int64
	info, _, _, err := scanGeoJSONSequence(ctx, path, func(ordinal int, feature *streamedGeoJSONFeature) error {
		if !feature.hasBounds || feature.featureBounds[2] < bounds[0] || feature.featureBounds[0] > bounds[2] ||
			feature.featureBounds[3] < bounds[1] || feature.featureBounds[1] > bounds[3] {
			return nil
		}
		if maxFeatures > 0 && len(result.Features) >= maxFeatures {
			return fmt.Errorf("spatial window exceeds the limit of %d features", maxFeatures)
		}
		loaded, err := streamedGeoJSONCoreFeature(ordinal, feature, includeProperties)
		if err != nil {
			return err
		}
		if includeProperties && len(result.Fields) == 0 {
			result.Fields = geoJSONFieldSchema(loaded.Properties)
		}
		featureBytes := estimateFeaturePayloadBytes(loaded)
		if maxBytes > 0 && featureBytes > maxBytes-payloadBytes {
			return fmt.Errorf("spatial window exceeds the limit of %d bytes", maxBytes)
		}
		payloadBytes += featureBytes
		result.Features = append(result.Features, loaded)
		if stopAfter > 0 && len(result.Features) >= stopAfter {
			return errGeoJSONSequenceScanStopped
		}
		return nil
	})
	if err != nil && !errors.Is(err, errGeoJSONSequenceScanStopped) {
		return core.Layer{}, err
	}
	if layerName != "" && layerName != info.Name {
		return core.Layer{}, fmt.Errorf("layer %q not found in %q", layerName, path)
	}
	result.Name = info.Name
	result.CRS = core.CRS{AuthorityCode: info.CRS}
	return result, nil
}

func readGeoJSONSequenceSnapshot(ctx context.Context, path, layerName string, includeProperties bool) (core.Layer, error) {
	result := core.Layer{Editable: true, Features: make([]core.Feature, 0, maxInitialFeatureCapacity)}
	var payloadBytes int64
	info, _, _, err := scanGeoJSONSequence(ctx, path, func(ordinal int, feature *streamedGeoJSONFeature) error {
		if len(result.Features) >= maxMaterializedSnapshotFeatures {
			return fmt.Errorf("feature limit of %d features exceeded", maxMaterializedSnapshotFeatures)
		}
		loaded, err := streamedGeoJSONCoreFeature(ordinal, feature, includeProperties)
		if err != nil {
			return err
		}
		if includeProperties && len(result.Fields) == 0 {
			result.Fields = geoJSONFieldSchema(loaded.Properties)
		}
		featureBytes := estimateFeaturePayloadBytes(loaded)
		if featureBytes > maxMaterializedSnapshotBytes-payloadBytes {
			return fmt.Errorf("feature payload limit of %d bytes exceeded", maxMaterializedSnapshotBytes)
		}
		payloadBytes += featureBytes
		result.Features = append(result.Features, loaded)
		return nil
	})
	if err != nil {
		return core.Layer{}, err
	}
	if layerName != "" && layerName != info.Name {
		return core.Layer{}, fmt.Errorf("layer %q not found in %q", layerName, path)
	}
	result.Name, result.CRS = info.Name, core.CRS{AuthorityCode: info.CRS}
	return result, nil
}

func readGeoJSONSequenceAttributePage(ctx context.Context, path, layerName string, offset, limit int) (core.Layer, int, error) {
	if err := validateAttributePage(offset, limit); err != nil {
		return core.Layer{}, 0, err
	}
	result := core.Layer{Editable: true, Features: make([]core.Feature, 0, limit)}
	var payloadBytes int64
	info, _, _, err := scanGeoJSONSequence(ctx, path, func(ordinal int, feature *streamedGeoJSONFeature) error {
		if ordinal <= offset || len(result.Features) >= limit {
			return nil
		}
		properties, err := decodeGeoJSONProperties(feature.Properties, ordinal)
		if err != nil {
			return err
		}
		if len(properties) > 0 && len(result.Fields) == 0 {
			result.Fields = geoJSONFieldSchema(properties)
		}
		return appendAttributePageFeature(&result, core.Feature{ID: uint64(ordinal), Properties: properties}, &payloadBytes)
	})
	if err != nil {
		return core.Layer{}, 0, err
	}
	if layerName != "" && layerName != info.Name {
		return core.Layer{}, 0, fmt.Errorf("layer %q not found in %q", layerName, path)
	}
	result.Name, result.CRS = info.Name, core.CRS{AuthorityCode: info.CRS}
	return result, info.Count, nil
}

func openGeoJSONSequenceFeature(ctx context.Context, path, layerName string, featureID uint64) (core.Feature, error) {
	if featureID == 0 {
		return core.Feature{}, fmt.Errorf("feature ID must be positive")
	}
	var result core.Feature
	info, _, _, err := scanGeoJSONSequence(ctx, path, func(ordinal int, feature *streamedGeoJSONFeature) error {
		if uint64(ordinal) != featureID {
			return nil
		}
		loaded, err := streamedGeoJSONCoreFeature(ordinal, feature, true)
		if err != nil {
			return err
		}
		result = loaded
		return errGeoJSONSequenceScanStopped
	})
	if err != nil && !errors.Is(err, errGeoJSONSequenceScanStopped) {
		return core.Feature{}, err
	}
	if layerName != "" && layerName != info.Name {
		return core.Feature{}, fmt.Errorf("layer %q not found in %q", layerName, path)
	}
	if result.ID == 0 {
		return core.Feature{}, fmt.Errorf("feature %d not found in layer %q", featureID, info.Name)
	}
	return result, nil
}

func readGeoJSONSourceSnapshot(ctx context.Context, path, layerName string, includeProperties bool) (core.Layer, error) {
	if isGeoJSONSequence(path) {
		return readGeoJSONSequenceSnapshot(ctx, path, layerName, includeProperties)
	}
	return readGeoJSONSnapshot(ctx, path, layerName, includeProperties)
}

func readGeoJSONSourceWindow(ctx context.Context, path, layerName string, bounds [4]float64, includeProperties bool, maxFeatures int, maxBytes int64) (core.Layer, error) {
	if isGeoJSONSequence(path) {
		return readGeoJSONSequenceWindow(ctx, path, layerName, bounds, includeProperties, maxFeatures, maxBytes, 0)
	}
	return readGeoJSONWindow(ctx, path, layerName, bounds, includeProperties, maxFeatures, maxBytes)
}

func readGeoJSONSourceWindowPrefix(ctx context.Context, path, layerName string, bounds [4]float64, includeProperties bool, maxFeatures int, maxBytes int64, stopAfter int) (core.Layer, error) {
	if isGeoJSONSequence(path) {
		return readGeoJSONSequenceWindow(ctx, path, layerName, bounds, includeProperties, maxFeatures, maxBytes, stopAfter)
	}
	return readGeoJSONWindowPrefix(ctx, path, layerName, bounds, includeProperties, maxFeatures, maxBytes, stopAfter)
}

func readGeoJSONSourceAttributePage(ctx context.Context, path, layerName string, offset, limit int) (core.Layer, int, error) {
	if isGeoJSONSequence(path) {
		return readGeoJSONSequenceAttributePage(ctx, path, layerName, offset, limit)
	}
	return openGeoJSONAttributePage(ctx, path, layerName, offset, limit)
}

func openGeoJSONSourceFeature(ctx context.Context, path, layerName string, featureID uint64) (core.Feature, error) {
	if isGeoJSONSequence(path) {
		return openGeoJSONSequenceFeature(ctx, path, layerName, featureID)
	}
	return openGeoJSONFeature(ctx, path, layerName, featureID)
}

func scanGeoJSONCollectionWithFeatureLimit(ctx context.Context, path string, maxFeatureBytes int, visit func(int, *streamedGeoJSONFeature) error) (geoJSONCollectionInfo, [4]float64, bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return geoJSONCollectionInfo{}, [4]float64{}, false, fmt.Errorf("open GeoJSON %q: %w", path, err)
	}
	defer file.Close()
	reader := bufio.NewReaderSize(file, 1<<20)
	decoder := json.NewDecoder(reader)
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		if err == nil {
			err = fmt.Errorf("expected object")
		}
		return geoJSONCollectionInfo{}, [4]float64{}, false, fmt.Errorf("read GeoJSON FeatureCollection: %w", err)
	}
	info := geoJSONCollectionInfo{}
	var bounds [4]float64
	hasBounds, featuresSeen, typeSeen := false, false, false
	sourceOffset := decoder.InputOffset()
	var feature streamedGeoJSONFeature
	featureBuffer := make([]byte, 0, 4096)
	memberNeedsComma := false
	collectionClosed := false
	for {
		if err := ctx.Err(); err != nil {
			return info, bounds, hasBounds, err
		}
		key, nextReader, closed, memberBytes, err := decodeBoundedJSONMember(decoder, reader, maxGeoJSONMetadataBytes, memberNeedsComma)
		if err != nil {
			return info, bounds, hasBounds, fmt.Errorf("read GeoJSON member: %w", err)
		}
		reader = nextReader
		sourceOffset += memberBytes
		if closed {
			collectionClosed = true
			break
		}
		decoder = json.NewDecoder(reader)
		if key != "features" {
			value, nextReader, valueBytes, err := decodeBoundedJSONRaw(decoder, reader, maxGeoJSONMetadataBytes)
			if err != nil {
				return info, bounds, hasBounds, fmt.Errorf("read GeoJSON %q: %w", key, err)
			}
			reader = nextReader
			sourceOffset += valueBytes
			decoder = json.NewDecoder(reader)
			memberNeedsComma = true
			switch key {
			case "type":
				var kind string
				if err := json.Unmarshal(value, &kind); err != nil || kind != "FeatureCollection" {
					return info, bounds, hasBounds, fmt.Errorf("GeoJSON %q is not a FeatureCollection", path)
				}
				typeSeen = true
			case "name":
				_ = json.Unmarshal(value, &info.Name)
			case "crs":
				info.CRS = geoJSONCRS(value)
			}
			continue
		}
		if featuresSeen {
			return info, bounds, hasBounds, fmt.Errorf("GeoJSON %q has duplicate features members", path)
		}
		featuresSeen = true
		arrayToken, err := decoder.Token()
		if err != nil || arrayToken != json.Delim('[') {
			return info, bounds, hasBounds, fmt.Errorf("GeoJSON features member must be an array")
		}
		sourceOffset += decoder.InputOffset()
		streamOffset := sourceOffset
		featureReader := bufio.NewReader(io.MultiReader(decoder.Buffered(), reader))
		for {
			if err := ctx.Err(); err != nil {
				return info, bounds, hasBounds, err
			}
			raw, atEnd, startOffset, endOffset, readErr := readJSONFeatureObject(featureReader, &featureBuffer, &streamOffset, maxFeatureBytes)
			if readErr != nil {
				return info, bounds, hasBounds, fmt.Errorf("read GeoJSON feature %d: %w", info.Count+1, readErr)
			}
			if atEnd {
				break
			}
			feature = streamedGeoJSONFeature{}
			if err := json.Unmarshal(raw, &feature); err != nil {
				return info, bounds, hasBounds, fmt.Errorf("decode GeoJSON feature %d: %w", info.Count+1, err)
			}
			if feature.Type != "Feature" {
				return info, bounds, hasBounds, fmt.Errorf("decode GeoJSON feature %d: type is %q, want Feature", info.Count+1, feature.Type)
			}
			if info.Count == int(^uint(0)>>1) {
				return info, bounds, hasBounds, fmt.Errorf("GeoJSON %q has too many features for this platform", path)
			}
			info.Count++
			feature.sourceOffset = startOffset
			feature.sourceLength = uint32(endOffset - startOffset)
			feature.featureBounds = [4]float64{}
			feature.hasBounds = false
			if len(feature.Geometry) > 0 && !bytes.Equal(bytes.TrimSpace(feature.Geometry), []byte("null")) {
				featureBounds, hasCoordinates, err := geoJSONGeometryBounds(feature.Geometry)
				if err != nil {
					return info, bounds, hasBounds, fmt.Errorf("read GeoJSON feature %d bounds: %w", info.Count, err)
				}
				if hasCoordinates {
					bounds, hasBounds = mergeGeoJSONBounds(bounds, hasBounds, featureBounds)
					feature.featureBounds = featureBounds
					feature.hasBounds = true
				}
			}
			if visit != nil {
				if err := visit(info.Count, &feature); err != nil {
					return info, bounds, hasBounds, err
				}
			}
		}
		var resumeBytes int64
		decoder, reader, collectionClosed, resumeBytes, err = resumeGeoJSONCollectionDecoder(featureReader)
		if err != nil {
			return info, bounds, hasBounds, err
		}
		sourceOffset = streamOffset + resumeBytes
		memberNeedsComma = false
		if collectionClosed {
			break
		}
	}
	if !collectionClosed {
		return info, bounds, hasBounds, fmt.Errorf("GeoJSON %q has no closing object delimiter", path)
	}
	if err := ensureGeoJSONStreamEOF(nil, reader); err != nil {
		return info, bounds, hasBounds, err
	}
	if !featuresSeen || !typeSeen {
		return info, bounds, hasBounds, fmt.Errorf("GeoJSON %q has no FeatureCollection features array", path)
	}
	info.Name = geoJSONLayerName(path, info.Name)
	if info.CRS == "" {
		info.CRS = "EPSG:4326"
	}
	return info, bounds, hasBounds, nil
}

func readGeoJSONSnapshot(ctx context.Context, path, layerName string, includeProperties bool) (core.Layer, error) {
	stamp, err := geoJSONSourceStamp(path)
	if err != nil {
		return core.Layer{}, fmt.Errorf("stat GeoJSON %q: %w", path, err)
	}
	info, _, _, err := scanGeoJSONCollectionWithFeatureLimit(ctx, path, maxMaterializedSnapshotFeatureBytes, nil)
	if err != nil {
		return core.Layer{}, err
	}
	if err := validateGeoJSONSourceStamp(path, stamp); err != nil {
		return core.Layer{}, err
	}
	if layerName != "" && layerName != info.Name {
		return core.Layer{}, fmt.Errorf("layer %q not found in %q", layerName, path)
	}
	if info.Count > maxMaterializedSnapshotFeatures {
		return core.Layer{}, snapshotLimitError(info.Name, maxMaterializedSnapshotFeatures, maxMaterializedSnapshotBytes)
	}
	result := core.Layer{Name: info.Name, CRS: core.CRS{AuthorityCode: info.CRS}, Editable: true, Features: make([]core.Feature, 0, min(info.Count, maxInitialFeatureCapacity))}
	var payloadBytes int64
	_, _, _, err = scanGeoJSONCollectionWithFeatureLimit(ctx, path, maxMaterializedSnapshotFeatureBytes, func(ordinal int, feature *streamedGeoJSONFeature) error {
		if len(result.Features) >= maxMaterializedSnapshotFeatures {
			return snapshotLimitError(info.Name, maxMaterializedSnapshotFeatures, maxMaterializedSnapshotBytes)
		}
		loaded, err := streamedGeoJSONCoreFeature(ordinal, feature, includeProperties)
		if err != nil {
			return err
		}
		if includeProperties && len(result.Fields) == 0 {
			result.Fields = geoJSONFieldSchema(loaded.Properties)
		}
		featureBytes := estimateFeaturePayloadBytes(loaded)
		if featureBytes > int64(maxMaterializedSnapshotBytes)-payloadBytes {
			return fmt.Errorf("layer %q exceeds the in-memory snapshot limit (%d features or %d MiB); use the read-only viewport session or an indexed source", info.Name, maxMaterializedSnapshotFeatures, maxMaterializedSnapshotBytes>>20)
		}
		payloadBytes += featureBytes
		result.Features = append(result.Features, loaded)
		return nil
	})
	if err != nil {
		return core.Layer{}, err
	}
	if err := validateGeoJSONSourceStamp(path, stamp); err != nil {
		return core.Layer{}, err
	}
	return result, nil
}

func ensureGeoJSONStreamEOF(decoder *json.Decoder, source io.Reader) error {
	var stream io.Reader = source
	if decoder != nil {
		stream = io.MultiReader(decoder.Buffered(), source)
	}
	reader := bufio.NewReader(stream)
	for {
		value, err := reader.ReadByte()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read trailing GeoJSON data: %w", err)
		}
		if value != ' ' && value != '\t' && value != '\r' && value != '\n' {
			return fmt.Errorf("unexpected trailing data after GeoJSON FeatureCollection")
		}
	}
}

func mergeGeoJSONBounds(current [4]float64, hasCurrent bool, next [4]float64) ([4]float64, bool) {
	if !hasCurrent {
		return next, true
	}
	return [4]float64{
		math.Min(current[0], next[0]), math.Min(current[1], next[1]),
		math.Max(current[2], next[2]), math.Max(current[3], next[3]),
	}, true
}

func appendGeoJSONFeatureIndex(index []geoJSONFeatureIndex, feature geoJSONFeatureIndex, limit int) ([]geoJSONFeatureIndex, bool) {
	if limit <= 0 || index == nil {
		return nil, false
	}
	if len(index) >= limit {
		return index, false
	}
	if len(index) == cap(index) {
		capacity := max(1, cap(index)*2)
		if capacity > limit {
			capacity = limit
		}
		grown := make([]geoJSONFeatureIndex, len(index), capacity)
		copy(grown, index)
		index = grown
	}
	return append(index, feature), true
}

func inspectGeoJSONCollectionWithIndex(ctx context.Context, path string) ([]LayerOverview, []geoJSONFeatureIndex, geoJSONFileStamp, error) {
	return inspectGeoJSONCollectionWithIndexLimit(ctx, path, maxGeoJSONInMemoryIndexFeatures)
}

func inspectGeoJSONCollectionWithIndexLimit(ctx context.Context, path string, indexLimit int) ([]LayerOverview, []geoJSONFeatureIndex, geoJSONFileStamp, error) {
	overviews, index, _, stamp, err := inspectGeoJSONCollectionWithIndexLimitAndTailBlocks(ctx, path, indexLimit)
	return overviews, index, stamp, err
}

func inspectGeoJSONCollectionWithIndexLimitAndTailBlocks(ctx context.Context, path string, indexLimit int) ([]LayerOverview, []geoJSONFeatureIndex, []geoJSONTailBlock, geoJSONFileStamp, error) {
	stampBefore, err := geoJSONSourceStamp(path)
	if err != nil {
		return nil, nil, nil, geoJSONFileStamp{}, fmt.Errorf("stat GeoJSON %q: %w", path, err)
	}
	index := make([]geoJSONFeatureIndex, 0)
	indexAvailable := true
	tailBuilder := geoJSONTailIndexBuilder{}
	info, bounds, hasBounds, err := scanGeoJSONCollection(ctx, path, func(ordinal int, feature *streamedGeoJSONFeature) error {
		if indexAvailable {
			index, indexAvailable = appendGeoJSONFeatureIndex(index, geoJSONFeatureIndex{
				offset: feature.sourceOffset,
				length: feature.sourceLength,
				bounds: feature.featureBounds,
				valid:  feature.hasBounds,
			}, indexLimit)
		}
		if !indexAvailable && indexLimit > 0 {
			tailBuilder.add(ordinal, feature)
		}
		return nil
	})
	if err != nil {
		return nil, nil, nil, geoJSONFileStamp{}, err
	}
	stampAfter, err := geoJSONSourceStamp(path)
	if err != nil {
		return nil, nil, nil, geoJSONFileStamp{}, fmt.Errorf("stat GeoJSON %q after indexing: %w", path, err)
	}
	if !sameGeoJSONFileStamp(stampBefore, stampAfter) {
		return nil, nil, nil, geoJSONFileStamp{}, fmt.Errorf("GeoJSON source %q changed while it was being indexed", path)
	}
	tailBuilder.flush()
	overview := LayerOverview{Name: info.Name, CRS: core.CRS{AuthorityCode: info.CRS}, FeatureCount: info.Count, Bounds: bounds, HasBounds: hasBounds}
	return []LayerOverview{overview}, index, tailBuilder.blocks, stampAfter, nil
}

func inspectGeoJSONSourceWithIndex(ctx context.Context, path string) ([]LayerOverview, []geoJSONFeatureIndex, geoJSONFileStamp, error) {
	return inspectGeoJSONSourceWithIndexLimit(ctx, path, maxGeoJSONInMemoryIndexFeatures)
}

func inspectGeoJSONSourceWithIndexLimit(ctx context.Context, path string, indexLimit int) ([]LayerOverview, []geoJSONFeatureIndex, geoJSONFileStamp, error) {
	overviews, index, _, stamp, err := inspectGeoJSONSourceWithIndexLimitAndTailBlocks(ctx, path, indexLimit)
	return overviews, index, stamp, err
}

func inspectGeoJSONSourceWithIndexLimitAndTailBlocks(ctx context.Context, path string, indexLimit int) ([]LayerOverview, []geoJSONFeatureIndex, []geoJSONTailBlock, geoJSONFileStamp, error) {
	if !isGeoJSONSequence(path) {
		return inspectGeoJSONCollectionWithIndexLimitAndTailBlocks(ctx, path, indexLimit)
	}
	stampBefore, err := geoJSONSourceStamp(path)
	if err != nil {
		return nil, nil, nil, geoJSONFileStamp{}, fmt.Errorf("stat GeoJSONSeq %q: %w", path, err)
	}
	index := make([]geoJSONFeatureIndex, 0)
	indexAvailable := true
	tailBuilder := geoJSONTailIndexBuilder{}
	info, bounds, hasBounds, err := scanGeoJSONSequence(ctx, path, func(ordinal int, feature *streamedGeoJSONFeature) error {
		if indexAvailable {
			index, indexAvailable = appendGeoJSONFeatureIndex(index, geoJSONFeatureIndex{
				offset: feature.sourceOffset,
				length: feature.sourceLength,
				bounds: feature.featureBounds,
				valid:  feature.hasBounds,
			}, indexLimit)
		}
		if !indexAvailable && indexLimit > 0 {
			tailBuilder.add(ordinal, feature)
		}
		return nil
	})
	if err != nil {
		return nil, nil, nil, geoJSONFileStamp{}, err
	}
	stampAfter, err := geoJSONSourceStamp(path)
	if err != nil {
		return nil, nil, nil, geoJSONFileStamp{}, fmt.Errorf("stat GeoJSONSeq %q after indexing: %w", path, err)
	}
	if !sameGeoJSONFileStamp(stampBefore, stampAfter) {
		return nil, nil, nil, geoJSONFileStamp{}, fmt.Errorf("GeoJSONSeq source %q changed while it was being indexed", path)
	}
	tailBuilder.flush()
	overview := LayerOverview{Name: info.Name, CRS: core.CRS{AuthorityCode: info.CRS}, FeatureCount: info.Count, Bounds: bounds, HasBounds: hasBounds}
	return []LayerOverview{overview}, index, tailBuilder.blocks, stampAfter, nil
}

func readGeoJSONIndexedWindow(ctx context.Context, path string, overview LayerOverview, index []geoJSONFeatureIndex, bounds [4]float64, includeProperties bool, maxFeatures int, maxBytes int64) (core.Layer, error) {
	return readGeoJSONIndexedWindowWithTailBlocks(ctx, path, overview, index, nil, bounds, includeProperties, maxFeatures, maxBytes)
}

func readGeoJSONIndexedWindowWithTailBlocks(ctx context.Context, path string, overview LayerOverview, index []geoJSONFeatureIndex,
	tailBlocks []geoJSONTailBlock, bounds [4]float64, includeProperties bool, maxFeatures int, maxBytes int64) (core.Layer, error) {
	file, err := os.Open(path)
	if err != nil {
		return core.Layer{}, fmt.Errorf("open GeoJSON %q: %w", path, err)
	}
	defer file.Close()
	result := core.Layer{Name: overview.Name, CRS: overview.CRS, Editable: true}
	var rawBuffer []byte
	var payloadBytes int64
	for ordinal, entry := range index {
		if ordinal&0x3fff == 0 {
			if err := ctx.Err(); err != nil {
				return core.Layer{}, err
			}
		}
		if !entry.valid || entry.bounds[2] < bounds[0] || entry.bounds[0] > bounds[2] || entry.bounds[3] < bounds[1] || entry.bounds[1] > bounds[3] {
			continue
		}
		if maxFeatures > 0 && len(result.Features) >= maxFeatures {
			return core.Layer{}, fmt.Errorf("spatial window exceeds the limit of %d features", maxFeatures)
		}
		feature, err := readIndexedGeoJSONFeature(file, entry, ordinal+1, &rawBuffer)
		if err != nil {
			return core.Layer{}, err
		}
		loaded, err := streamedGeoJSONCoreFeature(ordinal+1, &feature, includeProperties)
		if err != nil {
			return core.Layer{}, err
		}
		if includeProperties && len(result.Fields) == 0 {
			result.Fields = geoJSONFieldSchema(loaded.Properties)
		}
		featureBytes := estimateFeaturePayloadBytes(loaded)
		if maxBytes > 0 && featureBytes > maxBytes-payloadBytes {
			return core.Layer{}, fmt.Errorf("spatial window exceeds the limit of %d bytes", maxBytes)
		}
		payloadBytes += featureBytes
		result.Features = append(result.Features, loaded)
	}
	if overview.FeatureCount > len(index) && len(index) > 0 {
		last := index[len(index)-1]
		if len(tailBlocks) == 0 {
			if err := appendGeoJSONTailWindow(ctx, file, last.offset+int64(last.length), 0,
				len(index)+1, overview.FeatureCount, bounds, includeProperties, maxFeatures, maxBytes,
				&result, &payloadBytes, isGeoJSONSequence(path)); err != nil {
				return core.Layer{}, err
			}
		} else {
			for _, block := range tailBlocks {
				if err := ctx.Err(); err != nil {
					return core.Layer{}, err
				}
				if !block.hasBounds || !geoJSONBoundsIntersect(block.bounds, bounds) {
					continue
				}
				lastOrdinal := block.firstOrdinal + block.featureCount - 1
				if err := appendGeoJSONTailWindow(ctx, file, block.startOffset, block.endOffset,
					block.firstOrdinal, lastOrdinal, bounds, includeProperties, maxFeatures, maxBytes,
					&result, &payloadBytes, isGeoJSONSequence(path)); err != nil {
					return core.Layer{}, err
				}
			}
		}
	}
	return result, nil
}

func geoJSONBoundsIntersect(first, second [4]float64) bool {
	return first[2] >= second[0] && first[0] <= second[2] && first[3] >= second[1] && first[1] <= second[3]
}

func appendGeoJSONTailWindow(ctx context.Context, file *os.File, startOffset, expectedEndOffset int64,
	firstOrdinal, lastOrdinal int, bounds [4]float64, includeProperties bool, maxFeatures int, maxBytes int64,
	result *core.Layer, payloadBytes *int64, sequence bool) error {
	if sequence {
		return appendGeoJSONSequenceIndexedTailWindow(ctx, file, startOffset, expectedEndOffset,
			firstOrdinal, lastOrdinal, bounds, includeProperties, maxFeatures, maxBytes, result, payloadBytes)
	}
	return appendGeoJSONIndexedTailWindow(ctx, file, startOffset, expectedEndOffset,
		firstOrdinal, lastOrdinal, bounds, includeProperties, maxFeatures, maxBytes, result, payloadBytes)
}

func appendGeoJSONSequenceIndexedTailWindow(ctx context.Context, file *os.File, offset, expectedEndOffset int64,
	firstOrdinal, lastOrdinal int, bounds [4]float64, includeProperties bool, maxFeatures int, maxBytes int64,
	result *core.Layer, payloadBytes *int64) error {
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return fmt.Errorf("seek GeoJSONSeq tail at byte %d: %w", offset, err)
	}
	reader := bufio.NewReaderSize(file, 64<<10)
	streamOffset := offset
	var recordBuffer []byte
	var feature streamedGeoJSONFeature
	ordinal := firstOrdinal - 1
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		recordStart := streamOffset
		raw, bytesRead, eof, err := readGeoJSONSequenceRecord(reader, &recordBuffer, maxGeoJSONFeatureMiB<<20)
		streamOffset += int64(bytesRead)
		if err != nil {
			return fmt.Errorf("read GeoJSONSeq tail record %d: %w", ordinal+1, err)
		}
		if eof {
			if ordinal != lastOrdinal {
				return fmt.Errorf("GeoJSONSeq partial index tail ended at feature %d; expected %d", ordinal, lastOrdinal)
			}
			return nil
		}
		start, end := 0, len(raw)
		for start < end && (raw[start] == ' ' || raw[start] == '\t' || raw[start] == '\r' || raw[start] == '\n') {
			start++
		}
		if start < end && raw[start] == 0x1e {
			start++
		}
		for start < end && (raw[start] == ' ' || raw[start] == '\t' || raw[start] == '\r' || raw[start] == '\n') {
			start++
		}
		for end > start && (raw[end-1] == ' ' || raw[end-1] == '\t' || raw[end-1] == '\r' || raw[end-1] == '\n') {
			end--
		}
		if start == end {
			continue
		}
		object := raw[start:end]
		featureEndOffset := recordStart + int64(start+len(object))
		feature = streamedGeoJSONFeature{}
		if err := json.Unmarshal(object, &feature); err != nil {
			return fmt.Errorf("decode GeoJSONSeq tail record at byte %d: %w", recordStart, err)
		}
		if feature.Type != "Feature" {
			return fmt.Errorf("decode GeoJSONSeq tail record at byte %d: type is %q, want Feature", recordStart, feature.Type)
		}
		ordinal++
		if len(feature.Geometry) == 0 || bytes.Equal(bytes.TrimSpace(feature.Geometry), []byte("null")) {
			if ordinal == lastOrdinal {
				return validateGeoJSONTailBlockEnd(expectedEndOffset, featureEndOffset)
			}
			continue
		}
		featureBounds, hasBounds, err := geoJSONGeometryBounds(feature.Geometry)
		if err != nil {
			return fmt.Errorf("read GeoJSONSeq tail feature %d bounds: %w", ordinal, err)
		}
		if !hasBounds || featureBounds[2] < bounds[0] || featureBounds[0] > bounds[2] ||
			featureBounds[3] < bounds[1] || featureBounds[1] > bounds[3] {
			if ordinal == lastOrdinal {
				return validateGeoJSONTailBlockEnd(expectedEndOffset, featureEndOffset)
			}
			continue
		}
		if maxFeatures > 0 && len(result.Features) >= maxFeatures {
			return fmt.Errorf("spatial window exceeds the limit of %d features", maxFeatures)
		}
		feature.featureBounds, feature.hasBounds = featureBounds, true
		loaded, err := streamedGeoJSONCoreFeature(ordinal, &feature, includeProperties)
		if err != nil {
			return err
		}
		if includeProperties && len(result.Fields) == 0 {
			result.Fields = geoJSONFieldSchema(loaded.Properties)
		}
		featureBytes := estimateFeaturePayloadBytes(loaded)
		if maxBytes > 0 && featureBytes > maxBytes-*payloadBytes {
			return fmt.Errorf("spatial window exceeds the limit of %d bytes", maxBytes)
		}
		*payloadBytes += featureBytes
		result.Features = append(result.Features, loaded)
		if ordinal == lastOrdinal {
			return validateGeoJSONTailBlockEnd(expectedEndOffset, featureEndOffset)
		}
	}
}

// appendGeoJSONIndexedTailWindow scans only records beyond a retained partial
// index. This preserves bounded index memory without turning every viewport
// request into a full-file scan just because the source is slightly larger
// than the in-memory index cap.
func appendGeoJSONIndexedTailWindow(ctx context.Context, file *os.File, offset, expectedEndOffset int64,
	firstOrdinal, lastOrdinal int, bounds [4]float64, includeProperties bool, maxFeatures int, maxBytes int64,
	result *core.Layer, payloadBytes *int64) error {
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return fmt.Errorf("seek GeoJSON tail at byte %d: %w", offset, err)
	}
	reader := bufio.NewReaderSize(file, 1<<20)
	streamOffset := offset
	featureBuffer := make([]byte, 0, 4096)
	ordinal := firstOrdinal - 1
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		raw, atEnd, _, endOffset, err := readJSONFeatureObject(reader, &featureBuffer, &streamOffset, maxGeoJSONFeatureMiB<<20)
		if err != nil {
			return fmt.Errorf("read GeoJSON feature %d after partial index: %w", ordinal+1, err)
		}
		if atEnd {
			if ordinal != lastOrdinal {
				return fmt.Errorf("GeoJSON partial index tail ended at feature %d; expected %d", ordinal, lastOrdinal)
			}
			return nil
		}
		ordinal++
		var feature streamedGeoJSONFeature
		if err := json.Unmarshal(raw, &feature); err != nil {
			return fmt.Errorf("decode GeoJSON tail feature %d: %w", ordinal, err)
		}
		if feature.Type != "Feature" {
			return fmt.Errorf("decode GeoJSON tail feature %d: type is %q, want Feature", ordinal, feature.Type)
		}
		if len(feature.Geometry) == 0 || bytes.Equal(bytes.TrimSpace(feature.Geometry), []byte("null")) {
			if ordinal == lastOrdinal {
				return validateGeoJSONTailBlockEnd(expectedEndOffset, endOffset)
			}
			continue
		}
		featureBounds, hasBounds, err := geoJSONGeometryBounds(feature.Geometry)
		if err != nil {
			return fmt.Errorf("read GeoJSON tail feature %d bounds: %w", ordinal, err)
		}
		if !hasBounds || featureBounds[2] < bounds[0] || featureBounds[0] > bounds[2] ||
			featureBounds[3] < bounds[1] || featureBounds[1] > bounds[3] {
			if ordinal == lastOrdinal {
				return validateGeoJSONTailBlockEnd(expectedEndOffset, endOffset)
			}
			continue
		}
		if maxFeatures > 0 && len(result.Features) >= maxFeatures {
			return fmt.Errorf("spatial window exceeds the limit of %d features", maxFeatures)
		}
		feature.featureBounds, feature.hasBounds = featureBounds, true
		loaded, err := streamedGeoJSONCoreFeature(ordinal, &feature, includeProperties)
		if err != nil {
			return err
		}
		if includeProperties && len(result.Fields) == 0 {
			result.Fields = geoJSONFieldSchema(loaded.Properties)
		}
		featureBytes := estimateFeaturePayloadBytes(loaded)
		if maxBytes > 0 && featureBytes > maxBytes-*payloadBytes {
			return fmt.Errorf("spatial window exceeds the limit of %d bytes", maxBytes)
		}
		*payloadBytes += featureBytes
		result.Features = append(result.Features, loaded)
		if ordinal == lastOrdinal {
			return validateGeoJSONTailBlockEnd(expectedEndOffset, endOffset)
		}
	}
}

func validateGeoJSONTailBlockEnd(expected, actual int64) error {
	if expected > 0 && actual != expected {
		return fmt.Errorf("GeoJSON tail block ended at byte %d; expected %d", actual, expected)
	}
	return nil
}

func readGeoJSONTailFeatureWithBlocks(ctx context.Context, path, layerName string, featureID uint64,
	overview LayerOverview, blocks []geoJSONTailBlock) (core.Feature, error) {
	if layerName != "" && layerName != overview.Name {
		return core.Feature{}, fmt.Errorf("layer %q not found in %q", layerName, path)
	}
	if featureID == 0 || featureID > uint64(overview.FeatureCount) {
		return core.Feature{}, fmt.Errorf("feature %d not found in layer %q", featureID, overview.Name)
	}
	ordinal := int(featureID)
	blockIndex := sort.Search(len(blocks), func(index int) bool {
		return blocks[index].firstOrdinal+blocks[index].featureCount > ordinal
	})
	if blockIndex >= len(blocks) || ordinal < blocks[blockIndex].firstOrdinal {
		return core.Feature{}, fmt.Errorf("feature %d is not covered by the GeoJSON tail index", featureID)
	}
	block := blocks[blockIndex]
	lastOrdinal := block.firstOrdinal + block.featureCount - 1
	file, err := os.Open(path)
	if err != nil {
		return core.Feature{}, fmt.Errorf("open GeoJSON %q: %w", path, err)
	}
	defer file.Close()
	if isGeoJSONSequence(path) {
		return readGeoJSONSequenceTailFeature(ctx, file, block, lastOrdinal, ordinal)
	}
	return readGeoJSONCollectionTailFeature(ctx, file, block, lastOrdinal, ordinal)
}

func readGeoJSONSequenceTailFeature(ctx context.Context, file *os.File, block geoJSONTailBlock, lastOrdinal, targetOrdinal int) (core.Feature, error) {
	if _, err := file.Seek(block.startOffset, io.SeekStart); err != nil {
		return core.Feature{}, fmt.Errorf("seek GeoJSONSeq tail at byte %d: %w", block.startOffset, err)
	}
	reader := bufio.NewReaderSize(file, 64<<10)
	streamOffset := block.startOffset
	var recordBuffer []byte
	ordinal := block.firstOrdinal - 1
	for {
		if err := ctx.Err(); err != nil {
			return core.Feature{}, err
		}
		recordStart := streamOffset
		raw, bytesRead, eof, err := readGeoJSONSequenceRecord(reader, &recordBuffer, maxGeoJSONFeatureMiB<<20)
		streamOffset += int64(bytesRead)
		if err != nil {
			return core.Feature{}, fmt.Errorf("read GeoJSONSeq tail record %d: %w", ordinal+1, err)
		}
		if eof {
			return core.Feature{}, fmt.Errorf("GeoJSONSeq tail block ended before feature %d", targetOrdinal)
		}
		start, end := 0, len(raw)
		for start < end && (raw[start] == ' ' || raw[start] == '\t' || raw[start] == '\r' || raw[start] == '\n') {
			start++
		}
		if start < end && raw[start] == 0x1e {
			start++
		}
		for start < end && (raw[start] == ' ' || raw[start] == '\t' || raw[start] == '\r' || raw[start] == '\n') {
			start++
		}
		for end > start && (raw[end-1] == ' ' || raw[end-1] == '\t' || raw[end-1] == '\r' || raw[end-1] == '\n') {
			end--
		}
		if start == end {
			continue
		}
		object := raw[start:end]
		featureEndOffset := recordStart + int64(start+len(object))
		ordinal++
		if ordinal > targetOrdinal || featureEndOffset > block.endOffset {
			return core.Feature{}, fmt.Errorf("GeoJSONSeq tail block offsets do not cover feature %d", targetOrdinal)
		}
		if ordinal != targetOrdinal {
			continue
		}
		var feature streamedGeoJSONFeature
		if err := json.Unmarshal(object, &feature); err != nil {
			return core.Feature{}, fmt.Errorf("decode GeoJSONSeq tail feature %d: %w", ordinal, err)
		}
		if feature.Type != "Feature" {
			return core.Feature{}, fmt.Errorf("decode GeoJSONSeq tail feature %d: type is %q, want Feature", ordinal, feature.Type)
		}
		if ordinal == lastOrdinal {
			if err := validateGeoJSONTailBlockEnd(block.endOffset, featureEndOffset); err != nil {
				return core.Feature{}, err
			}
		}
		return streamedGeoJSONCoreFeature(ordinal, &feature, true)
	}
}

func readGeoJSONCollectionTailFeature(ctx context.Context, file *os.File, block geoJSONTailBlock, lastOrdinal, targetOrdinal int) (core.Feature, error) {
	if _, err := file.Seek(block.startOffset, io.SeekStart); err != nil {
		return core.Feature{}, fmt.Errorf("seek GeoJSON tail at byte %d: %w", block.startOffset, err)
	}
	reader := bufio.NewReaderSize(file, 1<<20)
	streamOffset := block.startOffset
	featureBuffer := make([]byte, 0, 4096)
	ordinal := block.firstOrdinal - 1
	for {
		if err := ctx.Err(); err != nil {
			return core.Feature{}, err
		}
		raw, atEnd, _, endOffset, err := readJSONFeatureObject(reader, &featureBuffer, &streamOffset, maxGeoJSONFeatureMiB<<20)
		if err != nil {
			return core.Feature{}, fmt.Errorf("read GeoJSON tail feature %d: %w", ordinal+1, err)
		}
		if atEnd {
			return core.Feature{}, fmt.Errorf("GeoJSON tail block ended before feature %d", targetOrdinal)
		}
		ordinal++
		if ordinal > targetOrdinal || endOffset > block.endOffset {
			return core.Feature{}, fmt.Errorf("GeoJSON tail block offsets do not cover feature %d", targetOrdinal)
		}
		if ordinal != targetOrdinal {
			continue
		}
		var feature streamedGeoJSONFeature
		if err := json.Unmarshal(raw, &feature); err != nil {
			return core.Feature{}, fmt.Errorf("decode GeoJSON tail feature %d: %w", ordinal, err)
		}
		if feature.Type != "Feature" {
			return core.Feature{}, fmt.Errorf("decode GeoJSON tail feature %d: type is %q, want Feature", ordinal, feature.Type)
		}
		if ordinal == lastOrdinal {
			if err := validateGeoJSONTailBlockEnd(block.endOffset, endOffset); err != nil {
				return core.Feature{}, err
			}
		}
		return streamedGeoJSONCoreFeature(ordinal, &feature, true)
	}
}

func readGeoJSONIndexedFeature(ctx context.Context, path, layerName string, featureID uint64, index []geoJSONFeatureIndex, expectedLayer string) (core.Feature, error) {
	if layerName != "" && layerName != expectedLayer {
		return core.Feature{}, fmt.Errorf("layer %q not found in %q", layerName, path)
	}
	if featureID == 0 || featureID > uint64(len(index)) {
		return core.Feature{}, fmt.Errorf("feature %d not found in layer %q", featureID, expectedLayer)
	}
	if err := ctx.Err(); err != nil {
		return core.Feature{}, err
	}
	file, err := os.Open(path)
	if err != nil {
		return core.Feature{}, fmt.Errorf("open GeoJSON %q: %w", path, err)
	}
	defer file.Close()
	var rawBuffer []byte
	feature, err := readIndexedGeoJSONFeature(file, index[featureID-1], int(featureID), &rawBuffer)
	if err != nil {
		return core.Feature{}, err
	}
	return streamedGeoJSONCoreFeature(int(featureID), &feature, true)
}

func readGeoJSONIndexedAttributePage(ctx context.Context, path, layerName string, offset, limit int, index []geoJSONFeatureIndex, overview LayerOverview) (core.Layer, int, error) {
	if err := validateAttributePage(offset, limit); err != nil {
		return core.Layer{}, 0, err
	}
	if layerName != "" && layerName != overview.Name {
		return core.Layer{}, 0, fmt.Errorf("layer %q not found in %q", layerName, path)
	}
	if err := ctx.Err(); err != nil {
		return core.Layer{}, 0, err
	}
	if offset >= len(index) || offset+limit > len(index) {
		page, total, err := readGeoJSONSourceAttributePage(ctx, path, layerName, offset, limit)
		if err != nil {
			return core.Layer{}, 0, err
		}
		if overview.FeatureCount >= 0 {
			total = overview.FeatureCount
		}
		return page, total, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return core.Layer{}, 0, fmt.Errorf("open GeoJSON %q: %w", path, err)
	}
	defer file.Close()
	remaining := max(0, len(index)-min(offset, len(index)))
	result := core.Layer{Name: overview.Name, CRS: overview.CRS, Editable: true, Features: make([]core.Feature, 0, min(limit, remaining))}
	var rawBuffer []byte
	var payloadBytes int64
	end := offset + min(limit, remaining)
	for ordinal := offset; ordinal < end; ordinal++ {
		if err := ctx.Err(); err != nil {
			return core.Layer{}, 0, err
		}
		feature, err := readIndexedGeoJSONFeature(file, index[ordinal], ordinal+1, &rawBuffer)
		if err != nil {
			return core.Layer{}, 0, err
		}
		properties, err := decodeGeoJSONProperties(feature.Properties, ordinal+1)
		if err != nil {
			return core.Layer{}, 0, err
		}
		loaded := core.Feature{ID: uint64(ordinal + 1), Properties: properties}
		if len(properties) > 0 {
			if len(result.Fields) == 0 {
				result.Fields = geoJSONFieldSchema(properties)
			}
		}
		if err := appendAttributePageFeature(&result, loaded, &payloadBytes); err != nil {
			return core.Layer{}, 0, fmt.Errorf("read feature %d: %w", ordinal+1, err)
		}
	}
	total := len(index)
	if overview.FeatureCount >= 0 {
		total = overview.FeatureCount
	}
	return result, total, nil
}

func readIndexedGeoJSONFeature(file *os.File, entry geoJSONFeatureIndex, ordinal int, buffer *[]byte) (streamedGeoJSONFeature, error) {
	if entry.length == 0 || entry.length > maxGeoJSONFeatureMiB<<20 {
		return streamedGeoJSONFeature{}, fmt.Errorf("GeoJSON feature %d has invalid indexed size %d", ordinal, entry.length)
	}
	if cap(*buffer) < int(entry.length) {
		*buffer = make([]byte, int(entry.length))
	}
	raw := (*buffer)[:int(entry.length)]
	if n, err := file.ReadAt(raw, entry.offset); err != nil && (err != io.EOF || n != len(raw)) {
		return streamedGeoJSONFeature{}, fmt.Errorf("read indexed GeoJSON feature %d: %w", ordinal, err)
	} else if n != len(raw) {
		return streamedGeoJSONFeature{}, fmt.Errorf("read indexed GeoJSON feature %d: unexpected EOF", ordinal)
	}
	*buffer = raw
	var feature streamedGeoJSONFeature
	if err := json.Unmarshal(raw, &feature); err != nil {
		return streamedGeoJSONFeature{}, fmt.Errorf("decode indexed GeoJSON feature %d: %w", ordinal, err)
	}
	if feature.Type != "Feature" {
		return streamedGeoJSONFeature{}, fmt.Errorf("indexed GeoJSON feature %d has type %q", ordinal, feature.Type)
	}
	return feature, nil
}

func decodeGeoJSONProperties(raw []byte, ordinal int) (map[string]any, error) {
	if len(raw) > maxGeoJSONPropertiesBytes {
		return nil, fmt.Errorf("GeoJSON feature %d properties exceed the %d MiB limit", ordinal, maxGeoJSONPropertiesBytes>>20)
	}
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, nil
	}
	if len(raw) > geoJSONPropertyPreflightThreshold ||
		bytes.Count(raw, []byte("{"))+bytes.Count(raw, []byte("[")) > maxGeoJSONPropertyDepth {
		if err := validateGeoJSONPropertyComplexity(raw); err != nil {
			return nil, fmt.Errorf("GeoJSON feature %d properties exceed complexity limits: %w", ordinal, err)
		}
	}
	var properties map[string]any
	if err := json.Unmarshal(raw, &properties); err != nil {
		return nil, fmt.Errorf("decode GeoJSON feature %d properties: %w", ordinal, err)
	}
	return properties, nil
}

func validateGeoJSONPropertyComplexity(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	nodeCount := 0
	return scanGeoJSONPropertyValue(decoder, &nodeCount, 0)
}

func scanGeoJSONPropertyValue(decoder *json.Decoder, nodeCount *int, depth int) error {
	if depth > maxGeoJSONPropertyDepth {
		return fmt.Errorf("JSON nesting exceeds the %d-level limit", maxGeoJSONPropertyDepth)
	}
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	*nodeCount++
	if *nodeCount > maxGeoJSONPropertyNodes {
		return fmt.Errorf("JSON value count exceeds the %d-node limit", maxGeoJSONPropertyNodes)
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		for decoder.More() {
			key, err := decoder.Token()
			if err != nil {
				return err
			}
			if _, ok := key.(string); !ok {
				return fmt.Errorf("object key is not a string")
			}
			*nodeCount++
			if *nodeCount > maxGeoJSONPropertyNodes {
				return fmt.Errorf("JSON value count exceeds the %d-node limit", maxGeoJSONPropertyNodes)
			}
			if err := scanGeoJSONPropertyValue(decoder, nodeCount, depth+1); err != nil {
				return err
			}
		}
		endToken, err := decoder.Token()
		if err != nil {
			return err
		}
		if endToken != json.Delim('}') {
			return fmt.Errorf("expected closing object delimiter")
		}
	case '[':
		for decoder.More() {
			if err := scanGeoJSONPropertyValue(decoder, nodeCount, depth+1); err != nil {
				return err
			}
		}
		endToken, err := decoder.Token()
		if err != nil {
			return err
		}
		if endToken != json.Delim(']') {
			return fmt.Errorf("expected closing array delimiter")
		}
	default:
		return fmt.Errorf("unexpected JSON delimiter %q", delimiter)
	}
	return nil
}

func openGeoJSONFeature(ctx context.Context, path, layerName string, featureID uint64) (core.Feature, error) {
	var result core.Feature
	info, _, _, err := scanGeoJSONCollection(ctx, path, func(ordinal int, feature *streamedGeoJSONFeature) error {
		if uint64(ordinal) != featureID {
			return nil
		}
		loaded, err := streamedGeoJSONCoreFeature(ordinal, feature, true)
		result = loaded
		return err
	})
	if err != nil {
		return core.Feature{}, err
	}
	if layerName != "" && layerName != info.Name {
		return core.Feature{}, fmt.Errorf("layer %q not found in %q", layerName, path)
	}
	if result.ID == 0 {
		return core.Feature{}, fmt.Errorf("feature %d not found in layer %q", featureID, info.Name)
	}
	return result, nil
}

func openGeoJSONAttributePage(ctx context.Context, path, layerName string, offset, limit int) (core.Layer, int, error) {
	if err := validateAttributePage(offset, limit); err != nil {
		return core.Layer{}, 0, err
	}
	result := core.Layer{Editable: true, Features: make([]core.Feature, 0, limit)}
	var payloadBytes int64
	info, _, _, err := scanGeoJSONCollection(ctx, path, func(ordinal int, feature *streamedGeoJSONFeature) error {
		if ordinal <= offset || len(result.Features) >= limit {
			return nil
		}
		properties, err := decodeGeoJSONProperties(feature.Properties, ordinal)
		if err != nil {
			return err
		}
		loaded := core.Feature{ID: uint64(ordinal), Properties: properties}
		if len(properties) > 0 {
			if len(result.Fields) == 0 {
				result.Fields = geoJSONFieldSchema(properties)
			}
		}
		return appendAttributePageFeature(&result, loaded, &payloadBytes)
	})
	if err != nil {
		return core.Layer{}, 0, err
	}
	if layerName != "" && layerName != info.Name {
		return core.Layer{}, 0, fmt.Errorf("layer %q not found in %q", layerName, path)
	}
	result.Name = info.Name
	result.CRS = core.CRS{AuthorityCode: info.CRS}
	return result, info.Count, nil
}

func streamedGeoJSONCoreFeature(ordinal int, feature *streamedGeoJSONFeature, includeProperties bool) (core.Feature, error) {
	result := core.Feature{ID: uint64(ordinal)}
	if includeProperties {
		properties, err := decodeGeoJSONProperties(feature.Properties, ordinal)
		if err != nil {
			return core.Feature{}, err
		}
		result.Properties = properties
	}
	if len(feature.Geometry) == 0 || bytes.Equal(bytes.TrimSpace(feature.Geometry), []byte("null")) {
		return result, nil
	}
	geometry, err := godal.NewGeometryFromGeoJSON(string(feature.Geometry))
	if err != nil {
		return core.Feature{}, fmt.Errorf("parse GeoJSON feature %d geometry: %w", ordinal, err)
	}
	wkb, err := geometryWKBWithLimit(geometry, uint64(ordinal))
	geometry.Close()
	if err != nil {
		return core.Feature{}, fmt.Errorf("encode GeoJSON feature %d geometry: %w", ordinal, err)
	}
	result.Geometry = core.WKBGeometry{WKB: wkb}
	return result, nil
}

// readGeoJSONWindow decodes one feature at a time, so the GeoJSON collection is
// never materialized as an OGR in-memory layer. RawMessage retains at most one
// source feature at a time; the configured feature cap is checked immediately
// after decode, while OGR_GEOJSON_MAX_OBJ_SIZE remains enforced on GDAL paths.
func readGeoJSONWindow(ctx context.Context, path, layerName string, bounds [4]float64, includeProperties bool, maxFeatures int, maxBytes int64) (core.Layer, error) {
	return readGeoJSONWindowPrefix(ctx, path, layerName, bounds, includeProperties, maxFeatures, maxBytes, 0)
}

func readGeoJSONWindowPrefix(ctx context.Context, path, layerName string, bounds [4]float64, includeProperties bool, maxFeatures int, maxBytes int64, stopAfter int) (core.Layer, error) {
	file, err := os.Open(path)
	if err != nil {
		return core.Layer{}, fmt.Errorf("open GeoJSON %q: %w", path, err)
	}
	defer file.Close()
	result := core.Layer{Editable: true}
	reader := bufio.NewReaderSize(file, 1<<20)
	decoder := json.NewDecoder(reader)
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		if err == nil {
			err = fmt.Errorf("expected object")
		}
		return core.Layer{}, fmt.Errorf("read GeoJSON FeatureCollection: %w", err)
	}
	featuresSeen := false
	typeSeen := false
	var ordinal uint64
	var payloadBytes int64
	var feature streamedGeoJSONFeature
	featureBuffer := make([]byte, 0, 4096)
	stoppedEarly := false
	memberNeedsComma := false
	collectionClosed := false
	for {
		if err := ctx.Err(); err != nil {
			return core.Layer{}, err
		}
		key, nextReader, closed, _, err := decodeBoundedJSONMember(decoder, reader, maxGeoJSONMetadataBytes, memberNeedsComma)
		if err != nil {
			return core.Layer{}, fmt.Errorf("read GeoJSON member: %w", err)
		}
		reader = nextReader
		if closed {
			collectionClosed = true
			break
		}
		decoder = json.NewDecoder(reader)
		if key != "features" {
			value, nextReader, _, err := decodeBoundedJSONRaw(decoder, reader, maxGeoJSONMetadataBytes)
			if err != nil {
				return core.Layer{}, fmt.Errorf("read GeoJSON %q: %w", key, err)
			}
			reader = nextReader
			decoder = json.NewDecoder(reader)
			memberNeedsComma = true
			if key == "type" {
				var kind string
				if err := json.Unmarshal(value, &kind); err != nil || kind != "FeatureCollection" {
					return core.Layer{}, fmt.Errorf("GeoJSON %q is not a FeatureCollection", path)
				}
				typeSeen = true
			}
			if key == "name" && result.Name == "" {
				_ = json.Unmarshal(value, &result.Name)
			}
			if key == "crs" {
				result.CRS.AuthorityCode = geoJSONCRS(value)
			}
			continue
		}
		if featuresSeen {
			return core.Layer{}, fmt.Errorf("GeoJSON %q has duplicate features members", path)
		}
		featuresSeen = true
		arrayToken, err := decoder.Token()
		if err != nil || arrayToken != json.Delim('[') {
			return core.Layer{}, fmt.Errorf("GeoJSON features member must be an array")
		}
		streamOffset := decoder.InputOffset()
		featureReader := bufio.NewReader(io.MultiReader(decoder.Buffered(), reader))
		for {
			if err := ctx.Err(); err != nil {
				return core.Layer{}, err
			}
			raw, atEnd, _, _, readErr := readJSONFeatureObject(featureReader, &featureBuffer, &streamOffset, maxGeoJSONFeatureMiB<<20)
			if readErr != nil {
				return core.Layer{}, fmt.Errorf("read GeoJSON feature %d: %w", ordinal+1, readErr)
			}
			if atEnd {
				break
			}
			feature = streamedGeoJSONFeature{}
			if err := json.Unmarshal(raw, &feature); err != nil {
				return core.Layer{}, fmt.Errorf("decode GeoJSON feature %d: %w", ordinal+1, err)
			}
			ordinal++
			if feature.Type != "Feature" {
				return core.Layer{}, fmt.Errorf("decode GeoJSON feature %d: type is %q, want Feature", ordinal, feature.Type)
			}
			if len(feature.Geometry) == 0 || bytes.Equal(bytes.TrimSpace(feature.Geometry), []byte("null")) {
				continue
			}
			envelope, hasCoordinates, envelopeErr := geoJSONGeometryBounds(feature.Geometry)
			if envelopeErr != nil {
				return core.Layer{}, fmt.Errorf("read GeoJSON feature %d bounds: %w", ordinal, envelopeErr)
			}
			if !hasCoordinates || envelope[2] < bounds[0] || envelope[0] > bounds[2] || envelope[3] < bounds[1] || envelope[1] > bounds[3] {
				continue
			}
			if maxFeatures > 0 && len(result.Features) >= maxFeatures {
				return core.Layer{}, fmt.Errorf("spatial window exceeds the limit of %d features", maxFeatures)
			}
			if includeProperties && len(feature.Properties) > maxGeoJSONPropertiesBytes {
				return core.Layer{}, fmt.Errorf("GeoJSON feature %d properties exceed the %d MiB limit", ordinal, maxGeoJSONPropertiesBytes>>20)
			}
			geometry, err := godal.NewGeometryFromGeoJSON(string(feature.Geometry))
			if err != nil {
				return core.Layer{}, fmt.Errorf("parse GeoJSON feature %d geometry: %w", ordinal, err)
			}
			wkb, wkbErr := geometryWKBWithLimit(geometry, ordinal)
			geometry.Close()
			if wkbErr != nil {
				return core.Layer{}, fmt.Errorf("encode GeoJSON feature %d geometry: %w", ordinal, wkbErr)
			}
			var properties map[string]any
			if includeProperties {
				properties, err = decodeGeoJSONProperties(feature.Properties, int(ordinal))
				if err != nil {
					return core.Layer{}, err
				}
			}
			if includeProperties && len(result.Fields) == 0 {
				result.Fields = geoJSONFieldSchema(properties)
			}
			loaded := core.Feature{ID: ordinal, Geometry: core.WKBGeometry{WKB: wkb}, Properties: properties}
			featureBytes := estimateFeaturePayloadBytes(loaded)
			if maxBytes > 0 && featureBytes > maxBytes-payloadBytes {
				return core.Layer{}, fmt.Errorf("spatial window exceeds the limit of %d bytes", maxBytes)
			}
			payloadBytes += featureBytes
			result.Features = append(result.Features, loaded)
			if stopAfter > 0 && len(result.Features) >= stopAfter {
				stoppedEarly = true
				break
			}
		}
		if stoppedEarly {
			break
		}
		decoder, reader, collectionClosed, _, err = resumeGeoJSONCollectionDecoder(featureReader)
		if err != nil {
			return core.Layer{}, err
		}
		memberNeedsComma = false
		if collectionClosed {
			break
		}
	}
	if !stoppedEarly {
		if !collectionClosed {
			return core.Layer{}, fmt.Errorf("GeoJSON %q has no closing object delimiter", path)
		}
		if err := ensureGeoJSONStreamEOF(nil, reader); err != nil {
			return core.Layer{}, err
		}
	}
	if !featuresSeen || (!typeSeen && !stoppedEarly) {
		return core.Layer{}, fmt.Errorf("GeoJSON %q has no features array", path)
	}
	if result.Name == "" {
		result.Name = geoJSONLayerName(path, "")
	}
	if result.CRS.AuthorityCode == "" {
		result.CRS.AuthorityCode = "EPSG:4326"
	}
	return result, nil
}

func geoJSONGeometryBounds(raw json.RawMessage) ([4]float64, bool, error) {
	coordinateCount := 0
	return geoJSONGeometryBoundsCounted(raw, &coordinateCount)
}

func countGeoJSONGeometryCollectionMembers(raw []byte, limit int) (int, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return 0, fmt.Errorf("GeometryCollection must be an object")
	}
	memberCount := 0
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return 0, fmt.Errorf("read GeometryCollection member name: %w", err)
		}
		key, ok := keyToken.(string)
		if !ok {
			return 0, fmt.Errorf("GeometryCollection member name is not a string")
		}
		if key != "geometries" {
			if err := skipGeoJSONJSONValue(decoder, 0); err != nil {
				return 0, fmt.Errorf("skip GeometryCollection member %q: %w", key, err)
			}
			continue
		}
		arrayToken, err := decoder.Token()
		if err != nil {
			return 0, fmt.Errorf("read GeometryCollection geometries: %w", err)
		}
		if arrayToken == nil {
			continue
		}
		if arrayToken != json.Delim('[') {
			return 0, fmt.Errorf("GeometryCollection geometries must be an array or null")
		}
		for decoder.More() {
			memberCount++
			if memberCount > limit {
				return memberCount, fmt.Errorf("GeometryCollection exceeds the %d-member safety limit", maxGeoJSONGeometryCollectionMembers)
			}
			if err := skipGeoJSONJSONValue(decoder, 0); err != nil {
				return 0, fmt.Errorf("scan GeometryCollection member %d: %w", memberCount, err)
			}
		}
		if endToken, err := decoder.Token(); err != nil || endToken != json.Delim(']') {
			return 0, fmt.Errorf("finish GeometryCollection geometries array")
		}
	}
	if endToken, err := decoder.Token(); err != nil || endToken != json.Delim('}') {
		return 0, fmt.Errorf("finish GeometryCollection object")
	}
	return memberCount, nil
}

func skipGeoJSONJSONValue(decoder *json.Decoder, depth int) error {
	if depth > maxGeoJSONGeometrySkipDepth {
		return fmt.Errorf("JSON nesting safety limit exceeded (%d levels)", maxGeoJSONGeometrySkipDepth)
	}
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		for decoder.More() {
			if _, err := decoder.Token(); err != nil {
				return err
			}
			if err := skipGeoJSONJSONValue(decoder, depth+1); err != nil {
				return err
			}
		}
		endToken, err := decoder.Token()
		if err != nil {
			return err
		}
		if endToken != json.Delim('}') {
			return fmt.Errorf("expected closing object delimiter")
		}
	case '[':
		for decoder.More() {
			if err := skipGeoJSONJSONValue(decoder, depth+1); err != nil {
				return err
			}
		}
		endToken, err := decoder.Token()
		if err != nil {
			return err
		}
		if endToken != json.Delim(']') {
			return fmt.Errorf("expected closing array delimiter")
		}
	default:
		return fmt.Errorf("unexpected JSON delimiter %q", delimiter)
	}
	return nil
}

func geoJSONGeometryBoundsCounted(raw json.RawMessage, coordinateCount *int) ([4]float64, bool, error) {
	collectionMemberCount := 0
	return geoJSONGeometryBoundsAtDepth(raw, coordinateCount, &collectionMemberCount, 0)
}

func geoJSONGeometryBoundsAtDepth(raw json.RawMessage, coordinateCount, collectionMemberCount *int, depth int) ([4]float64, bool, error) {
	if depth > maxGeoJSONGeometryCollectionDepth {
		return [4]float64{}, false, fmt.Errorf("GeoJSON GeometryCollection exceeds the %d-level nesting safety limit", maxGeoJSONGeometryCollectionDepth)
	}
	if bounds, hasCoordinates, handled := geoJSONPointBoundsFast(raw); handled {
		if hasCoordinates {
			*coordinateCount++
			if *coordinateCount > maxGeoJSONGeometryCoordinates {
				return [4]float64{}, false, fmt.Errorf("GeoJSON geometry exceeds the %d-coordinate safety limit", maxGeoJSONGeometryCoordinates)
			}
		}
		return bounds, hasCoordinates, nil
	}
	if bytes.Contains(raw, []byte("GeometryCollection")) || bytes.Contains(raw, []byte(`\u`)) {
		var kind struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(raw, &kind); err != nil {
			return [4]float64{}, false, err
		}
		if kind.Type == "GeometryCollection" {
			remaining := maxGeoJSONGeometryCollectionMembers - *collectionMemberCount
			if _, err := countGeoJSONGeometryCollectionMembers(raw, remaining); err != nil {
				return [4]float64{}, false, err
			}
		}
	}
	var geometry struct {
		Type        string            `json:"type"`
		Coordinates json.RawMessage   `json:"coordinates"`
		Geometries  []json.RawMessage `json:"geometries"`
	}
	if err := json.Unmarshal(raw, &geometry); err != nil {
		return [4]float64{}, false, err
	}
	var bounds [4]float64
	hasCoordinates := false
	if geometry.Type == "GeometryCollection" {
		if len(geometry.Geometries) > maxGeoJSONGeometryCollectionMembers-*collectionMemberCount {
			return bounds, false, fmt.Errorf("GeometryCollection exceeds the %d-member safety limit", maxGeoJSONGeometryCollectionMembers)
		}
		*collectionMemberCount += len(geometry.Geometries)
		for _, child := range geometry.Geometries {
			childBounds, childHasCoordinates, childErr := geoJSONGeometryBoundsAtDepth(child, coordinateCount, collectionMemberCount, depth+1)
			if childErr != nil {
				return [4]float64{}, false, childErr
			}
			if childHasCoordinates {
				if !hasCoordinates {
					bounds, hasCoordinates = childBounds, true
				} else {
					bounds[0], bounds[1] = math.Min(bounds[0], childBounds[0]), math.Min(bounds[1], childBounds[1])
					bounds[2], bounds[3] = math.Max(bounds[2], childBounds[2]), math.Max(bounds[3], childBounds[3])
				}
			}
		}
		return bounds, hasCoordinates, nil
	}
	if geometry.Type == "" {
		return bounds, false, fmt.Errorf("geometry type is missing")
	}
	if len(geometry.Coordinates) == 0 || bytes.Equal(bytes.TrimSpace(geometry.Coordinates), []byte("null")) {
		return bounds, false, nil
	}
	coordinateDepth := 0
	switch geometry.Type {
	case "MultiPoint", "LineString":
		coordinateDepth = 1
	case "MultiLineString", "Polygon":
		coordinateDepth = 2
	case "MultiPolygon":
		coordinateDepth = 3
	case "Point":
	default:
		return bounds, false, fmt.Errorf("unsupported GeoJSON geometry type %q", geometry.Type)
	}
	return geoJSONCoordinateBounds(geometry.Coordinates, coordinateDepth, coordinateCount)
}

// geoJSONCoordinateBounds walks coordinate arrays token-by-token. This keeps a
// large ring from expanding into several nested Go slice trees during the
// collection-wide metadata/index scan.
func geoJSONCoordinateBounds(raw []byte, depth int, coordinateCount *int) ([4]float64, bool, error) {
	return geoJSONCoordinateBoundsWithLimit(raw, depth, coordinateCount, maxGeoJSONGeometryCoordinates)
}

func geoJSONCoordinateBoundsWithLimit(raw []byte, depth int, coordinateCount *int, limit int) ([4]float64, bool, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	token, err := decoder.Token()
	if err != nil {
		return [4]float64{}, false, err
	}
	if token != json.Delim('[') {
		return [4]float64{}, false, fmt.Errorf("coordinates must be nested arrays")
	}
	var bounds [4]float64
	hasBounds := false
	var scanArray func(int) error
	scanArray = func(nesting int) error {
		if nesting == 0 {
			var first, second float64
			ordinateCount := 0
			for {
				value, tokenErr := decoder.Token()
				if tokenErr != nil {
					return tokenErr
				}
				if delimiter, ok := value.(json.Delim); ok && delimiter == ']' {
					break
				}
				var ordinate float64
				if value != nil {
					number, ok := value.(json.Number)
					if !ok {
						return fmt.Errorf("coordinate ordinate must be a number")
					}
					ordinate, tokenErr = number.Float64()
					if tokenErr != nil {
						return tokenErr
					}
				}
				switch ordinateCount {
				case 0:
					first = ordinate
				case 1:
					second = ordinate
				}
				ordinateCount++
				if ordinateCount > maxGeoJSONPositionOrdinates {
					return fmt.Errorf("coordinate position exceeds the %d-ordinate safety limit", maxGeoJSONPositionOrdinates)
				}
			}
			if ordinateCount < 2 {
				return fmt.Errorf("coordinate position has fewer than two ordinates")
			}
			*coordinateCount++
			if *coordinateCount > limit {
				return fmt.Errorf("GeoJSON geometry exceeds the %d-coordinate safety limit", limit)
			}
			if math.IsNaN(first) || math.IsInf(first, 0) || math.IsNaN(second) || math.IsInf(second, 0) {
				return fmt.Errorf("coordinate position is not finite")
			}
			if !hasBounds {
				bounds, hasBounds = [4]float64{first, second, first, second}, true
			} else {
				bounds[0], bounds[1] = math.Min(bounds[0], first), math.Min(bounds[1], second)
				bounds[2], bounds[3] = math.Max(bounds[2], first), math.Max(bounds[3], second)
			}
			return nil
		}
		for {
			value, tokenErr := decoder.Token()
			if tokenErr != nil {
				return tokenErr
			}
			if delimiter, ok := value.(json.Delim); ok {
				if delimiter == ']' {
					return nil
				}
				if delimiter == '[' {
					if err := scanArray(nesting - 1); err != nil {
						return err
					}
					continue
				}
			}
			if value != nil {
				return fmt.Errorf("coordinate nesting does not match geometry type")
			}
		}
	}
	if err := scanArray(depth); err != nil {
		return [4]float64{}, false, err
	}
	if _, err := decoder.Token(); err != io.EOF {
		if err == nil {
			return [4]float64{}, false, fmt.Errorf("unexpected value after coordinates")
		}
		return [4]float64{}, false, err
	}
	return bounds, hasBounds, nil
}

// geoJSONPointBoundsFast handles common compact 2D points without allocating
// nested coordinate slices. Any escaped/unknown shape falls back to the full
// JSON decoder, so this fast path does not weaken validation.
func geoJSONPointBoundsFast(raw []byte) ([4]float64, bool, bool) {
	index := skipGeoJSONSpace(raw, 0)
	if index >= len(raw) || raw[index] != '{' {
		return [4]float64{}, false, false
	}
	index++
	pointType, typeSeen := false, false
	commaPending := false
	coordinatesStart, coordinatesEnd := -1, -1
	for {
		index = skipGeoJSONSpace(raw, index)
		if index >= len(raw) {
			return [4]float64{}, false, false
		}
		if raw[index] == '}' {
			if commaPending {
				return [4]float64{}, false, false
			}
			break
		}
		keyStart := index
		keyEnd, ok := scanGeoJSONStringEnd(raw, index)
		if !ok {
			return [4]float64{}, false, false
		}
		commaPending = false
		key := raw[keyStart:keyEnd]
		if bytes.IndexByte(key, '\\') >= 0 {
			return [4]float64{}, false, false
		}
		index = skipGeoJSONSpace(raw, keyEnd)
		if index >= len(raw) || raw[index] != ':' {
			return [4]float64{}, false, false
		}
		valueStart := skipGeoJSONSpace(raw, index+1)
		valueEnd, valid := scanGeoJSONValueEnd(raw, valueStart)
		if !valid {
			return [4]float64{}, false, false
		}
		switch {
		case bytes.Equal(key, []byte(`"type"`)):
			typeSeen = true
			pointType = bytes.Equal(bytes.TrimSpace(raw[valueStart:valueEnd]), []byte(`"Point"`))
		case bytes.Equal(key, []byte(`"coordinates"`)):
			coordinatesStart, coordinatesEnd = valueStart, valueEnd
		}
		index = skipGeoJSONSpace(raw, valueEnd)
		if index < len(raw) && raw[index] == ',' {
			index++
			commaPending = true
			continue
		}
		if index < len(raw) && raw[index] == '}' {
			break
		}
		return [4]float64{}, false, false
	}
	if skipGeoJSONSpace(raw, index+1) != len(raw) {
		return [4]float64{}, false, false
	}
	if !typeSeen || !pointType {
		return [4]float64{}, false, false
	}
	if coordinatesStart < 0 {
		return [4]float64{}, false, true
	}
	coordinates := bytes.TrimSpace(raw[coordinatesStart:coordinatesEnd])
	if bytes.Equal(coordinates, []byte("null")) {
		return [4]float64{}, false, true
	}
	index = skipGeoJSONSpace(coordinates, 0)
	if index >= len(coordinates) || coordinates[index] != '[' {
		return [4]float64{}, false, false
	}
	index = skipGeoJSONSpace(coordinates, index+1)
	firstStart := index
	for index < len(coordinates) && coordinates[index] != ',' && coordinates[index] != ']' && !isGeoJSONSpace(coordinates[index]) {
		index++
	}
	firstEnd := index
	index = skipGeoJSONSpace(coordinates, index)
	if firstStart == firstEnd || index >= len(coordinates) || coordinates[index] != ',' {
		return [4]float64{}, false, false
	}
	index = skipGeoJSONSpace(coordinates, index+1)
	secondStart := index
	for index < len(coordinates) && coordinates[index] != ',' && coordinates[index] != ']' && !isGeoJSONSpace(coordinates[index]) {
		index++
	}
	secondEnd := index
	index = skipGeoJSONSpace(coordinates, index)
	if secondStart == secondEnd || index >= len(coordinates) || coordinates[index] != ']' {
		// Higher-dimensional points and malformed values use the validating path.
		return [4]float64{}, false, false
	}
	index = skipGeoJSONSpace(coordinates, index+1)
	if index != len(coordinates) {
		return [4]float64{}, false, false
	}
	var x, y float64
	if json.Unmarshal(coordinates[firstStart:firstEnd], &x) != nil ||
		json.Unmarshal(coordinates[secondStart:secondEnd], &y) != nil ||
		math.IsNaN(x) || math.IsInf(x, 0) || math.IsNaN(y) || math.IsInf(y, 0) {
		return [4]float64{}, false, false
	}
	return [4]float64{x, y, x, y}, true, true
}

func isGeoJSONSpace(value byte) bool {
	return value == ' ' || value == '\t' || value == '\r' || value == '\n'
}

func skipGeoJSONSpace(data []byte, index int) int {
	for index < len(data) && isGeoJSONSpace(data[index]) {
		index++
	}
	return index
}

func scanGeoJSONStringEnd(data []byte, start int) (int, bool) {
	if start >= len(data) || data[start] != '"' {
		return 0, false
	}
	for index := start + 1; index < len(data); index++ {
		switch data[index] {
		case '\\':
			index++
		case '"':
			return index + 1, true
		}
	}
	return 0, false
}

func scanGeoJSONValueEnd(data []byte, start int) (int, bool) {
	if start >= len(data) {
		return 0, false
	}
	if data[start] == '"' {
		return scanGeoJSONStringEnd(data, start)
	}
	if data[start] != '[' && data[start] != '{' {
		index := start
		for index < len(data) && data[index] != ',' && data[index] != ']' && data[index] != '}' && !isGeoJSONSpace(data[index]) {
			index++
		}
		return index, index > start
	}
	depth := 0
	inString, escaped := false, false
	for index := start; index < len(data); index++ {
		value := data[index]
		if inString {
			if escaped {
				escaped = false
			} else if value == '\\' {
				escaped = true
			} else if value == '"' {
				inString = false
			}
			continue
		}
		switch value {
		case '"':
			inString = true
		case '[', '{':
			depth++
		case ']', '}':
			depth--
			if depth == 0 {
				return index + 1, true
			}
			if depth < 0 {
				return 0, false
			}
		}
	}
	return 0, false
}

func geoJSONFieldSchema(properties map[string]any) []core.Field {
	names := make([]string, 0, len(properties))
	for name := range properties {
		names = append(names, name)
	}
	sort.Strings(names)
	fields := make([]core.Field, 0, len(names))
	for _, name := range names {
		typeOfField := core.FieldTypeText
		switch properties[name].(type) {
		case float64, json.Number:
			typeOfField = core.FieldTypeNumber
		case bool:
			typeOfField = core.FieldTypeBool
		}
		fields = append(fields, core.Field{Name: name, Type: typeOfField})
	}
	return fields
}

func geoJSONCRS(raw json.RawMessage) string {
	var crs struct {
		Properties struct {
			Name string `json:"name"`
		} `json:"properties"`
	}
	if json.Unmarshal(raw, &crs) != nil || crs.Properties.Name == "" {
		return ""
	}
	name := crs.Properties.Name
	if index := strings.LastIndex(strings.ToUpper(name), "EPSG:"); index >= 0 {
		code := strings.Trim(name[index+len("EPSG:"):], "/# ")
		if code != "" {
			return "EPSG:" + code
		}
	}
	return name
}
