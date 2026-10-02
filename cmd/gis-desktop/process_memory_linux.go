//go:build linux

package main

import (
	"os"
	"strconv"
	"strings"
)

func processMemoryBytes() (uint64, string, bool) {
	data, err := os.ReadFile("/proc/self/statm")
	if err != nil {
		return 0, "", false
	}
	fields := strings.Fields(string(data))
	if len(fields) < 2 {
		return 0, "", false
	}
	pages, err := strconv.ParseUint(fields[1], 10, 64)
	if err != nil || pages > ^uint64(0)/uint64(os.Getpagesize()) {
		return 0, "", false
	}
	return pages * uint64(os.Getpagesize()), "RSS", true
}
