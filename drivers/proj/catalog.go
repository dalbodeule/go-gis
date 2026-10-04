package proj

// CatalogEntry describes a non-deprecated, horizontal EPSG CRS from the
// installed PROJ database. Bounds, when present, are WGS84 longitude/latitude
// degrees for its area of use and are not map-coordinate bounds.
type CatalogEntry struct {
	Code      string     `json:"code"`
	Name      string     `json:"name"`
	Area      string     `json:"area,omitempty"`
	Kind      string     `json:"kind"`
	Bounds    [4]float64 `json:"bounds,omitempty"`
	HasBounds bool       `json:"hasBounds,omitempty"`
}
