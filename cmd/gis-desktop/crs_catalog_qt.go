//go:build qt && !native

package main

func desktopCRSCatalogJSON() string {
	return "{\"available\":false,\"error\":\"Full CRS catalog requires the PROJ-enabled desktop-native build\",\"entries\":[]}"
}
