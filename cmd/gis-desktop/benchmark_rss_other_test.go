//go:build qt && native && !darwin && !linux

package main

func benchmarkProcessMaxRSSBytes() (uint64, bool) {
	return 0, false
}
