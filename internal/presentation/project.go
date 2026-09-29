// Package presentation contains UI-neutral read models for GUI adapters.
package presentation

import "gogis/internal/core"

// LayerItem is a row in a layer tree.
type LayerItem struct {
	Name     string
	CRS      string
	Editable bool
}

// LayerTree returns a stable, UI-ready layer list.
func LayerTree(project core.Project) []LayerItem {
	items := make([]LayerItem, len(project.Layers))
	for i, layer := range project.Layers {
		items[i] = LayerItem{
			Name:     layer.Name,
			CRS:      layer.CRS.AuthorityCode,
			Editable: layer.Editable,
		}
	}
	return items
}

// AttributeRow is a feature row for an attribute table.
type AttributeRow struct {
	FeatureID uint64
	Values    map[string]any
}

// AttributeTable returns detached rows for one layer.
func AttributeTable(layer core.Layer) []AttributeRow {
	rows := make([]AttributeRow, len(layer.Features))
	for i, feature := range layer.Features {
		rows[i] = AttributeRow{FeatureID: feature.ID, Values: cloneProperties(feature.Properties)}
	}
	return rows
}

func cloneProperties(properties map[string]any) map[string]any {
	if properties == nil {
		return nil
	}
	clone := make(map[string]any, len(properties))
	for key, value := range properties {
		clone[key] = value
	}
	return clone
}
