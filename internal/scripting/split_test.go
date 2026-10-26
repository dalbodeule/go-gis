package scripting

import (
	"context"
	"testing"

	"gogis/internal/commands"
	"gogis/internal/core"
)

func TestLuaFilterAndSplitLayer(t *testing.T) {
	style := core.DefaultLayerStyle()
	style.PointColor = "#123456"
	layer := core.Layer{Name: "source", CRS: core.CRS{AuthorityCode: "EPSG:4326"}, SourcePath: "original.shp", Style: style, Features: []core.Feature{
		{ID: 1, Properties: map[string]any{"grade": "A"}},
		{ID: 2, Properties: map[string]any{"grade": "B"}},
		{ID: 3, Properties: map[string]any{"grade": "A"}},
	}}
	filtered, err := FilterLayerByLua(context.Background(), layer, `return feature.grade == "A"`)
	if err != nil || len(filtered.Features) != 2 {
		t.Fatalf("filtered=%+v err=%v", filtered, err)
	}
	iterated, err := FilterLayerByLua(context.Background(), layer, `local count = 0; for _ in pairs(feature) do count = count + 1 end; return count == 1 and feature.grade == "A"`)
	if err != nil || len(iterated.Features) != 2 {
		t.Fatalf("filter using pairs(feature)=%+v err=%v", iterated, err)
	}
	if filtered.Style != style || filtered.SourcePath != "" || !filtered.Visible {
		t.Fatalf("filtered presentation/source metadata = %+v", filtered)
	}
	groups, err := SplitLayerByLua(context.Background(), layer, "result", `return feature.grade`, 2)
	if err != nil || len(groups) != 2 || groups[0].Name != "result_A" || len(groups[0].Features) != 2 || groups[1].Name != "result_B" {
		t.Fatalf("groups=%+v err=%v", groups, err)
	}
	if _, err := SplitLayerByLua(context.Background(), layer, "result", `return feature.grade`, 1); err == nil {
		t.Fatal("expected result layer limit")
	}
	if _, err := FilterLayerByLua(context.Background(), layer, `return feature.grade`); err == nil {
		t.Fatal("expected boolean type error")
	}
}

func TestRuntimeSplitLuaAddsGroupedLayers(t *testing.T) {
	service, err := commands.NewProjectServiceWithLayers("test", core.CRS{AuthorityCode: "EPSG:4326"}, []core.Layer{{Name: "source", CRS: core.CRS{AuthorityCode: "EPSG:4326"}, Features: []core.Feature{{ID: 1, Properties: map[string]any{"grade": "A"}}, {ID: 2, Properties: map[string]any{"grade": "B"}}}}})
	if err != nil {
		t.Fatal(err)
	}
	runtime := NewRuntime(service, nil)
	defer runtime.Close()
	if err := runtime.Run(context.Background(), `gogis.split_lua("source", "part", 'return feature.grade')`); err != nil {
		t.Fatal(err)
	}
	if layer, ok := service.Layer("part_A"); !ok || len(layer.Features) != 1 {
		t.Fatalf("part_A = %+v, found=%t", layer, ok)
	}
}
