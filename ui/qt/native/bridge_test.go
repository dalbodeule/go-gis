//go:build qt

package native

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"gogis/internal/render"
)

func TestNativeVertexBatchLimit(t *testing.T) {
	for _, test := range []struct {
		count int
		want  bool
	}{{0, true}, {1, true}, {render.MaxBatchVertices, true}, {render.MaxBatchVertices + 1, false}, {-1, false}} {
		if got := nativeVertexBatchAllowed(test.count); got != test.want {
			t.Errorf("nativeVertexBatchAllowed(%d) = %t, want %t", test.count, got, test.want)
		}
	}
}

func TestDiagnosticLogIsBoundedAndRetainsRecentMessages(t *testing.T) {
	for index := 0; index < diagnosticLogEntryLimit+5; index++ {
		RecordDiagnostic("stderr", fmt.Sprintf("diagnostic-test-%03d", index))
	}
	var entries []diagnosticLogEntry
	if err := json.Unmarshal([]byte(DiagnosticLogJSON()), &entries); err != nil {
		t.Fatalf("decode diagnostic log: %v", err)
	}
	if len(entries) > diagnosticLogEntryLimit {
		t.Fatalf("diagnostic entry count = %d, limit = %d", len(entries), diagnosticLogEntryLimit)
	}
	if len(entries) == 0 || entries[len(entries)-1].Message != fmt.Sprintf("diagnostic-test-%03d", diagnosticLogEntryLimit+4) {
		t.Fatalf("latest diagnostic entry = %#v", entries[len(entries)-1])
	}
	if strings.Contains(DiagnosticLogJSON(), "diagnostic-test-000") {
		t.Fatal("old diagnostic entries were retained after the entry limit")
	}
}

func TestDiagnosticLogGenerationAdvancesWithContents(t *testing.T) {
	before := DiagnosticLogGeneration()
	RecordDiagnostic("application", "generation-test")
	if after := DiagnosticLogGeneration(); after <= before {
		t.Fatalf("diagnostic generation = %d after %d, want advancement", after, before)
	}
}

func TestDiagnosticLogTruncatesLargeUTF8Messages(t *testing.T) {
	RecordDiagnostic("stderr", strings.Repeat("한", diagnosticLineByteLimit+8))
	var entries []diagnosticLogEntry
	if err := json.Unmarshal([]byte(DiagnosticLogJSON()), &entries); err != nil {
		t.Fatalf("decode diagnostic log: %v", err)
	}
	latest := entries[len(entries)-1]
	if len(latest.Message) > diagnosticLineByteLimit+len(" … [truncated]") || !utf8.ValidString(latest.Message) || !strings.HasSuffix(latest.Message, "… [truncated]") {
		t.Fatalf("large diagnostic was not safely truncated: bytes=%d valid=%t message suffix=%q", len(latest.Message), utf8.ValidString(latest.Message), latest.Message[max(0, len(latest.Message)-20):])
	}
}

func TestDiagnosticLogEnforcesAggregateByteLimit(t *testing.T) {
	largeMessage := strings.Repeat("x", diagnosticLineByteLimit)
	for index := 0; index < 64; index++ {
		RecordDiagnostic("stderr", largeMessage)
	}
	payload := DiagnosticLogJSON()
	if len(payload) > diagnosticLogByteLimit+(diagnosticLogEntryLimit*64) {
		t.Fatalf("diagnostic JSON payload grew to %d bytes beyond its bounded contents", len(payload))
	}
	var entries []diagnosticLogEntry
	if err := json.Unmarshal([]byte(payload), &entries); err != nil {
		t.Fatalf("decode byte-limited diagnostics: %v", err)
	}
	if len(entries) == 0 || len(entries) >= 64 {
		t.Fatalf("byte limit retained %d large entries, want a positive subset of 64", len(entries))
	}
}

func TestRenderFailureStatusIsAlsoRetainedInDiagnosticLog(t *testing.T) {
	marker := fmt.Sprintf("query-budget-test-%d", time.Now().UnixNano())
	SetRenderStatus("Render incomplete; zoom in and try again: " + marker)
	if !strings.Contains(DiagnosticLogJSON(), marker) {
		t.Fatal("full render failure status was not retained in the diagnostic log")
	}
}

func TestRenderStatusGenerationSkipsDuplicateStatus(t *testing.T) {
	marker := fmt.Sprintf("status-generation-%d", time.Now().UnixNano())
	before := RenderStatusGeneration()
	SetRenderStatus(marker)
	afterFirst := RenderStatusGeneration()
	if afterFirst <= before {
		t.Fatalf("distinct render status generation = %d after %d, want advancement", afterFirst, before)
	}
	SetRenderStatus(marker)
	if afterDuplicate := RenderStatusGeneration(); afterDuplicate != afterFirst {
		t.Fatalf("duplicate render status generation = %d, want unchanged %d", afterDuplicate, afterFirst)
	}
	SetRenderStatus(marker + "-next")
	if afterNext := RenderStatusGeneration(); afterNext <= afterFirst {
		t.Fatalf("next render status generation = %d after %d, want advancement", afterNext, afterFirst)
	}
}

func TestLayerLabelGenerationSkipsDuplicatePayload(t *testing.T) {
	marker := fmt.Sprintf(`[{"text":"label-%d"}]`, time.Now().UnixNano())
	before := LayerLabelGeneration()
	SetLayerLabelPayload(marker)
	afterFirst := LayerLabelGeneration()
	if afterFirst <= before {
		t.Fatalf("distinct label payload generation = %d after %d, want advancement", afterFirst, before)
	}
	SetLayerLabelPayload(marker)
	if afterDuplicate := LayerLabelGeneration(); afterDuplicate != afterFirst {
		t.Fatalf("duplicate label payload generation = %d, want unchanged %d", afterDuplicate, afterFirst)
	}
	SetLayerLabelPayload(marker + " ")
	if afterNext := LayerLabelGeneration(); afterNext <= afterFirst {
		t.Fatalf("next label payload generation = %d after %d, want advancement", afterNext, afterFirst)
	}
}

func TestLayerTreeGenerationSkipsDuplicatePayload(t *testing.T) {
	marker := fmt.Sprintf(`[{"name":"layer-%d"}]`, time.Now().UnixNano())
	before := LayerTreeGeneration()
	SetLayerTreePayload(marker)
	afterFirst := LayerTreeGeneration()
	if afterFirst <= before {
		t.Fatalf("distinct layer tree payload generation = %d after %d, want advancement", afterFirst, before)
	}
	SetLayerTreePayload(marker)
	if afterDuplicate := LayerTreeGeneration(); afterDuplicate != afterFirst {
		t.Fatalf("duplicate layer tree payload generation = %d, want unchanged %d", afterDuplicate, afterFirst)
	}
	SetLayerTreePayload(marker + " ")
	if afterNext := LayerTreeGeneration(); afterNext <= afterFirst {
		t.Fatalf("next layer tree payload generation = %d after %d, want advancement", afterNext, afterFirst)
	}
}

func TestAttributeGenerationSkipsDuplicatePayload(t *testing.T) {
	marker := fmt.Sprintf(`{"columns":["name"],"rows":[{"featureId":%d}]}`, time.Now().UnixNano())
	before := AttributeGeneration()
	SetAttributePayload(marker)
	afterFirst := AttributeGeneration()
	if afterFirst <= before {
		t.Fatalf("distinct attribute payload generation = %d after %d, want advancement", afterFirst, before)
	}
	SetAttributePayload(marker)
	if afterDuplicate := AttributeGeneration(); afterDuplicate != afterFirst {
		t.Fatalf("duplicate attribute payload generation = %d, want unchanged %d", afterDuplicate, afterFirst)
	}
	SetAttributePayload(marker + " ")
	if afterNext := AttributeGeneration(); afterNext <= afterFirst {
		t.Fatalf("next attribute payload generation = %d after %d, want advancement", afterNext, afterFirst)
	}
}

func TestPostGISCatalogPayloadGenerationSkipsDuplicatePayload(t *testing.T) {
	marker := fmt.Sprintf(`{"tables":[{"schema":"public","name":"table-%d"}]}`, time.Now().UnixNano())
	before := PostGISCatalogPayloadGeneration()
	SetPostGISCatalogPayload(marker)
	afterFirst := PostGISCatalogPayloadGeneration()
	if afterFirst <= before {
		t.Fatalf("distinct PostGIS catalog generation = %d after %d, want advancement", afterFirst, before)
	}
	SetPostGISCatalogPayload(marker)
	if afterDuplicate := PostGISCatalogPayloadGeneration(); afterDuplicate != afterFirst {
		t.Fatalf("duplicate PostGIS catalog generation = %d, want unchanged %d", afterDuplicate, afterFirst)
	}
	SetPostGISCatalogPayload(marker + " ")
	if afterNext := PostGISCatalogPayloadGeneration(); afterNext <= afterFirst {
		t.Fatalf("next PostGIS catalog generation = %d after %d, want advancement", afterNext, afterFirst)
	}
}

func TestVertexHandleGenerationSkipsDuplicatePayload(t *testing.T) {
	marker := fmt.Sprintf(`[{"vertexIndex":%d,"x":0.5,"y":0.5}]`, time.Now().UnixNano())
	before := VertexHandleGeneration()
	SetVertexHandlePayload(marker)
	afterFirst := VertexHandleGeneration()
	if afterFirst <= before {
		t.Fatalf("distinct vertex handle generation = %d after %d, want advancement", afterFirst, before)
	}
	SetVertexHandlePayload(marker)
	if afterDuplicate := VertexHandleGeneration(); afterDuplicate != afterFirst {
		t.Fatalf("duplicate vertex handle generation = %d, want unchanged %d", afterDuplicate, afterFirst)
	}
	SetVertexHandlePayload(marker + " ")
	if afterNext := VertexHandleGeneration(); afterNext <= afterFirst {
		t.Fatalf("next vertex handle generation = %d after %d, want advancement", afterNext, afterFirst)
	}
}

func TestSelectionGenerationSkipsDuplicateState(t *testing.T) {
	marker := fmt.Sprintf("feature-%d", time.Now().UnixNano())
	before := SelectionGeneration()
	SetSelection("roads", marker, "name", "Selected")
	afterFirst := SelectionGeneration()
	if afterFirst <= before {
		t.Fatalf("distinct selection generation = %d after %d, want advancement", afterFirst, before)
	}
	SetSelection("roads", marker, "name", "Selected")
	if afterDuplicate := SelectionGeneration(); afterDuplicate != afterFirst {
		t.Fatalf("duplicate selection generation = %d, want unchanged %d", afterDuplicate, afterFirst)
	}
	SetSelection("roads", marker, "changed", "Selected")
	if afterNext := SelectionGeneration(); afterNext <= afterFirst {
		t.Fatalf("next selection generation = %d after %d, want advancement", afterNext, afterFirst)
	}
}

func TestMapMetadataGenerationSkipsDuplicatePayload(t *testing.T) {
	before := MapMetadataGeneration()
	extent := [4]float64{126, 36, 128, 38}
	SetMapMetadata("EPSG:4326", extent)
	afterFirst := MapMetadataGeneration()
	if afterFirst <= before {
		t.Fatalf("distinct map metadata generation = %d after %d, want advancement", afterFirst, before)
	}
	SetMapMetadata("EPSG:4326", extent)
	if afterDuplicate := MapMetadataGeneration(); afterDuplicate != afterFirst {
		t.Fatalf("duplicate map metadata generation = %d, want unchanged %d", afterDuplicate, afterFirst)
	}
	SetMapMetadata("EPSG:4326", [4]float64{126, 36, 129, 38})
	if afterNext := MapMetadataGeneration(); afterNext <= afterFirst {
		t.Fatalf("next map metadata generation = %d after %d, want advancement", afterNext, afterFirst)
	}
}

func TestSetVerticesCopiesAndClearsNativeBatch(t *testing.T) {
	vertices := []render.Vertex{
		{X: 0.125, Y: 0.25, Color: 0x112233ff, SizeMM: 1.5, Kind: 0},
		{X: 0.75, Y: 0.875, Color: 0x445566ff, SizeMM: 1.5, Kind: 0},
	}
	SetVertices(vertices)
	if retained := retainedNativeVertexBytes(); retained == 0 {
		t.Fatal("native bridge did not retain its owned vertex copy")
	}
	// The C++ bridge must own its copy after SetVertices returns; changing and
	// releasing this Go slice must not leave a retained Go pointer in native code.
	vertices[0] = render.Vertex{}
	vertices = nil
	SetVertices(nil)
	if retained := retainedNativeVertexBytes(); retained != 0 {
		t.Fatalf("empty viewport retained %d native vertex bytes", retained)
	}
}

func TestNativeVertexBufferShrinksAfterViewportReduction(t *testing.T) {
	SetVertices(make([]render.Vertex, 100_000))
	large := retainedNativeVertexBytes()
	if large < 100_000*20 {
		t.Fatalf("large native vertex buffer = %d bytes", large)
	}
	SetVertices([]render.Vertex{{X: 1, Y: 1}})
	small := retainedNativeVertexBytes()
	if small >= large {
		t.Fatalf("native vertex buffer did not shrink: before=%d after=%d", large, small)
	}
	SetVertices(nil)
}

func BenchmarkSetVertices100K(b *testing.B) {
	vertices := make([]render.Vertex, 100_000)
	for index := range vertices {
		vertices[index] = render.Vertex{X: float32(index) / 100_000, Y: 0.5}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		SetVertices(vertices)
	}
}
