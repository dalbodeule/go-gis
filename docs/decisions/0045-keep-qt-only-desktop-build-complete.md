# Keep the Qt-only desktop build complete

## Context

The build guide documents `go run -tags qt` as a desktop build without the native GIS drivers. The desktop package still called `installInMemoryAttributeReaders` and `splitLayerByBoundary` in this configuration, while their definitions were confined to files tagged `qt && native`. Consequently, the documented build failed before launch.

## Decision

Keep in-memory attribute reader installation in a `qt`-tagged file because it depends on the application service, not on a native GIS driver. Provide a `qt && !native` boundary-splitting implementation that returns a clear unsupported-operation error, consistent with the other spatial-operation fallback.

## Verification

Build and test both `qt` and `qt native` configurations, alongside the default Go test and vet suites.
