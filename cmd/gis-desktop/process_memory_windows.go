//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

var (
	kernel32                 = syscall.NewLazyDLL("kernel32.dll")
	getProcessMemoryInfoProc = kernel32.NewProc("K32GetProcessMemoryInfo")
)

type processMemoryCountersEx struct {
	cb                         uint32
	pageFaultCount             uint32
	peakWorkingSetSize         uintptr
	workingSetSize             uintptr
	quotaPeakPagedPoolUsage    uintptr
	quotaPagedPoolUsage        uintptr
	quotaPeakNonPagedPoolUsage uintptr
	quotaNonPagedPoolUsage     uintptr
	pagefileUsage              uintptr
	peakPagefileUsage          uintptr
	privateUsage               uintptr
}

func processMemoryBytes() (uint64, string, bool) {
	if err := kernel32.Load(); err != nil {
		return 0, "", false
	}
	var counters processMemoryCountersEx
	counters.cb = uint32(unsafe.Sizeof(counters))
	process := ^uintptr(0) // GetCurrentProcess pseudo-handle.
	result, _, _ := getProcessMemoryInfoProc.Call(process, uintptr(unsafe.Pointer(&counters)), uintptr(counters.cb))
	if result == 0 {
		return 0, "", false
	}
	return uint64(counters.workingSetSize), "Working set", true
}
