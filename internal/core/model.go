// Package core contains format-independent GIS domain models.
package core

import "maps"

// CRS identifies a coordinate reference system without coupling the core to PROJ.
type CRS struct {
	// AuthorityCode is typically an EPSG code such as "EPSG:4326".
	AuthorityCode string
}

// Geometry is implemented by format-specific drivers while remaining usable by
// the format-independent core and command layer.
type Geometry interface {
	GeometryType() string
	Clone() Geometry
}

// FieldType describes the initial attribute types understood by the core.
type FieldType string

const (
	FieldTypeText   FieldType = "text"
	FieldTypeNumber FieldType = "number"
	FieldTypeBool   FieldType = "bool"
)

// Field describes one attribute column.
type Field struct {
	Name string
	Type FieldType
}

// Feature is a vector feature with an application-level identifier.
type Feature struct {
	ID         uint64
	Geometry   Geometry
	Properties map[string]any
}

// Clone returns a detached copy suitable for edit snapshots.
func (f Feature) Clone() Feature {
	clone := f
	clone.Properties = maps.Clone(f.Properties)
	if f.Geometry != nil {
		clone.Geometry = f.Geometry.Clone()
	}
	return clone
}

// Layer is the initial in-memory representation of a vector layer.
type Layer struct {
	Name     string
	CRS      CRS
	Fields   []Field
	Features []Feature
	Editable bool
}

// Clone returns a detached copy suitable for edit snapshots.
func (l Layer) Clone() Layer {
	clone := l
	clone.Fields = append([]Field(nil), l.Fields...)
	clone.Features = make([]Feature, len(l.Features))
	for i, feature := range l.Features {
		clone.Features[i] = feature.Clone()
	}
	return clone
}

// Project groups the layers displayed and processed together.
type Project struct {
	Name   string
	CRS    CRS
	Layers []Layer
}

// Clone returns a detached copy suitable for edit snapshots.
func (p Project) Clone() Project {
	clone := p
	clone.Layers = make([]Layer, len(p.Layers))
	for i, layer := range p.Layers {
		clone.Layers[i] = layer.Clone()
	}
	return clone
}
