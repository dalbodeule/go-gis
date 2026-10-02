package commands

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"testing"

	"gogis/internal/core"
)

var benchmarkProjectSink any

func benchmarkProjectService10K(b *testing.B) *ProjectService {
	b.Helper()
	features := make([]core.Feature, 10_000)
	for index := range features {
		features[index] = core.Feature{
			ID:         uint64(index + 1),
			Geometry:   core.WKTGeometry{WKT: fmt.Sprintf("POINT (%d %d)", index, index)},
			Properties: map[string]any{"name": fmt.Sprintf("road-%d", index)},
		}
	}
	service := NewProjectService("benchmark", core.CRS{AuthorityCode: "EPSG:4326"})
	if err := service.BeginEdit(); err != nil {
		b.Fatal(err)
	}
	if err := service.AddLayer(core.Layer{Name: "roads", Features: features}); err != nil {
		b.Fatal(err)
	}
	if err := service.Commit(); err != nil {
		b.Fatal(err)
	}
	return service
}

func benchmarkMultiLayerProjectService10K(b *testing.B) *ProjectService {
	b.Helper()
	service := NewProjectService("benchmark", core.CRS{AuthorityCode: "EPSG:4326"})
	if err := service.BeginEdit(); err != nil {
		b.Fatal(err)
	}
	for layerIndex := 0; layerIndex < 4; layerIndex++ {
		features := make([]core.Feature, 2_500)
		for index := range features {
			id := uint64(layerIndex*2_500 + index + 1)
			features[index] = core.Feature{
				ID:         id,
				Geometry:   core.WKTGeometry{WKT: fmt.Sprintf("POINT (%d %d)", index, layerIndex)},
				Properties: map[string]any{"name": fmt.Sprintf("road-%d", id)},
			}
		}
		if err := service.AddLayer(core.Layer{Name: fmt.Sprintf("layer-%d", layerIndex), Features: features}); err != nil {
			b.Fatal(err)
		}
	}
	if err := service.Commit(); err != nil {
		b.Fatal(err)
	}
	return service
}

func BenchmarkProjectSnapshot10KFeatures(b *testing.B) {
	service := benchmarkProjectService10K(b)
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		benchmarkProjectSink = service.Project()
	}
}

func BenchmarkProjectRenderSnapshot10KFeatures(b *testing.B) {
	service := benchmarkProjectService10K(b)
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		benchmarkProjectSink = service.ProjectRenderSnapshot()
	}
}

func BenchmarkBeginEdit10KFeatures(b *testing.B) {
	service := benchmarkProjectService10K(b)
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if err := service.BeginEdit(); err != nil {
			b.Fatal(err)
		}
		if err := service.Rollback(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSaveLayer10KFeatures(b *testing.B) {
	service := benchmarkProjectService10K(b)
	writer := &recordingLayerWriter{}
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if err := service.SaveLayer(context.Background(), writer, "roads.gpkg", "roads"); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSaveLayerFullProjectCloneBaseline10KFeatures(b *testing.B) {
	service := benchmarkProjectService10K(b)
	writer := &recordingLayerWriter{}
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		project := service.Project()
		if err := writer.Write(context.Background(), "roads.gpkg", project.Layers[0].Clone()); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkGenerateLabelsPublic10KFeatures(b *testing.B) {
	service := benchmarkProjectService10K(b)
	layer, ok := service.Layer("roads")
	if !ok {
		b.Fatal("benchmark layer missing")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		result, err := GenerateLabels(context.Background(), layer, "name", 1, "default")
		if err != nil {
			b.Fatal(err)
		}
		benchmarkProjectSink = result
	}
}

func BenchmarkGenerateLabelsOwned10KFeatures(b *testing.B) {
	service := benchmarkProjectService10K(b)
	layer, ok := service.Layer("roads")
	if !ok {
		b.Fatal("benchmark layer missing")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		result, err := generateLabels(context.Background(), layer, "name", 1, "default", false)
		if err != nil {
			b.Fatal(err)
		}
		benchmarkProjectSink = result
	}
}

func BenchmarkGenerateLabelsProjectOwned10KFeatures(b *testing.B) {
	service := benchmarkProjectService10K(b)
	layer := service.project.Layers[0]
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		result, err := generateLabels(context.Background(), layer, "name", 1, "default", false)
		if err != nil {
			b.Fatal(err)
		}
		benchmarkProjectSink = result
	}
}

func BenchmarkGenerateLabelsWKT10KLines(b *testing.B) {
	features := make([]core.Feature, 10_000)
	for index := range features {
		features[index] = core.Feature{
			ID:         uint64(index + 1),
			Geometry:   core.WKTGeometry{WKT: "LINESTRING (0 0, 2 0, 2 10)"},
			Properties: map[string]any{"name": "road"},
		}
	}
	benchmarkGenerateLabelsOwned(b, core.Layer{Features: features})
}

func BenchmarkGenerateLabelsWKT10KPolygons(b *testing.B) {
	features := make([]core.Feature, 10_000)
	for index := range features {
		features[index] = core.Feature{
			ID:         uint64(index + 1),
			Geometry:   core.WKTGeometry{WKT: "POLYGON ((0 0, 10 0, 10 10, 0 10, 0 0))"},
			Properties: map[string]any{"name": "area"},
		}
	}
	benchmarkGenerateLabelsOwned(b, core.Layer{Features: features})
}

func benchmarkGenerateLabelsOwned(b *testing.B, layer core.Layer) {
	b.Helper()
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		result, err := generateLabels(context.Background(), layer, "name", 1, "default", false)
		if err != nil {
			b.Fatal(err)
		}
		benchmarkProjectSink = result
	}
}

func BenchmarkGenerateLabelsProjectSnapshotBaseline10KFeatures(b *testing.B) {
	service := benchmarkProjectService10K(b)
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		layer, ok := service.Layer("roads")
		if !ok {
			b.Fatal("benchmark layer missing")
		}
		result, err := generateLabels(context.Background(), layer, "name", 1, "default", false)
		if err != nil {
			b.Fatal(err)
		}
		benchmarkProjectSink = result
	}
}

func BenchmarkGenerateLabelsWKB10KPoints(b *testing.B) {
	data, err := hex.DecodeString("0101000000000000000000f03f0000000000000040")
	if err != nil {
		b.Fatal(err)
	}
	features := make([]core.Feature, 10_000)
	for index := range features {
		features[index] = core.Feature{
			ID:         uint64(index + 1),
			Geometry:   core.WKBGeometry{WKB: data},
			Properties: map[string]any{"name": "road"},
		}
	}
	layer := core.Layer{Features: features}
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		result, err := GenerateLabels(context.Background(), layer, "name", 1, "default")
		if err != nil {
			b.Fatal(err)
		}
		benchmarkProjectSink = result
	}
}

func BenchmarkGenerateLabelsWKB10KPolygons(b *testing.B) {
	data := make([]byte, 93)
	data[0] = 1
	binary.LittleEndian.PutUint32(data[1:5], 3)
	binary.LittleEndian.PutUint32(data[5:9], 1)
	binary.LittleEndian.PutUint32(data[9:13], 5)
	for index, point := range [][2]float64{{0, 0}, {10, 0}, {10, 10}, {0, 10}, {0, 0}} {
		offset := 13 + index*16
		binary.LittleEndian.PutUint64(data[offset:offset+8], math.Float64bits(point[0]))
		binary.LittleEndian.PutUint64(data[offset+8:offset+16], math.Float64bits(point[1]))
	}
	features := make([]core.Feature, 10_000)
	for index := range features {
		features[index] = core.Feature{
			ID:         uint64(index + 1),
			Geometry:   core.WKBGeometry{WKB: data},
			Properties: map[string]any{"name": "area"},
		}
	}
	layer := core.Layer{Features: features}
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		result, err := GenerateLabels(context.Background(), layer, "name", 1, "default")
		if err != nil {
			b.Fatal(err)
		}
		benchmarkProjectSink = result
	}
}

func BenchmarkFilterLayerByProperty10KFeatures(b *testing.B) {
	service := benchmarkProjectService10K(b)
	layer, ok := service.Layer("roads")
	if !ok {
		b.Fatal("benchmark layer missing")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		result, err := FilterLayerByProperty(context.Background(), layer, "name", "road-9999")
		if err != nil {
			b.Fatal(err)
		}
		benchmarkProjectSink = result
	}
}

func BenchmarkFilterLayerByNumericProperty10KFeatures(b *testing.B) {
	service := benchmarkProjectService10K(b)
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		result, err := FilterLayerByProperty(context.Background(), service.project.Layers[0], "value", "9999")
		if err != nil {
			b.Fatal(err)
		}
		benchmarkProjectSink = result
	}
}

func BenchmarkFilterLayerByPropertyCloneBaseline10KFeatures(b *testing.B) {
	service := benchmarkProjectService10K(b)
	layer, ok := service.Layer("roads")
	if !ok {
		b.Fatal("benchmark layer missing")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		result := layer.Clone()
		result.Features = result.Features[:0]
		for _, feature := range layer.Features {
			if value, exists := feature.Properties["name"]; exists && propertyEquals(value, "road-9999") {
				result.Features = append(result.Features, feature.Clone())
			}
		}
		benchmarkProjectSink = result
	}
}

func BenchmarkFilterProjectLayerOwned10KFeatures(b *testing.B) {
	service := benchmarkProjectService10K(b)
	layer := service.project.Layers[0]
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		result, err := filterLayerByProperty(context.Background(), layer, "name", "road-9999", false)
		if err != nil {
			b.Fatal(err)
		}
		benchmarkProjectSink = result
	}
}

func BenchmarkSpatialInputSnapshot10KFeatures(b *testing.B) {
	service := benchmarkProjectService10K(b)
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		left, ok := service.Layer("roads")
		if !ok {
			b.Fatal("benchmark layer missing")
		}
		benchmarkProjectSink = left
	}
}

func BenchmarkSpatialInputProjectCloneBaseline10KFeatures(b *testing.B) {
	service := benchmarkProjectService10K(b)
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		project := service.Project()
		benchmarkProjectSink = project.Layers[0]
	}
}

func BenchmarkSpatialInputNamedLayerMulti10KFeatures(b *testing.B) {
	service := benchmarkMultiLayerProjectService10K(b)
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		layer, ok := service.Layer("layer-2")
		if !ok {
			b.Fatal("benchmark layer missing")
		}
		benchmarkProjectSink = layer
	}
}

func BenchmarkSpatialInputProjectCloneMulti10KFeatures(b *testing.B) {
	service := benchmarkMultiLayerProjectService10K(b)
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		project := service.Project()
		benchmarkProjectSink = project.Layers[2]
	}
}

func BenchmarkMergeSelectedLayersDetached10KFeatures(b *testing.B) {
	service := benchmarkMultiLayerProjectService10K(b)
	layers := []core.Layer{service.project.Layers[0], service.project.Layers[1]}
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		result, err := mergeLayers(context.Background(), true, layers...)
		if err != nil {
			b.Fatal(err)
		}
		benchmarkProjectSink = result
	}
}

func BenchmarkMergeSelectedLayersOwned10KFeatures(b *testing.B) {
	service := benchmarkMultiLayerProjectService10K(b)
	layers := []core.Layer{service.project.Layers[0], service.project.Layers[1]}
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		result, err := mergeLayers(context.Background(), false, layers...)
		if err != nil {
			b.Fatal(err)
		}
		benchmarkProjectSink = result
	}
}

func BenchmarkLayerNames10KFeatures(b *testing.B) {
	service := benchmarkProjectService10K(b)
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		benchmarkProjectSink = service.LayerNames()
	}
}

func BenchmarkLayerAttributePage200Rows10KFeatures(b *testing.B) {
	service := benchmarkProjectService10K(b)
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		page, _, ok := service.LayerAttributePage("roads", 2_000, 200)
		if !ok {
			b.Fatal("attribute page missing")
		}
		benchmarkProjectSink = page
	}
}

func BenchmarkLayerAttributePageOwned200Rows10KFeatures(b *testing.B) {
	service := benchmarkProjectService10K(b)
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		page, _, ok := service.LayerAttributePageOwned("roads", 2_000, 200)
		if !ok {
			b.Fatal("attribute page missing")
		}
		benchmarkProjectSink = page
	}
}

func BenchmarkHasFeature10KFeatures(b *testing.B) {
	service := benchmarkProjectService10K(b)
	_ = service.HasFeature("roads", 10_000)
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		benchmarkProjectSink = service.HasFeature("roads", 10_000)
	}
}

func BenchmarkFeatureProperty10KFeatures(b *testing.B) {
	service := benchmarkProjectService10K(b)
	_, _ = service.FeatureProperty("roads", 10_000, "name")
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		benchmarkProjectSink, _ = service.FeatureProperty("roads", 10_000, "name")
	}
}

func BenchmarkHasFeatureFirstLookupMultiLayer10KFeatures(b *testing.B) {
	service := benchmarkMultiLayerProjectService10K(b)
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		service.invalidateFeatureIndex()
		benchmarkProjectSink = service.HasFeature("layer-0", 2_500)
	}
}

func BenchmarkHasFeatureLinearBaseline10KFeatures(b *testing.B) {
	service := benchmarkProjectService10K(b)
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		found := false
		for _, feature := range service.project.Layers[0].Features {
			if feature.ID == 10_000 {
				found = true
				break
			}
		}
		benchmarkProjectSink = found
	}
}

func BenchmarkFeaturePropertyLinearBaseline10KFeatures(b *testing.B) {
	service := benchmarkProjectService10K(b)
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		var value any
		for _, feature := range service.project.Layers[0].Features {
			if feature.ID == 10_000 {
				value = feature.Properties["name"]
				break
			}
		}
		benchmarkProjectSink = value
	}
}

func BenchmarkSetFeaturePropertyRepeated10KFeatures(b *testing.B) {
	service := benchmarkProjectService10K(b)
	service.project.Layers[0].Editable = true
	if err := service.BeginEdit(); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if err := service.SetFeatureProperty("roads", 10_000, "name", index); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	if err := service.Rollback(); err != nil {
		b.Fatal(err)
	}
}
