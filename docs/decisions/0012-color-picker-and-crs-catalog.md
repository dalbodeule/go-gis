# ADR 0012: Visual layer colors and local CRS catalog

## Status

Accepted for the desktop prototype. QML tests and native catalog tests cover
selection and publication; macOS native picker appearance still benefits from
an on-screen check.

## Context

Layer colors previously required typing a hexadecimal string. Project CRS
changes required knowing an authority code in advance, even though PROJ has
an installed CRS database.

## Decision

- Use a layer color dialog with ten fixed presets, an RGB hex field, and a
  button for the Qt system color dialog. Show an opaque swatch and the selected
  value in layer properties. Commit the color to the layer only through the
  existing layer-settings Apply action; fill opacity remains separate.
- Read non-deprecated EPSG geographic 2D and projected CRSs from the installed
  PROJ database in the desktop-native build. Publish code, name, and area of
  use to the QML picker. No network request or remote catalog is required.
- Separate common choices, country-specific projected-CRS suggestions, and a
  searchable full EPSG list. Country suggestions use PROJ's area-of-use names
  and put a small validated set of representative codes first. Show the
  selected CRS name and area before applying. Keep manual EPSG entry available;
  the existing runtime validation and reprojection remain authoritative.
- In the Qt-only build, retain the representative choices but explain that the
  full list and broader regional suggestions require the PROJ-enabled build.
  Do not imply that the fallback is the complete EPSG catalog.

## Consequences

The native desktop reads the local PROJ database once at startup and carries
the resulting catalog into QML. The picker lists horizontal CRSs supported by
the 2D map rather than geocentric, vertical-only, or deprecated definitions.
CRS availability follows the installed PROJ database version.
