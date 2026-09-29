// Package presentation contains UI-neutral read models for GUI adapters.
package presentation

import (
	"fmt"
	"sort"

	"gogis/internal/core"
)

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

// AttributeColumn describes one stable attribute-table column.
type AttributeColumn struct {
	Name string
	Type core.FieldType
}

// AttributeTableModel is a detached, UI-neutral attribute table read model.
// Columns are explicit so adapters do not need to infer schema from rows.
type AttributeTableModel struct {
	Columns []AttributeColumn
	Rows    []AttributeRow
}

// AttributeTable returns a detached attribute-table model for one layer.
func AttributeTable(layer core.Layer) AttributeTableModel {
	columns := make([]AttributeColumn, 0, len(layer.Fields))
	known := make(map[string]struct{}, len(layer.Fields))
	for _, field := range layer.Fields {
		if field.Name == "" {
			continue
		}
		columns = append(columns, AttributeColumn{Name: field.Name, Type: field.Type})
		known[field.Name] = struct{}{}
	}

	// Drivers may provide features before a complete schema is available. Add
	// those keys deterministically instead of silently dropping them.
	missing := map[string]struct{}{}
	for _, feature := range layer.Features {
		for key := range feature.Properties {
			if _, ok := known[key]; !ok {
				missing[key] = struct{}{}
			}
		}
	}
	missingNames := make([]string, 0, len(missing))
	for name := range missing {
		missingNames = append(missingNames, name)
	}
	sort.Strings(missingNames)
	for _, name := range missingNames {
		columns = append(columns, AttributeColumn{Name: name, Type: inferFieldType(layer.Features, name)})
	}

	rows := make([]AttributeRow, len(layer.Features))
	for i, feature := range layer.Features {
		rows[i] = AttributeRow{FeatureID: feature.ID, Values: cloneProperties(feature.Properties)}
	}
	return AttributeTableModel{Columns: columns, Rows: rows}
}

func inferFieldType(features []core.Feature, name string) core.FieldType {
	for _, feature := range features {
		value, ok := feature.Properties[name]
		if !ok || value == nil {
			continue
		}
		switch value.(type) {
		case bool:
			return core.FieldTypeBool
		case string:
			return core.FieldTypeText
		case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
			return core.FieldTypeNumber
		default:
			return core.FieldTypeText
		}
	}
	return core.FieldTypeText
}

// RenderProgress is the UI-neutral status read model for cancellable work.
type RenderProgress struct {
	Phase       string
	Completed   int
	Total       int
	Cancellable bool
}

// Message formats progress consistently for GUI adapters.
func (p RenderProgress) Message() string {
	if p.Total <= 0 {
		return p.Phase
	}
	return fmt.Sprintf("%s %d/%d", p.Phase, p.Completed, p.Total)
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
