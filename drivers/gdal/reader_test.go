//go:build native

package gdal

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unsafe"

	"gogis/internal/core"

	"github.com/airbusgeo/godal"
)

func TestInitialFeatureCapacityIsBounded(t *testing.T) {
	for _, test := range []struct {
		count int
		want  int
	}{{-1, 0}, {0, 0}, {12, 12}, {maxInitialFeatureCapacity, maxInitialFeatureCapacity}, {maxInitialFeatureCapacity * 100, maxInitialFeatureCapacity}} {
		if got := initialFeatureCapacity(test.count); got != test.want {
			t.Errorf("initialFeatureCapacity(%d) = %d, want %d", test.count, got, test.want)
		}
	}
}

func TestReaderImportsDXFThroughOGR(t *testing.T) {
	path := filepath.Join("..", "..", "testdata", "ares", "sample-utf8.dxf")
	layer, err := (Reader{}).Open(context.Background(), path, "entities")
	if err != nil {
		t.Fatalf("open DXF entities: %v", err)
	}
	if len(layer.Features) != 4 {
		t.Fatalf("DXF feature count = %d, want 4", len(layer.Features))
	}
	for index, feature := range layer.Features {
		if feature.Geometry == nil {
			t.Errorf("DXF feature %d has no geometry", index)
		}
	}
}

func TestValidateSpatialWindowRejectsNonFiniteAndReversedBounds(t *testing.T) {
	for _, bounds := range [][4]float64{
		{math.NaN(), 0, 1, 1},
		{0, math.Inf(1), 1, 1},
		{0, 0, math.Inf(-1), 1},
		{2, 0, 1, 1},
	} {
		if err := validateSpatialWindow(bounds); err == nil {
			t.Errorf("invalid bounds %v were accepted", bounds)
		}
	}
	if err := validateSpatialWindow([4]float64{0, 0, 1, 1}); err != nil {
		t.Fatalf("finite ordered bounds rejected: %v", err)
	}
}

func TestBoundedJSONMemberDecoderPreservesStream(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader(`{"type":"FeatureCollection","features":[]}`))
	decoder := json.NewDecoder(reader)
	if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
		t.Fatalf("root token = %v, %v", token, err)
	}
	key, reader, closed, _, err := decodeBoundedJSONMember(decoder, reader, maxGeoJSONMetadataBytes, false)
	if err != nil || closed || key != "type" {
		t.Fatalf("first key = %v, %v", key, err)
	}
	decoder = json.NewDecoder(reader)
	value, reader, _, err := decodeBoundedJSONRaw(decoder, reader, maxGeoJSONMetadataBytes)
	if err != nil || string(value) != `"FeatureCollection"` {
		t.Fatalf("first value = %s, %v", value, err)
	}
	decoder = json.NewDecoder(reader)
	key, reader, closed, _, err = decodeBoundedJSONMember(decoder, reader, maxGeoJSONMetadataBytes, true)
	if err != nil || closed || key != "features" {
		t.Fatalf("second key = %v, %v", key, err)
	}
	decoder = json.NewDecoder(reader)
	if token, err := decoder.Token(); err != nil || token != json.Delim('[') {
		t.Fatalf("features value token = %v, %v", token, err)
	}
}

func TestReaderRejectsNonFiniteSpatialWindowBeforeOpeningSource(t *testing.T) {
	_, err := (Reader{}).OpenWindow(context.Background(), "does-not-exist.geojson", "", [4]float64{math.NaN(), 0, 1, 1})
	if err == nil || !strings.Contains(err.Error(), "coordinates must be finite") {
		t.Fatalf("OpenWindow error = %v; want finite-coordinate validation", err)
	}
}

func TestReaderOpensGeoJSONFixtureThroughGDAL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "roads.geojson")
	fixture := `{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"name":"한글 도로","speed":40},"geometry":{"type":"Point","coordinates":[127.1,37.4]}}]}`
	if err := os.WriteFile(path, []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}

	layer, err := (Reader{}).Open(context.Background(), path, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(layer.Features) != 1 {
		t.Fatalf("features = %d, want 1", len(layer.Features))
	}
	if got := layer.Features[0].Properties["name"]; got != "한글 도로" {
		t.Fatalf("name = %v", got)
	}
}

func TestGeoJSONSingleFeatureSizeLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oversized.geojson")
	feature := `{"type":"Feature","properties":{"payload":"` + strings.Repeat("x", 1<<20) + `"},"geometry":{"type":"Point","coordinates":[1,2]}}`
	collection := `{"type":"FeatureCollection","features":[` + feature + `]}`
	if err := os.WriteFile(path, []byte(collection), 0o600); err != nil {
		t.Fatal(err)
	}
	registerDrivers()
	dataset, err := openDatasetWithGeoJSONLimit(path, "", 1)
	if dataset != nil {
		_ = dataset.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "too complex/large") {
		t.Fatalf("opening oversized feature error = %v, want GDAL size-limit error", err)
	}
}

func TestReaderOpenRejectsOversizedGeoJSONSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "large.geojson")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := bufio.NewWriter(file)
	_, _ = writer.WriteString(`{"type":"FeatureCollection","features":[`)
	for i := 0; i <= maxMaterializedSnapshotFeatures; i++ {
		if i > 0 {
			_, _ = writer.WriteString(",")
		}
		_, _ = fmt.Fprintf(writer, `{"type":"Feature","properties":{"n":%d},"geometry":{"type":"Point","coordinates":[%d,0]}}`, i, i)
	}
	_, _ = writer.WriteString("]}")
	if err := writer.Flush(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	_, err = (Reader{}).Open(context.Background(), path, "")
	if err == nil || !strings.Contains(err.Error(), "in-memory snapshot limit") {
		t.Fatalf("Open oversized GeoJSON error = %v, want snapshot-limit error", err)
	}
}

func TestAttributeSessionGeometrySnapshotsEnforceFeatureLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "large-session.geojson")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := bufio.NewWriter(file)
	_, _ = writer.WriteString(`{"type":"FeatureCollection","features":[`)
	for i := 0; i <= maxMaterializedSnapshotFeatures; i++ {
		if i > 0 {
			_, _ = writer.WriteString(",")
		}
		_, _ = fmt.Fprintf(writer, `{"type":"Feature","properties":null,"geometry":{"type":"Point","coordinates":[%d,0]}}`, i)
	}
	_, _ = writer.WriteString("]}")
	if err := writer.Flush(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	session, err := OpenAttributeSession(path)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if _, err := session.OpenAllGeometryOnly(context.Background()); err == nil || !strings.Contains(err.Error(), "in-memory snapshot limit") {
		t.Fatalf("OpenAllGeometryOnly oversized GeoJSON error = %v, want snapshot-limit error", err)
	}
	if _, err := session.OpenGeometryOnly(context.Background(), ""); err == nil || !strings.Contains(err.Error(), "in-memory snapshot limit") {
		t.Fatalf("OpenGeometryOnly oversized GeoJSON error = %v, want snapshot-limit error", err)
	}
	bounds := [4]float64{-1, -1, float64(maxMaterializedSnapshotFeatures + 1), 1}
	if _, err := session.OpenWindow(context.Background(), "", bounds, false); err == nil || !strings.Contains(err.Error(), "exceeds the limit of 100000 features") {
		t.Fatalf("default OpenWindow oversized result error = %v, want safe feature-budget error", err)
	}
	if _, err := (Reader{}).OpenWindowGeometryOnly(context.Background(), path, "", bounds); err == nil || !strings.Contains(err.Error(), "exceeds the limit of 100000 features") {
		t.Fatalf("Reader.OpenWindowGeometryOnly oversized result error = %v, want safe feature-budget error", err)
	}
}

func TestGeoJSONSnapshotRejectsOversizedSingleFeatureBeforeGeometryDecode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wide-feature.geojson")
	feature := `{"type":"Feature","properties":{"payload":"` + strings.Repeat("x", maxMaterializedSnapshotFeatureBytes) + `"},"geometry":{"type":"Point","coordinates":[1,2]}}`
	if err := os.WriteFile(path, []byte(`{"type":"FeatureCollection","features":[`+feature+`]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := (Reader{}).Open(context.Background(), path, "")
	if err == nil || !strings.Contains(err.Error(), "feature limit") {
		t.Fatalf("Open oversized single-feature error = %v, want per-feature limit error", err)
	}
}

func TestFeaturePayloadEstimateIncludesNestedGeoJSONProperties(t *testing.T) {
	nested := map[string]any{"array": []any{"a long string value", float64(3), map[string]any{"leaf": "nested value"}}}
	feature := core.Feature{Properties: map[string]any{"outer": nested}}
	got := estimateFeaturePayloadBytes(feature)
	if got < 200 {
		t.Fatalf("nested property estimate = %d bytes, want conservative container/key/value overhead", got)
	}
}

func TestFeaturePayloadEstimateBoundsNestedAndCyclicProperties(t *testing.T) {
	var nested any = "leaf"
	for range 50_000 {
		nested = []any{nested}
	}
	if got := estimateFeaturePayloadBytes(core.Feature{Properties: map[string]any{"deep": nested}}); got <= 50_000 {
		t.Fatalf("deep property estimate = %d bytes, want container overhead to be included", got)
	}

	cyclic := map[string]any{}
	cyclic["self"] = cyclic
	if got := estimateFeaturePayloadBytes(core.Feature{Properties: map[string]any{"cycle": cyclic}}); got != int64(^uint64(0)>>1) {
		t.Fatalf("cyclic property estimate = %d, want saturated estimate", got)
	}
}

func TestFeaturePayloadEstimateBoundsWidePropertyWorklist(t *testing.T) {
	const saturation = int64(^uint64(0) >> 1)
	wide := make([]any, (1<<16)+1)
	if got := estimateFeaturePayloadBytes(core.Feature{Properties: map[string]any{"wide": wide}}); got != saturation {
		t.Fatalf("wide property estimate = %d, want saturated estimate %d", got, saturation)
	}
	wideStrings := make([]string, (1<<16)+1)
	if got := estimateFeaturePayloadBytes(core.Feature{Properties: map[string]any{"wide": wideStrings}}); got != saturation {
		t.Fatalf("wide string property estimate = %d, want saturated estimate %d", got, saturation)
	}
	wideMap := make(map[string]any, (1<<16)+1)
	for index := 0; index < cap(wide); index++ {
		wideMap[fmt.Sprint(index)] = nil
	}
	if got := estimateFeaturePayloadBytes(core.Feature{Properties: map[string]any{"wide": wideMap}}); got != saturation {
		t.Fatalf("wide map property estimate = %d, want saturated estimate %d", got, saturation)
	}
	topLevel := make(map[string]any, (1<<16)+1)
	for index := 0; index < (1<<16)+1; index++ {
		topLevel[fmt.Sprint(index)] = nil
	}
	if got := estimateFeaturePayloadBytes(core.Feature{Properties: topLevel}); got != saturation {
		t.Fatalf("wide top-level properties estimate = %d, want saturated estimate %d", got, saturation)
	}
}

func TestReaderEncodingOverridesShapefileCPGPerOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "roads.shp")
	layer := core.Layer{Name: "roads", CRS: core.CRS{AuthorityCode: "EPSG:4326"}, Fields: []core.Field{{Name: "name", Type: core.FieldTypeText}}, Features: []core.Feature{{
		ID: 1, Geometry: core.WKTGeometry{WKT: "POINT (127 37)"}, Properties: map[string]any{"name": "한글 도로"},
	}}}
	if err := (Writer{}).Write(context.Background(), path, layer); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(strings.TrimSuffix(path, filepath.Ext(path))+".cpg", []byte("CP949\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := (Reader{Encoding: "UTF-8"}).Open(context.Background(), path, "roads")
	if err != nil {
		t.Fatal(err)
	}
	if value := got.Features[0].Properties["name"]; value != "한글 도로" {
		t.Fatalf("explicitly decoded value = %v; layer=%#v", value, got)
	}
	session, err := OpenAttributeSession(path, "UTF-8")
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	page, total, err := session.OpenAttributePage(context.Background(), "roads", 0, 10)
	if err != nil || total != 1 || page.Features[0].Properties["name"] != "한글 도로" {
		t.Fatalf("encoded attribute page=%#v total=%d err=%v", page, total, err)
	}
}

func TestReaderEncodingIsOnlyAppliedToShapefileDriver(t *testing.T) {
	path := filepath.Join(t.TempDir(), "roads.gpkg")
	if err := (Writer{}).Write(context.Background(), path, core.Layer{Name: "roads", CRS: core.CRS{AuthorityCode: "EPSG:4326"}, Features: []core.Feature{{
		ID: 1, Geometry: core.WKTGeometry{WKT: "POINT (127 37)"}, Properties: map[string]any{"name": "도로"},
	}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := (Reader{Encoding: "CP949"}).Open(context.Background(), path, "roads"); err != nil {
		t.Fatalf("encoding option leaked to GeoPackage open: %v", err)
	}
	if _, err := openDataset("roads.shp", "bad\nENCODING=OTHER"); err == nil {
		t.Fatal("invalid option injection was accepted")
	}
}

func TestReaderOpenWindowPushesSpatialFilterThroughGDAL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "roads.geojson")
	fixture := `{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"name":"inside"},"geometry":{"type":"Point","coordinates":[127.1,37.4]}},{"type":"Feature","properties":{"name":"outside"},"geometry":{"type":"Point","coordinates":[128.1,38.4]}}]}`
	if err := os.WriteFile(path, []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}

	layer, err := (Reader{}).OpenWindow(context.Background(), path, "", [4]float64{127, 37, 127.2, 37.6})
	if err != nil {
		t.Fatal(err)
	}
	if len(layer.Features) != 1 {
		t.Fatalf("features = %d, want 1", len(layer.Features))
	}
	if got := layer.Features[0].Properties["name"]; got != "inside" {
		t.Fatalf("name = %v, want inside", got)
	}
}

func TestReaderOpenWindowStreamsGeoJSONFeatureCollection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "roads.geojson")
	fixture := `{"type":"FeatureCollection","name":"roads","features":[{"type":"Feature","properties":{"name":"outside","speed":10},"geometry":{"type":"Point","coordinates":[0,0]}},{"type":"Feature","properties":{"name":"inside","speed":40,"active":true},"geometry":{"type":"Point","coordinates":[127.1,37.4]}},{"type":"Feature","properties":{"name":"collection"},"geometry":{"type":"GeometryCollection","geometries":[{"type":"Point","coordinates":[127.2,37.5]},{"type":"LineString","coordinates":[[127.3,37.2],[127.4,37.6]]}]}}]}`
	if err := os.WriteFile(path, []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := (Reader{}).OpenWindow(context.Background(), path, "", [4]float64{127, 37, 127.25, 37.55})
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "roads" || got.CRS.AuthorityCode != "EPSG:4326" || len(got.Features) != 2 {
		t.Fatalf("streamed window metadata/features = %#v", got)
	}
	if got.Features[0].ID != 2 || got.Features[1].ID != 3 {
		t.Fatalf("streamed IDs = %d, %d; want original collection ordinals 2, 3", got.Features[0].ID, got.Features[1].ID)
	}
	if got.Features[0].Properties["name"] != "inside" || got.Features[0].Properties["active"] != true || len(got.Fields) != 3 {
		t.Fatalf("streamed properties/schema = %#v / %#v", got.Features[0].Properties, got.Fields)
	}
	if got.Features[1].Geometry.GeometryType() != "GEOMETRYCOLLECTION" {
		t.Fatalf("streamed geometry type = %q", got.Features[1].Geometry.GeometryType())
	}
}

func TestReaderOpenWindowStreamsGeoJSONGeometryOnlyAndEnforcesLimits(t *testing.T) {
	path := filepath.Join(t.TempDir(), "points.geojson")
	fixture := `{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"name":"a"},"geometry":{"type":"Point","coordinates":[1,1]}},{"type":"Feature","properties":{"name":"b"},"geometry":{"type":"Point","coordinates":[2,2]}}]}`
	if err := os.WriteFile(path, []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}
	geometryOnly, err := (Reader{}).OpenWindowGeometryOnly(context.Background(), path, "", [4]float64{0, 0, 3, 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(geometryOnly.Features) != 2 || geometryOnly.Features[0].Properties != nil || len(geometryOnly.Fields) != 0 {
		t.Fatalf("geometry-only streamed result = %#v", geometryOnly)
	}
	if _, err := readGeoJSONWindow(context.Background(), path, "", [4]float64{0, 0, 3, 3}, true, 1, 0); err == nil || !strings.Contains(err.Error(), "limit of 1 features") {
		t.Fatalf("streamed feature cap error = %v", err)
	}
}

func TestGeoJSONStreamingAttributeSession(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.geojson")
	fixture := `{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"name":"outside","speed":10},"geometry":{"type":"Point","coordinates":[0,0]}},{"type":"Feature","properties":{"name":"inside","speed":40},"geometry":{"type":"Point","coordinates":[127.1,37.4]}},{"type":"Feature","properties":{"name":"last","speed":50},"geometry":{"type":"LineString","coordinates":[[127,37],[127.2,37.5]]}}],"name":"session-layer","crs":{"type":"name","properties":{"name":"EPSG:5179"}}}`
	if err := os.WriteFile(path, []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}
	session, err := OpenAttributeSession(path)
	if err != nil {
		t.Fatal(err)
	}
	if !session.streamGeoJSON || session.dataset != nil {
		t.Fatal("GeoJSON FeatureCollection session must use bounded streaming rather than opening the raw GDAL dataset")
	}
	overviews, err := session.Inspect(context.Background())
	if err != nil || len(overviews) != 1 || overviews[0].Name != "session-layer" || overviews[0].CRS.AuthorityCode != "EPSG:5179" || overviews[0].FeatureCount != 3 || !overviews[0].HasBounds || overviews[0].Bounds != [4]float64{0, 0, 127.2, 37.5} {
		t.Fatalf("stream inspect = %#v, err=%v", overviews, err)
	}
	prefix, err := session.OpenGeometryPrefix(context.Background(), "session-layer", 1)
	if err != nil || len(prefix.Features) != 1 || prefix.Features[0].ID != 1 {
		t.Fatalf("stream prefix = %#v, err=%v", prefix, err)
	}
	window, err := session.OpenWindowWithLimits(context.Background(), "session-layer", [4]float64{127, 37, 128, 38}, true, 2, 1<<20)
	if err != nil || len(window.Features) != 2 || window.Features[0].ID != 2 || window.Features[1].ID != 3 {
		t.Fatalf("stream window = %#v, err=%v", window, err)
	}
	selected, err := session.OpenFeature(context.Background(), "session-layer", 3)
	if err != nil || selected.ID != 3 || selected.Properties["name"] != "last" || selected.Geometry.GeometryType() != "LINESTRING" {
		t.Fatalf("stream selected feature = %#v, err=%v", selected, err)
	}
	page, total, err := session.OpenAttributePage(context.Background(), "session-layer", 1, 1)
	if err != nil || total != 3 || len(page.Features) != 1 || page.Features[0].ID != 2 || page.Features[0].Properties["name"] != "inside" {
		t.Fatalf("stream attribute page = %#v total=%d err=%v", page, total, err)
	}
	if err := os.WriteFile(path, append([]byte(fixture), ' '), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := session.OpenWindow(context.Background(), "session-layer", [4]float64{0, 0, 1, 1}, false); err == nil || !strings.Contains(err.Error(), "changed after indexing") {
		t.Fatalf("modified indexed source error = %v", err)
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	if session.streamOverview != nil || session.streamIndex != nil || session.streamTailBlocks != nil ||
		session.streamSpatialIndex != nil || session.streamSpatialReady || session.streamIndexStamp != (geoJSONFileStamp{}) {
		t.Fatal("closed GeoJSON session retained indexed source state")
	}
	if _, err := session.Inspect(context.Background()); err == nil {
		t.Fatal("inspect succeeded after stream session close")
	}
}

func TestGeoJSONSessionCloseReleasesSpatialIndex(t *testing.T) {
	const featureCount = 50_000
	path := filepath.Join(t.TempDir(), "large-session.geojson")
	var content strings.Builder
	content.Grow(featureCount * 90)
	content.WriteString(`{"type":"FeatureCollection","features":[`)
	for ordinal := 0; ordinal < featureCount; ordinal++ {
		if ordinal != 0 {
			content.WriteByte(',')
		}
		fmt.Fprintf(&content, `{"type":"Feature","properties":{},"geometry":{"type":"Point","coordinates":[%d,%d]}}`, ordinal%250, ordinal/250)
	}
	content.WriteString(`]}`)
	if err := os.WriteFile(path, []byte(content.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	session, err := OpenAttributeSession(path)
	if err != nil {
		t.Fatal(err)
	}
	window, err := session.OpenWindowWithLimits(context.Background(), "", [4]float64{0, 100, 0, 100}, false, 2, 1<<20)
	if err != nil || len(window.Features) != 1 {
		t.Fatalf("large-session window features=%d err=%v", len(window.Features), err)
	}
	if session.streamSpatialIndex == nil || len(session.streamSpatialIndex.ordinals) == 0 {
		t.Fatal("large GeoJSON query did not build the spatial candidate index")
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	if session.streamIndex != nil || session.streamTailBlocks != nil || session.streamSpatialIndex != nil ||
		session.streamSpatialReady || session.streamIndexStamp != (geoJSONFileStamp{}) {
		t.Fatal("closed large GeoJSON session retained feature or spatial index memory")
	}
}

func TestGeoJSONFileStampDetectsSameSizeSameTimeReplacement(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "source.geojson")
	replacement := filepath.Join(directory, "replacement.geojson")
	backup := filepath.Join(directory, "original.geojson")
	if err := os.WriteFile(path, []byte("original content"), 0o600); err != nil {
		t.Fatal(err)
	}
	expected, err := geoJSONSourceStamp(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(replacement, []byte("replaced content"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(replacement, expected.file.ModTime(), expected.file.ModTime()); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path, backup); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, path); err != nil {
		t.Fatal(err)
	}
	actual, err := geoJSONSourceStamp(path)
	if err != nil {
		t.Fatal(err)
	}
	if expected.size != actual.size || expected.modTimeNS != actual.modTimeNS {
		t.Fatalf("replacement did not preserve size/time: before=%#v after=%#v", expected, actual)
	}
	if sameGeoJSONFileStamp(expected, actual) {
		t.Fatal("source stamp accepted a different file with the same size and modtime")
	}
	if err := validateGeoJSONSourceStamp(path, expected); err == nil {
		t.Fatal("source validation accepted an atomic replacement")
	}
}

func TestReadJSONFeatureObjectHandlesEscapesAndStopsAtConfiguredLimit(t *testing.T) {
	input := `{"properties":{"text":"escaped quote \" and delimiters } ] {"},"geometry":null},]`
	buffer := make([]byte, 0, 32)
	var offset int64
	got, atEnd, start, end, err := readJSONFeatureObject(bufio.NewReader(strings.NewReader(input)), &buffer, &offset, 256)
	if err != nil || atEnd || !strings.Contains(string(got), `delimiters } ] {`) {
		t.Fatalf("bounded feature scan = %q, end=%v, err=%v", got, atEnd, err)
	}
	if start != 0 || end != int64(len(got)) {
		t.Fatalf("feature offsets = [%d,%d), raw bytes=%d", start, end, len(got))
	}
	oversized := `{"properties":{"payload":"` + strings.Repeat("x", 1<<20) + `"}}]`
	buffer = buffer[:0]
	offset = 0
	if _, _, _, _, err := readJSONFeatureObject(bufio.NewReader(strings.NewReader(oversized)), &buffer, &offset, 128); err == nil || !strings.Contains(err.Error(), "feature limit") {
		t.Fatalf("oversized streamed feature error = %v", err)
	}
}

func TestGeoJSONGeometryBoundsCoversStandardCoordinateNesting(t *testing.T) {
	for _, test := range []struct {
		kind        string
		coordinates string
		want        [4]float64
	}{
		{kind: "Point", coordinates: `[127,37,10]`, want: [4]float64{127, 37, 127, 37}},
		{kind: "MultiPoint", coordinates: `[[1,2],[3,4]]`, want: [4]float64{1, 2, 3, 4}},
		{kind: "LineString", coordinates: `[[1,2],[3,4]]`, want: [4]float64{1, 2, 3, 4}},
		{kind: "MultiLineString", coordinates: `[[[1,2],[3,4]],[[5,6],[7,8]]]`, want: [4]float64{1, 2, 7, 8}},
		{kind: "Polygon", coordinates: `[[[1,2],[3,4],[5,6],[1,2]]]`, want: [4]float64{1, 2, 5, 6}},
		{kind: "MultiPolygon", coordinates: `[[[[1,2],[3,4],[1,2]]],[[[5,6],[7,8],[5,6]]]]`, want: [4]float64{1, 2, 7, 8}},
	} {
		bounds, hasCoordinates, err := geoJSONGeometryBounds([]byte(`{"type":"` + test.kind + `","coordinates":` + test.coordinates + `}`))
		if err != nil || !hasCoordinates || bounds != test.want {
			t.Errorf("%s bounds = %v, has=%v, err=%v; want %v", test.kind, bounds, hasCoordinates, err, test.want)
		}
	}
}

func TestGeoJSONGeometryBoundsHandlesCollectionsAndMalformedCoordinates(t *testing.T) {
	collection := []byte(`{"type":"GeometryCollection","geometries":[{"type":"Point","coordinates":[2,3]},{"type":"LineString","coordinates":[[1,5],[4,0]]}]}`)
	if bounds, hasCoordinates, err := geoJSONGeometryBounds(collection); err != nil || !hasCoordinates || bounds != ([4]float64{1, 0, 4, 5}) {
		t.Fatalf("geometry collection bounds = %v, has=%t, err=%v", bounds, hasCoordinates, err)
	}
	for _, fixture := range []string{
		`{"type":"Point","coordinates":[1]}`,
		`{"type":"LineString","coordinates":[[1,2],[3]]}`,
		`{"type":"Polygon","coordinates":[[[1,2],3]]}`,
		`{"type":"Point","coordinates":[1e999,2]}`,
		`{"type":"Point","coordinates":[1,2]} {}`,
	} {
		if _, _, err := geoJSONGeometryBounds([]byte(fixture)); err == nil {
			t.Errorf("malformed geometry accepted: %s", fixture)
		}
	}
}

func TestGeoJSONPointBoundsFastPathMatchesValidatedShapes(t *testing.T) {
	for _, fixture := range []struct {
		geometry string
		want     [4]float64
	}{
		{`{"type":"Point","coordinates":[127.25,37.5]}`, [4]float64{127.25, 37.5, 127.25, 37.5}},
		{` { "coordinates" : [ -1e2 , 2.5 ] , "name":"brace } in string", "type":"Point" } `, [4]float64{-100, 2.5, -100, 2.5}},
	} {
		bounds, hasCoordinates, err := geoJSONGeometryBounds([]byte(fixture.geometry))
		if err != nil || !hasCoordinates || bounds != fixture.want {
			t.Errorf("fast point bounds = %v, has=%t, err=%v; want %v", bounds, hasCoordinates, err, fixture.want)
		}
	}
	for _, fixture := range []string{
		`{"type":"Point","coordinates":[1,2],}`,
		`{"type":"Point","coordinates":[1,2]} {}`,
	} {
		if _, _, err := geoJSONGeometryBounds([]byte(fixture)); err == nil {
			t.Errorf("fallback geometry should reject malformed input: %s", fixture)
		}
	}
	if bounds, hasCoordinates, err := geoJSONGeometryBounds([]byte(`{"type":"Point","coordinates":[1,2,3]}`)); err != nil || !hasCoordinates || bounds != ([4]float64{1, 2, 1, 2}) {
		t.Fatalf("3D point fallback = %v, has=%t, err=%v", bounds, hasCoordinates, err)
	}
}

func TestGeoJSONCoordinateScannerBoundsComplexity(t *testing.T) {
	count := 0
	bounds, hasCoordinates, err := geoJSONCoordinateBoundsWithLimit([]byte(`[[1,2],[3,4]]`), 1, &count, 2)
	if err != nil || !hasCoordinates || bounds != ([4]float64{1, 2, 3, 4}) || count != 2 {
		t.Fatalf("bounded coordinates = %v, has=%t count=%d err=%v", bounds, hasCoordinates, count, err)
	}
	count = 0
	if _, _, err := geoJSONCoordinateBoundsWithLimit([]byte(`[[1,2],[3,4],[5,6]]`), 1, &count, 2); err == nil || !strings.Contains(err.Error(), "safety limit") {
		t.Fatalf("over-budget coordinate scan error = %v, want safety-limit error", err)
	}
	widePosition := `[` + strings.TrimSuffix(strings.Repeat("1,", maxGeoJSONPositionOrdinates+1), ",") + `]`
	if _, _, err := geoJSONCoordinateBoundsWithLimit([]byte(widePosition), 0, new(int), 2); err == nil || !strings.Contains(err.Error(), "ordinate safety limit") {
		t.Fatalf("over-wide coordinate position error = %v, want ordinate-limit error", err)
	}
}

func TestGeoJSONGeometryCollectionDepthLimit(t *testing.T) {
	geometry := `{"type":"Point","coordinates":[1,2]}`
	for range maxGeoJSONGeometryCollectionDepth + 1 {
		geometry = `{"type":"GeometryCollection","geometries":[` + geometry + `]}`
	}
	if _, _, err := geoJSONGeometryBounds([]byte(geometry)); err == nil || !strings.Contains(err.Error(), "nesting safety limit") {
		t.Fatalf("over-depth GeometryCollection error = %v, want nesting safety limit", err)
	}
	geometry = `{"type":"Point","coordinates":[1,2]}`
	for range maxGeoJSONGeometryCollectionDepth {
		geometry = `{"type":"GeometryCollection","geometries":[` + geometry + `]}`
	}
	if bounds, hasCoordinates, err := geoJSONGeometryBounds([]byte(geometry)); err != nil || !hasCoordinates || bounds != ([4]float64{1, 2, 1, 2}) {
		t.Fatalf("at-limit GeometryCollection = %v, has=%t, err=%v", bounds, hasCoordinates, err)
	}
}

func TestGeoJSONGeometryCollectionMemberLimitPrecedesSliceMaterialization(t *testing.T) {
	child := `{"type":"GeometryCollection","geometries":[]}`
	geometry := `{"type":"\u0047eometryCollection","geometries":[` +
		strings.TrimSuffix(strings.Repeat(child+",", maxGeoJSONGeometryCollectionMembers+1), ",") + `]}`
	if _, _, err := geoJSONGeometryBounds([]byte(geometry)); err == nil || !strings.Contains(err.Error(), "member safety limit") {
		t.Fatalf("over-budget GeometryCollection members error = %v; want member safety limit", err)
	}
}

func FuzzGeoJSONGeometryBoundsNoPanic(f *testing.F) {
	f.Add([]byte(`{"type":"Point","coordinates":[127,37]}`))
	f.Add([]byte(`{"type":"GeometryCollection","geometries":[]}`))
	f.Add([]byte(`{"type":"GeometryCollection","geometries":[{"type":"Point","coordinates":[1,2]}]}`))
	f.Add([]byte(`{"type":"Polygon","coordinates":[[[0,0],[1,0],[1,1],[0,0]]]}`))
	f.Fuzz(func(t *testing.T, raw []byte) {
		_, _, _ = geoJSONGeometryBounds(raw)
	})
}

func FuzzGeoJSONSequenceScannerNoPanic(f *testing.F) {
	f.Add([]byte(`{"type":"Feature","properties":{},"geometry":{"type":"Point","coordinates":[127,37]}}\n`))
	f.Add([]byte("\x1e{\"type\":\"Feature\",\"properties\":null,\"geometry\":null}\n"))
	f.Add([]byte("\x1e{\"type\":\"Feature\",\"geometry\":{\"type\":\"GeometryCollection\",\"geometries\":["))
	f.Add([]byte("\n\r\n\x1e{}\nnot-json\n"))
	f.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) > 1<<20 {
			t.Skip()
		}
		path := filepath.Join(t.TempDir(), "fuzz.geojsonl")
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		_, _, _, _ = scanGeoJSONSequence(context.Background(), path, nil)
	})
}

func TestGeoJSONStreamingRejectsDuplicateMembersAndTrailingData(t *testing.T) {
	for _, fixture := range []string{
		`{"type":"FeatureCollection","features":[],"features":[]}`,
		`{"type":"FeatureCollection","features":[]} trailing`,
	} {
		path := filepath.Join(t.TempDir(), "invalid.geojson")
		if err := os.WriteFile(path, []byte(fixture), 0o600); err != nil {
			t.Fatal(err)
		}
		session, err := OpenAttributeSession(path)
		if err != nil {
			t.Fatal(err)
		}
		_, inspectErr := session.Inspect(context.Background())
		_ = session.Close()
		if inspectErr == nil {
			t.Errorf("invalid FeatureCollection was accepted: %s", fixture)
		}
	}
}

func TestGeoJSONFeatureIndexRetainsBoundedPrefixAtConfiguredLimit(t *testing.T) {
	index := make([]geoJSONFeatureIndex, 0)
	var available bool
	const limit = 1_003
	for ordinal := 0; ordinal < limit; ordinal++ {
		index, available = appendGeoJSONFeatureIndex(index, geoJSONFeatureIndex{offset: int64(ordinal), length: 20}, limit)
		if !available || len(index) != ordinal+1 || cap(index) > limit {
			t.Fatalf("index after entry %d has len=%d cap=%d available=%v; want bounded append", ordinal, len(index), cap(index), available)
		}
	}
	index, available = appendGeoJSONFeatureIndex(index, geoJSONFeatureIndex{offset: 30, length: 40}, limit)
	if available || len(index) != limit || cap(index) != limit || index[0].offset != 0 {
		t.Fatalf("index after limit = len:%d cap:%d available=%v; want exact bounded prefix", len(index), cap(index), available)
	}
}

func TestGeoJSONFeatureIndexEntryStaysCompact(t *testing.T) {
	if size := unsafe.Sizeof(geoJSONFeatureIndex{}); size > 48 {
		t.Fatalf("GeoJSON index entry is %d bytes; want at most 48", size)
	}
}

func TestGeoJSONSpatialCandidateIndexMatchesLinearScan(t *testing.T) {
	index := make([]geoJSONFeatureIndex, 50_002)
	for ordinal := 0; ordinal < 50_000; ordinal++ {
		x := float64(ordinal%250) / 2.5
		y := float64(ordinal/250) / 5
		xMax, yMax := x, y
		if ordinal%97 == 0 {
			xMax, yMax = min(100, x+0.21), min(100, y+0.21)
		}
		index[ordinal] = geoJSONFeatureIndex{bounds: [4]float64{x, y, xMax, yMax}, valid: true}
	}
	index[50_000] = geoJSONFeatureIndex{bounds: [4]float64{0, 0, 100, 100}, valid: true}
	index[50_001] = geoJSONFeatureIndex{bounds: [4]float64{10, 10, 20, 20}}
	spatial := newGeoJSONSpatialCandidateIndex(index, [4]float64{0, 0, 100, 100}, true)
	if spatial == nil {
		t.Fatal("spatial candidate index was not built")
	}

	query := [4]float64{10, 10, 20, 20}
	candidates, indexed, err := spatial.query(context.Background(), query)
	if err != nil || !indexed {
		t.Fatalf("query indexed=%t err=%v", indexed, err)
	}
	var got []uint32
	for _, ordinal := range candidates {
		if geoJSONBoundsIntersect(index[ordinal].bounds, query) {
			got = append(got, ordinal)
		}
	}
	var want []uint32
	for ordinal, entry := range index {
		if entry.valid && geoJSONBoundsIntersect(entry.bounds, query) {
			want = append(want, uint32(ordinal))
		}
	}
	if len(got) != len(want) {
		t.Fatalf("candidate result count=%d, linear scan count=%d", len(got), len(want))
	}
	for ordinal := range want {
		if got[ordinal] != want[ordinal] {
			t.Fatalf("candidate[%d]=%d, linear scan=%d", ordinal, got[ordinal], want[ordinal])
		}
	}
	random := rand.New(rand.NewSource(42))
	for queryNumber := 0; queryNumber < 128; queryNumber++ {
		x, y := random.Float64()*95, random.Float64()*95
		query := [4]float64{x, y, x + random.Float64()*5, y + random.Float64()*5}
		candidates, indexed, err := spatial.query(context.Background(), query)
		if err != nil || !indexed {
			t.Fatalf("random query %d indexed=%t err=%v", queryNumber, indexed, err)
		}
		got = got[:0]
		for _, ordinal := range candidates {
			if index[ordinal].valid && geoJSONBoundsIntersect(index[ordinal].bounds, query) {
				got = append(got, ordinal)
			}
		}
		want = want[:0]
		for ordinal, entry := range index {
			if entry.valid && geoJSONBoundsIntersect(entry.bounds, query) {
				want = append(want, uint32(ordinal))
			}
		}
		if len(got) != len(want) {
			t.Fatalf("random query %d candidate count=%d, linear scan count=%d", queryNumber, len(got), len(want))
		}
		for ordinal := range want {
			if got[ordinal] != want[ordinal] {
				t.Fatalf("random query %d candidate[%d]=%d, linear scan=%d", queryNumber, ordinal, got[ordinal], want[ordinal])
			}
		}
	}

	outside, indexed, err := spatial.query(context.Background(), [4]float64{101, 101, 102, 102})
	if err != nil || !indexed || len(outside) != 0 {
		t.Fatalf("outside query candidates=%d indexed=%t err=%v", len(outside), indexed, err)
	}
	_, indexed, err = spatial.query(context.Background(), [4]float64{0, 0, 100, 100})
	if err != nil || indexed {
		t.Fatalf("wide query should select bounded linear fallback: indexed=%t err=%v", indexed, err)
	}
	if _, _, err := spatial.query(context.Background(), [4]float64{math.NaN(), 0, 1, 1}); err == nil {
		t.Fatal("non-finite query bounds were accepted")
	}
	malformed := append([]geoJSONFeatureIndex(nil), index...)
	malformed[123].bounds = [4]float64{2, 2, 1, 1}
	if candidateIndex := newGeoJSONSpatialCandidateIndex(malformed, [4]float64{0, 0, 100, 100}, true); candidateIndex != nil {
		t.Fatal("malformed feature bounds should disable the candidate index")
	}
}

func TestGeoJSONSpatialCandidateIndexFallsBackWhenOverflowIsDense(t *testing.T) {
	index := make([]geoJSONFeatureIndex, 50_000)
	for ordinal := range index {
		index[ordinal] = geoJSONFeatureIndex{bounds: [4]float64{0, 0, 100, 100}, valid: true}
	}
	if spatial := newGeoJSONSpatialCandidateIndex(index, [4]float64{0, 0, 100, 100}, true); spatial != nil {
		t.Fatal("dense oversized-feature list should disable the coarse spatial index")
	}
}

func TestGeoJSONSeqStreamingUsesBoundedRecordsAndPreservesMissingFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "points.geojsonl")
	content := "{\"type\":\"Feature\",\"geometry\":{\"type\":\"Point\",\"coordinates\":[1,2]},\"properties\":{\"name\":\"first\"}}\n" +
		"\x1e{\"type\":\"Feature\",\"properties\":{\"name\":\"second\"}}\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if !isGeoJSONStreamSource(path) {
		t.Fatal("GeoJSONSeq source did not route through the bounded streaming reader")
	}
	session, err := OpenAttributeSession(path)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if !session.streamGeoJSON || session.dataset != nil {
		t.Fatal("GeoJSONSeq session must use bounded streaming rather than opening the raw GDAL dataset")
	}
	overviews, err := session.Inspect(context.Background())
	if err != nil || len(overviews) != 1 || overviews[0].FeatureCount != 2 {
		t.Fatalf("GeoJSONSeq overview = %#v, %v; want one layer with two features", overviews, err)
	}
	window, err := session.OpenWindow(context.Background(), "", [4]float64{0, 0, 3, 3}, true)
	if err != nil || len(window.Features) != 1 || window.Features[0].Properties["name"] != "first" {
		t.Fatalf("GeoJSONSeq point window = %#v, %v; want only first point", window.Features, err)
	}
	page, total, err := session.OpenAttributePage(context.Background(), "", 1, 1)
	if err != nil || total != 2 || len(page.Features) != 1 || page.Features[0].Properties["name"] != "second" {
		t.Fatalf("GeoJSONSeq attribute page = %#v total=%d err=%v", page.Features, total, err)
	}
	selected, err := session.OpenFeature(context.Background(), "", 2)
	if err != nil || selected.Geometry != nil || selected.Properties["name"] != "second" {
		t.Fatalf("GeoJSONSeq feature without geometry = %#v, %v", selected, err)
	}
}

func TestGeoJSONSequenceSessionRetainsBoundedIndexPrefix(t *testing.T) {
	path := filepath.Join(t.TempDir(), "two.geojsonl")
	fixture := "{\"type\":\"Feature\",\"properties\":{},\"geometry\":{\"type\":\"Point\",\"coordinates\":[1,2]}}\n" +
		"{\"type\":\"Feature\",\"properties\":{},\"geometry\":{\"type\":\"Point\",\"coordinates\":[3,4]}}\n"
	if err := os.WriteFile(path, []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}
	session, err := OpenAttributeSession(path)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if applied, err := session.SetGeoJSONStreamIndexFeatureLimit(1); err != nil || !applied {
		t.Fatalf("set stream index limit applied=%t err=%v", applied, err)
	}
	overviews, err := session.Inspect(context.Background())
	if err != nil || len(overviews) != 1 || overviews[0].FeatureCount != 2 {
		t.Fatalf("inspect with capped index = %#v, %v", overviews, err)
	}
	if len(session.streamIndex) != 1 {
		t.Fatalf("index retained %d entries after exceeding one-entry limit; want bounded prefix of 1", len(session.streamIndex))
	}
	body, err := session.OpenWindow(context.Background(), "", [4]float64{0, 0, 5, 5}, false)
	if err != nil || len(body.Features) != 2 || body.Features[1].ID != 2 {
		t.Fatalf("unindexed window read features=%d err=%v", len(body.Features), err)
	}
	feature, err := session.OpenFeature(context.Background(), "", 2)
	if err != nil || feature.Properties["name"] != nil {
		t.Fatalf("tail GeoJSONSeq feature = %#v err=%v", feature, err)
	}
	page, total, err := session.OpenAttributePage(context.Background(), "", 1, 1)
	if err != nil || total != 2 || len(page.Features) != 1 || page.Features[0].ID != 2 {
		t.Fatalf("tail GeoJSONSeq attribute page = %#v total=%d err=%v", page.Features, total, err)
	}
}

func TestGeoJSONSequenceTailBlocksSkipNonIntersectingRecordRanges(t *testing.T) {
	const featureCount = 1 + geoJSONTailBlockFeatureCount + 2
	path := filepath.Join(t.TempDir(), "tail-blocks.geojsonl")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	for ordinal := 1; ordinal <= featureCount; ordinal++ {
		if _, err := fmt.Fprintf(file,
			"\x1e{\"type\":\"Feature\",\"properties\":{\"id\":%d},\"geometry\":{\"type\":\"Point\",\"coordinates\":[%d,0]}}\n",
			ordinal, ordinal); err != nil {
			_ = file.Close()
			t.Fatal(err)
		}
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	session, err := OpenAttributeSession(path)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if applied, err := session.SetGeoJSONStreamIndexFeatureLimit(1); err != nil || !applied {
		t.Fatalf("set prefix index limit applied=%t err=%v", applied, err)
	}
	if _, err := session.Inspect(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(session.streamTailBlocks) != 2 {
		t.Fatalf("GeoJSONSeq tail blocks = %d; want 2", len(session.streamTailBlocks))
	}
	window, err := session.OpenWindow(context.Background(), "", [4]float64{featureCount - 1, -1, featureCount, 1}, false)
	if err != nil || len(window.Features) != 2 || window.Features[0].ID != uint64(featureCount-1) ||
		window.Features[1].ID != uint64(featureCount) {
		t.Fatalf("last GeoJSONSeq tail-block window features=%#v err=%v", window.Features, err)
	}
	feature, err := session.OpenFeature(context.Background(), "", uint64(featureCount))
	if err != nil || feature.ID != uint64(featureCount) || feature.Properties["id"] != float64(featureCount) {
		t.Fatalf("last GeoJSONSeq tail-block feature=%#v err=%v", feature, err)
	}
}

func TestGeoJSONAttributeSessionSerializesConcurrentStreamRequests(t *testing.T) {
	path := filepath.Join(t.TempDir(), "concurrent.geojson")
	var content strings.Builder
	content.WriteString(`{"type":"FeatureCollection","features":[`)
	for ordinal := 1; ordinal <= 64; ordinal++ {
		if ordinal > 1 {
			content.WriteByte(',')
		}
		fmt.Fprintf(&content, `{"type":"Feature","properties":{"id":%d},"geometry":{"type":"Point","coordinates":[%d,0]}}`, ordinal, ordinal)
	}
	content.WriteString(`]}`)
	if err := os.WriteFile(path, []byte(content.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	session, err := OpenAttributeSession(path)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	const workers = 12
	var group sync.WaitGroup
	errors := make(chan error, workers)
	for worker := 0; worker < workers; worker++ {
		group.Add(1)
		go func(worker int) {
			defer group.Done()
			ctx := context.Background()
			switch worker % 4 {
			case 0:
				_, err := session.Inspect(ctx)
				errors <- err
			case 1:
				layer, err := session.OpenWindow(ctx, "", [4]float64{float64(worker), -1, float64(worker + 2), 1}, false)
				if err == nil && len(layer.Features) == 0 {
					err = fmt.Errorf("concurrent window returned no intersecting features")
				}
				errors <- err
			case 2:
				page, total, err := session.OpenAttributePage(ctx, "", worker, 2)
				if err == nil && (total != 64 || len(page.Features) != 2) {
					err = fmt.Errorf("concurrent page returned %d features, total %d", len(page.Features), total)
				}
				errors <- err
			case 3:
				feature, err := session.OpenFeature(ctx, "", uint64(worker+1))
				if err == nil && feature.ID != uint64(worker+1) {
					err = fmt.Errorf("concurrent feature lookup returned ID %d", feature.ID)
				}
				errors <- err
			}
		}(worker)
	}
	group.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Error(err)
		}
	}
}

func TestAttributeSessionWaitForLockHonorsCancellation(t *testing.T) {
	session := &AttributeSession{source: "locked.geojson", streamGeoJSON: true}
	session.mu.Lock()
	defer session.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := session.Inspect(ctx)
		result <- err
	}()
	cancel()

	select {
	case err := <-result:
		if err != context.Canceled {
			t.Fatalf("Inspect error = %v; want context.Canceled", err)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("Inspect did not stop promptly after cancellation while waiting for session lock")
	}
}

func TestGeoJSONPartialIndexStillQueriesTailFeatures(t *testing.T) {
	path := filepath.Join(t.TempDir(), "partial-index.geojson")
	content := `{"type":"FeatureCollection","features":[` +
		`{"type":"Feature","properties":{"name":"first"},"geometry":{"type":"Point","coordinates":[1,2]}},` +
		`{"type":"Feature","properties":{"name":"second"},"geometry":{"type":"Point","coordinates":[3,4]}},` +
		`{"type":"Feature","properties":{"name":"tail"},"geometry":{"type":"Point","coordinates":[30,40]}}]}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	session, err := OpenAttributeSession(path)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if applied, err := session.SetGeoJSONStreamIndexFeatureLimit(2); err != nil || !applied {
		t.Fatalf("set two-entry index limit applied=%t err=%v", applied, err)
	}
	overviews, err := session.Inspect(context.Background())
	if err != nil || len(overviews) != 1 || overviews[0].FeatureCount != 3 || len(session.streamIndex) != 2 {
		t.Fatalf("partial index overview=%#v index=%d err=%v", overviews, len(session.streamIndex), err)
	}
	if len(session.streamTailBlocks) != 1 || session.streamTailBlocks[0].firstOrdinal != 3 ||
		session.streamTailBlocks[0].featureCount != 1 || !geoJSONBoundsIntersect(session.streamTailBlocks[0].bounds, [4]float64{30, 40, 30, 40}) {
		t.Fatalf("partial-index tail blocks = %#v", session.streamTailBlocks)
	}
	window, err := session.OpenWindow(context.Background(), "", [4]float64{29, 39, 31, 41}, true)
	if err != nil || len(window.Features) != 1 || window.Features[0].ID != 3 || window.Features[0].Properties["name"] != "tail" {
		t.Fatalf("tail spatial window = %#v, %v", window.Features, err)
	}
	feature, err := session.OpenFeature(context.Background(), "", 3)
	if err != nil || feature.Properties["name"] != "tail" {
		t.Fatalf("tail feature lookup = %#v, %v", feature, err)
	}
	page, total, err := session.OpenAttributePage(context.Background(), "", 2, 1)
	if err != nil || total != 3 || len(page.Features) != 1 || page.Features[0].ID != 3 || page.Features[0].Properties["name"] != "tail" {
		t.Fatalf("tail attribute page = %#v total=%d err=%v", page.Features, total, err)
	}
}

func TestGeoJSONTailBlocksSkipNonIntersectingRecordRanges(t *testing.T) {
	const featureCount = 2 + 2*geoJSONTailBlockFeatureCount + 1
	path := filepath.Join(t.TempDir(), "tail-blocks.geojson")
	var content strings.Builder
	content.Grow(featureCount * 90)
	content.WriteString(`{"type":"FeatureCollection","features":[`)
	for ordinal := 1; ordinal <= featureCount; ordinal++ {
		if ordinal > 1 {
			content.WriteByte(',')
		}
		fmt.Fprintf(&content, `{"type":"Feature","properties":{"id":%d},"geometry":{"type":"Point","coordinates":[%d,0]}}`, ordinal, ordinal)
	}
	content.WriteString(`]}`)
	if err := os.WriteFile(path, []byte(content.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	session, err := OpenAttributeSession(path)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if applied, err := session.SetGeoJSONStreamIndexFeatureLimit(2); err != nil || !applied {
		t.Fatalf("set prefix index limit applied=%t err=%v", applied, err)
	}
	if _, err := session.Inspect(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(session.streamTailBlocks) != 3 {
		t.Fatalf("tail blocks = %d; want 3", len(session.streamTailBlocks))
	}
	window, err := session.OpenWindow(context.Background(), "", [4]float64{featureCount - 1, -1, featureCount, 1}, false)
	if err != nil || len(window.Features) != 2 || window.Features[0].ID != uint64(featureCount-1) ||
		window.Features[1].ID != uint64(featureCount) {
		t.Fatalf("last-block window features=%#v err=%v", window.Features, err)
	}
	feature, err := session.OpenFeature(context.Background(), "", uint64(featureCount))
	if err != nil || feature.ID != uint64(featureCount) || feature.Properties["id"] != float64(featureCount) {
		t.Fatalf("last-block feature=%#v err=%v", feature, err)
	}
}

func TestGeoJSONCollectionDoesNotReuseMissingFeatureFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing-geometry.geojson")
	content := `{"type":"FeatureCollection","features":[` +
		`{"type":"Feature","geometry":{"type":"Point","coordinates":[1,2]},"properties":{"name":"first"}},` +
		`{"type":"Feature","properties":{"name":"second"}}]}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	layer, err := (Reader{}).Open(context.Background(), path, "")
	if err != nil || len(layer.Features) != 2 {
		t.Fatalf("open collection = %#v, %v", layer.Features, err)
	}
	if layer.Features[1].Geometry != nil || layer.Features[1].Properties["name"] != "second" {
		t.Fatalf("second feature inherited first feature data: %#v", layer.Features[1])
	}
}

func TestGeoJSONPropertiesByteLimitRejectsBeforeDecode(t *testing.T) {
	raw := make([]byte, maxGeoJSONPropertiesBytes+1)
	if _, err := decodeGeoJSONProperties(raw, 17); err == nil || !strings.Contains(err.Error(), "properties exceed") {
		t.Fatalf("oversized property error = %v; want property size limit", err)
	}
	if properties, err := decodeGeoJSONProperties([]byte("null"), 17); err != nil || properties != nil {
		t.Fatalf("null properties = %#v, %v; want nil, nil", properties, err)
	}
}

func TestGeoJSONPropertiesComplexityIsBoundedBeforeMapDecode(t *testing.T) {
	const propertyCount = maxGeoJSONPropertyNodes/2 + 1
	var raw strings.Builder
	raw.Grow(propertyCount * 12)
	raw.WriteByte('{')
	for index := 0; index < propertyCount; index++ {
		if index > 0 {
			raw.WriteByte(',')
		}
		fmt.Fprintf(&raw, `"k%d":null`, index)
	}
	raw.WriteByte('}')
	if raw.Len() <= geoJSONPropertyPreflightThreshold {
		t.Fatalf("test properties are only %d bytes; want preflight threshold > %d", raw.Len(), geoJSONPropertyPreflightThreshold)
	}
	if _, err := decodeGeoJSONProperties([]byte(raw.String()), 1); err == nil || !strings.Contains(err.Error(), "node limit") {
		t.Fatalf("over-complex properties error = %v; want node limit", err)
	}

	deep := strings.Repeat(`{"x":`, maxGeoJSONPropertyDepth+1) + "null" + strings.Repeat("}", maxGeoJSONPropertyDepth+1)
	if _, err := decodeGeoJSONProperties([]byte(deep), 2); err == nil || !strings.Contains(err.Error(), "nesting exceeds") {
		t.Fatalf("over-depth properties error = %v; want nesting limit", err)
	}
}

func FuzzGeoJSONPropertiesNoPanic(f *testing.F) {
	f.Add([]byte(`{"name":"road","width":4.5}`))
	f.Add([]byte(`{"nested":[{"a":1},null]}`))
	f.Add([]byte("null"))
	f.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) > maxGeoJSONPropertiesBytes {
			t.Skip()
		}
		_, _ = decodeGeoJSONProperties(raw, 1)
	})
}

func TestGeometryWKBSizeLimitRejectsBeforeExport(t *testing.T) {
	point, err := godal.NewGeometryFromWKT("POINT (1 2)", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := point.WKBSize(); got != 21 {
		point.Close()
		t.Fatalf("point WKB size = %d, want 21", got)
	}
	if encoded, err := point.WKBWithMaxSize(21); err != nil || len(encoded) != 21 {
		point.Close()
		t.Fatalf("bounded point export = %d bytes, %v", len(encoded), err)
	}
	if encoded, err := point.WKBWithMaxSize(20); err == nil || encoded != nil {
		point.Close()
		t.Fatalf("point export above 20-byte budget = %d bytes, %v", len(encoded), err)
	}
	point.Close()

	pointCount := uint32(maxGeometryWKBBytes/16 + 1)
	wkb := make([]byte, 9+int(pointCount)*16)
	wkb[0] = 1                                 // little endian
	binary.LittleEndian.PutUint32(wkb[1:5], 2) // LineString
	binary.LittleEndian.PutUint32(wkb[5:9], pointCount)
	line, err := godal.NewGeometryFromWKB(wkb, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer line.Close()
	if size := line.WKBSize(); size <= maxGeometryWKBBytes {
		t.Fatalf("large line WKB size = %d; want over %d", size, maxGeometryWKBBytes)
	}
	if exported, err := geometryWKBWithLimit(line, 99); err == nil || exported != nil || !strings.Contains(err.Error(), "exceeds the 8 MiB limit") {
		t.Fatalf("oversized WKB export = %d bytes, %v; want pre-export limit error", len(exported), err)
	}
}

func TestNewGeometryFromWKBRejectsEmptyInput(t *testing.T) {
	for name, input := range map[string][]byte{"nil": nil, "empty": {}} {
		t.Run(name, func(t *testing.T) {
			geometry, err := godal.NewGeometryFromWKB(input, nil)
			if err == nil {
				if geometry != nil {
					geometry.Close()
				}
				t.Fatal("empty WKB input was accepted")
			}
			if geometry != nil {
				geometry.Close()
				t.Fatal("empty WKB input returned a geometry")
			}
		})
	}
}

func TestReaderRejectsOversizedGeoJSONMetadataWithoutGDALFallback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "metadata.geojson")
	fixture := `{"type":"FeatureCollection","metadata":"` + strings.Repeat("x", maxGeoJSONMetadataBytes+1) + `","features":[]}`
	if err := os.WriteFile(path, []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}
	if !isGeoJSONCollection(path) {
		t.Fatal("oversized FeatureCollection metadata must route through the bounded parser")
	}
	if _, err := (Reader{}).Open(context.Background(), path, ""); err == nil || !strings.Contains(err.Error(), "member value exceeds") {
		t.Fatalf("Open oversized metadata error = %v; want bounded metadata error", err)
	}
}

func TestSQLSpatialWindowExposesProviderFIDSeparatelyFromWindowOrdinal(t *testing.T) {
	for _, extension := range []string{".shp", ".gpkg"} {
		t.Run(extension, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "roads"+extension)
			input := core.Layer{
				Name: "roads", CRS: core.CRS{AuthorityCode: "EPSG:4326"},
				Fields: []core.Field{{Name: "name", Type: core.FieldTypeText}},
				Features: []core.Feature{
					{ID: 1, Geometry: core.WKTGeometry{WKT: "POINT (127 37)"}, Properties: map[string]any{"name": "outside"}},
					{ID: 2, Geometry: core.WKTGeometry{WKT: "POINT (128 38)"}, Properties: map[string]any{"name": "inside"}},
				},
			}
			if err := (Writer{}).Write(context.Background(), path, input); err != nil {
				t.Fatal(err)
			}
			dataset, err := openDataset(path, "")
			if err != nil {
				t.Fatal(err)
			}
			defer dataset.Close()
			filter, err := godal.NewGeometryFromWKT("POLYGON ((127.5 37.5, 128.5 37.5, 128.5 38.5, 127.5 38.5, 127.5 37.5))", nil)
			if err != nil {
				t.Fatal(err)
			}
			defer filter.Close()
			result, err := dataset.ExecuteSQL("SELECT FID AS gogis_fid, * FROM "+quoteSQLIdentifier("roads"), godal.SpatialFilter(filter))
			if err != nil {
				t.Fatal(err)
			}
			if result == nil {
				t.Fatal("spatial query returned no result set")
			}
			defer result.Close()
			page, err := readLayerOptions(context.Background(), result.Layer, true)
			if err != nil {
				t.Fatal(err)
			}
			if len(page.Features) != 1 {
				t.Fatalf("filtered features = %d, want 1", len(page.Features))
			}
			wantFID := "1"
			if extension == ".gpkg" {
				wantFID = "2"
			}
			if got := fmt.Sprint(page.Features[0].Properties["gogis_fid"]); got != wantFID {
				t.Fatalf("provider FID = %q, want %q", got, wantFID)
			}
			if page.Features[0].ID != 1 {
				t.Fatalf("window-local application ID = %d, want 1", page.Features[0].ID)
			}
		})
	}
}

func TestReaderOpenWindowGeometryOnlySkipsProperties(t *testing.T) {
	path := filepath.Join(t.TempDir(), "roads.geojson")
	fixture := `{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"name":"inside"},"geometry":{"type":"Point","coordinates":[127.1,37.4]}},{"type":"Feature","properties":{"name":"outside"},"geometry":{"type":"Point","coordinates":[128.1,38.4]}}]}`
	if err := os.WriteFile(path, []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}

	layer, err := (Reader{}).OpenWindowGeometryOnly(context.Background(), path, "", [4]float64{127, 37, 127.2, 37.6})
	if err != nil {
		t.Fatal(err)
	}
	if len(layer.Features) != 1 || layer.Features[0].Properties != nil || len(layer.Fields) != 0 {
		t.Fatalf("geometry-only window layer = %#v", layer)
	}
}

func TestAttributeSessionOpenWindowGeometryOnlySharesDatasetSafely(t *testing.T) {
	path := filepath.Join(t.TempDir(), "roads.geojson")
	fixture := `{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"name":"outside"},"geometry":{"type":"Point","coordinates":[128.1,38.4]}},{"type":"Feature","properties":{"name":"inside"},"geometry":{"type":"Point","coordinates":[127.1,37.4]}}]}`
	if err := os.WriteFile(path, []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}
	session, err := OpenAttributeSession(path)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	window, err := session.OpenWindowGeometryOnly(context.Background(), "", [4]float64{127, 37, 127.2, 37.6})
	if err != nil {
		t.Fatal(err)
	}
	if len(window.Features) != 1 || window.Features[0].ID != 2 || window.Features[0].Properties != nil {
		t.Fatalf("window = %#v; want source ordinal 2 with no properties", window.Features)
	}
	withProperties, err := session.OpenWindow(context.Background(), "", [4]float64{127, 37, 127.2, 37.6}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(withProperties.Features) != 1 || withProperties.Features[0].ID != 2 || withProperties.Features[0].Properties["name"] != "inside" {
		t.Fatalf("window with properties = %#v", withProperties.Features)
	}
	page, total, err := session.OpenAttributePage(context.Background(), "", 0, 2)
	if err != nil || total != 2 || len(page.Features) != 2 {
		t.Fatalf("attribute page after window query: count=%d total=%d err=%v", len(page.Features), total, err)
	}
	if _, err := session.OpenWindowGeometryOnly(context.Background(), "", [4]float64{2, 3, 1, 4}); err == nil {
		t.Fatal("invalid bounds were accepted")
	}
}

func TestAttributeSessionOpenWindowLimitedFailsInsteadOfTruncating(t *testing.T) {
	path := filepath.Join(t.TempDir(), "roads.geojson")
	fixture := `{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"name":"first"},"geometry":{"type":"Point","coordinates":[1,1]}},{"type":"Feature","properties":{"name":"second"},"geometry":{"type":"Point","coordinates":[2,2]}}]}`
	if err := os.WriteFile(path, []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}
	session, err := OpenAttributeSession(path)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if _, err := session.OpenWindowLimited(context.Background(), "", [4]float64{0, 0, 3, 3}, false, 1); err == nil || !strings.Contains(err.Error(), "exceeds the limit of 1") {
		t.Fatalf("limited window error = %v", err)
	}
	if _, err := session.OpenWindowLimited(context.Background(), "", [4]float64{0, 0, 3, 3}, false, -1); err == nil {
		t.Fatal("negative window feature limit was accepted")
	}
	if _, err := session.OpenWindow(context.Background(), "", [4]float64{math.NaN(), 0, 3, 3}, false); err == nil || !strings.Contains(err.Error(), "coordinates must be finite") {
		t.Fatalf("non-finite window error = %v", err)
	}
}

func TestAttributeSessionOpenWindowWithLimitsBoundsDecodedPayload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "points.geojson")
	fixture := `{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"name":"first"},"geometry":{"type":"Point","coordinates":[1,1]}},{"type":"Feature","properties":{"name":"second"},"geometry":{"type":"Point","coordinates":[2,2]}}]}`
	if err := os.WriteFile(path, []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}
	session, err := OpenAttributeSession(path)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if _, err := session.OpenWindowWithLimits(context.Background(), "", [4]float64{0, 0, 3, 3}, false, 0, 1); err == nil || !strings.Contains(err.Error(), "exceeds the limit of 1 bytes") {
		t.Fatalf("decoded payload limit error = %v", err)
	}
	if _, err := session.OpenWindowWithLimits(context.Background(), "", [4]float64{0, 0, 3, 3}, false, 0, -1); err == nil {
		t.Fatal("negative window byte limit was accepted")
	}
	layer, err := session.OpenWindowWithLimits(context.Background(), "", [4]float64{0, 0, 3, 3}, false, 0, 1024)
	if err != nil || len(layer.Features) != 2 {
		t.Fatalf("window within decoded payload budget: features=%d err=%v", len(layer.Features), err)
	}
}

func TestReaderOpenGeometryOnlySkipsProperties(t *testing.T) {
	path := filepath.Join(t.TempDir(), "roads.geojson")
	fixture := `{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"name":"inside"},"geometry":{"type":"Point","coordinates":[127.1,37.4]}}]}`
	if err := os.WriteFile(path, []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}
	layer, err := (Reader{}).OpenGeometryOnly(context.Background(), path, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(layer.Features) != 1 || layer.Features[0].Properties != nil || len(layer.Fields) != 0 {
		t.Fatalf("geometry-only layer retained attributes: fields=%#v properties=%#v", layer.Fields, layer.Features[0].Properties)
	}
}

func TestReaderOpenFeatureLoadsOneFeatureByID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "roads.geojson")
	fixture := `{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"name":"first"},"geometry":{"type":"Point","coordinates":[127.1,37.4]}},{"type":"Feature","properties":{"name":"second"},"geometry":{"type":"Point","coordinates":[128.1,38.4]}}]}`
	if err := os.WriteFile(path, []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}
	feature, err := (Reader{}).OpenFeature(context.Background(), path, "", 2)
	if err != nil {
		t.Fatal(err)
	}
	if feature.ID != 2 || feature.Properties["name"] != "second" || feature.Geometry == nil {
		t.Fatalf("feature = %#v", feature)
	}
}

func TestReaderOpenAttributePageSkipsGeometry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "roads.geojson")
	fixture := `{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"name":"first"},"geometry":{"type":"Point","coordinates":[127.1,37.4]}},{"type":"Feature","properties":{"name":"second"},"geometry":{"type":"Point","coordinates":[128.1,38.4]}}]}`
	if err := os.WriteFile(path, []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}
	layer, total, err := (Reader{}).OpenAttributePage(context.Background(), path, "", 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || len(layer.Features) != 1 || layer.Features[0].ID != 2 || layer.Features[0].Geometry != nil || layer.Features[0].Properties["name"] != "second" {
		t.Fatalf("layer=%#v total=%d", layer, total)
	}
}

func TestAttributePageLimitIsBounded(t *testing.T) {
	if err := validateAttributePage(0, maxAttributePageSize); err != nil {
		t.Fatalf("maximum supported page rejected: %v", err)
	}
	if err := validateAttributePage(0, maxAttributePageSize+1); err == nil {
		t.Fatal("oversized attribute page accepted")
	}
	if err := validateAttributePage(int(^uint(0)>>1), 200); err != nil {
		t.Fatalf("large offset should remain representable: %v", err)
	}
}

func TestAppendAttributePageFeatureEnforcesAggregateByteBudget(t *testing.T) {
	first := core.Feature{ID: 1, Properties: map[string]any{"payload": strings.Repeat("x", 64)}}
	second := core.Feature{ID: 2, Properties: map[string]any{"payload": strings.Repeat("y", 64)}}
	firstBytes := estimateFeaturePayloadBytes(first)
	page := core.Layer{Features: make([]core.Feature, 0, 2)}
	var payloadBytes int64
	if err := appendAttributePageFeatureWithLimit(&page, first, &payloadBytes, firstBytes+1); err != nil {
		t.Fatalf("append first feature: %v", err)
	}
	if err := appendAttributePageFeatureWithLimit(&page, second, &payloadBytes, firstBytes+1); err == nil || !strings.Contains(err.Error(), "attribute page exceeds") {
		t.Fatalf("second feature error = %v; want aggregate page budget error", err)
	}
	if len(page.Features) != 1 || payloadBytes != firstBytes {
		t.Fatalf("over-budget append changed page state: features=%d bytes=%d; want 1 and %d", len(page.Features), payloadBytes, firstBytes)
	}
}

func TestAttributeSessionReusesDatasetAndCloses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "roads.geojson")
	fixture := `{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"name":"first"},"geometry":{"type":"Point","coordinates":[127.1,37.4]}}]}`
	if err := os.WriteFile(path, []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}
	session, err := OpenAttributeSession(path)
	if err != nil {
		t.Fatal(err)
	}
	layer, total, err := session.OpenAttributePage(context.Background(), "", 0, 1)
	if err != nil || total != 1 || len(layer.Features) != 1 || layer.Features[0].Properties["name"] != "first" {
		t.Fatalf("session page = layer=%#v total=%d err=%v", layer, total, err)
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := session.OpenAttributePage(context.Background(), "", 0, 1); err == nil {
		t.Fatal("closed attribute session accepted a request")
	}
}

func TestAttributeSessionFeatureCursorReusesSequentialScan(t *testing.T) {
	path := filepath.Join(t.TempDir(), "roads.geojson")
	fixture := `{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"name":"first"},"geometry":{"type":"Point","coordinates":[127.1,37.4]}},{"type":"Feature","properties":{"name":"second"},"geometry":{"type":"Point","coordinates":[127.2,37.5]}},{"type":"Feature","properties":{"name":"third"},"geometry":{"type":"Point","coordinates":[127.3,37.6]}}]}`
	if err := os.WriteFile(path, []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}
	session, err := OpenAttributeSession(path)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	for featureID, want := range []string{"first", "second", "third"} {
		feature, err := session.OpenFeature(context.Background(), "", uint64(featureID+1))
		if err != nil || feature.Properties["name"] != want {
			t.Fatalf("feature %d = %#v, err=%v", featureID+1, feature, err)
		}
	}
	feature, err := session.OpenFeature(context.Background(), "", 1)
	if err != nil || feature.Properties["name"] != "first" {
		t.Fatalf("backward feature = %#v, err=%v", feature, err)
	}
}

func TestAttributeSessionJSONPageCursorResetsForRandomPage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "roads.geojson")
	fixture := `{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"name":"first"},"geometry":{"type":"Point","coordinates":[127.1,37.4]}},{"type":"Feature","properties":{"name":"second"},"geometry":{"type":"Point","coordinates":[127.2,37.5]}},{"type":"Feature","properties":{"name":"third"},"geometry":{"type":"Point","coordinates":[127.3,37.6]}}]}`
	if err := os.WriteFile(path, []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}
	session, err := OpenAttributeSession(path)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	first, total, err := session.OpenAttributePage(context.Background(), "", 0, 1)
	if err != nil || total != 3 || first.Features[0].Properties["name"] != "first" {
		t.Fatalf("first page = %#v, err=%v", first, err)
	}
	second, total, err := session.OpenAttributePage(context.Background(), "", 1, 1)
	if err != nil || total != 3 || second.Features[0].ID != 2 || second.Features[0].Properties["name"] != "second" {
		t.Fatalf("second page = %#v, err=%v", second, err)
	}
	random, total, err := session.OpenAttributePage(context.Background(), "", 0, 1)
	if err != nil || total != 3 || random.Features[0].ID != 1 || random.Features[0].Properties["name"] != "first" {
		t.Fatalf("random page = %#v, err=%v", random, err)
	}
	third, total, err := session.OpenAttributePage(context.Background(), "", 2, 1)
	if err != nil || total != 3 || third.Features[0].ID != 3 || third.Features[0].Properties["name"] != "third" {
		t.Fatalf("non-sequential page = %#v, err=%v", third, err)
	}
}

func TestAttributeSessionSchemaCacheIsDetached(t *testing.T) {
	path := filepath.Join(t.TempDir(), "roads.geojson")
	fixture := `{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"name":"first"},"geometry":{"type":"Point","coordinates":[127.1,37.4]}},{"type":"Feature","properties":{"name":"second"},"geometry":{"type":"Point","coordinates":[127.2,37.5]}}]}`
	if err := os.WriteFile(path, []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}
	session, err := OpenAttributeSession(path)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	first, _, err := session.OpenAttributePage(context.Background(), "", 0, 1)
	if err != nil || len(first.Fields) != 1 {
		t.Fatalf("first page = %#v, err=%v", first, err)
	}
	first.Fields[0].Name = "mutated"
	second, _, err := session.OpenAttributePage(context.Background(), "", 0, 1)
	if err != nil || len(second.Fields) != 1 || second.Fields[0].Name != "name" {
		t.Fatalf("cached schema was mutated: %#v, err=%v", second.Fields, err)
	}
}

func TestGeometrySessionUsesBoundedGeoJSONStreamAndCloses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "roads.geojson")
	fixture := `{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"name":"inside"},"geometry":{"type":"Point","coordinates":[127.1,37.4]}}]}`
	if err := os.WriteFile(path, []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}
	session, err := OpenGeometrySession(path)
	if err != nil {
		t.Fatal(err)
	}
	if session.streamSession == nil || session.dataset != nil {
		t.Fatal("GeoJSON geometry session must reuse the bounded stream reader, not raw GDAL")
	}
	layer, err := session.OpenWindowGeometryOnly(context.Background(), "", [4]float64{127, 37, 128, 38})
	if err != nil || len(layer.Features) != 1 || layer.Features[0].Properties != nil {
		t.Fatalf("session window = layer=%#v err=%v", layer, err)
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := session.OpenWindowGeometryOnly(context.Background(), "", [4]float64{127, 37, 128, 38}); err == nil {
		t.Fatal("closed geometry session accepted a request")
	}
}

func TestGeometrySessionRoutesGeoJSONSequenceThroughBoundedStream(t *testing.T) {
	path := filepath.Join(t.TempDir(), "roads.geojsonl")
	fixture := "{\"type\":\"Feature\",\"properties\":{\"name\":\"inside\"},\"geometry\":{\"type\":\"Point\",\"coordinates\":[127.1,37.4]}}\n"
	if err := os.WriteFile(path, []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}
	session, err := OpenGeometrySession(path)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if session.streamSession == nil || session.dataset != nil {
		t.Fatal("GeoJSONSeq geometry session must reuse the bounded stream reader, not raw GDAL")
	}
	layer, err := session.OpenWindowGeometryOnly(context.Background(), "", [4]float64{127, 37, 128, 38})
	if err != nil || len(layer.Features) != 1 || layer.Features[0].Properties != nil {
		t.Fatalf("GeoJSONSeq geometry window = layer=%#v err=%v", layer, err)
	}
}

func TestReaderOpenAllGeometryOnlySkipsProperties(t *testing.T) {
	path := filepath.Join(t.TempDir(), "roads.geojson")
	fixture := `{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"name":"first"},"geometry":{"type":"Point","coordinates":[127.1,37.4]}}]}`
	if err := os.WriteFile(path, []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}
	layers, err := (Reader{}).OpenAllGeometryOnly(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if len(layers) != 1 || len(layers[0].Features) != 1 || layers[0].Features[0].Properties != nil {
		t.Fatalf("layers = %#v", layers)
	}
}

func TestWindowRejectsOversizedGPKGAttributeBeforeCopy(t *testing.T) {
	registerDrivers()
	path := filepath.Join(t.TempDir(), "large-attribute.gpkg")
	dataset, err := godal.CreateVector(godal.GeoPackage, path)
	if err != nil {
		t.Fatal(err)
	}
	spatialRef, err := godal.NewSpatialRef("EPSG:4326")
	if err != nil {
		_ = dataset.Close()
		t.Fatal(err)
	}
	defer spatialRef.Close()
	layer, err := dataset.CreateLayer("features", spatialRef, godal.GTPoint,
		godal.NewFieldDefinition("payload", godal.FTString))
	if err != nil {
		_ = dataset.Close()
		t.Fatal(err)
	}
	geometry, err := godal.NewGeometryFromWKT("POINT (1 1)", nil)
	if err != nil {
		_ = dataset.Close()
		t.Fatal(err)
	}
	feature, err := layer.NewFeature(geometry)
	geometry.Close()
	if err != nil {
		_ = dataset.Close()
		t.Fatal(err)
	}
	feature.SetFID(-1)
	fields := feature.Fields()
	if err := feature.SetFieldValue(fields["payload"], strings.Repeat("x", maxMaterializedSnapshotFeatureBytes+1)); err != nil {
		feature.Close()
		_ = dataset.Close()
		t.Fatal(err)
	}
	if err := layer.CreateFeature(feature); err != nil {
		feature.Close()
		_ = dataset.Close()
		t.Fatal(err)
	}
	feature.Close()
	if err := dataset.Close(); err != nil {
		t.Fatal(err)
	}

	session, err := OpenAttributeSession(path)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	_, err = session.OpenWindowWithLimits(context.Background(), "features", [4]float64{0, 0, 2, 2}, true, 10, 16<<20)
	if err == nil || !strings.Contains(err.Error(), "field") || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("oversized GPKG attribute error = %v", err)
	}
}

func multiLayerGeoPackage(tb testing.TB, featuresPerLayer int) string {
	tb.Helper()
	registerDrivers()
	path := filepath.Join(tb.TempDir(), "layers.gpkg")
	dataset, err := godal.CreateVector(godal.GeoPackage, path)
	if err != nil {
		tb.Fatal(err)
	}
	spatialRef, err := godal.NewSpatialRef("EPSG:4326")
	if err != nil {
		_ = dataset.Close()
		tb.Fatal(err)
	}
	defer spatialRef.Close()
	for _, name := range []string{"first", "selected", "last"} {
		layer, err := dataset.CreateLayer(name, spatialRef, godal.GTPoint)
		if err != nil {
			_ = dataset.Close()
			tb.Fatal(err)
		}
		for index := 0; index < featuresPerLayer; index++ {
			geometry, err := godal.NewGeometryFromWKT(fmt.Sprintf("POINT (%d %d)", index, index), nil)
			if err != nil {
				_ = dataset.Close()
				tb.Fatal(err)
			}
			feature, err := layer.NewFeature(geometry)
			geometry.Close()
			if err != nil {
				_ = dataset.Close()
				tb.Fatal(err)
			}
			if err := layer.UpdateFeature(feature); err != nil {
				feature.Close()
				_ = dataset.Close()
				tb.Fatal(err)
			}
			feature.Close()
		}
	}
	if err := dataset.Close(); err != nil {
		tb.Fatal(err)
	}
	return path
}

func TestAttributeSessionOpenGeometryOnlySelectsOneLayer(t *testing.T) {
	path := multiLayerGeoPackage(t, 2)
	session, err := OpenAttributeSession(path)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	layer, err := session.OpenGeometryOnly(context.Background(), "selected")
	if err != nil {
		t.Fatal(err)
	}
	if layer.Name != "selected" || len(layer.Features) != 2 || layer.CRS.AuthorityCode != "EPSG:4326" {
		t.Fatalf("selected layer = %#v", layer)
	}
	if layer.Features[0].Geometry == nil || layer.Features[0].Properties != nil {
		t.Fatalf("geometry-only feature = %#v", layer.Features[0])
	}
	page, total, err := session.OpenAttributePage(context.Background(), "selected", 0, 1)
	if err != nil || total != 2 || len(page.Features) != 1 {
		t.Fatalf("attribute page = %#v, total=%d, err=%v", page, total, err)
	}
	if _, err := session.OpenGeometryOnly(context.Background(), "missing"); err == nil {
		t.Fatal("missing layer was accepted")
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := session.OpenGeometryOnly(canceled, "selected"); err != context.Canceled {
		t.Fatalf("canceled read error = %v", err)
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := session.OpenGeometryOnly(context.Background(), "selected"); err == nil {
		t.Fatal("closed session accepted a geometry request")
	}
}

func TestAttributeSessionOverviewAndPrefixPreserveFullRead(t *testing.T) {
	path := multiLayerGeoPackage(t, 3)
	session, err := OpenAttributeSession(path)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	overviews, err := session.Inspect(context.Background())
	if err != nil || len(overviews) != 3 {
		t.Fatalf("overviews=%#v err=%v", overviews, err)
	}
	for _, overview := range overviews {
		if !overview.HasBounds || overview.Bounds != [4]float64{0, 0, 2, 2} || overview.FeatureCount != 3 || overview.CRS.AuthorityCode != "EPSG:4326" {
			t.Fatalf("unexpected overview: %#v", overview)
		}
	}
	preview, err := session.OpenAllGeometryPrefix(context.Background(), 2)
	if err != nil || len(preview) != 3 {
		t.Fatalf("preview=%#v err=%v", preview, err)
	}
	for _, layer := range preview {
		if len(layer.Features) != 2 || layer.Features[0].ID != 1 || layer.Features[1].ID != 2 || layer.Features[0].Properties != nil {
			t.Fatalf("invalid prefix layer: %#v", layer)
		}
	}
	selected, err := session.OpenGeometryPrefix(context.Background(), "selected", 2)
	if err != nil || selected.Name != "selected" || len(selected.Features) != 2 {
		t.Fatalf("selected prefix=%#v err=%v", selected, err)
	}
	full, err := session.OpenAllGeometryOnly(context.Background())
	if err != nil || len(full) != 3 {
		t.Fatalf("full layers=%#v err=%v", full, err)
	}
	for _, layer := range full {
		if len(layer.Features) != 3 || layer.Features[2].ID != 3 {
			t.Fatalf("full read changed by prefix: %#v", layer)
		}
	}
	_, total, err := session.OpenAttributePage(context.Background(), "selected", 0, 1)
	if err != nil || total != 3 {
		t.Fatalf("attribute page after preview: total=%d err=%v", total, err)
	}
	if _, err := session.OpenAllGeometryPrefix(context.Background(), 0); err == nil {
		t.Fatal("zero prefix limit was accepted")
	}
}

func TestValidLayerBoundsRejectsInvalidValues(t *testing.T) {
	for _, bounds := range [][4]float64{{1, 0, 0, 1}, {0, 1, 1, 0}, {0, 0, 1, math.NaN()}, {0, 0, math.Inf(1), 1}} {
		if validLayerBounds(bounds) {
			t.Fatalf("accepted invalid bounds %v", bounds)
		}
	}
}

func BenchmarkAttributeSessionOpenAllGeometryOnlyMultiLayerGeoPackage(b *testing.B) {
	path := multiLayerGeoPackage(b, 3_000)
	session, err := OpenAttributeSession(path)
	if err != nil {
		b.Fatal(err)
	}
	defer session.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, err := session.OpenAllGeometryOnly(context.Background()); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkAttributeSessionOpenGeometryOnlyMultiLayerGeoPackage(b *testing.B) {
	path := multiLayerGeoPackage(b, 3_000)
	session, err := OpenAttributeSession(path)
	if err != nil {
		b.Fatal(err)
	}
	defer session.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, err := session.OpenGeometryOnly(context.Background(), "selected"); err != nil {
			b.Fatal(err)
		}
	}
}

func TestGDALSQLAttributeOffsetForGeoJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "roads.geojson")
	fixture := `{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"name":"first"},"geometry":{"type":"Point","coordinates":[127.1,37.4]}},{"type":"Feature","properties":{"name":"second"},"geometry":{"type":"Point","coordinates":[128.1,38.4]}}]}`
	if err := os.WriteFile(path, []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}
	godal.RegisterAll()
	dataset, err := godal.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer dataset.Close()
	layers := dataset.Layers()
	if len(layers) != 1 {
		t.Fatalf("layers = %d", len(layers))
	}
	result, err := dataset.ExecuteSQL(`SELECT fid AS gogis_fid, * FROM "roads" LIMIT 1 OFFSET 1`, godal.OGRSQLDialect())
	if err != nil {
		t.Fatal(err)
	}
	defer result.Close()
	feature := result.NextFeature()
	if feature == nil {
		t.Fatal("SQL offset returned no feature")
	}
	defer feature.Close()
	fields := feature.Fields()
	if got := fields["name"].String(); got != "second" {
		t.Fatalf("name = %q", got)
	}
	if fid := fields["gogis_fid"].Int(); fid == 0 {
		t.Fatalf("FID was not exposed: %#v", fields)
	}
}

func BenchmarkReaderOpenGeoJSON10KPoints(b *testing.B) {
	benchmarkReaderOpenGeoJSON10KPoints(b, false)
}

func BenchmarkGDALOpenGeoJSON10KPoints(b *testing.B) {
	path := benchmarkGeoJSON10KPath(b)
	registerDrivers()
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		dataset, err := godal.Open(path)
		if err != nil {
			b.Fatal(err)
		}
		if err := dataset.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkGDALOpenVectorOnlyGeoJSON10KPoints(b *testing.B) {
	path := benchmarkGeoJSON10KPath(b)
	registerDrivers()
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		dataset, err := godal.Open(path, godal.VectorOnly())
		if err != nil {
			b.Fatal(err)
		}
		if err := dataset.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkGDALOpenAndBoundsGeoJSON10KPoints(b *testing.B) {
	path := benchmarkGeoJSON10KPath(b)
	registerDrivers()
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		dataset, err := godal.Open(path)
		if err != nil {
			b.Fatal(err)
		}
		layers := dataset.Layers()
		if len(layers) != 1 {
			b.Fatal("expected one layer")
		}
		if _, err := layers[0].Bounds(); err != nil {
			b.Fatal(err)
		}
		if err := dataset.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkGDALOpenAndFeatureCountGeoJSON10KPoints(b *testing.B) {
	path := benchmarkGeoJSON10KPath(b)
	registerDrivers()
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		dataset, err := godal.Open(path)
		if err != nil {
			b.Fatal(err)
		}
		layers := dataset.Layers()
		if len(layers) != 1 {
			b.Fatalf("layers = %d", len(layers))
		}
		if _, err := layers[0].FeatureCount(); err != nil {
			b.Fatal(err)
		}
		if err := dataset.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

var benchmarkGeometryAccessSink int

func BenchmarkGDALNextFeature10KPoints(b *testing.B) {
	benchmarkGDALGeometryAccess(b, 0)
}

func BenchmarkGDALFeatureGeometry10KPoints(b *testing.B) {
	benchmarkGDALGeometryAccess(b, 1)
}

func BenchmarkGDALFeatureWKB10KPoints(b *testing.B) {
	benchmarkGDALGeometryAccess(b, 2)
}

func benchmarkGDALGeometryAccess(b *testing.B, mode int) {
	path := benchmarkGeoJSON10KPath(b)
	godal.RegisterAll()
	dataset, err := godal.Open(path)
	if err != nil {
		b.Fatal(err)
	}
	defer dataset.Close()
	layers := dataset.Layers()
	if len(layers) != 1 {
		b.Fatalf("layers = %d", len(layers))
	}
	layer := layers[0]
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		layer.ResetReading()
		for {
			feature := layer.NextFeature()
			if feature == nil {
				break
			}
			if mode > 0 {
				geometry := feature.Geometry()
				if geometry != nil {
					if mode > 1 {
						wkb, wkbErr := geometry.WKB()
						if wkbErr != nil {
							feature.Close()
							geometry.Close()
							b.Fatal(wkbErr)
						}
						benchmarkGeometryAccessSink += len(wkb)
					}
					geometry.Close()
				}
			}
			feature.Close()
		}
	}
}

func BenchmarkReaderOpenGeometryOnlyGeoJSON10KPoints(b *testing.B) {
	benchmarkReaderOpenGeoJSON10KPoints(b, true)
}

func BenchmarkReaderAttributeSessionOpenAllGeometryOnlyGeoJSON10KPoints(b *testing.B) {
	path := benchmarkGeoJSON10KPath(b)
	session, err := OpenAttributeSession(path)
	if err != nil {
		b.Fatal(err)
	}
	defer session.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, err := session.OpenAllGeometryOnly(context.Background()); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkReaderAttributeSessionFirstPageAfterGeometryGeoJSON10KPoints(b *testing.B) {
	path := benchmarkGeoJSON10KPath(b)
	b.ReportAllocs()
	for index := 0; index < b.N; index++ {
		b.StopTimer()
		session, err := OpenAttributeSession(path)
		if err != nil {
			b.Fatal(err)
		}
		if _, err := session.OpenAllGeometryOnly(context.Background()); err != nil {
			_ = session.Close()
			b.Fatal(err)
		}
		b.StartTimer()
		page, total, err := session.OpenAttributePage(context.Background(), "", 0, 200)
		b.StopTimer()
		closeErr := session.Close()
		if err != nil || closeErr != nil || total != 10_000 || len(page.Features) != 200 {
			b.Fatalf("page=%d total=%d read=%v close=%v", len(page.Features), total, err, closeErr)
		}
	}
}

func BenchmarkReaderAttributeSessionFeatureCountAfterGeometryGeoJSON10KPoints(b *testing.B) {
	path := benchmarkGeoJSON10KPath(b)
	b.ReportAllocs()
	for index := 0; index < b.N; index++ {
		b.StopTimer()
		session, err := OpenAttributeSession(path)
		if err != nil {
			b.Fatal(err)
		}
		if _, err := session.OpenAllGeometryOnly(context.Background()); err != nil {
			_ = session.Close()
			b.Fatal(err)
		}
		layers := session.dataset.Layers()
		if len(layers) != 1 {
			b.Fatal("expected one layer")
		}
		b.StartTimer()
		count, err := layers[0].FeatureCount()
		b.StopTimer()
		closeErr := session.Close()
		if err != nil || closeErr != nil || count != 10_000 {
			b.Fatalf("count=%d read=%v close=%v", count, err, closeErr)
		}
	}
}

func BenchmarkReaderAttributeSessionRandomLastPageGeoJSON10KPoints(b *testing.B) {
	path := benchmarkGeoJSON10KPath(b)
	session, err := OpenAttributeSession(path)
	if err != nil {
		b.Fatal(err)
	}
	defer session.Close()
	if _, err := session.OpenAllGeometryOnly(context.Background()); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		page, total, err := session.OpenAttributePage(context.Background(), "", 9_800, 200)
		if err != nil || total != 10_000 || len(page.Features) != 200 {
			b.Fatalf("page=%d total=%d err=%v", len(page.Features), total, err)
		}
	}
}

func BenchmarkReaderAttributeSessionRandomLastPageCursorGeoJSON10KPoints(b *testing.B) {
	path := benchmarkGeoJSON10KPath(b)
	session, err := OpenAttributeSession(path)
	if err != nil {
		b.Fatal(err)
	}
	defer session.Close()
	if _, err := session.OpenAllGeometryOnly(context.Background()); err != nil {
		b.Fatal(err)
	}
	layers := session.dataset.Layers()
	if len(layers) != 1 {
		b.Fatal("expected one layer")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		page, _, err := readAttributePageLayerCursor(context.Background(), layers[0], 9_800, 200, 1, true, nil)
		if err != nil || len(page.Features) != 200 {
			b.Fatalf("page=%d err=%v", len(page.Features), err)
		}
	}
}

func BenchmarkReaderOpenAllThenAttributeSessionGeoJSON10KPoints(b *testing.B) {
	path := benchmarkGeoJSON10KPath(b)
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, err := (Reader{}).OpenAllGeometryOnly(context.Background(), path); err != nil {
			b.Fatal(err)
		}
		session, err := OpenAttributeSession(path)
		if err != nil {
			b.Fatal(err)
		}
		if err := session.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkReaderOpenWindowGeometryOnlyGeoJSON10KPoints(b *testing.B) {
	path := benchmarkGeoJSON10KPath(b)
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, err := (Reader{}).OpenWindowGeometryOnly(context.Background(), path, "", [4]float64{127, 37, 127.1, 37.1}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkReaderGeometrySessionWindowGeoJSON10KPoints(b *testing.B) {
	path := benchmarkGeoJSON10KPath(b)
	session, err := OpenGeometrySession(path)
	if err != nil {
		b.Fatal(err)
	}
	defer session.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, err := session.OpenWindowGeometryOnly(context.Background(), "", [4]float64{127, 37, 127.1, 37.1}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkReaderOpenGeometryOnlyGeoPackage10KPoints(b *testing.B) {
	path := benchmarkGeoPackage10KPath(b)
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, err := (Reader{}).OpenGeometryOnly(context.Background(), path, "points"); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkAttributeSessionPreviewGeoPackage10KPoints(b *testing.B) {
	path := benchmarkGeoPackage10KPath(b)
	session, err := OpenAttributeSession(path)
	if err != nil {
		b.Fatal(err)
	}
	defer session.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		overviews, err := session.Inspect(context.Background())
		if err != nil || len(overviews) != 1 || !overviews[0].HasBounds {
			b.Fatalf("overviews=%#v err=%v", overviews, err)
		}
		prefix, err := session.OpenGeometryPrefix(context.Background(), "points", 1_000)
		if err != nil || len(prefix.Features) != 1_000 {
			b.Fatalf("prefix=%d err=%v", len(prefix.Features), err)
		}
	}
}

func BenchmarkAttributeSessionFullGeoPackage10KPoints(b *testing.B) {
	path := benchmarkGeoPackage10KPath(b)
	session, err := OpenAttributeSession(path)
	if err != nil {
		b.Fatal(err)
	}
	defer session.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		layers, err := session.OpenAllGeometryOnly(context.Background())
		if err != nil || len(layers) != 1 || len(layers[0].Features) != 10_000 {
			b.Fatalf("layers=%d err=%v", len(layers), err)
		}
	}
}

func BenchmarkGDALOpenGeoPackage10KPoints(b *testing.B) {
	path := benchmarkGeoPackage10KPath(b)
	registerDrivers()
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		dataset, err := godal.Open(path)
		if err != nil {
			b.Fatal(err)
		}
		if err := dataset.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkGDALOpenAndBoundsGeoPackage10KPoints(b *testing.B) {
	path := benchmarkGeoPackage10KPath(b)
	registerDrivers()
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		dataset, err := godal.Open(path)
		if err != nil {
			b.Fatal(err)
		}
		layers := dataset.Layers()
		if len(layers) != 1 {
			b.Fatal("expected one layer")
		}
		if _, err := layers[0].Bounds(); err != nil {
			b.Fatal(err)
		}
		if err := dataset.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkReaderOpenWindowGeometryOnlyGeoPackage10KPoints(b *testing.B) {
	path := benchmarkGeoPackage10KPath(b)
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, err := (Reader{}).OpenWindowGeometryOnly(context.Background(), path, "points", [4]float64{127, 37, 127.1, 37.1}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkReaderGeometrySessionWindowGeoPackage10KPoints(b *testing.B) {
	path := benchmarkGeoPackage10KPath(b)
	session, err := OpenGeometrySession(path)
	if err != nil {
		b.Fatal(err)
	}
	defer session.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, err := session.OpenWindowGeometryOnly(context.Background(), "points", [4]float64{127, 37, 127.1, 37.1}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkReaderOpenGeometryOnlyShapefile10KPoints(b *testing.B) {
	path := benchmarkShapefile10KPath(b)
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, err := (Reader{}).OpenGeometryOnly(context.Background(), path, "points"); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkReaderOpenWindowGeometryOnlyShapefile10KPoints(b *testing.B) {
	path := benchmarkShapefile10KPath(b)
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, err := (Reader{}).OpenWindowGeometryOnly(context.Background(), path, "points", [4]float64{127, 37, 127.1, 37.1}); err != nil {
			b.Fatal(err)
		}
	}
}

func benchmarkReaderOpenGeoJSON10KPoints(b *testing.B, geometryOnly bool) {
	path := benchmarkGeoJSON10KPath(b)
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		var err error
		if geometryOnly {
			_, err = (Reader{}).OpenGeometryOnly(context.Background(), path, "")
		} else {
			_, err = (Reader{}).Open(context.Background(), path, "")
		}
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkReaderOpenAttributePageGeoJSON10KPoints(b *testing.B) {
	path := benchmarkGeoJSON10KPath(b)
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, _, err := (Reader{}).OpenAttributePage(context.Background(), path, "", (index%50)*200, 200); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkReaderAttributeSessionGeoJSON10KPoints(b *testing.B) {
	path := benchmarkGeoJSON10KPath(b)
	session, err := OpenAttributeSession(path)
	if err != nil {
		b.Fatal(err)
	}
	defer session.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, _, err := session.OpenAttributePage(context.Background(), "", (index%50)*200, 200); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkReaderAttributeSessionGeoPackage10KPoints(b *testing.B) {
	path := benchmarkGeoPackage10KPath(b)
	session, err := OpenAttributeSession(path)
	if err != nil {
		b.Fatal(err)
	}
	defer session.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		for page := 0; page < 50; page++ {
			if _, _, err := session.OpenAttributePage(context.Background(), "points", page*200, 200); err != nil {
				b.Fatal(err)
			}
		}
	}
}

func BenchmarkReaderAttributeSessionGeoPackageRandomPages10KPoints(b *testing.B) {
	path := benchmarkGeoPackage10KPath(b)
	session, err := OpenAttributeSession(path)
	if err != nil {
		b.Fatal(err)
	}
	defer session.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		page := (index * 17) % 50
		if _, _, err := session.OpenAttributePage(context.Background(), "points", page*200, 200); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkReaderAttributeSessionGeoPackageRandomPagesSQLBaseline10KPoints(b *testing.B) {
	path := benchmarkGeoPackage10KPath(b)
	session, err := OpenAttributeSession(path)
	if err != nil {
		b.Fatal(err)
	}
	defer session.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		page := (index * 17) % 50
		session.mu.Lock()
		_, _, err := openAttributePageDataset(context.Background(), session.dataset, session.source, "points", page*200, 200)
		session.mu.Unlock()
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkReaderAttributeSessionGeoJSONSequentialPages10KPoints(b *testing.B) {
	path := benchmarkGeoJSON10KPath(b)
	session, err := OpenAttributeSession(path)
	if err != nil {
		b.Fatal(err)
	}
	defer session.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		for page := 0; page < 50; page++ {
			if _, _, err := session.OpenAttributePage(context.Background(), "", page*200, 200); err != nil {
				b.Fatal(err)
			}
		}
	}
}

func BenchmarkReaderAttributeSessionSequentialPageGeoJSON10KPoints(b *testing.B) {
	path := benchmarkGeoJSON10KPath(b)
	godal.RegisterAll()
	dataset, err := godal.Open(path)
	if err != nil {
		b.Fatal(err)
	}
	defer dataset.Close()
	layers := dataset.Layers()
	if len(layers) != 1 {
		b.Fatalf("layers = %d", len(layers))
	}
	layer := layers[0]
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		offset := (index % 50) * 200
		layer.ResetReading()
		ordinal := 0
		for {
			feature := layer.NextFeature()
			if feature == nil {
				break
			}
			if ordinal < offset {
				feature.Close()
				ordinal++
				continue
			}
			if ordinal >= offset+200 {
				feature.Close()
				break
			}
			fields := feature.Fields()
			benchmarkGeometryAccessSink += len(fields)
			feature.Close()
			ordinal++
		}
	}
}

func BenchmarkReaderOpenFeatureGeoJSON10KPoints(b *testing.B) {
	path := benchmarkGeoJSON10KPath(b)
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, err := (Reader{}).OpenFeature(context.Background(), path, "", uint64(index%10_000+1)); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkReaderAttributeSessionFeatureGeoJSON10KPoints(b *testing.B) {
	path := benchmarkGeoJSON10KPath(b)
	session, err := OpenAttributeSession(path)
	if err != nil {
		b.Fatal(err)
	}
	defer session.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, err := session.OpenFeature(context.Background(), "", uint64(index%10_000+1)); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkReaderAttributeSessionLastFeatureGeoJSON10KPoints(b *testing.B) {
	path := benchmarkGeoJSON10KPath(b)
	session, err := OpenAttributeSession(path)
	if err != nil {
		b.Fatal(err)
	}
	defer session.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		feature, err := session.OpenFeature(context.Background(), "", 10_000)
		if err != nil || feature.ID != 10_000 {
			b.Fatalf("feature=%#v err=%v", feature, err)
		}
		// A repeated click should exercise a non-adjacent lookup rather than
		// the session's sequential cursor fast path.
		session.featureCursorReady = false
	}
}

func BenchmarkReaderAttributeSessionFeatureGeoPackage10KPoints(b *testing.B) {
	path := benchmarkGeoPackage10KPath(b)
	session, err := OpenAttributeSession(path)
	if err != nil {
		b.Fatal(err)
	}
	defer session.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		for featureID := uint64(1); featureID <= 10_000; featureID++ {
			if _, err := session.OpenFeature(context.Background(), "points", featureID); err != nil {
				b.Fatal(err)
			}
		}
	}
}

func BenchmarkReaderAttributeSessionLastFeatureGeoPackage10KPoints(b *testing.B) {
	path := benchmarkGeoPackage10KPath(b)
	session, err := OpenAttributeSession(path)
	if err != nil {
		b.Fatal(err)
	}
	defer session.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		feature, err := session.OpenFeature(context.Background(), "points", 10_000)
		if err != nil || feature.ID != 10_000 {
			b.Fatalf("feature=%#v err=%v", feature, err)
		}
		session.featureCursorReady = false
	}
}

func benchmarkGeoJSON10KPath(b *testing.B) string {
	b.Helper()
	path := filepath.Join(b.TempDir(), "points.geojson")
	var fixture strings.Builder
	fixture.WriteString(`{"type":"FeatureCollection","features":[`)
	for index := 0; index < 10_000; index++ {
		if index > 0 {
			fixture.WriteByte(',')
		}
		fmt.Fprintf(&fixture, `{"type":"Feature","properties":{"name":"road-%d","value":%d},"geometry":{"type":"Point","coordinates":[127.%04d,37.%04d]}}`, index, index, index%10_000, index%10_000)
	}
	fixture.WriteString(`]}`)
	if err := os.WriteFile(path, []byte(fixture.String()), 0o600); err != nil {
		b.Fatal(err)
	}
	return path
}

func benchmarkGeoPackage10KPath(b *testing.B) string {
	b.Helper()
	return benchmarkVector10KPath(b, ".gpkg")
}

func benchmarkShapefile10KPath(b *testing.B) string {
	b.Helper()
	return benchmarkVector10KPath(b, ".shp")
}

func benchmarkVector10KPath(b *testing.B, extension string) string {
	b.Helper()
	path := filepath.Join(b.TempDir(), "points"+extension)
	features := make([]core.Feature, 10_000)
	for index := range features {
		features[index] = core.Feature{
			ID:       uint64(index + 1),
			Geometry: core.WKTGeometry{WKT: fmt.Sprintf("POINT (127.%04d 37.%04d)", index, index)},
		}
	}
	if err := (Writer{}).Write(context.Background(), path, core.Layer{
		Name:     "points",
		CRS:      core.CRS{AuthorityCode: "EPSG:4326"},
		Features: features,
	}); err != nil {
		b.Fatal(err)
	}
	return path
}
