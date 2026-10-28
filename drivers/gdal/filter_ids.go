//go:build native

package gdal

import (
	"context"
	"fmt"
	"math"
	"path/filepath"
	"strings"
)

const maxStringFilterIndexedFeatures = 50_000_000

// FeatureIDSet holds one bit per source SHP feature. It contains no geometry
// or attributes and is read-only after construction.
type FeatureIDSet struct {
	bits      []uint64
	count     int
	limit     int
	bounds    [4]float64
	hasBounds bool
	allBounds bool
}

func (set *FeatureIDSet) Count() int {
	if set == nil {
		return 0
	}
	return set.count
}

// MayInclude returns true for an unknown FID so an unusual driver result
// still reaches the authoritative Lua predicate.
func (set *FeatureIDSet) MayInclude(fid int64) bool {
	if set == nil || fid < 0 || fid >= int64(set.limit) {
		return true
	}
	return set.bits[fid/64]&(uint64(1)<<uint(fid%64)) != 0
}

// MayIntersect conservatively rejects windows outside every accepted feature
// envelope. Unknown or unreadable envelopes keep the original query path.
func (set *FeatureIDSet) MayIntersect(bounds [4]float64) bool {
	if set == nil {
		return true
	}
	if set.count == 0 {
		return false
	}
	if !set.allBounds || !set.hasBounds {
		return true
	}
	return bounds[2] >= set.bounds[0] && bounds[0] <= set.bounds[2] &&
		bounds[3] >= set.bounds[1] && bounds[1] <= set.bounds[3]
}

// BuildStringFilterIDSet evaluates a safe string condition once over the
// source SHP and records accepted source FIDs. Window reads keep their QIX
// spatial query, checking this small bitset before decoding attributes.
func (s *AttributeSession) BuildStringFilterIDSet(ctx context.Context, layerName string, sourceFeatureCount int, filter StringAttributeFilter) (*FeatureIDSet, error) {
	if !validStringAttributeFilter(filter) {
		return nil, fmt.Errorf("invalid string attribute prefilter")
	}
	if !strings.EqualFold(filepath.Ext(s.source), ".shp") {
		return nil, fmt.Errorf("string filter FID index requires a shapefile")
	}
	if sourceFeatureCount < 0 || sourceFeatureCount > maxStringFilterIndexedFeatures {
		return nil, fmt.Errorf("source feature count %d exceeds the filter-index limit", sourceFeatureCount)
	}
	if err := s.lockContext(ctx); err != nil {
		return nil, err
	}
	defer s.mu.Unlock()
	if s.streamGeoJSON || s.dataset == nil {
		return nil, fmt.Errorf("string filter index requires an open vector dataset")
	}
	_, resolvedName, err := s.layerForName(layerName)
	if err != nil {
		return nil, err
	}
	field := quoteSQLIdentifier(filter.Field)
	query := "SELECT FID AS gogis_fid FROM " + quoteSQLIdentifier(resolvedName)
	if filter.Prefix {
		query += " WHERE (" + field + " LIKE '" + filter.Value + "%' OR " + field + " IS NULL)"
	} else {
		query += " WHERE " + field + " = '" + filter.Value + "'"
	}
	result, err := s.dataset.ExecuteSQL(query)
	if err != nil {
		return nil, fmt.Errorf("build string filter index for %q: %w", resolvedName, err)
	}
	set := &FeatureIDSet{bits: make([]uint64, (sourceFeatureCount+63)/64), limit: sourceFeatureCount, allBounds: true}
	if result == nil {
		return nil, fmt.Errorf("string filter index returned no result layer")
	}
	defer result.Close()
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		feature := result.Layer.NextFeature()
		if feature == nil {
			break
		}
		fid := feature.FID()
		geometry := feature.Geometry()
		if geometry.IsNull() || geometry.Empty() {
			set.allBounds = false
		} else if envelope, boundsErr := geometry.Bounds(); boundsErr != nil ||
			!validFilterEnvelope(envelope) {
			set.allBounds = false
		} else if !set.hasBounds {
			set.bounds, set.hasBounds = envelope, true
		} else {
			set.bounds[0] = math.Min(set.bounds[0], envelope[0])
			set.bounds[1] = math.Min(set.bounds[1], envelope[1])
			set.bounds[2] = math.Max(set.bounds[2], envelope[2])
			set.bounds[3] = math.Max(set.bounds[3], envelope[3])
		}
		if geometry != nil {
			geometry.Close()
		}
		feature.Close()
		if fid < 0 || fid >= int64(sourceFeatureCount) {
			return nil, fmt.Errorf("filter index returned out-of-range source FID %d", fid)
		}
		word := fid / 64
		mask := uint64(1) << uint(fid%64)
		if set.bits[word]&mask == 0 {
			set.bits[word] |= mask
			set.count++
		}
	}
	if set.hasBounds {
		set.bounds[0] = math.Nextafter(set.bounds[0], math.Inf(-1))
		set.bounds[1] = math.Nextafter(set.bounds[1], math.Inf(-1))
		set.bounds[2] = math.Nextafter(set.bounds[2], math.Inf(1))
		set.bounds[3] = math.Nextafter(set.bounds[3], math.Inf(1))
	}
	return set, nil
}

func validFilterEnvelope(bounds [4]float64) bool {
	if bounds[0] > bounds[2] || bounds[1] > bounds[3] {
		return false
	}
	for _, value := range bounds {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return false
		}
	}
	return true
}
