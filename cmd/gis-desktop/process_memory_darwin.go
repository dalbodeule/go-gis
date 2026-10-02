//go:build darwin && cgo

package main

/*
#include <mach/mach.h>
#include <stdint.h>

static uint64_t gogis_current_resident_bytes(void) {
	mach_task_basic_info_data_t info;
	mach_msg_type_number_t count = MACH_TASK_BASIC_INFO_COUNT;
	if (task_info(mach_task_self(), MACH_TASK_BASIC_INFO,
	              (task_info_t)&info, &count) != KERN_SUCCESS) {
		return 0;
	}
	return (uint64_t)info.resident_size;
}
*/
import "C"

func processMemoryBytes() (uint64, string, bool) {
	bytes := uint64(C.gogis_current_resident_bytes())
	return bytes, "RSS", bytes > 0
}
