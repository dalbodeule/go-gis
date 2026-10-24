package commands

import (
	"context"
	"fmt"
	"strings"

	"gogis/internal/core"
)

// SplitPolygonLayerByBoundaries clips polygon features to each identified
// boundary. A native SpatialOperator performs geometry work behind its driver
// boundary; the command layer owns naming, CRS, and attribute policy.
func SplitPolygonLayerByBoundaries(ctx context.Context, operator SpatialOperator, source, boundaries core.Layer, prefix string, maxPairs int) ([]core.Layer, error) {
	if operator == nil {
		return nil, ErrSpatialOperatorMissing
	}
	if source.CRS.AuthorityCode == "" || !strings.EqualFold(source.CRS.AuthorityCode, boundaries.CRS.AuthorityCode) {
		return nil, fmt.Errorf("source and boundary layers require the same explicit CRS")
	}
	if prefix == "" {
		return nil, fmt.Errorf("result prefix is required")
	}
	if maxPairs > 0 && len(source.Features) > 0 && len(boundaries.Features) > maxPairs/len(source.Features) {
		return nil, fmt.Errorf("boundary split exceeds %d geometry comparisons; reduce the input or use CLI", maxPairs)
	}
	for _, field := range source.Fields {
		if strings.EqualFold(field.Name, BoundaryIdentifierField) {
			return nil, fmt.Errorf("source already contains %q", BoundaryIdentifierField)
		}
	}
	if source.GeometryType != "" && !strings.Contains(strings.ToUpper(source.GeometryType), "POLYGON") {
		return nil, fmt.Errorf("boundary split requires polygon source geometry")
	}
	for _, feature := range source.Features {
		if feature.Geometry == nil || !strings.Contains(strings.ToUpper(feature.Geometry.GeometryType()), "POLYGON") {
			return nil, fmt.Errorf("source feature %d is not a polygon", feature.ID)
		}
	}
	results := make([]core.Layer, 0, len(boundaries.Features))
	seen := make(map[string]bool)
	for _, boundary := range boundaries.Features {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if boundary.Geometry == nil || !strings.Contains(strings.ToUpper(boundary.Geometry.GeometryType()), "POLYGON") {
			return nil, fmt.Errorf("boundary feature %d is not a polygon", boundary.ID)
		}
		identifier := strings.TrimSpace(fmt.Sprint(boundary.Properties[BoundaryIdentifierField]))
		if identifier == "" || identifier == "<nil>" || seen[identifier] || len(identifier) > 128 || strings.ContainsAny(identifier, "/\\\r\n\x00") {
			return nil, fmt.Errorf("boundary feature %d has invalid or duplicate shape ID", boundary.ID)
		}
		seen[identifier] = true
		paired := core.Layer{CRS: boundaries.CRS, Features: make([]core.Feature, len(source.Features))}
		for index := range paired.Features {
			paired.Features[index] = core.Feature{ID: source.Features[index].ID, Geometry: boundary.Geometry}
		}
		clipped, err := operator.Intersect(ctx, source, paired)
		if err != nil {
			return nil, fmt.Errorf("clip boundary %s: %w", identifier, err)
		}
		result := core.Layer{Name: prefix + "_" + identifier, DisplayName: prefix + "_" + identifier, CRS: source.CRS, GeometryType: "POLYGON", Editable: true, GeometryLocked: true,
			Fields: append(append([]core.Field(nil), source.Fields...), core.Field{Name: BoundaryIdentifierField, Type: core.FieldTypeText})}
		for _, feature := range clipped.Features {
			if feature.Geometry == nil {
				continue
			}
			wkt, err := core.ToWKT(feature.Geometry)
			if err != nil {
				return nil, err
			}
			if strings.Contains(strings.ToUpper(wkt.WKT), "EMPTY") {
				continue
			}
			if !strings.Contains(strings.ToUpper(feature.Geometry.GeometryType()), "POLYGON") {
				continue // Border-only intersection has no area to deliver.
			}
			if feature.Properties == nil {
				feature.Properties = make(map[string]any)
			}
			feature.Properties[BoundaryIdentifierField] = identifier
			result.Features = append(result.Features, feature)
		}
		if len(result.Features) > 0 {
			// SHP readers expose a synthetic FID; GeoPackage reserves that name.
			fidField := ""
			used := make(map[string]bool, len(result.Fields))
			for _, field := range result.Fields {
				used[strings.ToLower(field.Name)] = true
				if strings.EqualFold(field.Name, "FID") {
					fidField = field.Name
				}
			}
			if fidField != "" {
				name := "source_fid"
				for suffix := 2; used[strings.ToLower(name)]; suffix++ {
					name = fmt.Sprintf("source_fid_%d", suffix)
				}
				for index := range result.Fields {
					if result.Fields[index].Name == fidField {
						result.Fields[index].Name = name
						break
					}
				}
				for index := range result.Features {
					props := result.Features[index].Properties
					if value, exists := props[fidField]; exists {
						props[name] = value
						delete(props, fidField)
					}
				}
			}
			results = append(results, result)
		}
	}
	return results, nil
}
