package presentation

import (
	"testing"

	"gogis/internal/core"
)

func TestLayerTreeAndAttributeTableAreDetached(t *testing.T) {
	project := core.Project{Layers: []core.Layer{{
		Name: "roads", CRS: core.CRS{AuthorityCode: "EPSG:5179"}, Editable: true,
		Features: []core.Feature{{ID: 7, Properties: map[string]any{"name": "한글 도로"}}},
	}}}

	tree := LayerTree(project)
	table := AttributeTable(project.Layers[0])
	tree[0].Name = "changed"
	table.Rows[0].Values["name"] = "changed"

	if project.Layers[0].Name != "roads" {
		t.Fatal("layer tree changed the source project")
	}
	if project.Layers[0].Features[0].Properties["name"] != "한글 도로" {
		t.Fatal("attribute table changed the source feature")
	}
}

func TestAttributeTableAddsUndeclaredKeysDeterministically(t *testing.T) {
	layer := core.Layer{Features: []core.Feature{
		{ID: 1, Properties: map[string]any{"z": true, "a": 12}},
		{ID: 2, Properties: map[string]any{"a": 13}},
	}}
	table := AttributeTable(layer)
	if len(table.Columns) != 2 || table.Columns[0].Name != "a" || table.Columns[1].Name != "z" {
		t.Fatalf("unexpected columns: %#v", table.Columns)
	}
	if table.Columns[0].Type != core.FieldTypeNumber || table.Columns[1].Type != core.FieldTypeBool {
		t.Fatalf("unexpected inferred types: %#v", table.Columns)
	}
}

func TestRenderProgressMessage(t *testing.T) {
	if got := (RenderProgress{Phase: "Loading", Completed: 2, Total: 5}).Message(); got != "Loading 2/5" {
		t.Fatalf("unexpected progress message: %q", got)
	}
	if got := (RenderProgress{Phase: "Ready"}).Message(); got != "Ready" {
		t.Fatalf("unexpected terminal message: %q", got)
	}
}
