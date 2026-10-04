//go:build qt

package main

import (
	"math"

	"gogis/internal/render"
)

const maxReadOnlyOverviewFeatures = 240_000
const maxReadOnlyDetailWorkers = 2

func readOnlyWindowChunkSize(zoomBucket int) float64 {
	// Coarse overviews use 1/32 windows. Larger cells at high zoom reduce
	// per-viewport GDAL query overhead while detail still increases with zoom.
	zoomBucket = max(-10, min(20, zoomBucket))
	if zoomBucket <= 4 {
		return math.Max(0.0078125, math.Ldexp(0.03125, -max(0, zoomBucket)))
	}
	if zoomBucket >= 7 {
		// The extra high-zoom plateau bounds GDAL window-query overhead when
		// distant point outliers make the normalized data extent much larger
		// than the parcel area. Subdivision still caps dense individual windows.
		return math.Ldexp(0.0078125, -max(2, zoomBucket-6))
	}
	return math.Ldexp(0.0078125, -(zoomBucket - 4))
}

func readOnlyWindowZoomBucket(zoom float64) int {
	if math.IsNaN(zoom) || math.IsInf(zoom, 0) {
		return 0
	}
	if zoom <= 0 {
		return -10
	}
	bucket := int(math.Floor(math.Log2(zoom)))
	return max(-10, min(20, bucket))
}

// readOnlyOverviewZoomBucket measures detail relative to the preferred fit
// extent while chunk addressing continues to use the complete data extent.
func readOnlyOverviewZoomBucket(zoomBucket int, dataExtent, fitExtent [4]float64) int {
	dataWidth, dataHeight := dataExtent[2]-dataExtent[0], dataExtent[3]-dataExtent[1]
	fitWidth, fitHeight := fitExtent[2]-fitExtent[0], fitExtent[3]-fitExtent[1]
	if dataWidth <= 0 || dataHeight <= 0 || fitWidth <= 0 || fitHeight <= 0 {
		return zoomBucket
	}
	fraction := math.Max(fitWidth/dataWidth, fitHeight/dataHeight)
	zoom := math.Ldexp(1, max(-10, min(20, zoomBucket))) * fraction
	return readOnlyWindowZoomBucket(zoom)
}

func readOnlyOverviewStride(zoomBucket int) int {
	if zoomBucket >= -1 {
		return 1
	}
	shift := max(0, min(8, 1-zoomBucket))
	return 1 << shift
}

// readOnlyOverviewStrideForFeatureCount caps the approximate feature payload
// while retaining full fidelity once a dense layer is sufficiently zoomed in.
func readOnlyOverviewStrideForFeatureCount(zoomBucket, featureCount int) int {
	stride := readOnlyOverviewStride(zoomBucket)
	if featureCount <= 0 || zoomBucket == 2 {
		return stride
	}
	target := maxReadOnlyOverviewFeatures
	for level := min(10, max(0, zoomBucket-3)); level > 0 && target < featureCount; level-- {
		if target > featureCount/4 {
			target = featureCount
			break
		}
		target *= 4
	}
	if featureCount <= target {
		return stride
	}
	featureStride := featureCount / target
	if featureCount%target != 0 {
		featureStride++
	}
	return max(stride, featureStride)
}

// A read-only source has a known extent before its geometry is loaded. Avoid
// scheduling GDAL queries for tiles that cannot contain any of that layer's
// features, while keeping layers with missing/invalid metadata fail-open.
func filterReadOnlyKeysByLayerBounds(keys []render.ChunkKey, extent [4]float64, size float64,
	layerBounds map[string][4]float64) []render.ChunkKey {
	if size <= 0 || extent[0] >= extent[2] || extent[1] >= extent[3] {
		return keys
	}
	spanX, spanY := extent[2]-extent[0], extent[3]-extent[1]
	retained := keys[:0]
	for _, key := range keys {
		bounds, known := layerBounds[key.Layer]
		if !known || bounds[0] > bounds[2] || bounds[1] > bounds[3] {
			retained = append(retained, key)
			continue
		}
		x0, y0 := math.Max(0, float64(key.X)*size), math.Max(0, float64(key.Y)*size)
		x1, y1 := math.Min(1, float64(key.X+1)*size), math.Min(1, float64(key.Y+1)*size)
		if x0 >= x1 || y0 >= y1 {
			continue
		}
		world := [4]float64{
			extent[0] + x0*spanX, extent[1] + y0*spanY,
			extent[0] + x1*spanX, extent[1] + y1*spanY,
		}
		if world[0] <= bounds[2] && world[2] >= bounds[0] &&
			world[1] <= bounds[3] && world[3] >= bounds[1] {
			retained = append(retained, key)
		}
	}
	return retained
}
