# ADR 0019: Linux desktop packaging targets

## Status

Accepted for the Linux packaging pilot.

## Decision

Build the Qt desktop on the target Linux host and produce a portable directory
and `tar.gz`, a `.deb`, and optionally an AppImage. The package contains the
executable and adjacent `resources/gdal`, `resources/proj`, and
`resources/gdalplugins` data. `linuxdeploy` is used when available to collect
Qt and non-system shared libraries; without it the AppDir is a development
artifact and must not be published as a self-contained release.

The package script refuses cross-architecture native builds. GDAL, PROJ, GEOS,
Qt, and the C/C++ toolchain must be installed for the target distribution and
ABI. The runtime resolves `resources/` beside the executable on Linux, while
explicit `GDAL_DATA`, `GDAL_DRIVER_PATH`, and `PROJ_DATA` values still win.

## Release gates

The artifacts remain unsigned and require clean-machine tests for Qt startup,
SHP/GeoPackage I/O, CRS grids, PostGIS connectivity, and DXF export. Native
dependency licenses and the selected Qt licensing obligations must be included
or documented before public redistribution. This decision does not claim that
a local package build has passed those gates.
