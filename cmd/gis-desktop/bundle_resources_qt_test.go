//go:build qt

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBundledGISResourceEnvironmentUsesBundleAndPreservesOverrides(t *testing.T) {
	root := t.TempDir()
	contents := filepath.Join(root, "GoGIS.app", "Contents")
	executable := filepath.Join(contents, "MacOS", "GoGIS")
	resources := filepath.Join(contents, "Resources")
	for _, name := range []string{"gdal", "gdalplugins", "proj"} {
		if err := os.MkdirAll(filepath.Join(resources, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("GDAL_DATA", "")
	t.Setenv("GDAL_DRIVER_PATH", "")
	t.Setenv("PROJ_DATA", "/custom/proj-data")

	got := bundledGISResourceEnvironment(executable)
	if got["GDAL_DATA"] != filepath.Join(resources, "gdal") {
		t.Fatalf("GDAL_DATA = %q", got["GDAL_DATA"])
	}
	if got["GDAL_DRIVER_PATH"] != filepath.Join(resources, "gdalplugins") {
		t.Fatalf("GDAL_DRIVER_PATH = %q", got["GDAL_DRIVER_PATH"])
	}
	if _, exists := got["PROJ_DATA"]; exists {
		t.Fatalf("explicit PROJ_DATA override was replaced: %#v", got)
	}
}

func TestBundledGISResourceEnvironmentIgnoresNonBundleExecutable(t *testing.T) {
	if got := bundledGISResourceEnvironment("/tmp/gogis-desktop-native"); len(got) != 0 {
		t.Fatalf("non-bundle executable resources = %#v", got)
	}
}
