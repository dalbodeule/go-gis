//go:build qt && native

package main

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"gogis/internal/render"
	"gogis/ui/qt/native"
)

// The workspace and its source SHPs stay outside the repository. Set the
// environment variable only when validating the user's local data.
func TestGongjuWorkspaceLayerSchemaAndFiltersRealSource(t *testing.T) {
	path := os.Getenv("GOGIS_TEST_GONGJU_WORKSPACE")
	if path == "" {
		t.Skip("set GOGIS_TEST_GONGJU_WORKSPACE")
	}
	previousIndexPolicy := activeShapefileIndexPolicy
	configureShapefileIndexPolicy(nil)
	defer func() { activeShapefileIndexPolicy = previousIndexPolicy }()
	start := time.Now()
	runtime, err := loadWorkspaceRuntime(context.Background(), path, true, "")
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.closeAttributeSource()
	if !runtime.viewportReadOnly || len(runtime.features) != 0 {
		t.Fatalf("read-only=%t retained=%d", runtime.viewportReadOnly, len(runtime.features))
	}
	names := runtime.service.LayerNames()
	if len(names) != 3 {
		t.Fatalf("loaded %d layers: %v", len(names), names)
	}
	for _, name := range names {
		binding, ok := runtime.readOnlyBindings[name]
		if !ok {
			t.Fatalf("missing read-only layer %q", name)
		}
		metadata := binding.layer
		if metadata.GeometryType == "" {
			t.Fatalf("layer %q lost its source geometry type", name)
		}
		page, total, err := runtime.attributePageReader(context.Background(), name, 0, 1)
		if err != nil || len(page.Fields) != len(metadata.Fields) {
			t.Fatalf("layer %q page fields=%d metadata=%d err=%v", name, len(page.Fields), len(metadata.Fields), err)
		}
		fields := make(map[string]bool)
		for i, field := range page.Fields {
			if field != metadata.Fields[i] {
				t.Fatalf("layer %q field %d differs: page=%v metadata=%v", name, i, field, metadata.Fields[i])
			}
			fields[field.Name] = true
		}
		if total != binding.featureCount {
			t.Fatalf("layer %q count=%d binding=%d exists=%t", name, total, binding.featureCount, ok)
		}
		accepted := -1
		if binding.acceptedIDs != nil {
			accepted = binding.acceptedIDs.Count()
		}
		if metadata.DisplayRule != "" && (accepted < 0 || accepted >= total) {
			t.Fatalf("layer %q filter accepted=%d source=%d", name, accepted, total)
		}
		if name == "LPTD_LDREG_DOGEUN_POINT_INFO_5186_44_202611" &&
			(!fields["POINT_X"] || !fields["POINT_Y"] || fields["PNU"] || fields["JIBUN"] || fields["BCHK"]) {
			t.Fatalf("point layer has mixed fields: %v", fields)
		}
		if name == "LSMD_CONT_LDREG_44150_202611" && (!fields["PNU"] || fields["POINT_X"]) {
			t.Fatalf("parcel layer has mixed fields: %v", fields)
		}
		t.Logf("layer=%s source=%d accepted=%d fields=%d", name, total, accepted, len(page.Fields))
	}
	t.Logf("load=%s mapExtent=%v fitExtent=%v", time.Since(start).Round(time.Millisecond), runtime.mapExtent, runtime.mapFitExtent)
	if runtime.mapExtent[2] < 1_000_000 || runtime.mapFitExtent[2] > 1_000_000 {
		t.Fatal("distant coordinate outlier distorted the preferred fit")
	}
	if runtime.workspaceView == nil {
		t.Fatal("workspace has no saved viewport")
	}
	view := runtime.workspaceView
	viewport := render.Viewport{Center: render.Point{X: view.CenterX, Y: view.CenterY}, Zoom: view.Zoom,
		ScreenWidth: 1172, ScreenHeight: 732, CanvasWidth: 640, CanvasHeight: 732}
	startEntry := diagnosticEntryCount(t, native.DiagnosticLogJSON())
	runtime.refresh(context.Background(), viewport)
	terminal := waitForRenderTerminalDiagnostic(t, startEntry, 60*time.Second)
	if !strings.Contains(terminal, "ready generation=") ||
		!strings.Contains(terminal, `fill_vertices_by_layer={"AL_D010_44_20261109":`) {
		t.Fatalf("saved view lost filtered building fills: %s", terminal)
	}
	t.Logf("saved viewport render: %s", terminal)
	// Nearby dense views need exact detail, including polygon fills. Moving
	// between them also exercises release of offscreen window payloads.
	for _, target := range []struct {
		center render.Point
		zoom   float64
	}{
		{render.Point{X: 0.0518, Y: 0.1025}, 256},
		{render.Point{X: 0.0518, Y: 0.1025}, 512},
		{render.Point{X: 0.0553, Y: 0.1000}, 256},
		{render.Point{X: 0.0570, Y: 0.1030}, 256},
		{render.Point{X: 0.0530, Y: 0.0990}, 256},
	} {
		viewport.Center, viewport.Zoom = target.center, target.zoom
		if lod := readOnlyOverviewZoomBucket(readOnlyWindowZoomBucket(viewport.Zoom), runtime.mapExtent, runtime.mapFitExtent); lod <= 1 {
			t.Fatalf("dense viewport %v at zoom %g remained an approximate overview: LOD=%d", target.center, target.zoom, lod)
		}
		startEntry = diagnosticEntryCount(t, native.DiagnosticLogJSON())
		runtime.refresh(context.Background(), viewport)
		dense := waitForRenderTerminalDiagnostic(t, startEntry, 90*time.Second)
		if !strings.Contains(dense, "ready generation=") ||
			!strings.Contains(dense, `fill_vertices_by_layer={"AL_D010_44_20261109":`) {
			t.Fatalf("dense viewport %v at zoom %g lost exact building fills: %s", target.center, target.zoom, dense)
		}
		var diagnostics []struct{ Stream, Message string }
		if err := json.Unmarshal([]byte(native.DiagnosticLogJSON()), &diagnostics); err != nil {
			t.Fatal(err)
		}
		for _, entry := range diagnostics[min(startEntry, len(diagnostics)):] {
			if entry.Stream == "render" && (strings.Contains(entry.Message, "retry generation=") ||
				strings.Contains(entry.Message, "outline-only layer=AL_D010_44_20261109")) {
				t.Fatalf("dense viewport used an approximate building render: %s", entry.Message)
			}
		}
		runtime.mu.Lock()
		payloadBytes := runtime.windowVisiblePayloadBytes
		payloadLimit := runtime.readOnlyPayloadLimitLocked()
		retainedFeatures := runtime.windowVisibleFeatureCount
		retainedWindows := len(runtime.windowPayloadBytes)
		for key := range runtime.windowPayloadBytes {
			if _, visible := runtime.windowVisibleKeys[key]; !visible {
				runtime.mu.Unlock()
				t.Fatalf("offscreen window %v remained in the payload cache", key)
			}
		}
		runtime.mu.Unlock()
		if payloadBytes > payloadLimit || retainedFeatures > defaultVisibleFeatureLimit {
			t.Fatalf("dense viewport retained too much data: %d bytes/%d, %d features", payloadBytes, payloadLimit, retainedFeatures)
		}
		t.Logf("dense retention: windows=%d features=%d payloadMiB=%.1f", retainedWindows, retainedFeatures, float64(payloadBytes)/(1<<20))
		t.Logf("dense detail center=%v zoom=%g render: %s", target.center, target.zoom, dense)
	}
}
