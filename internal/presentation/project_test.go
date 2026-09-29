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
	rows := AttributeTable(project.Layers[0])
	tree[0].Name = "changed"
	rows[0].Values["name"] = "changed"

	if project.Layers[0].Name != "roads" {
		t.Fatal("layer tree changed the source project")
	}
	if project.Layers[0].Features[0].Properties["name"] != "한글 도로" {
		t.Fatal("attribute table changed the source feature")
	}
}
