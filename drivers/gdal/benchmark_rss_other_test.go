//go:build native && !darwin && !linux

package gdal

func benchmarkGDALProcessMaxRSSBytes() uint64 { return 0 }
