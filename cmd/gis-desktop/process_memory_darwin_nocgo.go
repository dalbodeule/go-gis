//go:build darwin && !cgo

package main

import "syscall"

func processMemoryBytes() (uint64, string, bool) {
	var usage syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &usage); err != nil || usage.Maxrss < 0 {
		return 0, "", false
	}
	return uint64(usage.Maxrss), "Peak RSS", true
}
