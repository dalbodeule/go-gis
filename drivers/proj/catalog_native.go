//go:build native

package proj

/*
#cgo pkg-config: proj
#include <stdlib.h>
#include <proj.h>

static int gogis_is_horizontal_crs(const PROJ_CRS_INFO *info) {
    return info->type == PJ_TYPE_GEOGRAPHIC_2D_CRS ||
           info->type == PJ_TYPE_PROJECTED_CRS;
}

static int gogis_is_projected_crs(const PROJ_CRS_INFO *info) {
    return info->type == PJ_TYPE_PROJECTED_CRS;
}
*/
import "C"

import (
	"fmt"
	"sort"
	"unsafe"
)

// ListEPSGCRS reads the installed PROJ database, so the picker presents the
// CRSs actually available to the application's coordinate transformer.
func ListEPSGCRS() ([]CatalogEntry, error) {
	ctx := C.proj_context_create()
	if ctx == nil {
		return nil, fmt.Errorf("create PROJ context")
	}
	defer C.proj_context_destroy(ctx)
	authority := C.CString("EPSG")
	defer C.free(unsafe.Pointer(authority))
	var count C.int
	list := C.proj_get_crs_info_list_from_database(ctx, authority, nil, &count)
	if list == nil || count <= 0 {
		return nil, fmt.Errorf("read EPSG CRS catalog from PROJ database")
	}
	defer C.proj_crs_info_list_destroy(list)
	entries := make([]CatalogEntry, 0, int(count))
	for _, info := range unsafe.Slice(list, int(count)) {
		if info == nil || C.gogis_is_horizontal_crs(info) == 0 || info.deprecated != 0 ||
			info.code == nil || info.name == nil {
			continue
		}
		entry := CatalogEntry{Code: "EPSG:" + C.GoString(info.code), Name: C.GoString(info.name)}
		if C.gogis_is_projected_crs(info) != 0 {
			entry.Kind = "projected"
		} else {
			entry.Kind = "geographic"
		}
		if info.area_name != nil {
			entry.Area = C.GoString(info.area_name)
		}
		if info.bbox_valid != 0 {
			entry.Bounds = [4]float64{float64(info.west_lon_degree), float64(info.south_lat_degree),
				float64(info.east_lon_degree), float64(info.north_lat_degree)}
			entry.HasBounds = true
		}
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Code < entries[j].Code })
	return entries, nil
}
