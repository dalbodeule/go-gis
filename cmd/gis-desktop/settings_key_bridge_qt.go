//go:build qt

package main

/*
#cgo CXXFLAGS: -std=c++17
#cgo pkg-config: Qt6Core
#include <stdlib.h>
#include "settings_key_bridge_qt.h"
*/
import "C"

import (
	"unsafe"

	qt "github.com/mappu/miqt/qt6"
)

// miqt's QAnyStringView constructors refer to temporary strings. Keep the
// QString key owned by C++ for the entire QSettings operation instead.
func settingsInt(settings *qt.QSettings, key string) (int, bool) {
	cKey := C.CString(key)
	defer C.free(unsafe.Pointer(cKey))
	var present C.bool
	value := C.gogis_settings_int(settings.UnsafePointer(), cKey, C.int(len(key)), &present)
	return int(value), bool(present)
}

func settingsSetInt(settings *qt.QSettings, key string, value int) {
	cKey := C.CString(key)
	defer C.free(unsafe.Pointer(cKey))
	C.gogis_settings_set_int(settings.UnsafePointer(), cKey, C.int(len(key)), C.int(value))
}

func settingsRemove(settings *qt.QSettings, key string) {
	cKey := C.CString(key)
	defer C.free(unsafe.Pointer(cKey))
	C.gogis_settings_remove(settings.UnsafePointer(), cKey, C.int(len(key)))
}
