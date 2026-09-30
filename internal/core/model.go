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

// Label describes a CAD-ready text annotation without coupling the core to a
// particular export format.
type Label struct {
	Text      string
	X         float64
	Y         float64
	AnchorSet bool
	Rotation  float64
	Height    float64
	Style     string
}

// Feature is a vector feature with an application-level identifier.
type Feature struct {
	ID         uint64
	Geometry   Geometry
	Properties map[string]any
	Label      *Label
}

// Clone returns a detached copy suitable for edit snapshots.
func (f Feature) Clone() Feature {
	clone := cloneFeatureWithoutLabel(f)
	if f.Label != nil {
		label := *f.Label
		clone.Label = &label
	}
	return clone
}

func cloneFeatureWithoutLabel(f Feature) Feature {
	clone := f
	clone.Properties = maps.Clone(f.Properties)
	if f.Geometry != nil {
		clone.Geometry = f.Geometry.Clone()
	}
	return clone
}

// Layer is the initial in-memory representation of a vector layer.
type Layer struct {
	Name            string
	DisplayName     string
	SourcePath      string
	SourceLayerName string
	SourceEncoding  string
	SourceCRS       string
	CRS             CRS
	Fields          []Field
	Features        []Feature
	Editable        bool
	Visible         bool
	Style           LayerStyle
	Labels          LabelSettings
}

// LayerStyle stores renderer-independent display options for a vector layer.
// Colors use #RRGGBB or #RRGGBBAA notation.
type LayerStyle struct {
	PointColor   string  `json:"pointColor"`
	LineColor    string  `json:"lineColor"`
	PolygonColor string  `json:"polygonColor"`
	PointSizeMM  float64 `json:"pointSizeMm"`
	LineWidthMM  float64 `json:"lineWidthMm"`
	FillOpacity  float64 `json:"fillOpacity"`
}

// LabelSettings describes data-driven label rendering in map units and
// physical millimeters. Expression is a field name or safe property template;
// LuaScript is reserved for an explicitly enabled, sandboxed label evaluator.
type LabelSettings struct {
	Enabled       bool    `json:"enabled"`
	Expression    string  `json:"expression"`
	LuaScript     string  `json:"luaScript,omitempty"`
	Placement     string  `json:"placement"` // center, center-rotated, or free-angle
	RotationField string  `json:"rotationField,omitempty"`
	HeightMM      float64 `json:"heightMm"`
	MinScale      float64 `json:"minScale,omitempty"`
	MaxScale      float64 `json:"maxScale,omitempty"`
	Rule          string  `json:"rule,omitempty"`
}

// Clone returns a detached copy suitable for edit snapshots.
func (l Layer) Clone() Layer {
	clone := l
	clone.Fields = append([]Field(nil), l.Fields...)
	clone.Features = make([]Feature, len(l.Features))
	wkbBytes := 0
	for _, feature := range l.Features {
		geometry, ok := feature.Geometry.(WKBGeometry)
		if !ok || len(geometry.WKB) == 0 {
			continue
		}
		if wkbBytes > int(^uint(0)>>1)-len(geometry.WKB) {
			wkbBytes = 0
			break
		}
		wkbBytes += len(geometry.WKB)
	}
	var wkbArena []byte
	if wkbBytes > 0 {
		wkbArena = make([]byte, 0, wkbBytes)
	}
	var labelArena []Label
	for i, feature := range l.Features {
		clone.Features[i] = cloneFeatureForLayer(feature, &wkbArena)
		if feature.Label != nil {
			if labelArena == nil {
				labelArena = make([]Label, len(l.Features))
			}
			labelArena[i] = *feature.Label
			clone.Features[i].Label = &labelArena[i]
		}
	}
	return clone
}

func cloneFeatureForLayer(f Feature, wkbArena *[]byte) Feature {
	clone := f
	clone.Properties = maps.Clone(f.Properties)
	if geometry, ok := f.Geometry.(WKBGeometry); ok {
		start := len(*wkbArena)
		*wkbArena = append(*wkbArena, geometry.WKB...)
		clone.Geometry = WKBGeometry{WKB: (*wkbArena)[start:]}
		return clone
	}
	if f.Geometry != nil {
		clone.Geometry = f.Geometry.Clone()
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
