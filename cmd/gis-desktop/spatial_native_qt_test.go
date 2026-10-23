//go:build qt && native

package main

import (
	"context"
	"testing"
	"time"

	"gogis/internal/core"
)

func TestApplySpatialRequestBuildsResultLayerInDesktopRuntime(t *testing.T) {
	ctx := context.Background()
	runtime, err := buildDataRuntime(ctx, []core.Layer{
		{
			Name: "left", CRS: core.CRS{AuthorityCode: "EPSG:4326"}, Editable: true,
			Features: []core.Feature{{ID: 1, Geometry: core.WKTGeometry{WKT: "POLYGON ((0 0, 10 0, 10 10, 0 10, 0 0))"}}},
		},
		{
			Name: "right", CRS: core.CRS{AuthorityCode: "EPSG:4326"}, Editable: true,
			Features: []core.Feature{{ID: 2, Geometry: core.WKTGeometry{WKT: "POLYGON ((5 5, 15 5, 15 15, 5 15, 5 5))"}}},
		},
	}, "", "", "", "", "", false, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if runtime.closeAttributeSource != nil {
			runtime.closeAttributeSource()
		}
	})

	if err := applySpatialRequest(runtime, `{"operation":"intersect","left":"left","right":"right","result":"intersection"}`); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		runtime.mu.Lock()
		service := runtime.service
		_, found := service.Layer("intersection")
		operationRunning := runtime.operationCancel != nil
		runtime.mu.Unlock()
		if found && !operationRunning {
			layer, ok := service.Layer("intersection")
			if !ok || len(layer.Features) == 0 {
				t.Fatal("spatial result layer has no features")
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("desktop spatial operation did not publish its result layer")
}
