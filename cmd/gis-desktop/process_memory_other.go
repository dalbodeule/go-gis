//go:build !linux && !darwin && !windows

package main

func processMemoryBytes() (uint64, string, bool) { return 0, "", false }
