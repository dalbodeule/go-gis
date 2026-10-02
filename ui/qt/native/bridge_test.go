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
