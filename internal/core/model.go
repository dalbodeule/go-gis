// Package core contains format-independent GIS domain models.
package core

// CRS identifies a coordinate reference system without coupling the core to PROJ.
type CRS struct {
	// AuthorityCode is typically an EPSG code such as "EPSG:4326".
	AuthorityCode string
}

// Layer is the smallest initial representation of a vector layer.
type Layer struct {
	Name string
	CRS  CRS
}

// Project groups the layers displayed and processed together.
type Project struct {
	Name   string
	Layers []Layer
}
