//go:build qt

package main

import "math"

const maxReadOnlyOverviewFeatures = 240_000

func readOnlyWindowChunkSize(zoomBucket int) float64 {
	// Coarse overviews use 1/32 windows. Larger cells at high zoom reduce
	// per-viewport GDAL query overhead while detail still increases with zoom.
	zoomBucket = max(-10, min(20, zoomBucket))
	if zoomBucket <= 4 {
		return math.Max(0.0078125, math.Ldexp(0.03125, -max(0, zoomBucket)))
	}
	if zoomBucket >= 7 {
		return math.Ldexp(0.0078125, -(zoomBucket - 5))
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
